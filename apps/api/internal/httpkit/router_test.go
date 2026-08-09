package httpkit

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func TestCORSMiddlewareAllowsConfiguredOrigin(t *testing.T) {
	router := NewRouter(zap.NewNop(), "https://app.example.com")
	router.Engine.GET("/ok", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	req := httptest.NewRequest(http.MethodGet, "/ok", nil)
	req.Header.Set("Origin", "https://app.example.com")
	rec := httptest.NewRecorder()

	router.Engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
		t.Fatalf("allow origin = %q", got)
	}
}

func TestCORSMiddlewareRejectsUnexpectedOrigin(t *testing.T) {
	router := NewRouter(zap.NewNop(), "https://app.example.com")
	router.Engine.POST("/write", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	req := httptest.NewRequest(http.MethodPost, "/write", nil)
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()

	router.Engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}
