package httpkit

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// envelope 是统一响应包装。成功时省略 Error 字段。
type envelope struct {
	Error *errEnvelope `json:"error,omitempty"`
}

type errEnvelope struct {
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	RequestID string         `json:"requestId"`
	Details   map[string]any `json:"details,omitempty"`
}

// Router 封装 gin.Engine，统一安装横切中间件。
type Router struct {
	Engine *gin.Engine
	Logger *zap.Logger
}

// NewRouter 创建带 request-id + recovery + access log 的路由。
func NewRouter(logger *zap.Logger, allowedOrigin string) *Router {
	gin.SetMode(gin.ReleaseMode)
	e := gin.New()
	e.Use(requestIDMiddleware())
	e.Use(corsMiddleware(allowedOrigin))
	e.Use(accessLogMiddleware(logger))
	e.Use(recoveryMiddleware(logger))
	return &Router{Engine: e, Logger: logger}
}

// corsMiddleware 只允许配置的 Web origin 携带平台 session。
// 没有 Origin 的同源服务调用和 CLI 请求继续放行。
func corsMiddleware(allowedOrigin string) gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin != "" && origin != allowedOrigin {
			c.AbortWithStatusJSON(http.StatusForbidden, envelope{Error: &errEnvelope{
				Code: "FORBIDDEN", Message: "origin not allowed",
				RequestID: c.GetString(string(RequestIDKey)),
			}})
			return
		}
		if origin == allowedOrigin {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Access-Control-Allow-Credentials", "true")
			c.Header("Access-Control-Allow-Headers", "Content-Type, X-Request-ID")
			c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			c.Header("Vary", "Origin")
		}
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

// requestIDMiddleware 注入 X-Request-ID（取 header 或生成 UUID）。
// 注入到 gin.Context 与响应 header，便于客户端 / 链路追踪。
func requestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		rid := c.GetHeader("X-Request-ID")
		if rid == "" {
			rid = uuid.NewString()
		}
		c.Set(string(RequestIDKey), rid)
		c.Writer.Header().Set("X-Request-ID", rid)
		c.Next()
	}
}

// accessLogMiddleware 记录每个请求的访问日志（结构化）。
func accessLogMiddleware(logger *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		logger.Info("http_request",
			zap.String("request_id", c.GetString(string(RequestIDKey))),
			zap.String("method", c.Request.Method),
			zap.String("path", c.Request.URL.Path),
			zap.Int("status", c.Writer.Status()),
			zap.Duration("latency", time.Since(start)),
			zap.String("remote", c.ClientIP()),
		)
	}
}

// recoveryMiddleware panic 后返回 500 并记录堆栈。
func recoveryMiddleware(logger *zap.Logger) gin.HandlerFunc {
	return gin.CustomRecoveryWithWriter(nil, func(c *gin.Context, err any) {
		logger.Error("panic_recovered",
			zap.Any("err", err),
			zap.String("path", c.Request.URL.Path),
		)
		c.AbortWithStatusJSON(500, envelope{
			Error: &errEnvelope{
				Code:      "INTERNAL",
				Message:   "internal server error",
				RequestID: c.GetString(string(RequestIDKey)),
			},
		})
	})
}
