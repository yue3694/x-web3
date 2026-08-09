package httpkit

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

func TestRateLimitRejectsRequestsOverLimit(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	router := NewRouter(zap.NewNop(), "http://localhost:5173")
	router.Engine.GET("/limited", RateLimit(client, "test", 2, ClientIPKey), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	for i, expected := range []int{http.StatusNoContent, http.StatusNoContent, http.StatusTooManyRequests} {
		req := httptest.NewRequest(http.MethodGet, "/limited", nil)
		req.RemoteAddr = "192.0.2.1:1234"
		rec := httptest.NewRecorder()
		router.Engine.ServeHTTP(rec, req)
		if rec.Code != expected {
			t.Fatalf("request %d status = %d, want %d", i+1, rec.Code, expected)
		}
	}
}
