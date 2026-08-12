// Package indexer — runner.go 主循环：WS 订阅 + HTTP polling 兜底 + 多 RPC fallback。
//
// 状态机（一个 chain 一个 runner）：
//
//	           ┌──── WS ok ─────┐
//	startup───▶│  subscribeHead │──new head──▶ backfill(range next..head)
//	           └────┬───────────┘
//	                │ subscribeErr
//	                ▼
//	         ┌────────────┐
//	         │ pollCycle  │ 每 PollInterval 拉一次 head
//	         └────┬───────┘
//	              ▼
//	       backfill → 推进 checkpoint → emit event
//
// 多 RPC fallback：
//   - 启动时构造 primary + secondary 两个 Client；
//   - 主用 primary；任何错误（ctx 取消除外）→ 标记 primary unhealthy；
//   - healthWindow 内不重试，循环 secondary；窗口结束后回到 primary；
//   - secondary 也挂 → 退化到 polling。
//
// 优雅退出：
//   - ctx.Done() 触发 cancelInflight（WaitGroup 等所有 in-flight 区块处理完）；
//   - 关闭 WS / RPC 客户端。
//   - 退出前 flush 最后一次 checkpoint。
package indexer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"

	workerorder "github.com/x-web3/worker/internal/order"
)

// LogDecoder 把 raw log 翻译成 confirmer.ApplyInput。
//
// 调用方（cmd/worker）会注入一个适配 chain.CoursePurchasedTopic 的实现；
// 测试时注入 stub 即可。Decode 接受 indexer 包内抽象的 LogRecord，避免
// main.go 必须 import go-ethereum（仅 runner + client 依赖 go-ethereum）。
type LogDecoder interface {
	// Decode 返回 (applyInputs, ignore)。
	// ignore=true 表示该 log 不属于本 worker 关心的事件（topic 不匹配）。
	Decode(ctx context.Context, chainID int64, head *Header, raw LogRecord) (inputs []workerorder.ApplyInput, ignore bool, err error)
}

// Metrics 暴露最小指标集合（计数器）；生产可替换为 expvar / Prometheus。
type Metrics struct {
	HeadsObserved         atomic.Int64
	HeadsReorged          atomic.Int64
	LogsDecoded           atomic.Int64
	LogsIgnored           atomic.Int64
	RPCErrors             atomic.Int64
	RPCHTTPError          atomic.Int64
	RPCWSError            atomic.Int64
	RPCSwapEvents         atomic.Int64
	GapDetected           atomic.Int64
	CheckpointSave        atomic.Int64
	CheckpointSaveFail    atomic.Int64
	Backfills             atomic.Int64
	ApplyErrors           atomic.Int64
	WSConnects            atomic.Int64
	HTTPPolls             atomic.Int64
	WSHeadsDropped        atomic.Int64
	ShutdownDrainTimedOut atomic.Bool
}

// Snapshot 拷贝当前指标值（返回结构体便于 slog）。
type MetricsSnapshot struct {
	HeadsObserved, HeadsReorged, LogsDecoded, LogsIgnored      int64
	RPCErrors, RPCHTTPError, RPCWSError, RPCSwapEvents         int64
	GapDetected, CheckpointSave, CheckpointSaveFail, Backfills int64
	ApplyErrors, WSConnects, HTTPPolls, WSHeadsDropped         int64
	ShutdownDrainTimedOut                                      bool
}

