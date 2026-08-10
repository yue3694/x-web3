// Package metrics 暴露 worker 侧的 Prometheus 指标 + /metrics 端点。
//
// 设计：
//   - 独立 Registry（不与 api 默认 registry 共享），避免双向误抓；
//   - 计数器通过 CounterFunc / GaugeFunc 包装各子系统的 atomic 快照，
//     写时零成本；/metrics 抓取时即时读取；
//   - chain_indexer_lag / head_block / next_block 在 scrape 触发时
//     通过 ChainLagFunc 拉取（DB + RPC），delay 5s 节流；
//   - 标准 go runtime / process collector 在 init() 自动注册；
//   - /metrics 由独立 http.Server 提供（与 worker 主逻辑解耦），
//     地址由 caller 传入；为空时直接 no-op，本地 dev 不强行拖端口。
//
// 命名约定：所有指标以 `worker_` 前缀避免与 API 指标同名串扰。
package metrics

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/x-web3/worker/internal/certificate"
	"github.com/x-web3/worker/internal/indexer"
)

// indexerAdapter 把 *indexer.Metrics 适配为 IndexerMetrics 接口。
type indexerAdapter struct{ m *indexer.Metrics }

func (a indexerAdapter) HeadsObserved() int64 { return a.m.HeadsObserved.Load() }
func (a indexerAdapter) HeadsReorged() int64  { return a.m.HeadsReorged.Load() }
func (a indexerAdapter) LogsDecoded() int64   { return a.m.LogsDecoded.Load() }
func (a indexerAdapter) LogsIgnored() int64   { return a.m.LogsIgnored.Load() }
func (a indexerAdapter) RPCErrors() (int64, int64) {
	return a.m.RPCHTTPError.Load(), a.m.RPCWSError.Load()
}
func (a indexerAdapter) RPCSwapEvents() int64  { return a.m.RPCSwapEvents.Load() }
func (a indexerAdapter) GapDetected() int64    { return a.m.GapDetected.Load() }
func (a indexerAdapter) Backfills() int64      { return a.m.Backfills.Load() }
func (a indexerAdapter) ApplyErrors() int64    { return a.m.ApplyErrors.Load() }
func (a indexerAdapter) WSConnects() int64     { return a.m.WSConnects.Load() }
func (a indexerAdapter) HTTPPolls() int64      { return a.m.HTTPPolls.Load() }
func (a indexerAdapter) WSHeadsDropped() int64 { return a.m.WSHeadsDropped.Load() }
func (a indexerAdapter) CheckpointSave() int64 { return a.m.CheckpointSave.Load() }
func (a indexerAdapter) CheckpointSaveFail() int64 {
	return a.m.CheckpointSaveFail.Load()
}

// WrapIndexer 把 *indexer.Metrics 适配为 IndexerMetrics 接口；nil 时返 nil。
func WrapIndexer(m *indexer.Metrics) IndexerMetrics {
	if m == nil {
		return nil
	}
	return indexerAdapter{m: m}
}

// certAdapter 把 *certificate.ConsumerMetrics 适配为 CertConsumerMetrics 接口。
// ConsumerMetrics 用 atomic.Uint64，metrics 期望 int64；这里转换。
type certAdapter struct{ m *certificate.ConsumerMetrics }

func (a certAdapter) Processed() int64         { return int64(a.m.Processed.Load()) }
func (a certAdapter) Succeeded() int64         { return int64(a.m.Succeeded.Load()) }
func (a certAdapter) Retried() int64           { return int64(a.m.Retried.Load()) }
func (a certAdapter) DeadLet() int64           { return int64(a.m.DeadLet.Load()) }
func (a certAdapter) BroadcastRetries() int64  { return int64(a.m.BroadcastRetries.Load()) }

// WrapCertConsumer ...
func WrapCertConsumer(m *certificate.ConsumerMetrics) CertConsumerMetrics {
	if m == nil {
		return nil
	}
	return certAdapter{m: m}
}

// Registry 是 worker 暴露指标的注册表，自动注册 go / process collector。
var Registry = prometheus.NewRegistry()

func init() {
	Registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
}

// IndexerMetrics 是 indexer.Metrics 的最小子集（避免 metrics 包反向依赖 indexer 私有字段）。
//
// worker 的 main.go 在构造 indexer.Runner 时拿到 *indexer.Metrics；调用方
// 通过 IndexerMetricsAdapter 即可把 *indexer.Metrics 暴露为本接口。
type IndexerMetrics interface {
	HeadsObserved() int64
	HeadsReorged() int64
	LogsDecoded() int64
	LogsIgnored() int64
	RPCErrors() (http, ws int64)
	RPCSwapEvents() int64
	GapDetected() int64
	Backfills() int64
	ApplyErrors() int64
	WSConnects() int64
	HTTPPolls() int64
	WSHeadsDropped() int64
	CheckpointSave() int64
	CheckpointSaveFail() int64
}

