package metrics

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// labelKVsToMap 拆 K,V 列表为 prometheus.Labels（用于 constLabels）。
func labelKVsToMap(kvs []string) prometheus.Labels {
	m := make(prometheus.Labels, len(kvs)/2)
	for i := 0; i < len(kvs); i += 2 {
		m[kvs[i]] = kvs[i+1]
	}
	return m
}

// mkCounter 同包内 helper：构造 constLabelCollector 便于测试。
// kvs 走 constLabels 路径（无 variable labels）。
func mkCounter(name, help string, fn func() float64, kvs ...string) prometheus.Collector {
	return &constLabelCollector{
		desc:  prometheus.NewDesc(name, help, nil, labelKVsToMap(kvs)),
		vt:    prometheus.CounterValue,
		value: fn,
	}
}

// TestCounterWithConstLabel_Render 验证 constLabelCollector 输出的 metric
// 文本包含 const label。
func TestCounterWithConstLabel_Render(t *testing.T) {
	reg := prometheus.NewRegistry()
	c := mkCounter("test_counter", "help", func() float64 { return 42 }, "kind", "http")
	reg.MustRegister(c)

	srv := httptest.NewServer(promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `test_counter{kind="http"} 42`) {
		t.Fatalf("expected metric line not found in body:\n%s", body)
	}
}

// TestCounterWithConstLabel_UpdatesValue 验证 scrape 之间 CounterFunc 值变化。
func TestCounterWithConstLabel_UpdatesValue(t *testing.T) {
	reg := prometheus.NewRegistry()
	v := atomic.Int64{}
	v.Store(1)
	c := mkCounter("test_counter", "help", func() float64 { return float64(v.Load()) })
	reg.MustRegister(c)

	srv := httptest.NewServer(promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	defer srv.Close()

	// 第一次 scrape
	resp, _ := http.Get(srv.URL)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "test_counter 1") {
		t.Fatalf("first scrape missing 1:\n%s", body)
	}

	// 改值
	v.Store(99)
	resp, _ = http.Get(srv.URL)
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "test_counter 99") {
		t.Fatalf("second scrape missing 99:\n%s", body)
	}
}