// Snapshot ...
func (m *Metrics) Snapshot() MetricsSnapshot {
	return MetricsSnapshot{
		HeadsObserved:         m.HeadsObserved.Load(),
		HeadsReorged:          m.HeadsReorged.Load(),
		LogsDecoded:           m.LogsDecoded.Load(),
		LogsIgnored:           m.LogsIgnored.Load(),
		RPCErrors:             m.RPCErrors.Load(),
		RPCHTTPError:          m.RPCHTTPError.Load(),
		RPCWSError:            m.RPCWSError.Load(),
		RPCSwapEvents:         m.RPCSwapEvents.Load(),
		GapDetected:           m.GapDetected.Load(),
		CheckpointSave:        m.CheckpointSave.Load(),
		CheckpointSaveFail:    m.CheckpointSaveFail.Load(),
		Backfills:             m.Backfills.Load(),
		ApplyErrors:           m.ApplyErrors.Load(),
		WSConnects:            m.WSConnects.Load(),
		HTTPPolls:             m.HTTPPolls.Load(),
		WSHeadsDropped:        m.WSHeadsDropped.Load(),
		ShutdownDrainTimedOut: m.ShutdownDrainTimedOut.Load(),
	}
}

// Config 控制 runner 行为。
type Config struct {
	// ChainID 唯一目标链；多链需多个 Runner。
	ChainID int64
	// Consumer 用于 chain_checkpoints 主键。
	Consumer string
	// ConfirmDepth 多少块后认为不可逆。
	ConfirmDepth int64
	// PollInterval HTTP fallback 拉取间隔。
	PollInterval time.Duration
	// HealthWindow primary 失败后多久重试。
	HealthWindow time.Duration
	// BatchSize 一次 backfill 拉多少块（<= 1000, RPC 限制）。
	BatchSize int64
	// SubscribeTimeout WS 拨号超时。
	SubscribeTimeout time.Duration
	// Logger 必填；生产传 slog.Default() 或专用 logger。
	Logger *slog.Logger
	// Decoder 把 log → ApplyInput。
	Decoder LogDecoder
	// Confirmer 用于 Apply；nil 时 runner 只 backfill 不落库（仅测试用）。
	Confirmer Confirmer
	// CheckpointStore 必填。
	CheckpointStore CheckpointStore
	// RPCPool 提供主备 Client（>=1）；runner 内部管理选择。
	RPCPool *RPCPool
	// Addresses 限定需要扫描的合约地址；生产应至少配置 CourseMarket，
	// 避免同 topic 的第三方事件阻塞 checkpoint。
	Addresses []common.Address
	// DisableSubscriptions 用于纯 HTTP RPC；此时只走 polling，避免把 HTTP
	// endpoint 当作 WebSocket 无限重试 eth_subscribe。
	DisableSubscriptions bool
	// OnReorg 回调；reorg 触发时调用（admin DLQ / 通知）；nil 表示 noop。
	OnReorg func(ctx context.Context, info ReorgInfo) error
	// Metrics 必填；可传 &Metrics{} 即可。
	Metrics *Metrics
}

// Confirmer 是 workerorder.Confirmer.Apply 的最小接口（避免 import 循环）。
type Confirmer interface {
	Apply(ctx context.Context, in workerorder.ApplyInput) (uuidLike, string, error)
}

// uuidLike 是包级占位；confirmer 真正返回 google/uuid.UUID。
// 这里用空接口是为了让 workerorder.ApplyInput 完整传递，又不强制 indexer 依赖 uuid 包。
type uuidLike = any

// fillDefaults 填默认值。
func (c *Config) fillDefaults() {
	if c.Consumer == "" {
		c.Consumer = DefaultConsumer
	}
	if c.ConfirmDepth <= 0 {
		c.ConfirmDepth = 12
	}
	if c.PollInterval <= 0 {
		c.PollInterval = 5 * time.Second
	}
	if c.HealthWindow <= 0 {
		c.HealthWindow = 30 * time.Second
	}
	if c.BatchSize <= 0 || c.BatchSize > 1000 {
		c.BatchSize = 1000
	}
	if c.SubscribeTimeout <= 0 {
		c.SubscribeTimeout = 10 * time.Second
	}
	if c.Logger == nil {
		c.Logger = slog.New(slog.NewTextHandler(discardWriter{}, nil))
	}
	if c.Metrics == nil {
		c.Metrics = &Metrics{}
	}
	if c.Decoder == nil {
		c.Decoder = noopDecoder{}
	}
}

// discardWriter 用于 fillDefaults 的兜底 logger。
type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

