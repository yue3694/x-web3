// Package metrics — TreasuryMetrics：treasury 包告警 → Prometheus counter。
//
// 设计：
//   - 三个 label：address / asset (ETH|YD|...) / severity (info|warn|crit)；
//   - IncAlert 同步实现（counter.Add 是 atomic，sub-microsecond）；
//   - Register 把 counter 注册到 metrics 包共享的 prometheus.Registry（与
//     worker_chain_indexer_logs_decoded_total 等 series 同 namespace）。
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

// TreasuryMetrics 暴露 worker_treasury_alerts_total counter。
//
// counter 标签：address / asset / severity。
// 在测试 / 重复 NewTreasuryMetrics 调用时不会 panic；prometheus.NewCounterVec
// 的 panic 仅在「同 metric 名 + 同 registry」重复注册时发生，所以这里把
// prometheus.NewCounterVec 的 panic 透传给调用方即可。
type TreasuryMetrics struct {
	alerts *prometheus.CounterVec
}

// NewTreasuryMetrics 构造并把 counter 注册到 reg。
func NewTreasuryMetrics(reg prometheus.Registerer) *TreasuryMetrics {
	cv := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "worker_treasury_alerts_total",
		Help: "Total treasury monitor alerts emitted by severity tier.",
	}, []string{"address", "asset", "severity"})
	reg.MustRegister(cv)
	return &TreasuryMetrics{alerts: cv}
}

// IncAlert 实现 treasury.Metrics 接口。
func (t *TreasuryMetrics) IncAlert(address, asset, severity string) {
	if t == nil || t.alerts == nil {
		return
	}
	t.alerts.WithLabelValues(address, asset, severity).Inc()
}

// Alerts 返回底层 counter（测试用：prometheus testutil 读取）。
func (t *TreasuryMetrics) Alerts() *prometheus.CounterVec { return t.alerts }