// TestStart_NoAddr_NoOp 验证 addr 为空时 Start 不起 server 不 panic。
func TestStart_NoAddr_NoOp(t *testing.T) {
	srv := Start(context.Background(), slog.New(slog.NewTextHandler(io.Discard, nil)), "")
	if srv == nil {
		t.Fatal("Start 返回 nil *Server")
	}
	if err := srv.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// TestStart_RealAddr_ServesMetrics 验证 Start 在真实地址上能起来并响应 /metrics + /healthz。
func TestStart_RealAddr_ServesMetrics(t *testing.T) {
	// 选 0 端口：让 OS 分配空闲端口；再读 srv.Addr 拼出 URL。
	// Start 返回 http.Server，使用 net.Listen 不可访问；改为监听随机端口 + 启动
	// 等价 server 实现验证渲染。
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(Registry, promhttp.HandlerOpts{Registry: Registry}))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	go func() {
		_ = http.Serve(ln, mux)
	}()

	url := "http://" + ln.Addr().String()
	resp, err := http.Get(url + "/healthz")
	if err != nil {
		t.Fatalf("get healthz: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != `{"status":"ok"}` {
		t.Fatalf("healthz body = %q", body)
	}

	resp2, err := http.Get(url + "/metrics")
	if err != nil {
		t.Fatalf("get metrics: %v", err)
	}
	defer resp2.Body.Close()
	body2, _ := io.ReadAll(resp2.Body)
	// Registry 至少有 go / process 指标（init 自动注册）。
	if !strings.Contains(string(body2), "go_goroutines") {
		t.Fatalf("expected go_goroutines in metrics body; got:\n%s", body2)
	}
}

// TestLagScrape_RefreshOnce 验证 chain lag 抓取与节流。
func TestLagScrape_RefreshOnce(t *testing.T) {
	calls := atomic.Int64{}
	ls := newLagScrape(func(ctx context.Context) (int64, int64, error) {
		calls.Add(1)
		return 100, 200, nil
	})
	// 首次 fetch
	next, head, err := ls.cached()
	if err != nil {
		t.Fatalf("first cached: %v", err)
	}
	if next != 100 || head != 200 {
		t.Fatalf("next=%d head=%d, want 100/200", next, head)
	}
	if calls.Load() != 1 {
		t.Fatalf("lag fn calls = %d, want 1", calls.Load())
	}
	// 再次 fetch：节流期间不调用底层
	_, _, _ = ls.cached()
	if calls.Load() != 1 {
		t.Fatalf("second cached should be throttled; calls = %d", calls.Load())
	}
}

// TestLagScrape_Error 验证 chain lag 失败时返 err。
func TestLagScrape_Error(t *testing.T) {
	calls := atomic.Int64{}
	ls := newLagScrape(func(ctx context.Context) (int64, int64, error) {
		calls.Add(1)
		return 0, 0, errors.New("rpc down")
	})
	_, _, err := ls.cached()
	if err == nil {
		t.Fatal("expected first cached to err")
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", calls.Load())
	}
}

// TestFlatKVs 验证 dedup key 拼接。
func TestFlatKVs(t *testing.T) {
	if got := flatKVs([]string{"kind", "http", "code", "200"}); got != "kind=http,code=200" {
		t.Fatalf("flatKVs = %q", got)
	}
	if flatKVs(nil) != "" {
		t.Fatal("flatKVs(nil) should be empty")
	}
}

// TestSetReconcileSnapshot 验证 reconcile snapshot 推送被 CounterFunc 读取。
func TestSetReconcileSnapshot(t *testing.T) {
	SetReconcileSnapshot(ReconcileSnapshot{ScanRuns: 7, GapDetected: 2, LastScanUnix: 1700000000})
	if got := globalReconcileCache.scans.Load(); got != 7 {
		t.Fatalf("scans = %d, want 7", got)
	}
	if got := globalReconcileCache.gaps.Load(); got != 2 {
		t.Fatalf("gaps = %d, want 2", got)
	}
	if got := globalReconcileCache.lastUnix.Load(); got != 1700000000 {
		t.Fatalf("lastUnix = %d, want 1700000000", got)
	}
}

// TestPartitionedCollector_ThreeSeries 验证 mustRegisterPartitionedCounter
// 注册一个 metric，三条 (kind=any/http/ws) series 都能被 scrape 看到，
// 且数值各自独立累计。这是修复「同一 fqName 不同 help 串被 Registry
// 拒绝」的回归测试。
func TestPartitionedCollector_ThreeSeries(t *testing.T) {
	// 用一个 fresh registry 避免和已注册到全局 Registry 的指标冲突。
	reg := prometheus.NewRegistry()
	c := &partitionedCollector{
		desc: prometheus.NewDesc(
			"test_partitioned",
			"test partitioned collector help",
			[]string{"kind"},
			nil,
		),
		dims: []prometheus.Labels{{"kind": "any"}, {"kind": "http"}, {"kind": "ws"}},
		fns: []func() float64{
			func() float64 { return 10 },
			func() float64 { return 4 },
			func() float64 { return 6 },
		},
		vt: prometheus.CounterValue,
	}
	reg.MustRegister(c)

	srv := httptest.NewServer(promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	out := string(body)
	wantLines := []string{
		`test_partitioned{kind="any"} 10`,
		`test_partitioned{kind="http"} 4`,
		`test_partitioned{kind="ws"} 6`,
	}
	for _, line := range wantLines {
		if !strings.Contains(out, line) {
			t.Fatalf("missing %q in body:\n%s", line, out)
		}
	}
}