// CertConsumerMetrics 暴露 certificate.ConsumerMetrics 的读取。
type CertConsumerMetrics interface {
	Processed() int64
	Succeeded() int64
	Retried() int64
	DeadLet() int64
	BroadcastRetries() int64
}

// ReconcileSnapshot 是 reconcile.Scanner.Metrics() 的最小子集
// （避免 metrics 包依赖 reconcile 包）。
type ReconcileSnapshot struct {
	LastScanUnix int64
	ScanRuns     int64
	GapDetected  int64
}

// ReconcileSnapFunc 由 caller 提供；reconcile 主循环每轮 ScanOnce 后
// 调用 metrics 包的 SetReconcileSnapshot 推一次。
type ReconcileSnapFunc func() ReconcileSnapshot

// ChainLagFunc 在 /metrics 抓取时调用：返回 (nextBlock, headBlock, err)。
//
// 业务方实现：
//   - SELECT next_block FROM chain_checkpoints WHERE chain_id=$1 AND consumer=$2
//   - 调用 primary RPC 的 eth_blockNumber
type ChainLagFunc func(ctx context.Context) (nextBlock, headBlock int64, err error)

// Sources 暴露所有 worker 自监控信号；任一字段可为 nil，对应指标恒为 0。
type Sources struct {
	Indexer      IndexerMetrics
	CertConsumer CertConsumerMetrics
	ChainID      int64
	Consumer     string // checkpoint consumer key
}

// Server 是 /metrics HTTP 监听器。
type Server struct {
	srv *http.Server
}

// 已注册的 collectors（避免重复注册导致 panic）。
var registered sync.Map // map[string]struct{}

// Register 注册所有 workers 自监控 collectors。重复调用幂等。
//
// 注意：CounterFunc + 静态标签不支持开箱即用（仅 CounterFunc 没有 labels，
// 而 CounterVec 的标签是动态的）；这里用 constLabelCollector 把固定的
// {kind: http/ws/any} 注入到 desc。
func Register(sources Sources, reconcileSnap ReconcileSnapFunc, chainLag ChainLagFunc) {
	registerIndexerCounters(sources.Indexer)
	registerCertConsumerCounters(sources.CertConsumer)
	registerReconcileCounters(reconcileSnap)
	if chainLag != nil {
		registerLagGauges(sources.ChainID, chainLag)
	}
}

// Start 起 :<addr>/metrics；返回 Server 控制优雅退出。
//
// addr 形如 ":9090" 或 "127.0.0.1:9090"。空字符串 → no-op（dev 模式）。
func Start(ctx context.Context, logger *slog.Logger, addr string) *Server {
	if addr == "" {
		logger.Info("worker_metrics_disabled", "reason", "addr empty")
		return &Server{}
	}
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(Registry, promhttp.HandlerOpts{Registry: Registry}))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      10 * time.Second,
	}
	go func() {
		logger.Info("worker_metrics_listen", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("worker_metrics_listen_failed", "err", err.Error())
		}
	}()
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()
	return &Server{srv: srv}
}

// Close 优雅关闭；addr 为空时 no-op。
func (s *Server) Close() error {
	if s == nil || s.srv == nil {
		return nil
	}
	return s.srv.Close()
}

// ---------------------------------------------------------------------------
// indexer 计数器
// ---------------------------------------------------------------------------

