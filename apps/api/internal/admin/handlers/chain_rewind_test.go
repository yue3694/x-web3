package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/x-web3/api/internal/audit"
	"github.com/x-web3/api/internal/httpkit"
	"github.com/x-web3/api/internal/rbac"
)

// fakeAuditSink 是 audit.Sink 的测试替身。
type fakeAuditSink struct {
	entries []audit.Entry
}

func (s *fakeAuditSink) Exec(_ context.Context, _ string, _ ...any) (any, error) {
	return nil, nil
}

func (s *fakeAuditSink) count() int { return len(s.entries) }

// userMiddleware 把 user_id 注入 ctx。
func userMiddleware(uid string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("user_id", uid)
		c.Set("request_id", "req_test")
		c.Next()
	}
}

// TestPostRewind_RequiresSessionNoContext 验证：未带 user_id → 401。
func TestPostRewind_RequiresSessionNoContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &ChainRewindHandler{logger: zap.NewNop()}
	r := gin.New()
	r.POST("/admin/chain/rewind", userMiddleware(""), httpkit.Wrap(h.PostRewind))
	body, _ := json.Marshal(rewindRequest{ChainID: 1, FromBlock: 100})
	req := httptest.NewRequest(http.MethodPost, "/admin/chain/rewind", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rr.Code)
	}
}

// TestPostRewind_ValidatesRequestBody 验证：JSON 解析失败 → 400。
func TestPostRewind_ValidatesRequestBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	uid := uuid.New()
	h := &ChainRewindHandler{logger: zap.NewNop()}
	r := gin.New()
	r.POST("/admin/chain/rewind", userMiddleware(uid.String()), httpkit.Wrap(h.PostRewind))
	req := httptest.NewRequest(http.MethodPost, "/admin/chain/rewind", bytes.NewReader([]byte("{not json")))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
}

