// Package rbac 实现 permission 缓存 + 中间件 + 对象级 service hook。
package rbac

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/x-web3/api/internal/user"
)

// Engine 提供：
//   - PermissionsFor(userID) []string  // 带 TTL 内存缓存
//   - HasPermission(userID, code) bool
//   - Require(code) middleware
type Engine struct {
	repo   *user.Repo
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

func NewEngine(pool *pgxpool.Pool, logger *zap.Logger) *Engine {
	return &Engine{
		pool:   pool,
		repo:   user.NewRepo(pool),
		logger: logger,
		ttl:    defaultTTL,
		c:      make(map[uuid.UUID]cacheEntry),
	}
}

// Permissions 返回用户 permission 列表（含缓存）。
func (e *Engine) Permissions(ctx context.Context, userID uuid.UUID) ([]string, error) {
	if e.hit(userID) {
		return e.c[userID].perms, nil
	}
	perms, err := e.repo.ListPermissions(ctx, userID)
	if err != nil {
		return nil, err
	}
	roles, err := e.repo.ListRoleCodes(ctx, userID)
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
	roles, _ := e.repo.ListRoleCodes(ctx, userID)
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