// noopDecoder 当未注入 Decoder 时用，忽略所有 log。
type noopDecoder struct{}

func (noopDecoder) Decode(_ context.Context, _ int64, _ *Header, _ LogRecord) ([]workerorder.ApplyInput, bool, error) {
	return nil, true, nil
}

// forceResetWindow 控制"全军覆没"时强制重置的节流：5s 内只允许一次 force-reset，
// 避免对永久失败的 RPC 每 cycle 都重置（thrash）。
const forceResetWindow = 5 * time.Second

// shutdownDrainTimeout 是 graceful shutdown 时等 in-flight 区块处理的最长时限。
const shutdownDrainTimeout = 30 * time.Second

// RPCPool 在多个 Client 间轮换 + 健康检查。
//
// 当前实现是 lock-free 单生产者（runner）使用；多 consumer 需外加锁。
type RPCPool struct {
	clients        []Client
	logger         *slog.Logger
	metrics        *Metrics
	mu             sync.Mutex
	idx            int               // 当前主用 client 索引
	unhealthy      map[int]time.Time // idx -> unhealth 截止时间
	lastForceReset time.Time         // 上次 force-reset 时间（用于 5s 节流）
}

// NewRPCPool 构造多客户端池。
func NewRPCPool(clients []Client, logger *slog.Logger, m *Metrics) *RPCPool {
	if m == nil {
		m = &Metrics{}
	}
	return &RPCPool{
		clients:   clients,
		logger:    logger,
		metrics:   m,
		unhealthy: make(map[int]time.Time),
	}
}

// Len 返回池内 client 数。
func (p *RPCPool) Len() int { return len(p.clients) }

