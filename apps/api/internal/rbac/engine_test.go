package rbac_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/x-web3/api/internal/rbac"
	"github.com/x-web3/api/internal/user"
)

// stubSource 是 rbac.Source 的测试替身：返回预设的 role/permission，
// 并记录调用次数，方便断言缓存命中行为。
type stubSource struct {
	perms map[uuid.UUID][]string
	roles map[uuid.UUID][]string
	calls atomic.Int32
	err   error
}

func (s *stubSource) ListPermissions(_ context.Context, id uuid.UUID) ([]string, error) {
	s.calls.Add(1)
	if s.err != nil {
		return nil, s.err
	}
	return s.perms[id], nil
}

func (s *stubSource) ListRoleCodes(_ context.Context, id uuid.UUID) ([]string, error) {
	s.calls.Add(1)
	if s.err != nil {
		return nil, s.err
	}
	return s.roles[id], nil
}

// fixture 构造常用角色 → permission 映射（与 design 文档一致）。
func fixture() *stubSource {
	s := &stubSource{
		perms: make(map[uuid.UUID][]string),
		roles: make(map[uuid.UUID][]string),
	}
	student := uuid.New()
	teacher := uuid.New()
	admin := uuid.New()
	nobody := uuid.New()

	s.roles[student] = []string{user.RoleStudent}
	s.perms[student] = []string{
		user.PermOrderCreate,
		user.PermLessonProgress,
		user.PermCertificateRead,
	}

	s.roles[teacher] = []string{user.RoleTeacher}
	s.perms[teacher] = []string{
		user.PermCourseCreate,
		user.PermCourseEdit,
		user.PermMediaUpload,
		user.PermOrderCreate,
		user.PermLessonProgress,
		user.PermCertificateRead,
	}

	s.roles[admin] = []string{user.RoleSuperAdmin}
	// 即使 admin 没有具体 permission，Engine 的 super_admin 通配逻辑应放行
	s.perms[admin] = nil

	s.roles[nobody] = nil
	s.perms[nobody] = nil

	return s
}

// lookup 通过角色拿到 UUID（fixture 内部约定的查找方式）。
func userIDByRole(s *stubSource, role string) uuid.UUID {
	for id, roles := range s.roles {
		for _, r := range roles {
			if r == role {
				return id
			}
		}
	}
	return uuid.Nil
}

// TestRBACMatrix_StudentHasBaselineOnly 验证 student 默认权限与 design 文档一致。
func TestRBACMatrix_StudentHasBaselineOnly(t *testing.T) {
	s := fixture()
	e := rbac.NewEngineWithSource(s, zap.NewNop())
	ctx := context.Background()
	student := userIDByRole(s, user.RoleStudent)

	ok, err := e.Has(ctx, student, user.PermOrderCreate)
	if err != nil || !ok {
		t.Fatalf("student should have ORDER_CREATE: ok=%v err=%v", ok, err)
	}
	ok, err = e.Has(ctx, student, user.PermCourseCreate)
	if err != nil {
		t.Fatalf("Has: %v", err)
	}
	if ok {
		t.Errorf("student must NOT have COURSE_CREATE")
	}
	ok, _ = e.Has(ctx, student, user.PermCourseApprove)
	if ok {
		t.Errorf("student must NOT have COURSE_APPROVE")
	}
	ok, _ = e.Has(ctx, student, user.PermSystemAdmin)
	if ok {
		t.Errorf("student must NOT have SYSTEM_ADMIN")
	}
}

// TestRBACMatrix_TeacherHasCoursePermissions 验证 teacher 角色权限范围。
func TestRBACMatrix_TeacherHasCoursePermissions(t *testing.T) {
	s := fixture()
	e := rbac.NewEngineWithSource(s, zap.NewNop())
	ctx := context.Background()
	teacher := userIDByRole(s, user.RoleTeacher)

	for _, code := range []string{
		user.PermCourseCreate,
		user.PermCourseEdit,
		user.PermMediaUpload,
		user.PermOrderCreate,
	} {
		ok, err := e.Has(ctx, teacher, code)
		if err != nil {
			t.Fatalf("Has(%s): %v", code, err)
		}
		if !ok {
			t.Errorf("teacher should have %s", code)
		}
	}
	if ok, _ := e.Has(ctx, teacher, user.PermCourseApprove); ok {
		t.Errorf("teacher must NOT have COURSE_APPROVE")
	}
	if ok, _ := e.Has(ctx, teacher, user.PermSystemAdmin); ok {
		t.Errorf("teacher must NOT have SYSTEM_ADMIN")
	}
}

// TestRBACMatrix_SuperAdminWildcard 验证 super_admin 通配：即使 permission 表为空也放行任意 code。
func TestRBACMatrix_SuperAdminWildcard(t *testing.T) {
	s := fixture()
	e := rbac.NewEngineWithSource(s, zap.NewNop())
	ctx := context.Background()
	admin := userIDByRole(s, user.RoleSuperAdmin)

	for _, code := range []string{
		user.PermCourseApprove,
		user.PermSystemAdmin,
		user.PermChainSyncReplay,
		user.PermRoleManage,
		"ANY_FUTURE_PERMISSION",
	} {
		ok, err := e.Has(ctx, admin, code)
		if err != nil {
			t.Fatalf("Has(%s): %v", code, err)
		}
		if !ok {
			t.Errorf("super_admin should have %s via wildcard", code)
		}
	}
}

// TestRBACMatrix_AnonymousRejected Require() 必须对 uuid.Nil 拒绝。
func TestRBACMatrix_AnonymousRejected(t *testing.T) {
	s := fixture()
	e := rbac.NewEngineWithSource(s, zap.NewNop())
	ctx := context.Background()

	require := e.Require(user.PermCourseCreate)
	if err := require(ctx, uuid.Nil); err == nil {
		t.Fatal("anonymous must be rejected by Require()")
	}
}