func registerIndexerCounters(m IndexerMetrics) {
	if m == nil {
		return
	}
	mustRegisterConstLabelCounter("worker_indexer_heads_observed_total",
		"Number of block heads observed by the worker indexer.",
		func() float64 { return float64(m.HeadsObserved()) })
	mustRegisterConstLabelCounter("worker_indexer_logs_decoded_total",
		"Logs successfully decoded into ApplyInputs.",
		func() float64 { return float64(m.LogsDecoded()) })
	mustRegisterConstLabelCounter("worker_indexer_logs_ignored_total",
		"Logs ignored because topic did not match.",
		func() float64 { return float64(m.LogsIgnored()) })
	mustRegisterConstLabelCounter("worker_indexer_apply_errors_total",
		"Confirmer.Apply returned an error.",
		func() float64 { return float64(m.ApplyErrors()) })
	mustRegisterConstLabelCounter("worker_indexer_reorgs_total",
		"Number of reorg events observed.",
		func() float64 { return float64(m.HeadsReorged()) })
	mustRegisterConstLabelCounter("worker_indexer_gaps_total",
		"Number of gaps detected during backfill.",
		func() float64 { return float64(m.GapDetected()) })
	mustRegisterConstLabelCounter("worker_indexer_backfills_total",
		"Number of backfill rounds executed.",
		func() float64 { return float64(m.Backfills()) })
	mustRegisterConstLabelCounter("worker_indexer_checkpoint_saves_total",
		"Successful checkpoint saves.",
		func() float64 { return float64(m.CheckpointSave()) })
	mustRegisterConstLabelCounter("worker_indexer_checkpoint_save_failures_total",
		"Failed checkpoint save attempts (each retry counts).",
		func() float64 { return float64(m.CheckpointSaveFail()) })

	httpErr, wsErr := m.RPCErrors()
	mustRegisterPartitionedCounter(
		"worker_indexer_rpc_errors_total",
		"RPC errors partitioned by transport (http/ws/any).",
		"kind",
		[]prometheus.Labels{{"kind": "any"}, {"kind": "http"}, {"kind": "ws"}},
		[]func() float64{
			func() float64 { return float64(httpErr + wsErr) },
			func() float64 { return float64(httpErr) },
			func() float64 { return float64(wsErr) },
		},
	)
	mustRegisterConstLabelCounter("worker_indexer_rpc_swap_events_total",
		"RPC swap events seen (used to detect primary failover).",
		func() float64 { return float64(m.RPCSwapEvents()) })
	mustRegisterConstLabelCounter("worker_indexer_ws_connects_total",
		"Successful WS dials / reconnects.",
		func() float64 { return float64(m.WSConnects()) })
	mustRegisterConstLabelCounter("worker_indexer_http_polls_total",
		"HTTP poll iterations.",
		func() float64 { return float64(m.HTTPPolls()) })
	mustRegisterConstLabelCounter("worker_indexer_ws_heads_dropped_total",
		"WS heads dropped due to lag.",
		func() float64 { return float64(m.WSHeadsDropped()) })
}

// ---------------------------------------------------------------------------
// certificate consumer 计数器
// ---------------------------------------------------------------------------

func registerCertConsumerCounters(m CertConsumerMetrics) {
	if m == nil {
		return
	}
	mustRegisterConstLabelCounter("worker_cert_jobs_processed_total",
		"Certificate jobs picked from the queue.",
		func() float64 { return float64(m.Processed()) })
	mustRegisterConstLabelCounter("worker_cert_jobs_succeeded_total",
		"Certificate jobs reaching the confirmed state.",
		func() float64 { return float64(m.Succeeded()) })
	mustRegisterConstLabelCounter("worker_cert_jobs_retried_total",
		"Certificate jobs that failed once and were retried.",
		func() float64 { return float64(m.Retried()) })
	mustRegisterConstLabelCounter("worker_cert_jobs_dead_letter_total",
		"Certificate jobs sent to DLQ after max attempts.",
		func() float64 { return float64(m.DeadLet()) })
	mustRegisterConstLabelCounter("worker_cert_jobs_broadcast_retries_total",
		"Broadcast-layer retries (eth_sendTransaction failures).",
		func() float64 { return float64(m.BroadcastRetries()) })
}

// ---------------------------------------------------------------------------
// reconcile 计数器（cached snapshot）
// ---------------------------------------------------------------------------

// cachedReconcile 缓存 reconcile.Metrics 的同步快照。reconcile 主循环
// 每轮 ScanOnce 后调 SetReconcileSnapshot 推一次；CounterFunc 在 scrape 时
// 读取，避免在主循环加锁。
type cachedReconcile struct {
	scans    atomic.Int64
	gaps     atomic.Int64
	lastUnix atomic.Int64
}

var globalReconcileCache = &cachedReconcile{}

// SetReconcileSnapshot reconcile 主循环在 ScanOnce 后调一次。
func SetReconcileSnapshot(s ReconcileSnapshot) {
	globalReconcileCache.scans.Store(s.ScanRuns)
	globalReconcileCache.gaps.Store(s.GapDetected)
	globalReconcileCache.lastUnix.Store(s.LastScanUnix)
}

