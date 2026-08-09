package auth

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/x-web3/api/internal/errcode"
	"github.com/x-web3/api/internal/httpkit"
)

// CookieName 是 sid 在浏览器侧的 cookie 名。
const CookieName = "sid"

// Middleware 校验 cookie sid，并把 subject 解析成 user_id 注入到 ctx。
// 失败返回 401，不写 body 之外的细节。
func Middleware(verifier Verifier, store *SessionStore, pool *pgxpool.Pool) gin.HandlerFunc {
	_ = verifier // reserved for future token-bound session refresh
	return func(c *gin.Context) {
		sid, err := c.Cookie(CookieName)
		if err != nil || sid == "" {
			respond401(c, "missing session")
			return
		}
		data, err := store.Read(c.Request.Context(), sid)
		if err != nil {
			respond401(c, "session invalid")
			return
		}
		if data == nil {
			respond401(c, "session expired")
			return
		}
		// Resolve subject → user_id (lazy DB lookup; 结果放 ctx)。
		const q = `SELECT id, status FROM users WHERE privy_user_id = $1`
		var uid uuid.UUID
		var status string
		err = pool.QueryRow(c.Request.Context(), q, data.Subject).Scan(&uid, &status)
		if errors.Is(err, pgx.ErrNoRows) {
			_ = store.Destroy(c.Request.Context(), sid)
			respond401(c, "user missing")
			return
		}
		if err != nil {
			respond401(c, "user lookup failed")
			return
		}
		if status != "active" {
			_ = store.Destroy(c.Request.Context(), sid)
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": gin.H{
				"code": string(errcode.Forbidden), "message": "account unavailable",
				"requestId": c.GetString("request_id"),
			}})
			return
		}
		c.Set("user_id", uid.String())
		c.Set("subject", data.Subject)
		c.Set("sid", sid)
		c.Next()
	}
}

func respond401(c *gin.Context, msg string) {
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
		"error": gin.H{
			"code":      string(errcode.SessionExpired),
			"message":   msg,
			"requestId": c.GetString("request_id"),
		},
	})
}

// SetSessionCookie 在 handler 中调用，设置 httpOnly cookie。
func SetSessionCookie(c *httpkit.Context, sid string, ttl int, secure bool) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(CookieName, sid, ttl, "/", "", secure, true)
}

// ClearSessionCookie 登出时清 cookie。
func ClearSessionCookie(c *httpkit.Context, secure bool) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(CookieName, "", -1, "/", "", secure, true)
}