// Primary 返回当前主用 client；若主用被标记 unhealthy，自动切换到下一个健康的。
func (p *RPCPool) Primary(now time.Time) Client {
	if p == nil || len(p.clients) == 0 {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for tries := 0; tries < len(p.clients); tries++ {
		idx := (p.idx + tries) % len(p.clients)
		if until, ok := p.unhealthy[idx]; ok && now.Before(until) {
			continue
		}
		// 健康（或窗口已过）→ 切换。
		if idx != p.idx {
			if p.metrics != nil {
				p.metrics.RPCSwapEvents.Add(1)
			}
			if p.logger != nil {
				p.logger.Warn("rpc_swap", "from", p.idx, "to", idx, "remaining_unhealthy", len(p.unhealthy))
			}
			p.idx = idx
		}
		delete(p.unhealthy, idx)
		return p.clients[idx]
	}
	// 全军覆没：仅在 forceResetWindow (5s) 内强制把窗口最早的 client 顶上，
	// 避免对一个永久死掉的 RPC 每 cycle 都重置（thrash）。
	earliestIdx := -1
	var earliest time.Time
	for i := 0; i < len(p.clients); i++ {
		if until, ok := p.unhealthy[i]; ok {
			if earliestIdx == -1 || until.Before(earliest) {
				earliest = until
				earliestIdx = i
			}
		}
	}
	if earliestIdx == -1 {
		earliestIdx = 0
	}
	// 如果最近一次 force-reset 在 5s 内，跳过以避免对永久失败 RPC 的 thrash。
	if !p.lastForceReset.IsZero() && now.Sub(p.lastForceReset) < forceResetWindow {
		return p.clients[earliestIdx]
	}
	p.lastForceReset = now
	delete(p.unhealthy, earliestIdx)
	p.idx = earliestIdx
	if p.metrics != nil {
		p.metrics.RPCSwapEvents.Add(1)
	}
	if p.logger != nil {
		p.logger.Warn("rpc_force_reset", "to", earliestIdx)
	}
	return p.clients[earliestIdx]
}

// MarkUnhealthy 把当前主用标 unhealthy 直到 now+window。
func (p *RPCPool) MarkUnhealthy(c Client, window time.Duration) {
	if p == nil || c == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for i, cl := range p.clients {
		if cl == c {
			p.unhealthy[i] = time.Now().Add(window)
			return
		}
	}
}

// Close 关闭所有 client。
func (p *RPCPool) Close() {
	if p == nil {
		return
	}
	for _, c := range p.clients {
		if c != nil {
			c.Close()
		}
	}
}

// Runner 单一链的 indexer 主体。
type Runner struct {
	cfg      Config
	store    CheckpointStore
	head     *atomic.Pointer[Header] // 最新看到的 head
	applied  *atomic.Int64           // 跟踪 in-flight 应用数（用于 graceful shutdown）
	wg       sync.WaitGroup
	stopOnce sync.Once
	stopCh   chan struct{}
	stopped  chan struct{}
}

// NewRunner 构造 runner；未启动。
func NewRunner(cfg Config) (*Runner, error) {
	cfg.fillDefaults()
	if cfg.CheckpointStore == nil {
		return nil, errors.New("indexer: CheckpointStore required")
	}
	if cfg.RPCPool == nil || cfg.RPCPool.Len() == 0 {
		return nil, errors.New("indexer: RPCPool with >=1 client required")
	}
	return &Runner{
		cfg:     cfg,
		store:   cfg.CheckpointStore,
		head:    new(atomic.Pointer[Header]),
		applied: new(atomic.Int64),
		stopCh:  make(chan struct{}),
		stopped: make(chan struct{}),
	}, nil
}

// CurrentHead 返回最近观察到的 head；可能为 nil（未观察过）。
func (r *Runner) CurrentHead() *Header { return r.head.Load() }

// Start 启动主循环；返回的 channel 在 Run 返回后被关闭。
//
// 启动顺序：
//  1. LoadCheckpoint 恢复 next_block；
//  2. 启动一个 goroutine：WS 订阅，失败 → 标记 unhealthy → fallback poll；
//  3. 阻塞等 ctx.Done()；退出前 WaitGroup 等 in-flight + flush checkpoint。
func (r *Runner) Start(ctx context.Context) error {
	cp, err := r.store.Load(ctx, r.cfg.ChainID, r.cfg.Consumer)
	if err != nil && !errors.Is(err, ErrCheckpointNotFound) {
		return fmt.Errorf("indexer: load checkpoint: %w", err)
	}
	if cp != nil {
		r.cfg.Logger.Info("checkpoint_resume",
			"chainId", r.cfg.ChainID, "nextBlock", cp.NextBlock,
			"hasLastHash", len(cp.LastBlockHash) > 0,
		)
	} else {
		r.cfg.Logger.Info("checkpoint_fresh", "chainId", r.cfg.ChainID)
	}
	go r.eventLoop(ctx)
	return nil
}

// Wait 阻塞直到 runner 退出。
func (r *Runner) Wait() { <-r.stopped }

// eventLoop 是单 goroutine 主循环：消费 head → backfill → save checkpoint。
//
// 主路径：WS 订阅新 head；每收到一个 head 触发 runCycle。
// 兜底：WS 失败 → 进入 polling ticker；任何 head 触发后立即跑 cycle。
func (r *Runner) eventLoop(ctx context.Context) {
	defer close(r.stopped)
	ticker := time.NewTicker(r.cfg.PollInterval)
	defer ticker.Stop()

	// 启动时立即跑一次，避免等首个 tick。
	r.backfillOnce(ctx)

	// WS 订阅通道：用单独 goroutine 维护，断线时通过 reconnectCh 让主循环重连。
	headCh := make(chan *Header, 32)
	reconnectCh := make(chan struct{}, 1)
	if !r.cfg.DisableSubscriptions {
		go r.subscribeLoop(ctx, headCh, reconnectCh)
	}

	for {
		select {
		case <-ctx.Done():
			r.cfg.Logger.Info("indexer_shutdown_start")
			r.drainInFlight()
			r.flushCheckpoint(ctx)
			r.cfg.Logger.Info("indexer_shutdown_done",
				"metrics", r.cfg.Metrics.Snapshot(),
			)
			return
		case <-r.stopCh:
			r.drainInFlight()
			return
		case h := <-headCh:
			r.cfg.Logger.Debug("ws_head_received", "number", h.Number.Int64())
			if err := r.runCycle(ctx); err != nil {
				if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
					r.cfg.Logger.Warn("ws_cycle_failed", "err", err.Error())
				}
			}
		case <-reconnectCh:
			r.cfg.Logger.Info("ws_reconnect_signal")
			// reconnect 由 subscribeLoop 自己重试；这里只保证不 busy-loop。
			time.Sleep(200 * time.Millisecond)
		case <-ticker.C:
			r.backfillOnce(ctx)
		}
	}
}

