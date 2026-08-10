// Package rbac 实现 permission 缓存 + 中间件 + 对象级 service hook。
package rbac

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/x-web3/api/internal/user"
)

// Source 抽象出 role/permission 查询；生产由 user.Repo 提供，
// 单测可注入 stub，避免依赖真实数据库。
type Source interface {
	ListPermissions(ctx context.Context, userID uuid.UUID) ([]string, error)
	ListRoleCodes(ctx context.Context, userID uuid.UUID) ([]string, error)
}

// Engine 提供：
//   - PermissionsFor(userID) []string  // 带 TTL 内存缓存
//   - HasPermission(userID, code) bool
//   - Require(code) middleware
type Engine struct {
	src    Source
	pool   *pgxpool.Pool
	logger *zap.Logger

	mu  sync.RWMutex
	ttl time.Duration
	c   map[uuid.UUID]cacheEntry
}

type cacheEntry struct {
	perms    []string
	roles    []string
	cachedAt time.Time
}

const defaultTTL = 60 * time.Second

// NewEngine 构造生产用 Engine；底层 Source 走 user.Repo。
func NewEngine(pool *pgxpool.Pool, logger *zap.Logger) *Engine {
	return NewEngineWithSource(user.NewRepo(pool), logger)
}

// NewEngineWithSource 允许测试注入自定义 Source（保持生产路径不变）。
func NewEngineWithSource(src Source, logger *zap.Logger) *Engine {
	return &Engine{
		src:    src,
		logger: logger,
		ttl:    defaultTTL,
		c:      make(map[uuid.UUID]cacheEntry),
	}
}

// SetTTL 仅测试用：缩短/延长缓存 TTL。
func (e *Engine) SetTTL(d time.Duration) {
	e.mu.Lock()
	e.ttl = d
	e.mu.Unlock()
}

// Permissions 返回用户 permission 列表（含缓存）。
func (e *Engine) Permissions(ctx context.Context, userID uuid.UUID) ([]string, error) {
	if e.hit(userID) {
		return e.c[userID].perms, nil
	}
	perms, err := e.src.ListPermissions(ctx, userID)
	if err != nil {
		return nil, err
	}
	roles, err := e.src.ListRoleCodes(ctx, userID)
	if err != nil {
		return nil, err
	}
	e.store(userID, perms, roles)
	return perms, nil
}

// Has 简易判定。底层会拉一次缓存。
func (e *Engine) Has(ctx context.Context, userID uuid.UUID, code string) (bool, error) {
	perms, err := e.Permissions(ctx, userID)
	if err != nil {
		return false, err
	}
	for _, p := range perms {
		if p == code {
			return true, nil
		}
	}
	// super_admin 通配：单查一次角色，命中即放行
	roles, _ := e.src.ListRoleCodes(ctx, userID)
	for _, r := range roles {
		if r == user.RoleSuperAdmin {
			return true, nil
		}
	}
	return false, nil
}

// Invalidate 在角色变更后调用。
func (e *Engine) Invalidate(userID uuid.UUID) {
	e.mu.Lock()
	delete(e.c, userID)
	e.mu.Unlock()
}

func (e *Engine) hit(userID uuid.UUID) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	entry, ok := e.c[userID]
	if !ok {
		return false
	}
	return time.Since(entry.cachedAt) < e.ttl
}

func (e *Engine) store(userID uuid.UUID, perms, roles []string) {
	e.mu.Lock()
	e.c[userID] = cacheEntry{perms: perms, roles: roles, cachedAt: time.Now()}
	e.mu.Unlock()
}

// Require 是路由级 middleware。注入到 gin router 后，
// 没有对应 permission 返回 403。
//
// 调用方式：rbac.Require("COURSE_CREATE")
func (e *Engine) Require(code string) func(ctx context.Context, userID uuid.UUID) error {
	return func(ctx context.Context, userID uuid.UUID) error {
		if userID == uuid.Nil {
			return errors.New("rbac: anonymous")
		}
		ok, err := e.Has(ctx, userID, code)
		if err != nil {
			return err
		}
		if !ok {
			return errors.New("rbac: forbidden")
		}
		return nil
	}
}

// HasRole 判断用户是否持有任一指定 role code（含缓存）。用于 handler 内部
// 二次校验或单纯按角色判断的场景。
func (e *Engine) HasRole(ctx context.Context, userID uuid.UUID, codes ...string) (bool, error) {
	if userID == uuid.Nil {
		return false, nil
	}
	// 命中缓存时跳过 source 二次调用。
	e.mu.RLock()
	entry, ok := e.c[userID]
	e.mu.RUnlock()
	if ok && time.Since(entry.cachedAt) < e.ttl {
		for _, r := range entry.roles {
			for _, want := range codes {
				if r == want {
					return true, nil
				}
			}
		}
		return false, nil
	}
	roles, err := e.src.ListRoleCodes(ctx, userID)
	if err != nil {
		return false, err
	}
	for _, r := range roles {
		for _, want := range codes {
			if r == want {
				return true, nil
			}
		}
	}
	return false, nil
}

// RequireRole 是基于角色的 guard；用于「仅 super_admin 可访问」一类硬门。
//
// 调用方式：rbac.RequireRole(user.RoleSuperAdmin)
// 与 Require(code) 的语义差异：这里只看角色，不看 permission 表项；
// 对 RoleSuperAdmin 维持通配语义（与 Has() 一致）。
func (e *Engine) RequireRole(codes ...string) func(ctx context.Context, userID uuid.UUID) error {
	return func(ctx context.Context, userID uuid.UUID) error {
		if userID == uuid.Nil {
			return errors.New("rbac: anonymous")
		}
		ok, err := e.HasRole(ctx, userID, codes...)
		if err != nil {
			return err
		}
		if !ok {
			return errors.New("rbac: forbidden")
		}
		return nil
	}
}

// Middleware adapts permission checks to Gin after auth.Middleware has set user_id.
func (e *Engine) Middleware(code string) gin.HandlerFunc {
	require := e.Require(code)
	return func(c *gin.Context) {
		userID, err := uuid.Parse(c.GetString("user_id"))
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": gin.H{
				"code": "UNAUTHORIZED", "message": "authentication required",
				"requestId": c.GetString("request_id"),
			}})
			return
		}
		if err := require(c.Request.Context(), userID); err != nil {
			e.logger.Warn("permission_denied", zap.String("permission", code), zap.String("user_id", userID.String()))
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": gin.H{
				"code": "FORBIDDEN", "message": "permission denied",
				"requestId": c.GetString("request_id"),
			}})
			return
		}
		c.Next()
	}
}
