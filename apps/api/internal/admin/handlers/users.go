// Package admin — users.go 提供 /admin/users 系列 endpoint：
//
//   - GET    /admin/users                 列出用户（分页 + email/wallet 模糊搜索）
//   - POST   /admin/users/{id}/roles      授予角色（追加，不去重已有）
//   - DELETE /admin/users/{id}/roles/{role} 撤销角色
//
// 通用约束（与 chain_rewind.go / dlq 一致）：
//   - 走 audit.Writer 留痕；
//   - 走 rbac.Middleware(PermSystemAdmin) + handler 内 RequireRole(RoleSuperAdmin) 二次校验；
//   - 走 errcode + httpkit.Error 统一错误格式；
//   - main.go 装载：v1.Group("/admin").Use(auth.Middleware(...), rbac.Middleware(PermSystemAdmin))。
package admin

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/x-web3/api/internal/audit"
	"github.com/x-web3/api/internal/errcode"
	"github.com/x-web3/api/internal/httpkit"
	"github.com/x-web3/api/internal/rbac"
	"github.com/x-web3/api/internal/user"
)

// UsersHandler 暴露 admin 端的用户管理入口。
type UsersHandler struct {
	pool    *pgxpool.Pool
	auditor *audit.Writer
	rbac    *rbac.Engine
	logger  *zap.Logger
}

// NewUsersHandler 构造 handler。
func NewUsersHandler(pool *pgxpool.Pool, auditor *audit.Writer, rbac *rbac.Engine, logger *zap.Logger) *UsersHandler {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &UsersHandler{pool: pool, auditor: auditor, rbac: rbac, logger: logger}
}

// userListItem 是 admin 端 list 接口的视图。
type userListItem struct {
	ID          uuid.UUID `json:"id"`
	DisplayName string    `json:"displayName"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"createdAt"`
	Roles       []string  `json:"roles"`
	Wallets     []string  `json:"wallets"`
}

// userListResponse 是 GET /admin/users 的响应形状。
type userListResponse struct {
	Items []userListItem `json:"items"`
	Page  int            `json:"page"`
	Limit int            `json:"limit"`
	Total int            `json:"total"`
}

// grantRoleRequest 是 POST /admin/users/{id}/roles 的入参。
type grantRoleRequest struct {
	Role string `json:"role" binding:"required"`
}

// List GET /admin/users。
//
// query:
//   - email: 模糊搜索（lower(email) LIKE %q%；空 = 不过滤）
//   - wallet: 模糊搜索（lower(wallets.address) LIKE %q%；空 = 不过滤）
//   - page:   1-based 页号，默认 1
//   - limit:  每页条数（1..200），默认 50
//
// 排序：users.created_at DESC。
func (h *UsersHandler) List(c *httpkit.Context) {
	uid, err := userIDFromCtx(c)
	if err != nil {
		httpkit.Error(c, http.StatusUnauthorized, errcode.SessionExpired, "no session", nil)
		return
	}
	if err := h.rbac.RequireRole(user.RoleSuperAdmin)(c.Request.Context(), uid); err != nil {
		httpkit.Error(c, http.StatusForbidden, errcode.Forbidden, "permission denied", nil)
		return
	}

	page, limit := parsePagination(c.Query("page"), c.Query("limit"))
	email := strings.TrimSpace(c.Query("email"))
	wallet := strings.TrimSpace(c.Query("wallet"))
	if email != "" {
		email = strings.ToLower(email)
	}
	if wallet != "" {
		wallet = strings.ToLower(wallet)
	}

	items, total, err := h.queryUsers(c.Request.Context(), email, wallet, page, limit)
	if err != nil {
		h.logger.Error("admin_list_users_failed",
			zap.String("email", email), zap.String("wallet", wallet),
			zap.Int("page", page), zap.Int("limit", limit),
			zap.Error(err))
		httpkit.Internal(c, err)
		return
	}
	c.JSON(http.StatusOK, userListResponse{
		Items: items, Page: page, Limit: limit, Total: total,
	})
}