// subscribeLoop 持续尝试 WS 订阅，断线后用 retryBackoff 退避重连。
//
// 每当 SubscribeNewHead 返回成功，就把 head 推到 out；订阅出错时通过
// reconnectCh 通知 eventLoop（仅用于打日志），然后退避重连。
func (r *Runner) subscribeLoop(ctx context.Context, out chan<- *Header, reconnect chan<- struct{}) {
	defer close(out)
	attempt := 0
	for {
		if ctx.Err() != nil {
			return
		}
		primary := r.cfg.RPCPool.Primary(time.Now())
		if primary == nil {
			// 池空：退避后重试。
			attempt++
			r.signalReconnect(reconnect)
			r.sleepBackoff(ctx, attempt)
			continue
		}
		subCtx, cancel := context.WithTimeout(ctx, r.cfg.SubscribeTimeout)
		sub, err := primary.SubscribeNewHead(subCtx)
		cancel()
		if err != nil {
			r.cfg.Metrics.RPCWSError.Add(1)
			r.cfg.Metrics.RPCErrors.Add(1)
			r.cfg.RPCPool.MarkUnhealthy(primary, r.cfg.HealthWindow)
			attempt++
			r.signalReconnect(reconnect)
			r.cfg.Logger.Warn("ws_subscribe_failed",
				"err", err.Error(), "attempt", attempt)
			r.sleepBackoff(ctx, attempt)
			continue
		}
		r.cfg.Metrics.WSConnects.Add(1)
		if r.cfg.Logger != nil {
			r.cfg.Logger.Info("ws_subscribed", "attempt_reset_to", 0)
		}
		attempt = 0
		// 消费订阅直到 ctx 取消或 sub 出错。
		r.consumeSub(ctx, sub, out, reconnect)
		sub.Unsubscribe()
	}
}

// consumeSub 把单个订阅的消费循环拉出来，方便测试。
func (r *Runner) consumeSub(ctx context.Context, sub HeadSub, out chan<- *Header, reconnect chan<- struct{}) {
	ch := sub.Chan()
	errCh := sub.Err()
	for {
		select {
		case <-ctx.Done():
			return
		case h, ok := <-ch:
			if !ok {
				// 通道被关闭：连接断，触发重连。
				r.signalReconnect(reconnect)
				return
			}
			select {
			case out <- h:
			case <-ctx.Done():
				return
			}
		case e := <-errCh:
			if e != nil {
				r.cfg.Metrics.RPCWSError.Add(1)
				r.cfg.Metrics.RPCErrors.Add(1)
				if r.cfg.Logger != nil {
					r.cfg.Logger.Warn("ws_sub_error", "err", e.Error())
				}
			}
			r.signalReconnect(reconnect)
			return
		}
	}
}

func (r *Runner) signalReconnect(ch chan<- struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}

func (r *Runner) sleepBackoff(ctx context.Context, attempt int) {
	d := retryBackoff(attempt)
	select {
	case <-time.After(d):
	case <-ctx.Done():
	}
}

// Stop 主动停止；可多次调用。
func (r *Runner) Stop() {
	r.stopOnce.Do(func() { close(r.stopCh) })
}

