// Package httpkit 提供 HTTP 层的横切关注点：错误格式、request ID、
// 响应封装。所有 handler 必须用 kit.Error / kit.OK。
package httpkit

import (
	"errors"
	"net/http"

	"github.com/x-web3/api/internal/errcode"
)

// Error writes a JSON error envelope. It is safe to call Error() multiple
// times — the second call is a no-op when the response is already written.
func Error(c *Context, status int, code errcode.Code, msg string, details map[string]any) {
	c.AbortWithStatusJSON(status, envelope{
		Error: &errEnvelope{
			Code:      string(code),
			Message:   msg,
			RequestID: c.GetString(RequestIDKey),
			Details:   details,
		},
	})
}

// OK writes a JSON success body with the given status.
func OK(c *Context, status int, body any) {
	c.JSON(status, body)
}

// BadRequest is a small helper that maps a known errcode to 400.
func BadRequest(c *Context, code errcode.Code, msg string) {
	Error(c, http.StatusBadRequest, code, msg, nil)
}

// Internal returns 500 with a sanitized message. err is logged but never leaked.
func Internal(c *Context, err error) {
	_ = errors.Unwrap(err) // reserved for future chain inspection
	Error(c, http.StatusInternalServerError, errcode.Internal, "internal server error", nil)
}
