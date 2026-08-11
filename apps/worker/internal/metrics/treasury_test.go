package metrics_test

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/x-web3/worker/internal/metrics"
)

func TestTreasuryMetrics_RegisterAndScrape(t *testing.T) {
	reg := prometheus.NewRegistry()
	tm := metrics.NewTreasuryMetrics(reg)
	if tm == nil {
		t.Fatal("NewTreasuryMetrics returned nil")
	}

	// 多 address / asset / severity 维度计数
	tm.IncAlert("0xabc", "ETH", "warn")
	tm.IncAlert("0xabc", "ETH", "warn")
	tm.IncAlert("0xdef", "YD", "crit")

	expected := `
# HELP worker_treasury_alerts_total Total treasury monitor alerts emitted by severity tier.
# TYPE worker_treasury_alerts_total counter
worker_treasury_alerts_total{address="0xabc",asset="ETH",severity="warn"} 2
worker_treasury_alerts_total{address="0xdef",asset="YD",severity="crit"} 1
`
	if err := testutil.GatherAndCompare(reg, strings.NewReader(expected), "worker_treasury_alerts_total"); err != nil {
		t.Errorf("scrape mismatch: %v", err)
	}
}

func TestTreasuryMetrics_NilReceiver(t *testing.T) {
	var tm *metrics.TreasuryMetrics
	// nil receiver 不能 panic；用作 noop
	tm.IncAlert("0xabc", "ETH", "info")
}

func TestTreasuryMetrics_DuplicateRegisterPanics(t *testing.T) {
	// prometheus.MustRegister 在重复名 + 重复 registry 时 panic；
	// 这里验证 NewTreasuryMetrics 用两个独立 registry 都成功（不重复）。
	reg1 := prometheus.NewRegistry()
	reg2 := prometheus.NewRegistry()
	_ = metrics.NewTreasuryMetrics(reg1)
	_ = metrics.NewTreasuryMetrics(reg2)
}
