// Package treasury — metrics 适配：把 *metrics.TreasuryMetrics 接到 Monitor.SetMetrics。
//
// 设计动机：
//   - treasury 包不依赖 prometheus/client_golang（保持包纯净）；
//   - metrics 包实现 IncAlert 接口，主进程装配时把两个包拼起来。
package treasury

import "github.com/x-web3/worker/internal/metrics"

// TreasuryMetricsAdapter 把 *metrics.TreasuryMetrics 适配为 treasury.Metrics。
//
// 之所以不是直接用 *metrics.TreasuryMetrics（避免 treasury → metrics 反向依赖）；
// 调用方 SetMetrics(adapter) 一次即可。
type TreasuryMetricsAdapter struct {
	M *metrics.TreasuryMetrics
}

// IncAlert 实现 treasury.Metrics。
func (a TreasuryMetricsAdapter) IncAlert(address, asset, severity string) {
	if a.M == nil {
		return
	}
	a.M.IncAlert(address, asset, severity)
}
