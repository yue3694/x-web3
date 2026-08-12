// Package httpkit 提供轻量级 Context 抽象。底层用 Gin（最小依赖）。
package httpkit

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// ContextKey 用于 gin.Context 内部 KV。
type ContextKey string

const (
	RequestIDKey ContextKey = "request_id"
	UserIDKey    ContextKey = "user_id"
	SubjectKey   ContextKey = "subject"
)

// Context 是 handler 接收的参数类型；当前是对 gin.Context 的薄包装。
type Context struct {
	*gin.Context
}

func wrap(c *gin.Context) *Context { return &Context{Context: c} }

// HandlerFunc 是 handler 签名。
type HandlerFunc func(*Context)

// Wrap 把 httpkit.HandlerFunc 转成 gin.HandlerFunc。
func Wrap(h HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) { h(wrap(c)) }
}

func (c *Context) JSON(status int, body any) { c.Context.JSON(status, body) }
func (c *Context) AbortWithStatusJSON(status int, body any) {
	c.Context.AbortWithStatusJSON(status, body)
}
func (c *Context) GetString(k ContextKey) string {
	return c.Context.GetString(string(k))
}

// RequestID 返回注入的 request id。
func (c *Context) RequestID() string { return c.GetString(RequestIDKey) }

// UserID 返回 middleware 注入的 user id。
func (c *Context) UserID() string  { return c.GetString(UserIDKey) }
func (c *Context) Subject() string { return c.GetString(SubjectKey) }

// MustJSON 解析 JSON body 到 dst；解析失败时直接 400 返回。
func (c *Context) MustJSON(dst any) bool {
	if err := c.Context.ShouldBindJSON(dst); err != nil {
		Error(c, http.StatusBadRequest, "BAD_REQUEST", err.Error(), nil)
		return false
	}
	return true
}