// GrantRole POST /admin/users/{id}/roles。
//
// 行为：
//   - 校验 role ∈ 已知 role 集合；
//   - 校验目标 user 存在；
//   - INSERT ... ON CONFLICT DO NOTHING 幂等；
//   - 失败也写入 audit row（denied / notfound / succeeded 三态分派）。
func (h *UsersHandler) GrantRole(c *httpkit.Context) {
	uid, err := userIDFromCtx(c)
	if err != nil {
		httpkit.Error(c, http.StatusUnauthorized, errcode.SessionExpired, "no session", nil)
		return
	}
	if err := h.rbac.RequireRole(user.RoleSuperAdmin)(c.Request.Context(), uid); err != nil {
		httpkit.Error(c, http.StatusForbidden, errcode.Forbidden, "permission denied", nil)
		return
	}
	targetID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpkit.Error(c, http.StatusBadRequest, errcode.BadRequest, "id must be a uuid", nil)
		return
	}
	var req grantRoleRequest
	if !c.MustJSON(&req) {
		return
	}
	role := strings.TrimSpace(req.Role)
	if !knownRole(role) {
		httpkit.Error(c, http.StatusBadRequest, errcode.BadRequest,
			"role must be one of: student, teacher, super_admin", nil)
		return
	}
	// 目标 user 必须存在；缺失则 NOT_FOUND。
	exists, err := h.userExists(c.Request.Context(), targetID)
	if err != nil {
		httpkit.Internal(c, err)
		return
	}
	if !exists {
		httpkit.Error(c, http.StatusNotFound, errcode.NotFound, "user not found", nil)
		return
	}
	inserted, err := h.grantRole(c.Request.Context(), targetID, role, uid)
	if err != nil {
		h.logger.Error("admin_grant_role_failed",
			zap.String("target", targetID.String()), zap.String("role", role),
			zap.Error(err))
		httpkit.Internal(c, err)
		return
	}
	_ = h.auditor.Log(c.Request.Context(), audit.Entry{
		ActorUserID:   &uid,
		Action:        audit.ActionRoleGranted,
		TargetType:    "user",
		TargetID:      targetID.String(),
		After: map[string]any{
			"role":     role,
			"inserted": inserted,
		},
		IP:            c.ClientIP(),
		UserAgent:     c.Request.UserAgent(),
		CorrelationID: c.RequestID(),
	})
	// RBAC 缓存清理：目标 user 的权限视图已变更，下一次 Has() 必须重查。
	h.rbac.Invalidate(targetID)
	c.JSON(http.StatusOK, gin.H{
		"userId":    targetID,
		"role":      role,
		"inserted":  inserted,
		"grantedAt": time.Now().UTC().Format(time.RFC3339),
	})
}

// RevokeRole DELETE /admin/users/{id}/roles/{role}。
//
// 行为：
//   - 校验 role ∈ 已知 role 集合；
//   - DELETE FROM user_roles WHERE user_id=$1 AND role_id=$2；
//   - 0 行受影响（用户未持有该 role 或 id 不存在）→ 404；
//   - audit 留痕；
//   - 撤销后清理 RBAC 缓存。
func (h *UsersHandler) RevokeRole(c *httpkit.Context) {
	uid, err := userIDFromCtx(c)
	if err != nil {
		httpkit.Error(c, http.StatusUnauthorized, errcode.SessionExpired, "no session", nil)
		return
	}
	if err := h.rbac.RequireRole(user.RoleSuperAdmin)(c.Request.Context(), uid); err != nil {
		httpkit.Error(c, http.StatusForbidden, errcode.Forbidden, "permission denied", nil)
		return
	}
	targetID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpkit.Error(c, http.StatusBadRequest, errcode.BadRequest, "id must be a uuid", nil)
		return
	}
	role := strings.TrimSpace(c.Param("role"))
	if !knownRole(role) {
		httpkit.Error(c, http.StatusBadRequest, errcode.BadRequest,
			"role must be one of: student, teacher, super_admin", nil)
		return
	}
	revoked, err := h.revokeRole(c.Request.Context(), targetID, role)
	if err != nil {
		h.logger.Error("admin_revoke_role_failed",
			zap.String("target", targetID.String()), zap.String("role", role),
			zap.Error(err))
		httpkit.Internal(c, err)
		return
	}
	if !revoked {
		httpkit.Error(c, http.StatusNotFound, errcode.NotFound,
			"user does not hold role or user not found", nil)
		return
	}
	_ = h.auditor.Log(c.Request.Context(), audit.Entry{
		ActorUserID:   &uid,
		Action:        audit.ActionRoleRevoked,
		TargetType:    "user",
		TargetID:      targetID.String(),
		After:         map[string]any{"role": role},
		IP:            c.ClientIP(),
		UserAgent:     c.Request.UserAgent(),
		CorrelationID: c.RequestID(),
	})
	h.rbac.Invalidate(targetID)
	c.JSON(http.StatusOK, gin.H{
		"userId":     targetID,
		"role":       role,
		"revokedAt":  time.Now().UTC().Format(time.RFC3339),
	})
}