func registerReconcileCounters(snap ReconcileSnapFunc) {
	if snap == nil {
		return
	}
	mustRegisterConstLabelCounter("worker_reconcile_scans_total",
		"Reconcile ScanOnce iterations executed.",
		func() float64 { return float64(globalReconcileCache.scans.Load()) })
	mustRegisterConstLabelCounter("worker_reconcile_gaps_total",
		"Total gaps detected across all reconcile scans.",
		func() float64 { return float64(globalReconcileCache.gaps.Load()) })
	mustRegisterGaugeFunc("worker_reconcile_last_scan_unix_seconds",
		"Unix timestamp of the most recent reconcile scan; 0 if never.",
		func() float64 { return float64(globalReconcileCache.lastUnix.Load()) })
}

// ---------------------------------------------------------------------------
// chain lag gauges（scrape 时按需拉取 + 5s 节流）
// ---------------------------------------------------------------------------

// globalLagScrape scrape 触发时刷新；多个 gauge 共享同一缓存。
var globalLagScrape *lagScrape

func registerLagGauges(chainID int64, lag ChainLagFunc) {
	if globalLagScrape != nil {
		return
	}
	globalLagScrape = newLagScrape(lag)
	mustRegisterGaugeFunc("worker_chain_indexer_next_block",
		"Next block the worker intends to consume (from chain_checkpoints).",
		func() float64 {
			v, ok := globalLagScrape.cachedNext()
			if !ok {
				return 0
			}
			return float64(v)
		})
	mustRegisterGaugeFunc("worker_chain_indexer_head_block",
		"Current chain head block (from RPC).",
		func() float64 {
			_, head, _ := globalLagScrape.cached()
			return float64(head)
		})
	mustRegisterGaugeFunc("worker_chain_indexer_lag_blocks",
		"Difference between chain head and worker next_block (>= 0).",
		func() float64 {
			next, head, err := globalLagScrape.cached()
			if err != nil || head < next {
				return 0
			}
			return float64(head - next)
		})
	mustRegisterGaugeFunc("worker_chain_indexer_rpc_available",
		"1 if chain RPC + DB reads succeeded during the last scrape, 0 otherwise.",
		func() float64 {
			if _, _, err := globalLagScrape.cached(); err != nil {
				return 0
			}
			return 1
		})
}

// lagScrape 5s 节流刷新；多 gauge 共享。
type lagScrape struct {
	lag          ChainLagFunc
	lastUpdateNs atomic.Int64
	mu           atomic.Int64 // 0 = idle, 1 = busy
	cNext        atomic.Int64
	cHead        atomic.Int64
	cErr         atomic.Int64 // 1 = last fetch failed
}

func newLagScrape(lag ChainLagFunc) *lagScrape {
	return &lagScrape{lag: lag}
}

func (l *lagScrape) refresh(ctx context.Context) {
	const minIntervalNs = int64(5 * time.Second)
	now := time.Now().UnixNano()
	last := l.lastUpdateNs.Load()
	if now-last < minIntervalNs {
		return
	}
	if l.mu.Swap(1) == 1 {
		return
	}
	defer l.mu.Store(0)

	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	next, head, err := l.lag(cctx)
	if err == nil {
		l.cNext.Store(next)
		l.cHead.Store(head)
		l.cErr.Store(0)
	} else {
		l.cErr.Store(1)
	}
	l.lastUpdateNs.Store(time.Now().UnixNano())
}

func (l *lagScrape) cached() (int64, int64, error) {
	l.refresh(context.Background())
	if l.cErr.Load() == 1 {
		return l.cNext.Load(), l.cHead.Load(), errors.New("chain lag fetch failed")
	}
	return l.cNext.Load(), l.cHead.Load(), nil
}

func (l *lagScrape) cachedNext() (int64, bool) {
	l.refresh(context.Background())
	if l.cErr.Load() == 1 {
		return 0, false
	}
	return l.cNext.Load(), true
}

// ---------------------------------------------------------------------------
// collector helpers
// ---------------------------------------------------------------------------

// mustRegisterConstLabelCounter 注册一个 CounterFunc 并附加 const labels。
//
// labelKVs 全部作为 const labels（例如 "kind","http"），不导出 variable
// labels：相同 metric 名 + 不同 const 组合会变成不同 series（这样才能
// 在 worker_indexer_rpc_errors_total 下分 http/ws/any 三条 series）。
// 注意：NewDesc 的 variableLabels 必须是 nil，否则同一 label 名既出现在
// const 又出现在 variable 会被 Registry 拒绝（duplicate label names）。
func mustRegisterConstLabelCounter(name, help string, fn func() float64, labelKVs ...string) {
	if len(labelKVs)%2 != 0 {
		panic("metrics: label kvs must be pairs")
	}
	key := name + "|" + flatKVs(labelKVs)
	if _, ok := registered.Load(key); ok {
		return
	}
	constLabels := make(prometheus.Labels, len(labelKVs)/2)
	for i := 0; i < len(labelKVs); i += 2 {
		constLabels[labelKVs[i]] = labelKVs[i+1]
	}
	c := &constLabelCollector{
		desc: prometheus.NewDesc(
			name,
			help,
			nil, // variable labels：const-only 计数器一律不导出
			constLabels,
		),
		value: fn,
		vt:   prometheus.CounterValue,
	}
	Registry.MustRegister(c)
	registered.Store(key, struct{}{})
}