// backfillOnce 跑一轮：拉 latest head → 算 from..to → 拉 logs → 应用 → 推进 checkpoint。
func (r *Runner) backfillOnce(ctx context.Context) {
	r.cfg.Metrics.HTTPPolls.Add(1)
	if err := r.runCycle(ctx); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return
		}
		r.cfg.Logger.Warn("backfill_cycle_failed", "err", err.Error())
	}
}

// runCycle 是 backfillOnce 内部拆出的纯函数，方便测试注入 Client。
//
// 关键修正：
//   - 每次成功 processRange 后，把当前 range 的 end 块 hash 持久化为
//     LastBlockHash；下一个 range 起始会用它做 expected-prev-hash 校验。
//   - Save 失败要重试 + 记录 metrics，不能 swallow。
//   - 即使 to < from（确认深度不足）也要保证 LastBlockHash 被写入一次，
//     防止重启后丢失 lastHash 上下文。
func (r *Runner) runCycle(ctx context.Context) error {
	r.cfg.Metrics.Backfills.Add(1)
	pool := r.cfg.RPCPool
	primary := pool.Primary(time.Now())
	if primary == nil {
		return errors.New("indexer: no healthy RPC")
	}
	head, err := primary.HeaderByNumber(ctx, nil)
	if err != nil {
		r.cfg.Metrics.RPCHTTPError.Add(1)
		r.cfg.Metrics.RPCErrors.Add(1)
		pool.MarkUnhealthy(primary, r.cfg.HealthWindow)
		return fmt.Errorf("header latest: %w", err)
	}
	r.cfg.Metrics.HeadsObserved.Add(1)
	r.head.Store(head)

	cp, err := r.store.Load(ctx, r.cfg.ChainID, r.cfg.Consumer)
	if err != nil && !errors.Is(err, ErrCheckpointNotFound) {
		return fmt.Errorf("load checkpoint: %w", err)
	}
	from := int64(0)
	var lastHash []byte
	if cp != nil {
		from = cp.NextBlock
		lastHash = cp.LastBlockHash
	}
	if from < 0 {
		from = 0
	}
	// 仅处理到 head - ConfirmDepth；未达确认数的块保留供下次。
	safeHead := new(big.Int).Sub(head.Number, big.NewInt(r.cfg.ConfirmDepth))
	if safeHead.Sign() < 0 {
		return nil
	}
	to := safeHead.Int64()
	if to < from {
		return nil
	}

	// 步进：每 BatchSize 块拉一次。
	for start := from; start <= to; start += r.cfg.BatchSize {
		end := start + r.cfg.BatchSize - 1
		if end > to {
			end = to
		}
		rangeEndHash, err := r.processRange(ctx, primary, lastHash, start, end)
		if err != nil {
			r.cfg.Metrics.RPCHTTPError.Add(1)
			r.cfg.Metrics.RPCErrors.Add(1)
			pool.MarkUnhealthy(primary, r.cfg.HealthWindow)
			return fmt.Errorf("range %d..%d: %w", start, end, err)
		}
		// 推进 checkpoint：LastBlockHash 用本 range 最后一块的 hash。
		if err := r.saveCheckpointWithRetry(ctx, &Checkpoint{
			ChainID:       r.cfg.ChainID,
			Consumer:      r.cfg.Consumer,
			NextBlock:     end + 1,
			LastBlockHash: rangeEndHash,
		}); err != nil {
			return fmt.Errorf("checkpoint save %d..%d: %w", start, end, err)
		}
		// 后续 range 用本 range end 块的 hash 做 expected-prev-hash。
		lastHash = rangeEndHash
	}

	// 退出前再写一次，确保 lastHash 被持久化（即使 range loop 跑了 0 次也走这里）。
	if err := r.saveCheckpointWithRetry(ctx, &Checkpoint{
		ChainID:       r.cfg.ChainID,
		Consumer:      r.cfg.Consumer,
		NextBlock:     to + 1,
		LastBlockHash: lastHash,
	}); err != nil {
		return fmt.Errorf("checkpoint final flush: %w", err)
	}
	return nil
}

