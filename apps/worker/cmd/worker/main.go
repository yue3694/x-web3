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
	"fmt"
	"log/slog"
	"math/big"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/x-web3/worker/internal/certificate"
	"github.com/x-web3/worker/internal/chain"
	"github.com/x-web3/worker/internal/config"
	"github.com/x-web3/worker/internal/indexer"
	"github.com/x-web3/worker/internal/metrics"
	workerorder "github.com/x-web3/worker/internal/order"
	"github.com/x-web3/worker/internal/reconcile"
	"github.com/x-web3/worker/internal/treasury"
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

	// CertConsumer：证书 mint 闭环。
	//
	// 启动条件：rpcPool != nil + SIGNER_DRIVER 已设 + CERT_NFT_ADDRESS 非零。
	// 任一缺失 → log warn 跳过（与 treasury monitor 一致的「empty config short-circuit」）。
	certConsumer, certMetrics := buildAndStartCertConsumer(ctx, logger, pool, rpcPool, chainID, confirmDepth)
	if certConsumer != nil {
		// Run 阻塞到 ctx.Done()；这里用 goroutine 让 main 不卡。
		go func() {
			if err := certConsumer.Run(ctx); err != nil {
				logger.Error("cert_consumer_exited", "err", err.Error())
			}
		}()
	}

	// TreasuryMonitor：余额告警 + hot-wallet / treasury 地址监控。
	//
	// 启动条件：rpcPool != nil + 至少配了一个监控地址 + YD_TOKEN_ADDRESS。
	// 任一缺失 → log warn 跳过；本地开发允许空配置（user 还没配监控目标）。
	startTreasuryMonitor(ctx, logger, pool, rpcPool)

	// /metrics 端点：注册 + 起 server。
	metricsSources := metrics.Sources{
		Indexer:  metrics.WrapIndexer(idxMetrics),
		ChainID:  chainID,
		Consumer: consumer,
	}
	if certMetrics != nil {
		metricsSources.CertConsumer = metrics.WrapCertConsumer(certMetrics)
	}
	metrics.Register(
		metricsSources,
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
	marketAddresses := parseAddressCSV(os.Getenv("WORKER_MARKET_ADDRESSES"))
	logger.Info("indexer_filter_configured", "marketAddresses", marketAddresses)

	confirmer := workerorder.NewConfirmer(pool)
	runner, err := indexer.NewRunner(indexer.Config{
		ChainID:              chainID,
		Consumer:             consumer,
		ConfirmDepth:         confirmDepth,
		PollInterval:         time.Duration(envInt64("WORKER_POLL_INTERVAL_SECONDS", 5)) * time.Second,
		HealthWindow:         time.Duration(envInt64("WORKER_RPC_HEALTH_WINDOW_SECONDS", 30)) * time.Second,
		BatchSize:            envInt64("WORKER_BATCH_SIZE", 1000),
		SubscribeTimeout:     time.Duration(envInt64("WORKER_WS_SUBSCRIBE_TIMEOUT_SECONDS", 10)) * time.Second,
		Logger:               logger,
		Decoder:              indexerLogDecoder{},
		Confirmer:            confirmerAdapter{c: confirmer},
		CheckpointStore:      indexer.NewPGCheckpointStore(pool),
		RPCPool:              poolClient,
		Addresses:            marketAddresses,
		DisableSubscriptions: strings.TrimSpace(os.Getenv("WORKER_WS_URL")) == "",
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

// indexerLogDecoder 把 raw log 翻译成 ApplyInput。
//
// 用 chain.Decode 解码 CoursePurchased；topic mismatch / removed log / decode
// 错误都返回 ok=false 让 indexer 跳过（不计入 Apply，不写 chain_events）。
// SSOT：chain.CoursePurchasedTopic = keccak256("CoursePurchased(bytes32,address,address,uint256,bytes16,uint256)").
type indexerLogDecoder struct{}

func (indexerLogDecoder) Decode(_ context.Context, chainID int64, _ *indexer.Header, rec indexer.LogRecord) ([]workerorder.ApplyInput, bool, error) {
	// 长度 < 3 是 base case（topic[0]+2 indexed），用 ErrTooFewTopics 表达。
	if len(rec.Topics) < 3 {
		return nil, true, nil
	}
	// topic0 不匹配 → 静默 skip，不计入 Apply
	if rec.Topics[0] != chain.CoursePurchasedTopic {
		return nil, true, nil
	}
	decoded, err := chain.Decode(&rec)
	if err != nil {
		if errors.Is(err, chain.ErrLogRemoved) {
			// reorged log：返回 ok=false 让 indexer 走 reorg 路径
			return nil, true, nil
		}
		// ErrTooFewTopics / ErrDecodeData / ErrTopicMismatch 已在上层过滤；
		// 其它错误按 generic 处理 — 返回 (nil, false, err) 让 runner 计数 + log。
		return nil, false, fmt.Errorf("chain decode: %w", err)
	}

	// Confirmer.ApplyInput 用 Event 字段承载解码后的 CoursePurchased；
	// CourseKey / Token / IntentID 等都在 decoded 里。
	in := workerorder.ApplyInput{
		ChainID:         chainID,
		ContractAddress: rec.Address,
		TxHash:          rec.TxHash.Bytes(),
		LogIndex:        int(rec.LogIndex),
		BlockNumber:     int64(rec.BlockNumber),
		BlockHash:       rec.BlockHash.Bytes(),
		EventSig:        chain.CoursePurchasedTopic,
		Event:           decoded,
	}
	return []workerorder.ApplyInput{in}, false, nil
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

// startTreasuryMonitor 装配 + 启动 TreasuryMonitor。
//
// 启动条件：
//   - rpcPool != nil
//   - 至少配了 TREASURY_ADDRESSES / MINTER_ADDRESS / HOT_WALLET_ADDRESSES / YD_TOKEN_ADDRESS 之一
//
// 空配置 short-circuit（log info「treasury_monitor_disabled_empty」）让本地开发
// 可以跑 worker 而不需要先配监控目标；其它错误（地址格式错、RPC unreachable）log warn。
//
// 指标：worker_treasury_alerts_total{address,asset,severity}；通过 metrics.Registry 注册。
func startTreasuryMonitor(ctx context.Context, logger *slog.Logger, pool *pgxpool.Pool, rpcPool *indexer.RPCPool) {
	if rpcPool == nil {
		logger.Warn("treasury_monitor_disabled_no_rpc")
		return
	}
	// 空配置 short-circuit：AllAddresses 与 YDToken 同时为空 → 跳过
	ydAddr := common.HexToAddress(strings.TrimSpace(os.Getenv("YD_TOKEN_ADDRESS")))
	treasuryCSV := strings.TrimSpace(os.Getenv("TREASURY_ADDRESSES"))
	minterCSV := strings.TrimSpace(os.Getenv("MINTER_ADDRESS"))
	hotCSV := strings.TrimSpace(os.Getenv("HOT_WALLET_ADDRESSES"))
	if treasuryCSV == "" && minterCSV == "" && hotCSV == "" && ydAddr == (common.Address{}) {
		logger.Info("treasury_monitor_disabled_empty",
			"hint", "设置 TREASURY_ADDRESSES / MINTER_ADDRESS / HOT_WALLET_ADDRESSES / YD_TOKEN_ADDRESS 中任意一个以启用")
		return
	}

	primary := rpcPool.Primary(time.Now())
	if primary == nil {
		logger.Warn("treasury_monitor_disabled_no_healthy_rpc")
		return
	}
	rc, ok := indexer.AsRPCClient(primary)
	if !ok {
		logger.Warn("treasury_monitor_disabled_rpc_type_assertion_failed")
		return
	}

	cfg, err := treasury.LoadConfigFromEnv(pool, rc.RawRPC())
	if err != nil {
		logger.Warn("treasury_monitor_load_config_failed", "err", err.Error())
		return
	}
	monitor, err := treasury.NewMonitor(cfg)
	if err != nil {
		logger.Warn("treasury_monitor_init_failed", "err", err.Error())
		return
	}

	tm := metrics.NewTreasuryMetrics(metrics.Registry)
	monitor.SetMetrics(treasury.TreasuryMetricsAdapter{M: tm})

	logger.Info("treasury_monitor_initialized",
		"interval", cfg.Interval.String(),
		"treasuryAddresses", len(cfg.TreasuryAddresses),
		"hotWallets", len(cfg.HotWalletAddresses),
		"ydToken", cfg.YDToken.Hex(),
	)
	go func() {
		monitor.Start(ctx) // 阻塞到 ctx.Done()
	}()
}

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

func parseAddressCSV(v string) []common.Address {
	parts := splitCSV(v)
	out := make([]common.Address, 0, len(parts))
	for _, part := range parts {
		if common.IsHexAddress(part) {
			out = append(out, common.HexToAddress(part))
		}
	}
	return out
}

// buildAndStartCertConsumer 装配 certificate.Consumer 并把它跑起来。
//
// 启动条件（全部满足才启动，否则 log warn 跳过）：
//   - rpcPool != nil（无 RPC 没法发交易）
//   - SIGNER_DRIVER 环境变量非空（避免 keystore / KMS driver 隐式启动）
//   - CERT_NFT_ADDRESS 环境变量非零（合约地址必填）
//
// 装配链路：
//  1. SignerConfigFromEnv(chainID) — driver / contract / KMS key
//  2. Params = ChainTxParams(rpcPool) — 实时 nonce（PR-A4）
//  3. NewMintSigner — driver 工厂
//  4. rpcPool.Primary().RawRPC() → ethclient.NewClient → NewEthClientAdapter
//  5. NewConsumer — DLQ 默认 PG、ConfirmDepth = confirmDepth
//
// 返回 (*Consumer, *ConsumerMetrics)。metrics 用于 metrics.Sources.CertConsumer。
// 调用方把 Run 放到 goroutine 里跑；本函数不阻塞。
func buildAndStartCertConsumer(
	ctx context.Context,
	logger *slog.Logger,
	pool *pgxpool.Pool,
	rpcPool *indexer.RPCPool,
	chainID int64,
	confirmDepth int64,
) (*certificate.Consumer, *certificate.ConsumerMetrics) {
	if rpcPool == nil {
		logger.Warn("cert_consumer_disabled_no_rpc")
		return nil, nil
	}
	driver := strings.TrimSpace(os.Getenv("SIGNER_DRIVER"))
	if driver == "" {
		logger.Warn("cert_consumer_disabled_no_signer_driver",
			"hint", "设置 SIGNER_DRIVER=anvil（本地）或 keystore / kms（生产）以启用证书 mint 闭环")
		return nil, nil
	}
	contract := common.HexToAddress(os.Getenv("CERT_NFT_ADDRESS"))
	if contract == (common.Address{}) {
		logger.Warn("cert_consumer_disabled_no_contract",
			"hint", "设置 CERT_NFT_ADDRESS（已部署 CertificateNFT 合约地址）")
		return nil, nil
	}

	signerCfg := certificate.SignerConfigFromEnv(big.NewInt(chainID))
	signerCfg.Params = certificate.NewChainTxParams(rpcPool, 0)
	signer, err := certificate.NewMintSigner(ctx, signerCfg)
	if err != nil {
		logger.Error("cert_signer_init_failed", "err", err.Error())
		return nil, nil
	}

	primary := rpcPool.Primary(time.Now())
	if primary == nil {
		logger.Error("cert_consumer_disabled_no_healthy_rpc")
		return nil, nil
	}
	rc, ok := indexer.AsRPCClient(primary)
	if !ok {
		logger.Error("cert_consumer_disabled_rpc_type_assertion_failed")
		return nil, nil
	}
	ec := ethclient.NewClient(rc.RawRPC())
	ethClient := certificate.NewEthClientAdapter(ec, confirmDepth, time.Second)

	certMetrics := &certificate.ConsumerMetrics{}
	consumer, err := certificate.NewConsumer(certificate.ConsumerConfig{
		Pool:         pool,
		Signer:       signer,
		Client:       ethClient,
		ChainID:      chainID,
		ConfirmDepth: confirmDepth,
		Logger:       logger,
		Metrics:      certMetrics,
	})
	if err != nil {
		logger.Error("cert_consumer_init_failed", "err", err.Error())
		return nil, nil
	}
	logger.Info("cert_consumer_initialized",
		"signerDriver", driver,
		"contract", contract.Hex(),
		"confirmDepth", confirmDepth,
	)
	return consumer, certMetrics
}
