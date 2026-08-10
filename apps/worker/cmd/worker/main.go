// Command worker runs asynchronous chain-indexing and certificate jobs.
//
// 当前实现的子系统：
//   - Indexer: WS 监听 + HTTP 回扫 + checkpoint 推进 + reorg 检测
//     （apps/worker/internal/indexer）。
//   - Confirmer: 消费外部投递（HTTP / 或 in-process channel）的链事件，
//     入库 chain_events → 推进 orders → 派生 enrollments → 写 outbox_events
//     （apps/worker/internal/order）。
//   - Reconcile: 周期性漏块扫描，写 DLQ（apps/worker/internal/reconcile）。
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/x-web3/worker/internal/indexer"
	workerorder "github.com/x-web3/worker/internal/order"
	"github.com/x-web3/worker/internal/reconcile"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		logger.Error("DATABASE_URL not set; exiting")
		os.Exit(1)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		logger.Error("pgx_connect_failed", "err", err.Error())
		os.Exit(1)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		logger.Error("pg_ping_failed", "err", err.Error())
		os.Exit(1)
	}

	chainID := envInt64("WORKER_CHAIN_ID", 11155111)
	confirmDepth := envInt64("CHAIN_CONFIRMATION_DEPTH", 12)

	// Indexer（仅当至少一个 RPC URL 配置时才启用；否则保留 confirmer 主线）。
	if urls := splitCSV(os.Getenv("WORKER_RPC_URLS")); len(urls) > 0 {
		runIndexer(ctx, logger, pool, chainID, confirmDepth, urls)
	} else if wsURL := strings.TrimSpace(os.Getenv("WORKER_WS_URL")); wsURL != "" {
		runIndexer(ctx, logger, pool, chainID, confirmDepth, []string{wsURL})
	} else {
		logger.Warn("indexer_disabled_no_rpc_urls")
	}

	// Confirmer：MVP 内存 channel；生产可替换为 inbox queue。
	confirmer := workerorder.NewConfirmer(pool)
	var (
		mu   sync.Mutex
		pend []workerorder.ApplyInput
	)
	if err := loadPendingFromOutbox(ctx, pool, &mu); err != nil {
		logger.Warn("load_pending_failed", "err", err.Error())
	}

	// Reconcile：启动一个 scanner（默认 30 min）。
	scanner, err := reconcile.NewScanner(reconcile.Config{
		Pool:         pool,
		Writer:       reconcile.NewWriter(reconcile.NewPGDLQStore(pool), logger),
		Logger:       logger,
		Consumer:     "indexer",
		ChainID:      chainID,
		ConfirmDepth: confirmDepth,
		Interval:     time.Duration(envInt64("RECONCILE_INTERVAL_MINUTES", 30)) * time.Minute,
	})
	if err != nil {
		logger.Warn("reconcile_init_failed", "err", err.Error())
	} else {
		go scanner.Start(ctx)
	}

	logger.Info("worker_started", "chainId", chainID, "confirmDepth", confirmDepth)
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			logger.Info("worker_stopped")
			return
		case <-tick.C:
			mu.Lock()
			batch := pend
			pend = nil
			mu.Unlock()
			for _, in := range batch {
				enrollID, state, err := confirmer.Apply(ctx, in)
				if err != nil {
					logger.Error("apply_failed", "err", err.Error(), "orderId", in.OrderID.String())
					mu.Lock()
					pend = append(pend, in)
					mu.Unlock()
					continue
				}
				logger.Info("apply_ok",
					"orderId", in.OrderID.String(),
					"state", state,
					"enrollmentId", enrollID.String(),
				)
			}
		}
	}
}