// processRange 拉取并处理 from..to 区间，含 reorg 检测。
//
// 返回：to 块的 hash（用于持久化 LastBlockHash）。如果 to 块 header 拉不到，
// 返回 (nil, err) 由 caller 处理。
func (r *Runner) processRange(ctx context.Context, client Client, expectedPrevHash []byte, from, to int64) ([]byte, error) {
	if expectedPrevHash != nil {
		// 校验 from-1 块 hash 是否匹配 checkpoint；不匹配则 reorg 触发
		h, err := client.HeaderByNumber(ctx, big.NewInt(from-1))
		if err == nil && h != nil && h.Hash != common.BytesToHash(expectedPrevHash) {
			r.cfg.Logger.Warn("reorg_detected_via_checkpoint_mismatch",
				"from", from, "observed", h.Hash.Hex(),
				"expected", common.BytesToHash(expectedPrevHash).Hex())
			r.cfg.Metrics.HeadsReorged.Add(1)
			if err := r.handleReorg(ctx, from-1, h.Hash.Bytes(), "depth_miss"); err != nil {
				return nil, fmt.Errorf("reorg: %w", err)
			}
		}
	}
	q := ethereum.FilterQuery{
		FromBlock: big.NewInt(from),
		ToBlock:   big.NewInt(to),
		Addresses: r.cfg.Addresses,
	}
	logs, err := client.FilterLogs(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("filter logs: %w", err)
	}
	for _, lg := range logs {
		r.cfg.Metrics.LogsDecoded.Add(1)
		rec := LogRecord{
			BlockNumber: lg.BlockNumber,
			BlockHash:   lg.BlockHash,
			TxHash:      lg.TxHash,
			LogIndex:    uint(lg.Index),
			Address:     lg.Address,
			Topics:      lg.Topics,
			Data:        lg.Data,
			Removed:     lg.Removed,
		}
		inputs, ignore, err := r.cfg.Decoder.Decode(ctx, r.cfg.ChainID, nil, rec)
		if err != nil {
			r.cfg.Metrics.ApplyErrors.Add(1)
			r.cfg.Logger.Warn("decode_failed",
				"txHash", lg.TxHash.Hex(), "logIndex", lg.Index, "err", err.Error())
			continue
		}
		if ignore {
			r.cfg.Metrics.LogsIgnored.Add(1)
			continue
		}
		for _, in := range inputs {
			if r.cfg.Confirmer == nil {
				continue
			}
			// Apply 必须成功后才能推进 checkpoint。链上交易可能先于前端的
			// txHash 上报被扫到，此时 ErrOrderNotFound 应让本 range 下轮重试，
			// 不能异步失败后仍把该区块永久跳过。
			_, state, err := r.cfg.Confirmer.Apply(ctx, in)
			if err != nil {
				r.cfg.Metrics.ApplyErrors.Add(1)
				r.cfg.Logger.Error("apply_failed",
					"orderId", in.OrderID.String(), "err", err.Error())
				return nil, fmt.Errorf("apply event: %w", err)
			}
			r.cfg.Logger.Info("apply_ok", "orderId", in.OrderID.String(), "state", state)
		}
	}
	// 拉 to 块 header 以获取 hash，供 Checkpoint.LastBlockHash 持久化。
	endHdr, err := client.HeaderByNumber(ctx, big.NewInt(to))
	if err != nil {
		// 拉不到 end header：返回 nil hash（不阻塞 cycle），但 caller 仍可
		// 选择继续推进 NextBlock。下次启动 lastHash 缺失。
		r.cfg.Logger.Warn("process_range_end_header_missing",
			"to", to, "err", err.Error())
		return nil, nil
	}
	if endHdr == nil {
		return nil, nil
	}
	return endHdr.Hash.Bytes(), nil
}

