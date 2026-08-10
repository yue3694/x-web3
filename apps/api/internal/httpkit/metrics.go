// Package httpkit: Prometheus 指标注册表 + Gin 中间件。
//
// 设计：
//   - 单一 DefaultRegistry，业务方用 package-level 计数器直读；
//   - 默认 collector（go runtime + process）由 promauto 自动注册；
//   - /metrics 由 promhttp.Handler() 提供，Gin 通过 adapter 挂载；
//     也可独立 http.Server 暴露（保持 /metrics 不经业务路由，便于抓取）。
//
// 注意：/metrics 是公开抓取端点（按 Prometheus 惯例），本仓库默认 0.0.0.0 绑定。
// 生产请用防火墙 / sidecar 把端口暴露给内部抓取网段，不要直接对外网开放。
package httpkit

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// DefaultRegistry 是该服务暴露指标的注册表。业务 / 中间件都向它注册。
//
// 选 default 而非 new registry 的原因：与 promhttp.Handler() 配合时更简单，
// 同时避免重复注册 go/process collector。
var DefaultRegistry = prometheus.NewRegistry()

// httpRequestsTotal 按 method × path × status 计数。
//
// 注意 path 用 FullPath()（路由模板而非真实路径）以避免 cardinality 爆炸。
var httpRequestsTotal = promauto.With(DefaultRegistry).NewCounterVec(
	prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "Total HTTP requests handled, partitioned by method, route path, and status code.",
	},
	[]string{"method", "path", "status"},
)

// httpRequestDurationSeconds 直方图，按 method × path 分桶。
//
// Buckets 覆盖 5ms — 10s，APIs 的典型响应时间分布。
var httpRequestDurationSeconds = promauto.With(DefaultRegistry).NewHistogramVec(
	prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "HTTP request latency in seconds, partitioned by method and route path.",
		Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
	},
	[]string{"method", "path"},
)

// 业务计数器（按领域拆分；调用方在 handler 完成时 Inc）。

// OrdersCreatedTotal 订单创建成功数（含幂等命中返回原 intent 的情况）。
var OrdersCreatedTotal = promauto.With(DefaultRegistry).NewCounter(
	prometheus.CounterOpts{
		Name: "orders_created_total",
		Help: "Total purchase intents successfully created (includes idempotent replays).",
	},
)

// CertificatesConfirmedTotal 证书上链 confirmed 终态计数。
//
// 注：API 侧只发起 mint job，confirmed 是 worker 上链回调；当前实现
// 通过 admin/handler 的 reconcile endpoint 触发统计回调，保留打点为对接入口。
var CertificatesConfirmedTotal = promauto.With(DefaultRegistry).NewCounter(
	prometheus.CounterOpts{
		Name: "certificates_confirmed_total",
		Help: "Total certificate jobs reaching the on-chain confirmed terminal state.",
	},
)

// CertificatesDeadTotal 证书 mint 超过 max_attempts 后进入 dead 状态计数。
var CertificatesDeadTotal = promauto.With(DefaultRegistry).NewCounter(
	prometheus.CounterOpts{
		Name: "certificates_dead_total",
		Help: "Total certificate jobs exhausted retries and entered the dead state.",
	},
)

// AuditWritesTotal 审计写入次数（含 success + error）。失败由 caller 自加 result label。
var AuditWritesTotal = promauto.With(DefaultRegistry).NewCounterVec(
	prometheus.CounterOpts{
		Name: "audit_writes_total",
		Help: "Total audit log insert attempts, partitioned by outcome.",
	},
	[]string{"result"}, // success | error
)

// promInit 注册默认 collectors（go runtime + process）。init() 保证一次。
func init() {
	DefaultRegistry.MustRegister(
		prometheus.NewGoCollector(),
		prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}),
	)
}

// MetricsMiddleware 给 gin 安装 HTTP 请求计数 + 时延直方图中间件。
//
// path 用 FullPath()（路由模板，如 "/api/v1/me"），避免把动态 ID 计入 label
// 造成 cardinality 爆炸；命中 404（无路由）时 path 留 "" 让所有 404 共享一个 bucket。
func MetricsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		path := c.FullPath()
		if path == "" {
			path = "unmatched"
		}
		method := c.Request.Method
		status := strconv.Itoa(c.Writer.Status())

		httpRequestsTotal.WithLabelValues(method, path, status).Inc()
		httpRequestDurationSeconds.WithLabelValues(method, path).Observe(time.Since(start).Seconds())
	}
}

// RecordAuditResult 审计写入完成时由 caller 调用，统一打点。
//
// result 取值："success" | "error"。
func RecordAuditResult(result string) {
	AuditWritesTotal.WithLabelValues(result).Inc()
}