// runIndexer 启动链事件 indexer。
//
// 设计：
//   - 优先用 WORKER_RPC_URLS（HTTP 多个，逗号分隔）作为 multi-RPC pool；
//   - WORKER_WS_URL 若提供则放最前面（head 订阅用；fallback 仍走 HTTP pool）。
//
// 退避 / 优雅退出由 indexer.Runner 内部负责。
func runIndexer(ctx context.Context, logger *slog.Logger, pool *pgxpool.Pool, chainID, confirmDepth int64, fallbackURLs []string) {
	clients := make([]indexer.Client, 0, len(fallbackURLs))
	for _, u := range fallbackURLs {
		cl, err := indexer.DialHTTP(ctx, u)
		if err != nil {
			logger.Warn("rpc_dial_failed", "url", indexer.RedactURL(u), "err", err.Error())
			continue
		}
		clients = append(clients, cl)
	}
	// WS 优先：尝试作为主 client 之一。
	if ws := strings.TrimSpace(os.Getenv("WORKER_WS_URL")); ws != "" {
		if cl, err := indexer.DialWS(ctx, ws); err == nil {
			clients = append([]indexer.Client{cl}, clients...)
		} else {
			logger.Warn("ws_dial_failed", "err", err.Error())
		}
	}
	if len(clients) == 0 {
		logger.Warn("indexer_no_clients")
		return
	}
	poolClient := indexer.NewRPCPool(clients, logger, &indexer.Metrics{})

	confirmer := workerorder.NewConfirmer(pool)
	runner, err := indexer.NewRunner(indexer.Config{
		ChainID:         chainID,
		Consumer:        "indexer",
		ConfirmDepth:    confirmDepth,
		PollInterval:    time.Duration(envInt64("WORKER_POLL_INTERVAL_SECONDS", 5)) * time.Second,
		HealthWindow:    time.Duration(envInt64("WORKER_RPC_HEALTH_WINDOW_SECONDS", 30)) * time.Second,
		BatchSize:       envInt64("WORKER_BATCH_SIZE", 1000),
		SubscribeTimeout: time.Duration(envInt64("WORKER_WS_SUBSCRIBE_TIMEOUT_SECONDS", 10)) * time.Second,
		Logger:          logger,
		Decoder:         indexerLogDecoder{},
		Confirmer:       confirmerAdapter{c: confirmer},
		CheckpointStore: indexer.NewPGCheckpointStore(pool),
		RPCPool:         poolClient,
		OnReorg: func(ctx context.Context, info indexer.ReorgInfo) error {
			_, _, err := indexer.HandleReorg(ctx, pool, info, map[string]any{"source": "runner"})
			return err
		},
		Metrics: &indexer.Metrics{},
	})
	if err != nil {
		logger.Error("indexer_init_failed", "err", err.Error())
		return
	}
	if err := runner.Start(ctx); err != nil {
		logger.Error("indexer_start_failed", "err", err.Error())
		return
	}
	logger.Info("indexer_started", "rpcClients", len(clients), "chainId", chainID)
	<-ctx.Done()
	runner.Stop()
	poolClient.Close()
}

// confirmerAdapter 把 workerorder.Confirmer 适配成 indexer.Confirmer 接口。
type confirmerAdapter struct{ c *workerorder.Confirmer }

func (a confirmerAdapter) Apply(ctx context.Context, in workerorder.ApplyInput) (any, string, error) {
	return a.c.Apply(ctx, in)
}

// indexerLogDecoder 把 raw log 翻译成 ApplyInput；当前用占位实现（topic 匹配 placeholder）。
//
// 后续应换成 chain.CoursePurchased 解码逻辑；F03-T10 已实现，TODO 接入。
// 真实解码后通过 pool 把 ApplyInput 灌入 Confirmer；本占位仅在主流程不依赖
// 实时 decoder 时使用。
type indexerLogDecoder struct{}

func (indexerLogDecoder) Decode(_ context.Context, _ int64, _ *indexer.Header, _ indexer.LogRecord) ([]workerorder.ApplyInput, bool, error) {
	return nil, true, nil
}

// loadPendingFromOutbox MVP 阶段不持久化 pending 队列。
func loadPendingFromOutbox(_ context.Context, _ *pgxpool.Pool, _ *sync.Mutex) error {
	if v := os.Getenv("WORKER_DEBUG_PENDING"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			_ = errors.New("no-op")
		}
	}
	return nil
}

// envInt64 读 int64 env，缺省值。
func envInt64(key string, def int64) int64 {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	if n, err := strconv.ParseInt(v, 10, 64); err == nil {
		return n
	}
	return def
}

// splitCSV 逗号分隔，trim 空白。
func splitCSV(v string) []string {
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