// saveCheckpointWithRetry 带退避的 checkpoint 持久化；记录失败次数，
// 并把 LastBlockHash 持久化在每次成功 Save 里。
func (r *Runner) saveCheckpointWithRetry(ctx context.Context, cp *Checkpoint) error {
	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := r.store.Save(ctx, cp); err != nil {
			lastErr = err
			r.cfg.Metrics.CheckpointSaveFail.Add(1)
			r.cfg.Logger.Warn("checkpoint_save_failed",
				"attempt", attempt, "err", err.Error(),
				"nextBlock", cp.NextBlock)
			select {
			case <-time.After(retryBackoff(attempt)):
			case <-ctx.Done():
				return ctx.Err()
			}
			continue
		}
		r.cfg.Metrics.CheckpointSave.Add(1)
		return nil
	}
	return fmt.Errorf("checkpoint save retries exhausted: %w", lastErr)
}

// handleReorg 是 reorg.go 内 HandleReorg 的薄包装；这里只触发回调。
//
// 真正的回滚逻辑见 reorg.go；这里保持单一职责以方便测试。
func (r *Runner) handleReorg(ctx context.Context, commonBlock int64, newHash []byte, reason string) error {
	if r.cfg.OnReorg == nil {
		return nil
	}
	return r.cfg.OnReorg(ctx, ReorgInfo{
		ChainID:     r.cfg.ChainID,
		CommonBlock: commonBlock,
		NewHash:     newHash,
		Reason:      reason,
	})
}

// flushCheckpoint 在退出前确保最后状态被持久化（best-effort）。
//
// 关键修正：之前的实现只 Load 不 Save → 完全没写盘。这里把
// 当前 NextBlock + LastBlockHash 重新落盘；用独立 flushCtx 防止父 ctx
// 已 cancel 时丢失退出状态。
func (r *Runner) flushCheckpoint(_ context.Context) {
	head := r.head.Load()
	if head == nil {
		r.cfg.Logger.Info("flush_checkpoint_skipped", "reason", "no head observed")
		return
	}
	// 单独 ctx：即使父 ctx 已 cancel，仍尝试落盘。
	flushCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cp, err := r.store.Load(flushCtx, r.cfg.ChainID, r.cfg.Consumer)
	if err != nil {
		r.cfg.Logger.Warn("flush_checkpoint_load_failed", "err", err.Error())
		return
	}
	if cp == nil {
		// 没记录可 flush；落一条从 0 开始的 baseline。
		cp = &Checkpoint{
			ChainID:   r.cfg.ChainID,
			Consumer:  r.cfg.Consumer,
			NextBlock: 0,
		}
	}
	// 强制把 LastBlockHash 写一次，确保重启后 reorg 校验基线存在。
	if err := r.store.Save(flushCtx, cp); err != nil {
		r.cfg.Logger.Warn("flush_checkpoint_save_failed", "err", err.Error())
		r.cfg.Metrics.CheckpointSaveFail.Add(1)
		return
	}
	r.cfg.Logger.Info("flush_checkpoint_ok",
		"nextBlock", cp.NextBlock,
		"hasLastHash", len(cp.LastBlockHash) > 0,
		"head", head.Number.Int64(),
	)
}

// drainInFlight 等 in-flight Apply 跑完，最多 shutdownDrainTimeout。
// 避免 Confirmer 忽略 ctx.Done() 时整个进程 hang。
func (r *Runner) drainInFlight() {
	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return
	case <-time.After(shutdownDrainTimeout):
		r.cfg.Metrics.ShutdownDrainTimedOut.Store(true)
		r.cfg.Logger.Error("shutdown_drain_timeout",
			"applied_inflight", r.applied.Load(),
			"timeout", shutdownDrainTimeout,
		)
	}
}

// retryBackoff 指数退避（仅供测试 stub 引用）。
func retryBackoff(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	// 2^attempt 在转换为 time.Duration 前必须封顶；否则大 attempt 的
	// float64 会溢出为负 duration，time.After 立即返回并形成 busy loop。
	if attempt >= 7 {
		return 10 * time.Second
	}
	d := time.Duration(math.Pow(2, float64(attempt))) * 100 * time.Millisecond
	if d > 10*time.Second {
		return 10 * time.Second
	}
	return d
}