// mustRegisterPartitionedCounter 注册一个由多个 fn 共享 fqName 的多 series
// 计数器，每个 (kind → fn) 组成一条 series。最终 desc 帮助字符串统一，避免
// Prometheus 因「same fqName with different help」拒绝注册。
//
// 用法示例（worker_indexer_rpc_errors_total 分 http/ws/any 三 series）：
//
//	mustRegisterPartitionedCounter(
//	    "worker_indexer_rpc_errors_total",
//	    "RPC errors partitioned by transport (http/ws/any).",
//	    "kind",
//	    []prometheus.Labels{
//	        {"kind": "any"},
//	        {"kind": "http"},
//	        {"kind": "ws"},
//	    },
//	    []func() float64{httpErr+wsErr, httpErr, wsErr},
//	)
func mustRegisterPartitionedCounter(name, help, dim string, dims []prometheus.Labels, fns []func() float64) {
	if len(dims) != len(fns) {
		panic("metrics: dims and fns length mismatch")
	}
	key := name + "|" + dim + "|n=" + itoa(len(dims))
	if _, ok := registered.Load(key); ok {
		return
	}
	c := &partitionedCollector{
		desc:  prometheus.NewDesc(name, help, []string{dim}, nil),
		dims:  dims,
		fns:   fns,
		vt:    prometheus.CounterValue,
	}
	Registry.MustRegister(c)
	registered.Store(key, struct{}{})
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// partitionedCollector 在一次 scrape 中按 dims/fns 多发射（multi-emit）若干
// ConstMetric。Describe 仍只发一次 desc，Collect 调每个 fn 取最新值并逐条
// 发射——保证同名 metric 下不同 (kind=…) 值独立累计。
type partitionedCollector struct {
	desc *prometheus.Desc
	dims []prometheus.Labels
	fns  []func() float64
	vt   prometheus.ValueType
}

func (c *partitionedCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.desc
}

func (c *partitionedCollector) Collect(ch chan<- prometheus.Metric) {
	for i, dim := range c.dims {
		ch <- prometheus.MustNewConstMetric(
			c.desc,
			c.vt,
			c.fns[i](),
			singleLabelValue(dim),
		)
	}
}

// singleLabelValue 取 dim 中唯一一对 KV 的 value。调用方约定 dims 元素
// 只含一个 variable label（如 {"kind":"http"}）。
func singleLabelValue(dim prometheus.Labels) string {
	if len(dim) == 0 {
		return ""
	}
	for _, v := range dim {
		return v
	}
	return ""
}

func mustRegisterGaugeFunc(name, help string, fn func() float64) {
	if _, ok := registered.Load(name); ok {
		return
	}
	Registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: name, Help: help}, fn))
	registered.Store(name, struct{}{})
}

// flatKVs 把 "k1","v1","k2","v2" 拼成 "k1=v1,k2=v2" 用作 dedup key。
func flatKVs(kvs []string) string {
	out := ""
	for i := 0; i < len(kvs); i += 2 {
		if i > 0 {
			out += ","
		}
		out += kvs[i] + "=" + kvs[i+1]
	}
	return out
}

// constLabelCollector 是 CounterFunc + 静态标签的实现。
//
// prometheus 库没有 CounterFunc 带 labels 的直接 API；最简方案是
// 自定义 Collector，desc 用 constLabels 静态绑定。每次 scrape 调用
// value() 读取最新值，顺序维持 labelNames[] 顺序（constLabels 顺序
// 在 Labels map 中无效，但 NewDesc 接收的 constLabels map 顺序由
// Library 内部决定；Prometheus 允许 constLabels 的 key 顺序自由）。
type constLabelCollector struct {
	desc  *prometheus.Desc
	vt    prometheus.ValueType
	value func() float64
}

func (c *constLabelCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.desc
}

func (c *constLabelCollector) Collect(ch chan<- prometheus.Metric) {
	ch <- prometheus.MustNewConstMetric(
		c.desc,
		c.vt,
		c.value(),
	)
}