// TestRBACMatrix_UserWithoutRolesHasNothing 验证未授予任何角色的用户拒绝所有权限。
func TestRBACMatrix_UserWithoutRolesHasNothing(t *testing.T) {
	s := fixture()
	e := rbac.NewEngineWithSource(s, zap.NewNop())
	ctx := context.Background()
	nobody := userIDByRole(s, "")

	ok, err := e.Has(ctx, nobody, user.PermOrderCreate)
	if err != nil {
		t.Fatalf("Has: %v", err)
	}
	if ok {
		t.Errorf("user with no roles should not have any permission")
	}
}

// TestRBACMatrix_CacheHitDoesNotQuerySource 缓存命中后 source 不再被调用。
func TestRBACMatrix_CacheHitDoesNotQuerySource(t *testing.T) {
	s := fixture()
	e := rbac.NewEngineWithSource(s, zap.NewNop())
	ctx := context.Background()
	student := userIDByRole(s, user.RoleStudent)

	// 第一次：cold cache，应调用 source 两次（perms + roles）
	before := s.calls.Load()
	if _, err := e.Has(ctx, student, user.PermOrderCreate); err != nil {
		t.Fatalf("first Has: %v", err)
	}
	afterFirst := s.calls.Load()
	if delta := afterFirst - before; delta < 2 {
		t.Fatalf("expected at least 2 calls (perms+roles), got %d", delta)
	}

	// 第二次：warm cache，应不调用 source
	before = s.calls.Load()
	if _, err := e.Has(ctx, student, user.PermOrderCreate); err != nil {
		t.Fatalf("second Has: %v", err)
	}
	if delta := s.calls.Load() - before; delta != 0 {
		t.Errorf("cache hit should not query source, got %d calls", delta)
	}
}

// TestRBACMatrix_CacheExpiresAfterTTL 缓存过期后重新查询。
func TestRBACMatrix_CacheExpiresAfterTTL(t *testing.T) {
	s := fixture()
	e := rbac.NewEngineWithSource(s, zap.NewNop())
	e.SetTTL(10 * time.Millisecond)
	ctx := context.Background()
	student := userIDByRole(s, user.RoleStudent)

	if _, err := e.Has(ctx, student, user.PermOrderCreate); err != nil {
		t.Fatalf("first Has: %v", err)
	}
	// 等待 TTL 过期
	time.Sleep(30 * time.Millisecond)

	before := s.calls.Load()
	if _, err := e.Has(ctx, student, user.PermOrderCreate); err != nil {
		t.Fatalf("post-expiry Has: %v", err)
	}
	if s.calls.Load()-before < 2 {
		t.Errorf("after TTL expiry, source should be queried again")
	}
}

// TestRBACMatrix_InvalidateClearsCache 验证 Invalidate 后会重新查询 source。
func TestRBACMatrix_InvalidateClearsCache(t *testing.T) {
	s := fixture()
	e := rbac.NewEngineWithSource(s, zap.NewNop())
	ctx := context.Background()
	teacher := userIDByRole(s, user.RoleTeacher)

	if _, err := e.Has(ctx, teacher, user.PermCourseCreate); err != nil {
		t.Fatalf("Has: %v", err)
	}
	before := s.calls.Load()
	e.Invalidate(teacher)
	if _, err := e.Has(ctx, teacher, user.PermCourseCreate); err != nil {
		t.Fatalf("post-invalidate Has: %v", err)
	}
	if s.calls.Load()-before < 2 {
		t.Errorf("after Invalidate, source should be queried again")
	}
}

// TestRBACMatrix_SourceErrorPropagated 验证底层 error 透传给调用方。
func TestRBACMatrix_SourceErrorPropagated(t *testing.T) {
	s := fixture()
	wantErr := errors.New("db down")
	s.err = wantErr
	e := rbac.NewEngineWithSource(s, zap.NewNop())
	student := userIDByRole(s, user.RoleStudent)

	if _, err := e.Has(context.Background(), student, user.PermOrderCreate); !errors.Is(err, wantErr) {
		t.Fatalf("expected source error to propagate, got %v", err)
	}
}

// TestRBACMatrix_MiddlewareRejectsAnonymous Middleware 必须对未鉴权请求返回 401。
func TestRBACMatrix_MiddlewareRejectsAnonymous(t *testing.T) {
	s := fixture()
	e := rbac.NewEngineWithSource(s, zap.NewNop())
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.GET("/x", e.Middleware(user.PermCourseCreate), func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

// TestRBACMatrix_MiddlewareRejectsForbidden Middleware 必须对无权限用户返回 403。
func TestRBACMatrix_MiddlewareRejectsForbidden(t *testing.T) {
	s := fixture()
	e := rbac.NewEngineWithSource(s, zap.NewNop())
	student := userIDByRole(s, user.RoleStudent)
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.GET("/x", func(c *gin.Context) {
		c.Set("user_id", student.String())
		c.Set("request_id", "test-req")
		c.Next()
	}, e.Middleware(user.PermCourseCreate), func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

// TestRBACMatrix_MiddlewareAllowsPermitted Middleware 必须放行有权限的用户。
func TestRBACMatrix_MiddlewareAllowsPermitted(t *testing.T) {
	s := fixture()
	e := rbac.NewEngineWithSource(s, zap.NewNop())
	teacher := userIDByRole(s, user.RoleTeacher)
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.GET("/x", func(c *gin.Context) {
		c.Set("user_id", teacher.String())
		c.Set("request_id", "test-req")
		c.Next()
	}, e.Middleware(user.PermCourseCreate), func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}
