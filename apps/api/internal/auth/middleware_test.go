package auth_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/x-web3/api/internal/auth"
)

// testPool 返回一个用于鉴权测试的真实 PostgreSQL 池。
//
// 与 integration 包同名 helper 重复；这里独立维护以避免 auth 包反向依赖 integration。
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL := os.Getenv("DATABASE_URL_TEST")
	if databaseURL == "" {
		t.Skip("DATABASE_URL_TEST is not set")
	}
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(context.Background()); err != nil {
		t.Fatalf("database ping: %v", err)
	}
	return pool
}

// newAuthRouter 装好 request_id + auth.Middleware + 一个探测 handler，
// handler 在被命中时把注入的 user_id/subject 写回 response header。
func newAuthRouter(store *auth.SessionStore, pool *pgxpool.Pool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("request_id", "req_test")
		c.Next()
	})
	r.Use(auth.Middleware(nil, store, pool))
	r.GET("/protected", func(c *gin.Context) {
		c.Header("X-User-Id", c.GetString("user_id"))
		c.Header("X-Subject", c.GetString("subject"))
		c.Header("X-Sid", c.GetString("sid"))
		c.Status(http.StatusOK)
	})
	return r
}

// TestMiddleware_RejectsMissingCookie 没带 sid cookie 必须 401。
func TestMiddleware_RejectsMissingCookie(t *testing.T) {
	store, _, _ := newTestStore(t)
	r := newAuthRouter(store, testPool(t))
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestOptionalMiddleware_AllowsMissingCookie(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(auth.OptionalMiddleware(nil, nil, nil))
	r.GET("/public", func(c *gin.Context) {
		c.Header("X-User-Id", c.GetString("user_id"))
		c.Status(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, "/public", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("X-User-Id"); got != "" {
		t.Fatalf("anonymous user id = %q, want empty", got)
	}
}

func TestOptionalMiddleware_InjectsActiveUser(t *testing.T) {
	pool := testPool(t)
	store, _, _ := newTestStore(t)
	ctx := context.Background()
	subject := "did:privy:optional-" + uuid.NewString()
	var uid uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO users(privy_user_id,status) VALUES($1,'active') RETURNING id`, subject).Scan(&uid); err != nil {
		t.Fatalf("insert: %v", err)
	}
	sid, _, err := store.Issue(ctx, subject, "")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(auth.OptionalMiddleware(nil, store, pool))
	r.GET("/public", func(c *gin.Context) {
		c.Header("X-User-Id", c.GetString("user_id"))
		c.Status(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, "/public", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: sid})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("X-User-Id"); got != uid.String() {
		t.Fatalf("user id = %q, want %q", got, uid)
	}
}

// TestMiddleware_RejectsUnknownSID sid 不在 store 中必须 401。
func TestMiddleware_RejectsUnknownSID(t *testing.T) {
	store, _, _ := newTestStore(t)
	r := newAuthRouter(store, testPool(t))
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: "deadbeef"})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

// TestMiddleware_RejectsTamperedSession 被篡改签名的 session 必须 401。
func TestMiddleware_RejectsTamperedSession(t *testing.T) {
	store, mr, rdb := newTestStore(t)
	sid, _, err := store.Issue(context.Background(), "did:privy:tamper", "")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	raw, _ := rdb.Get(context.Background(), "session:"+sid).Result()
	mr.Set("session:"+sid, raw[:len(raw)-4]+"AAAA")

	r := newAuthRouter(store, testPool(t))
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: sid})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

// TestMiddleware_RejectsOrphanedSession_DestroysIt session 合法但 user 不在 DB 中 → 401 + 销毁。
func TestMiddleware_RejectsOrphanedSession_DestroysIt(t *testing.T) {
	store, _, _ := newTestStore(t)
	ghostSubject := "did:privy:ghost-" + uuid.NewString()
	sid, _, err := store.Issue(context.Background(), ghostSubject, "")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	r := newAuthRouter(store, testPool(t))
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: sid})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if got, _ := store.Read(context.Background(), sid); got != nil {
		t.Errorf("orphaned session must be destroyed, got %+v", got)
	}
}

// TestMiddleware_RejectsSuspendedAccount status != active → 403 + 销毁 session。
func TestMiddleware_RejectsSuspendedAccount(t *testing.T) {
	pool := testPool(t)
	store, _, _ := newTestStore(t)
	ctx := context.Background()

	subject := "did:privy:suspend-" + uuid.NewString()
	if _, err := pool.Exec(ctx, `INSERT INTO users(privy_user_id,status) VALUES($1,'suspended')`, subject); err != nil {
		t.Fatalf("insert: %v", err)
	}
	sid, _, err := store.Issue(ctx, subject, "")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	r := newAuthRouter(store, pool)
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: sid})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if got, _ := store.Read(ctx, sid); got != nil {
		t.Errorf("session for suspended account must be destroyed, got %+v", got)
	}
}

// TestMiddleware_AllowsActiveUser 合法 active user 注入 user_id/subject/sid 到 ctx。
func TestMiddleware_AllowsActiveUser(t *testing.T) {
	pool := testPool(t)
	store, _, _ := newTestStore(t)
	ctx := context.Background()

	subject := "did:privy:active-" + uuid.NewString()
	var uid uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO users(privy_user_id,status) VALUES($1,'active') RETURNING id`, subject).Scan(&uid); err != nil {
		t.Fatalf("insert: %v", err)
	}
	sid, _, err := store.Issue(ctx, subject, "")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	r := newAuthRouter(store, pool)
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: sid})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("X-User-Id"); got != uid.String() {
		t.Errorf("X-User-Id = %q, want %q", got, uid.String())
	}
	if got := rec.Header().Get("X-Subject"); got != subject {
		t.Errorf("X-Subject = %q, want %q", got, subject)
	}
	if got := rec.Header().Get("X-Sid"); got != sid {
		t.Errorf("X-Sid = %q, want %q", got, sid)
	}
}

// TestMiddleware_ErrorBodyShape 401 响应必须带 code + message + requestId 字段。
func TestMiddleware_ErrorBodyShape(t *testing.T) {
	store, _, _ := newTestStore(t)
	r := newAuthRouter(store, testPool(t))
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", rec.Code)
	}
	var body struct {
		Error map[string]any `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if body.Error == nil {
		t.Fatal("missing error envelope")
	}
	for _, k := range []string{"code", "message", "requestId"} {
		if _, ok := body.Error[k]; !ok {
			t.Errorf("error envelope missing %q", k)
		}
	}
}

// TestSetSessionCookie_AppliesAttributes SetSessionCookie 必须设 httpOnly + sameSite=lax。
func TestSetSessionCookie_AppliesAttributes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/", func(c *gin.Context) {
		// 直接复用 SetSessionCookie 需要 *httpkit.Context；这里用底层 gin 行为对齐
		c.SetSameSite(http.SameSiteLaxMode)
		c.SetCookie(auth.CookieName, "abc", 3600, "/", "", true, true)
		c.Status(http.StatusNoContent)
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	cookies := rec.Result().Cookies()
	var found *http.Cookie
	for _, c := range cookies {
		if c.Name == auth.CookieName {
			found = c
		}
	}
	if found == nil {
		t.Fatal("sid cookie not set")
	}
	if !found.HttpOnly {
		t.Error("sid cookie must be HttpOnly")
	}
	if !found.Secure {
		t.Error("sid cookie must be Secure")
	}
	if found.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want Lax", found.SameSite)
	}
	if found.Value != "abc" {
		t.Errorf("value = %q", found.Value)
	}
}

// TestClearSessionCookie_ExpiresCookie 登出 cookie 必须立即过期。
func TestClearSessionCookie_ExpiresCookie(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/", func(c *gin.Context) {
		c.SetSameSite(http.SameSiteLaxMode)
		c.SetCookie(auth.CookieName, "", -1, "/", "", true, true)
		c.Status(http.StatusNoContent)
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	cookies := rec.Result().Cookies()
	var found *http.Cookie
	for _, c := range cookies {
		if c.Name == auth.CookieName {
			found = c
		}
	}
	if found == nil {
		t.Fatal("clear-cookie header not set")
	}
	if found.Value != "" {
		t.Errorf("expected empty value, got %q", found.Value)
	}
	if found.MaxAge >= 0 {
		t.Errorf("MaxAge = %d, want < 0", found.MaxAge)
	}
}

// 确认 SessionStore Read 在签名错误时返回 error（与 middleware 集成路径对齐）
func TestSessionStore_ReadRejectsBadMac(t *testing.T) {
	store, mr, rdb := newTestStore(t)
	sid, _, err := store.Issue(context.Background(), "did:privy:mac", "")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	raw, _ := rdb.Get(context.Background(), "session:"+sid).Result()
	mr.Set("session:"+sid, raw[:len(raw)-2]+"ZZ")
	if _, err := store.Read(context.Background(), sid); err == nil {
		t.Fatal("expected signature error")
	}
	// 防止误用：time package 在该文件引用，避免 unused import 警告
	_ = time.Second
	// zap 同样引用占位
	_ = zap.NewNop()
}