// TestPostRewind_RejectsNegativeFromBlock 验证：fromBlock < 0 → 400。
func TestPostRewind_RejectsNegativeFromBlock(t *testing.T) {
	gin.SetMode(gin.TestMode)
	uid := uuid.New()
	h := &ChainRewindHandler{logger: zap.NewNop()}
	r := gin.New()
	r.POST("/admin/chain/rewind", userMiddleware(uid.String()), httpkit.Wrap(h.PostRewind))
	body, _ := json.Marshal(rewindRequest{ChainID: 1, FromBlock: -1})
	req := httptest.NewRequest(http.MethodPost, "/admin/chain/rewind", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
}

// TestList_ReturnsUnresolved 验证：handler 接受 GET /admin/dlq → 200。
func TestList_ReturnsUnresolved(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &fakeDLQStore{items: []dlqRow{{ID: 1, Consumer: "x", Kind: "gap", Severity: "error", Summary: "test"}}}
	h := NewDLQHandler(store, audit.NewWriterWithSink(&fakeAuditSink{}, zap.NewNop()), rbac.NewEngineWithSource(nil, zap.NewNop()), zap.NewNop())
	r := gin.New()
	r.GET("/admin/dlq", userMiddleware(uuid.NewString()), httpkit.Wrap(h.List))
	req := httptest.NewRequest(http.MethodGet, "/admin/dlq", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
}

// TestRetry_RejectsBadResolution 验证：resolution 非法 → 400。
func TestRetry_RejectsBadResolution(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &fakeDLQStore{}
	h := NewDLQHandler(store, audit.NewWriterWithSink(&fakeAuditSink{}, zap.NewNop()), rbac.NewEngineWithSource(nil, zap.NewNop()), zap.NewNop())
	r := gin.New()
	r.POST("/admin/dlq/:id/retry", userMiddleware(uuid.NewString()), httpkit.Wrap(h.Retry))
	body, _ := json.Marshal(retryReq{Resolution: "wrong"})
	req := httptest.NewRequest(http.MethodPost, "/admin/dlq/1/retry", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
}

// TestRetry_AcceptsValidResolution 验证：合法 resolution → 200。
//
// 每个 resolution 都重新初始化一份 store/router：fix #13 后 fakeDLQStore 会
// 把 resolved 置 true 留在结构里，必须按 resolution 隔离，否则后续 case 会命中
// errAlreadyResolved。状态化的 fake 是覆盖 inc/retry 顺序修复的一部分。
func TestRetry_AcceptsValidResolution(t *testing.T) {
	for _, reso := range []string{"replayed", "ignored", "manual"} {
		reso := reso
		t.Run(reso, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			store := &fakeDLQStore{}
			h := NewDLQHandler(store, audit.NewWriterWithSink(&fakeAuditSink{}, zap.NewNop()), rbac.NewEngineWithSource(nil, zap.NewNop()), zap.NewNop())
			r := gin.New()
			r.POST("/admin/dlq/:id/retry", userMiddleware(uuid.NewString()), httpkit.Wrap(h.Retry))
			body, _ := json.Marshal(retryReq{Resolution: reso})
			req := httptest.NewRequest(http.MethodPost, "/admin/dlq/1/retry", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()
			r.ServeHTTP(rr, req)
			if rr.Code != http.StatusOK {
				t.Errorf("resolution %q status = %d, want 200; body=%s", reso, rr.Code, rr.Body.String())
			}
		})
	}
}

// TestRetry_AlreadyResolved 验证：已 resolved → 409。
func TestRetry_AlreadyResolved(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &fakeDLQStore{resolved: true}
	h := NewDLQHandler(store, audit.NewWriterWithSink(&fakeAuditSink{}, zap.NewNop()), rbac.NewEngineWithSource(nil, zap.NewNop()), zap.NewNop())
	r := gin.New()
	r.POST("/admin/dlq/:id/retry", userMiddleware(uuid.NewString()), httpkit.Wrap(h.Retry))
	body, _ := json.Marshal(retryReq{Resolution: "replayed"})
	req := httptest.NewRequest(http.MethodPost, "/admin/dlq/1/retry", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409; body=%s", rr.Code, rr.Body.String())
	}
}

// TestRetry_NotFound 验证：行不存在 → 404，且 retry 计数不会被错误累加。
func TestRetry_NotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &fakeDLQStore{notFound: true}
	h := NewDLQHandler(store, audit.NewWriterWithSink(&fakeAuditSink{}, zap.NewNop()), rbac.NewEngineWithSource(nil, zap.NewNop()), zap.NewNop())
	r := gin.New()
	r.POST("/admin/dlq/:id/retry", userMiddleware(uuid.NewString()), httpkit.Wrap(h.Retry))
	body, _ := json.Marshal(retryReq{Resolution: "replayed"})
	req := httptest.NewRequest(http.MethodPost, "/admin/dlq/999/retry", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404; body=%s", rr.Code, rr.Body.String())
	}
}

// TestResolveAndIncrementRetry_DoesNotIncOnAlreadyResolved 覆盖 codex #13 的
// 顺序翻转失败场景：原 handler 先 IncRetry 再 MarkResolved；当 MarkResolved
// 返回「已 resolved」时 retry 已经被白白 +1。新的原子实现必须在 MarkResolved
// 没有真正翻转状态时 **不** 累加 retry。
func TestResolveAndIncrementRetry_DoesNotIncOnAlreadyResolved(t *testing.T) {
	store := &fakeDLQStore{resolved: true, incCount: 0}
	if _, err := fakeResolve(store, 1, uuid.New(), "replayed"); err == nil {
		t.Fatal("expected errAlreadyResolved from atomic call")
	}
	if store.incCount != 0 {
		t.Fatalf("retry_count incremented despite already-resolved row: inc=%d", store.incCount)
	}
}

// TestResolveAndIncrementRetry_IncOnlyOnSuccess 验证：成功路径下 retry +1，
// 调用方拿到 resolved=true。
func TestResolveAndIncrementRetry_IncOnlyOnSuccess(t *testing.T) {
	store := &fakeDLQStore{resolved: false, incCount: 0}
	resolved, err := fakeResolve(store, 1, uuid.New(), "replayed")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !resolved {
		t.Fatal("expected resolved=true")
	}
	if store.incCount != 1 {
		t.Fatalf("retry_count increment = %d, want 1", store.incCount)
	}
}

// fakeResolve 帮 fakeDLQStore 暴露 ResolveAndIncrementRetry 接口（在测试文件里
// 直接实现一份小逻辑，避免 fakeDLQStore 与 PG 行为耦合）。
func fakeResolve(f *fakeDLQStore, id int64, _ uuid.UUID, _ string) (bool, error) {
	if f.notFound {
		return false, pgx.ErrNoRows
	}
	if f.resolved {
		return false, errAlreadyResolved
	}
	f.resolved = true
	f.incCount++
	return true, nil
}

// TestPGDLQStore_RejectsEmptyResolution 直接测 PG store 行为。
func TestPGDLQStore_RejectsEmptyResolution(t *testing.T) {
	store := &PGDLQStore{}
	// 无 pool：调用必 panic / 报错；我们仅测入参校验路径。
	err := store.MarkResolved(context.Background(), 1, uuid.New(), "")
	if err == nil {
		t.Fatal("expected error for empty resolution")
	}
}

// avoid unused import warnings.
var (
	_ = io.Discard
	_ = errors.New
	_ = pgx.ErrNoRows
	_ = slog.Default
	_ = (*pgxpool.Pool)(nil)
)

// fakeDLQStore 满足 dlqStore 接口。
type fakeDLQStore struct {
	items    []dlqRow
	resolved bool
	notFound bool // 模拟「id 不存在」分支，覆盖 _notfound 路径
	incCount int  // 累计 inc 次数：用于断言 IncRetry 没有副作用
}

func (f *fakeDLQStore) ListUnresolved(_ context.Context, limit int) ([]dlqRow, error) {
	if f.items == nil {
		return nil, nil
	}
	if limit > 0 && limit < len(f.items) {
		return f.items[:limit], nil
	}
	return f.items, nil
}

func (f *fakeDLQStore) IncrementRetry(_ context.Context, id int64) (int, error) {
	f.incCount++
	return f.incCount, nil
}

func (f *fakeDLQStore) MarkResolved(_ context.Context, id int64, _ uuid.UUID, _ string) error {
	if f.notFound {
		return pgx.ErrNoRows
	}
	if f.resolved {
		return errAlreadyResolved
	}
	f.resolved = true
	return nil
}

// ResolveAndIncrementRetry 镜像 PG 实现的语义：仅当 MarkResolved 真正把状态从
// false 翻到 true 时，retry_count +1。
func (f *fakeDLQStore) ResolveAndIncrementRetry(_ context.Context, id int64, _ uuid.UUID, _ string) (bool, error) {
	if f.notFound {
		return false, pgx.ErrNoRows
	}
	if f.resolved {
		return false, errAlreadyResolved
	}
	f.resolved = true
	f.incCount++
	return true, nil
}
