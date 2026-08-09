package httpkit

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

type RateLimitKey func(*gin.Context) string

func ClientIPKey(c *gin.Context) string { return c.ClientIP() }

func UserIDKeyFunc(c *gin.Context) string { return c.GetString(string(UserIDKey)) }

// RateLimit implements a fixed-window limiter backed by Redis.
// It fails closed for protected mutations when Redis is unavailable.
func RateLimit(rdb *redis.Client, prefix string, limit int, keyFn RateLimitKey) gin.HandlerFunc {
	return func(c *gin.Context) {
		identity := keyFn(c)
		if identity == "" {
			identity = "anonymous"
		}
		window := time.Now().UTC().Unix() / 60
		key := "rate:" + prefix + ":" + identity + ":" + strconv.FormatInt(window, 10)

		pipe := rdb.TxPipeline()
		count := pipe.Incr(c.Request.Context(), key)
		pipe.Expire(c.Request.Context(), key, 2*time.Minute)
		if _, err := pipe.Exec(c.Request.Context()); err != nil {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, envelope{Error: &errEnvelope{
				Code: "INTERNAL", Message: "request protection unavailable",
				RequestID: c.GetString(string(RequestIDKey)),
			}})
			return
		}
		remaining := int64(limit) - count.Val()
		if remaining < 0 {
			remaining = 0
		}
		c.Header("X-RateLimit-Limit", strconv.Itoa(limit))
		c.Header("X-RateLimit-Remaining", strconv.FormatInt(remaining, 10))
		if count.Val() > int64(limit) {
			c.Header("Retry-After", "60")
			c.AbortWithStatusJSON(http.StatusTooManyRequests, envelope{Error: &errEnvelope{
				Code: "RATE_LIMITED", Message: "too many requests",
				RequestID: c.GetString(string(RequestIDKey)),
			}})
			return
		}
		c.Next()
	}
}