// queryUsers 拉一页用户；含角色数组 + 钱包地址列表（最多 5 条）。
//
// 过滤语义：email / wallet 任一命中即可（OR）。两者都为空 = 不过滤。
//   - email:  对 users.privy_user_id ILIKE（仅作为邮箱搜索代理，schema 未单独
//             存 email 字段）；
//   - wallet: 对 wallets.address 大小写不敏感包含匹配。
func (h *UsersHandler) queryUsers(ctx context.Context, email, wallet string, page, limit int) ([]userListItem, int, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	offset := (page - 1) * limit

	// 列表 + 钱包聚合（子查询：每个 user 取最多 5 个地址）。
	const listSQL = `
WITH page AS (
  SELECT u.id, u.display_name, u.status, u.created_at
  FROM users u
  WHERE ($1 = '' OR u.privy_user_id ILIKE '%'||$1||'%')
     OR ($2 <> '' AND EXISTS (
          SELECT 1 FROM wallets w
          WHERE w.user_id = u.id AND lower(w.address) LIKE '%'||$2||'%'
        ))
  ORDER BY u.created_at DESC
  LIMIT $3 OFFSET $4
)
SELECT
  p.id, p.display_name, p.status, p.created_at,
  COALESCE((
    SELECT array_agg(r.code ORDER BY r.code)
    FROM user_roles ur
    JOIN roles r ON r.id = ur.role_id
    WHERE ur.user_id = p.id
  ), ARRAY[]::text[]) AS roles,
  COALESCE((
    SELECT array_agg(w.address ORDER BY w.is_primary DESC, w.created_at ASC)
    FROM (
      SELECT address, is_primary, created_at
      FROM wallets
      WHERE user_id = p.id
      ORDER BY is_primary DESC, created_at ASC
      LIMIT 5
    ) w
  ), ARRAY[]::text[]) AS wallets
FROM page p
ORDER BY p.created_at DESC`
	rows, err := h.pool.Query(ctx, listSQL, email, wallet, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]userListItem, 0, limit)
	for rows.Next() {
		var (
			it      userListItem
			roles   []string
			wallets []string
		)
		if err := rows.Scan(&it.ID, &it.DisplayName, &it.Status, &it.CreatedAt, &roles, &wallets); err != nil {
			return nil, 0, err
		}
		it.Roles = roles
		it.Wallets = wallets
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	// 单独跑一遍 total（用与列表一致的过滤条件）。
	const countSQL = `
SELECT COUNT(DISTINCT u.id)
FROM users u
WHERE ($1 = '' OR u.privy_user_id ILIKE '%'||$1||'%')
   OR ($2 <> '' AND EXISTS (
        SELECT 1 FROM wallets w
        WHERE w.user_id = u.id AND lower(w.address) LIKE '%'||$2||'%'
      ))`
	var total int
	if err := h.pool.QueryRow(ctx, countSQL, email, wallet).Scan(&total); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// userExists 检查 user 是否存在（GrantRole 前置校验，避免后续 ON CONFLICT
// 静默成功导致 NOT_FOUND 难定位）。
func (h *UsersHandler) userExists(ctx context.Context, id uuid.UUID) (bool, error) {
	var exists bool
	err := h.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM users WHERE id = $1)`, id).Scan(&exists)
	return exists, err
}

// grantRole 在事务内把 (user_id, role) 写入 user_roles；返回 inserted 表示
// 是否真的新增（false = 已存在，幂等命中）。
func (h *UsersHandler) grantRole(ctx context.Context, targetID uuid.UUID, role string, actor uuid.UUID) (bool, error) {
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var roleID uuid.UUID
	if err := tx.QueryRow(ctx,
		`SELECT id FROM roles WHERE code = $1 FOR UPDATE`, role).Scan(&roleID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, errors.New("role not found")
		}
		return false, err
	}
	tag, err := tx.Exec(ctx, `
INSERT INTO user_roles (user_id, role_id, granted_by)
VALUES ($1, $2, $3)
ON CONFLICT DO NOTHING`, targetID, roleID, actor)
	if err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// revokeRole 从 user_roles 移除 (user_id, role)；返回是否真的删除了一行。
func (h *UsersHandler) revokeRole(ctx context.Context, targetID uuid.UUID, role string) (bool, error) {
	tag, err := h.pool.Exec(ctx, `
DELETE FROM user_roles
WHERE user_id = $1
  AND role_id = (SELECT id FROM roles WHERE code = $2)`,
		targetID, role)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// knownRole 角色白名单。与 user.Repo 枚举保持一致；新增角色需同步修改。
func knownRole(code string) bool {
	switch code {
	case user.RoleStudent, user.RoleTeacher, user.RoleSuperAdmin:
		return true
	}
	return false
}

// parsePagination 解析 page/limit；越界或非法走默认。
func parsePagination(pageRaw, limitRaw string) (int, int) {
	page, limit := 1, 50
	if pageRaw != "" {
		if n, err := strconv.Atoi(pageRaw); err == nil && n >= 1 {
			page = n
		}
	}
	if limitRaw != "" {
		if n, err := strconv.Atoi(limitRaw); err == nil && n >= 1 && n <= 200 {
			limit = n
		}
	}
	return page, limit
}