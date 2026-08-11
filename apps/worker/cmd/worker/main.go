// Command worker runs asynchronous chain-indexing and certificate jobs.
//
// 当前实现的子系统：
//   - Indexer: WS 监听 + HTTP 回扫 + checkpoint 推进 + reorg 检测
//     （apps/worker/internal/indexer）。
//   - Confirmer: 消费外部投递（HTTP / 或 in-process channel）的链事件，
//     入库 chain_events → 推进 orders → 派生 enrollments → 写 outbox_events
//     （apps/worker/internal/order）。
//   - Reconcile: 周期性漏块扫描，写 DLQ（apps/worker/internal/reconcile）。
//   - Metrics:  Prometheus /metrics 端点（apps/worker/internal/metrics）。
package main

import (
	"context"
	"errors"
	"log/slog"
	"math/big"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/x-web3/worker/internal/config"
	"github.com/x-web3/worker/internal/indexer"
	"github.com/x-web3/worker/internal/metrics"
	workerorder "github.com/x-web3/worker/internal/order"
	"github.com/x-web3/worker/internal/reconcile"
)

func main() {
	// 自动从 CWD 或祖先目录加载 .env；找不到也不报错（prod 走真 env）。
	dotenv := config.LoadDotenv()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	logger.Info("dotenv_loaded",
		"loaded", dotenv.Loaded,
		"path", dotenv.Path,
		"cwd", dotenv.CWD,
	)

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		if !dotenv.Loaded {
			logger.Error("DATABASE_URL not set; exiting",
				"candidates", dotenv.Candidates,
				"hint", "复制仓库根 .env.example 为 .env 并填 DATABASE_URL，或在启动前 export DATABASE_URL",
			)
		} else {
			logger.Error("DATABASE_URL not set after loading .env; exiting", "dotenvPath", dotenv.Path)
		}
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
	consumer := envOrDefault("WORKER_CONSUMER", "indexer")
	metricsAddr := strings.TrimSpace(os.Getenv("WORKER_METRICS_ADDR"))

	// 共享 indexer.Metrics 实例，让 metrics 包能读到（counter / 状态）。
	idxMetrics := &indexer.Metrics{}

	// Indexer（仅当至少一个 RPC URL 配置时才启用；否则保留 confirmer 主线）。
	var rpcPool *indexer.RPCPool
	if urls := splitCSV(os.Getenv("WORKER_RPC_URLS")); len(urls) > 0 {
		rpcPool = runIndexer(ctx, logger, pool, chainID, confirmDepth, consumer, urls, idxMetrics)
	} else if wsURL := strings.TrimSpace(os.Getenv("WORKER_WS_URL")); wsURL != "" {
		rpcPool = runIndexer(ctx, logger, pool, chainID, confirmDepth, consumer, []string{wsURL}, idxMetrics)
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
	// scanner 不是 goroutine 暴露 runner 内部 metrics 的通道，metrics 包
	// 通过 ReconcileSnapFunc 反向拉取；这里用一个 ctx-driven 协程每轮 ScanOnce
	// 后调 metrics.SetReconcileSnapshot 推一次。
	scanner, err := reconcile.NewScanner(reconcile.Config{
		Pool:         pool,
		Writer:       reconcile.NewWriter(reconcile.NewPGDLQStore(pool), logger),
		Logger:       logger,
		Consumer:     consumer,
		ChainID:      chainID,
		ConfirmDepth: confirmDepth,
		Interval:     time.Duration(envInt64("RECONCILE_INTERVAL_MINUTES", 30)) * time.Minute,
	})
	if err != nil {
		logger.Warn("reconcile_init_failed", "err", err.Error())
	} else {
		go func() {
			// 启动即跑一次，与 scanner.Start 行为一致；这里改用手动循环
			// 以便在每一轮 ScanOnce 后把 snapshot 推到 metrics 包。
			tick := time.NewTicker(scanner.Interval())
			defer tick.Stop()
			scanOnceWithMetrics(ctx, logger, scanner)
			for {
				select {
				case <-ctx.Done():
					return
				case <-tick.C:
					scanOnceWithMetrics(ctx, logger, scanner)
				}
			}
		}()
	}

	// /metrics 端点：注册 + 起 server。
	metrics.Register(
		metrics.Sources{
			Indexer:  metrics.WrapIndexer(idxMetrics),
			ChainID:  chainID,
			Consumer: consumer,
		},
		func() metrics.ReconcileSnapshot {
			m := reconcileMetricsOrEmpty(scanner)
			return metrics.ReconcileSnapshot{
				LastScanUnix: m.LastScanUnix,
				ScanRuns:     m.ScanRuns,
				GapDetected:  m.GapDetected,
			}
		},
		makeChainLagFunc(ctx, pool, rpcPool, chainID, consumer, logger),
	)
	metricsSrv := metrics.Start(ctx, logger, metricsAddr)
	defer metricsSrv.Close()

	logger.Info("worker_started",
		"chainId", chainID,
		"confirmDepth", confirmDepth,
		"metricsAddr", metricsAddr,
	)
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

// runIndexer 启动链事件 indexer 并返回 RPCPool（用于 metrics 拉取 head）。
//
// 设计：
//   - 优先用 WORKER_RPC_URLS（HTTP 多个，逗号分隔）作为 multi-RPC pool；
//   - WORKER_WS_URL 若提供则放最前面（head 订阅用；fallback 仍走 HTTP pool）。
//
// 退避 / 优雅退出由 indexer.Runner 内部负责。
func runIndexer(
	ctx context.Context,
	logger *slog.Logger,
	pool *pgxpool.Pool,
	chainID, confirmDepth int64,
	consumer string,
	fallbackURLs []string,
	idxMetrics *indexer.Metrics,
) *indexer.RPCPool {
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
		return nil
	}
	poolClient := indexer.NewRPCPool(clients, logger, idxMetrics)

	confirmer := workerorder.NewConfirmer(pool)
	runner, err := indexer.NewRunner(indexer.Config{
		ChainID:          chainID,
		Consumer:         consumer,
		ConfirmDepth:     confirmDepth,
		PollInterval:     time.Duration(envInt64("WORKER_POLL_INTERVAL_SECONDS", 5)) * time.Second,
		HealthWindow:     time.Duration(envInt64("WORKER_RPC_HEALTH_WINDOW_SECONDS", 30)) * time.Second,
		BatchSize:        envInt64("WORKER_BATCH_SIZE", 1000),
		SubscribeTimeout: time.Duration(envInt64("WORKER_WS_SUBSCRIBE_TIMEOUT_SECONDS", 10)) * time.Second,
		Logger:           logger,
		Decoder:          indexerLogDecoder{},
		Confirmer:        confirmerAdapter{c: confirmer},
		CheckpointStore:  indexer.NewPGCheckpointStore(pool),
		RPCPool:          poolClient,
		OnReorg: func(ctx context.Context, info indexer.ReorgInfo) error {
			_, _, err := indexer.HandleReorg(ctx, pool, info, map[string]any{"source": "runner"})
			return err
		},
		Metrics: idxMetrics,
	})
	if err != nil {
		logger.Error("indexer_init_failed", "err", err.Error())
		return poolClient
	}
	if err := runner.Start(ctx); err != nil {
		logger.Error("indexer_start_failed", "err", err.Error())
		return poolClient
	}
	logger.Info("indexer_started", "rpcClients", len(clients), "chainId", chainID)
	go func() {
		<-ctx.Done()
		runner.Stop()
		poolClient.Close()
	}()
	return poolClient
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

// scanOnceWithMetrics 跑一轮 ScanOnce 然后把 snapshot 推到 metrics 包。
//
// 注意：scanner 内部自带的 Start 是 goroutine 化的轮询，不暴露每轮结束回调；
// 这里复用一个轻量 ticker 代替，scanner.Start 仅保留作为 Stop 入口调用。
func scanOnceWithMetrics(ctx context.Context, logger *slog.Logger, scanner *reconcile.Scanner) {
	if scanner == nil {
		return
	}
	if _, err := scanner.ScanOnce(ctx); err != nil {
		logger.Warn("reconcile_scan_failed", "err", err.Error())
	}
	metrics.SetReconcileSnapshot(metrics.ReconcileSnapshot{
		LastScanUnix: scanner.Metrics().LastScanUnix,
		ScanRuns:     scanner.Metrics().ScanRuns,
		GapDetected:  scanner.Metrics().GapDetected,
	})
}

// reconcileMetricsOrEmpty 防 scanner 为 nil 时 metrics 拉取 panic。
func reconcileMetricsOrEmpty(s *reconcile.Scanner) reconcile.Metrics {
	if s == nil {
		return reconcile.Metrics{}
	}
	return s.Metrics()
}

// makeChainLagFunc 给 metrics 包用的 lag 抓取函数。
//
// 同时关闭时报 nil error；metrics 包的 lagScrape 内部会保留上次成功值。
func makeChainLagFunc(
	ctx context.Context,
	pool *pgxpool.Pool,
	rpcPool *indexer.RPCPool,
	chainID int64,
	consumer string,
	logger *slog.Logger,
) metrics.ChainLagFunc {
	return func(callCtx context.Context) (int64, int64, error) {
		// 1) next_block from chain_checkpoints
		var nextBlock int64
		err := pool.QueryRow(callCtx, `
			SELECT next_block FROM chain_checkpoints
			WHERE chain_id=$1 AND consumer=$2`,
			chainID, consumer,
		).Scan(&nextBlock)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return 0, 0, err
		}
		// 没记录时 next_block=0（与 chain_indexer 启动一致），不算错。
		// 2) head from RPC
		if rpcPool == nil {
			return nextBlock, 0, nil
		}
		primary := rpcPool.Primary(time.Now())
		if primary == nil {
			return nextBlock, 0, errors.New("no healthy RPC")
		}
		hdr, err := primary.HeaderByNumber(callCtx, nil)
		if err != nil {
			return nextBlock, 0, err
		}
		if hdr == nil {
			return nextBlock, 0, nil
		}
		head := hdr.Number
		if head == nil {
			return nextBlock, 0, nil
		}
		return nextBlock, head.Int64(), nil
	}
}

// bigIntHead 临时 helper：HeaderByNumber 返回 *Header；Number 是 *big.Int。
// 仅在 makeChainLagFunc 内部使用，避免外部依赖。
var _ = big.Int{}

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

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

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
