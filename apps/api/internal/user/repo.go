// Package user 实现 users / wallets / roles / permissions 的 repository。
//
// 设计要点：
//   - 所有 SQL 走 pgx，避免 ORM；
//   - Unique 约束由数据库保证（migration 0001）；
//   - 上层 service 用 tx 包裹多个 repo 调用。
package user

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Role 角色枚举，与 packages/shared 同步。
const (
	RoleStudent    = "student"
	RoleTeacher    = "teacher"
	RoleSuperAdmin = "super_admin"
)

// Permission 权限编码。
const (
	PermCourseCreate     = "COURSE_CREATE"
	PermCourseEdit       = "COURSE_EDIT"
	PermCourseApprove    = "COURSE_APPROVE"
	PermOrderCreate      = "ORDER_CREATE"
	PermLessonProgress   = "LESSON_PROGRESS_WRITE"
	PermCertificateRead  = "CERTIFICATE_READ"
	PermSystemAdmin      = "SYSTEM_ADMIN"
	PermChainSyncReplay  = "CHAIN_SYNC_REPLAY"
	PermCertificateRetry = "CERTIFICATE_RETRY"
	PermCommentModerate  = "COMMENT_MODERATE"
	PermMediaUpload      = "MEDIA_UPLOAD"
	PermRoleManage       = "ROLE_MANAGE"
)

// User 主表记录。
type User struct {
	ID           uuid.UUID
	PrivySubject string
	DisplayName  string
	Status       string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Wallet 钱包绑定。
type Wallet struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	ChainID   int64
	Address   string
	IsPrimary bool
	BoundAt   time.Time
}

// Repo 是 user 子系统的 DB 入口。
type Repo struct {
	pool *pgxpool.Pool
}

func NewRepo(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }

// UpsertByPrivySubject 是登录入口：不存在则建，否则刷新 updated_at。
// 在同一个事务内可同步钱包。
func (r *Repo) UpsertByPrivySubject(ctx context.Context, tx pgx.Tx, subject, displayName string) (*User, error) {
	if subject == "" {
		return nil, errors.New("user: subject required")
	}
	const q = `
INSERT INTO users (privy_user_id, display_name)
VALUES ($1, COALESCE(NULLIF($2, ''), 'Anonymous'))
ON CONFLICT (privy_user_id) DO UPDATE
  SET display_name = COALESCE(NULLIF(EXCLUDED.display_name, ''), users.display_name),
      updated_at = now()
RETURNING id, privy_user_id, display_name, status, created_at, updated_at
`
	row := tx.QueryRow(ctx, q, subject, displayName)
	var u User
	if err := row.Scan(&u.ID, &u.PrivySubject, &u.DisplayName, &u.Status, &u.CreatedAt, &u.UpdatedAt); err != nil {
		return nil, err
	}
	return &u, nil
}

// GetByID 返回 user；不存在返回 nil, nil。
func (r *Repo) GetByID(ctx context.Context, id uuid.UUID) (*User, error) {
	const q = `SELECT id, privy_user_id, display_name, status, created_at, updated_at FROM users WHERE id = $1`
	var u User
	err := r.pool.QueryRow(ctx, q, id).Scan(&u.ID, &u.PrivySubject, &u.DisplayName, &u.Status, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// ListWallets 返回用户全部钱包，按 is_primary desc, bound_at asc。
func (r *Repo) ListWallets(ctx context.Context, userID uuid.UUID) ([]Wallet, error) {
	const q = `
SELECT id, user_id, chain_id, address, is_primary, created_at
FROM wallets
WHERE user_id = $1
ORDER BY is_primary DESC, created_at ASC`
	rows, err := r.pool.Query(ctx, q, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Wallet
	for rows.Next() {
		var w Wallet
		if err := rows.Scan(&w.ID, &w.UserID, &w.ChainID, &w.Address, &w.IsPrimary, &w.BoundAt); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// WalletByAddress 查 (chain_id, address) 已绑定谁。用于绑前冲突检测。
func (r *Repo) WalletByAddress(ctx context.Context, tx pgx.Tx, chainID int64, address string) (*Wallet, error) {
	const q = `SELECT id, user_id, chain_id, address, is_primary, created_at
FROM wallets WHERE chain_id = $1 AND address = $2 FOR UPDATE`
	row := tx.QueryRow(ctx, q, chainID, address)
	var w Wallet
	err := row.Scan(&w.ID, &w.UserID, &w.ChainID, &w.Address, &w.IsPrimary, &w.BoundAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &w, nil
}

// BindWallet 把钱包插入 wallets。Unique 冲突由 DB 抛出（race）。
func (r *Repo) BindWallet(ctx context.Context, tx pgx.Tx, w *Wallet) error {
	const q = `
INSERT INTO wallets (user_id, chain_id, address, is_primary)
VALUES ($1, $2, $3, $4)
RETURNING id, created_at`
	return tx.QueryRow(ctx, q, w.UserID, w.ChainID, w.Address, w.IsPrimary).
		Scan(&w.ID, &w.BoundAt)
}

// UnbindWallet 删除指定 wallet。
func (r *Repo) UnbindWallet(ctx context.Context, tx pgx.Tx, userID, walletID uuid.UUID) (int64, error) {
	tag, err := tx.Exec(ctx, `DELETE FROM wallets WHERE user_id = $1 AND id = $2`, userID, walletID)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// CountWallets 给解绑保护用。
func (r *Repo) CountWallets(ctx context.Context, userID uuid.UUID) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM wallets WHERE user_id = $1`, userID).Scan(&n)
	return n, err
}

// HasPendingOrderByUser 防止解绑最后一钱包时丢掉链上凭证入口。
func (r *Repo) HasPendingOrderByUser(ctx context.Context, tx pgx.Tx, userID uuid.UUID) (bool, error) {
	var exists bool
	err := tx.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1 FROM orders o
  JOIN purchase_intents pi ON pi.id = o.intent_id
  WHERE pi.user_id = $1 AND o.status IN ('submitted','confirming')
)`, userID).Scan(&exists)
	return exists, err
}

// GrantDefaultRole 保证新用户至少是 student。
func (r *Repo) GrantDefaultRole(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	const q = `
INSERT INTO user_roles (user_id, role_id)
SELECT $1, id FROM roles WHERE code = 'student'
ON CONFLICT DO NOTHING`
	_, err := tx.Exec(ctx, q, userID)
	return err
}

// ListRoleCodes 返回用户的角色 code 列表。
func (r *Repo) ListRoleCodes(ctx context.Context, userID uuid.UUID) ([]string, error) {
	const q = `
SELECT r.code FROM roles r
JOIN user_roles ur ON ur.role_id = r.id
WHERE ur.user_id = $1`
	rows, err := r.pool.Query(ctx, q, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ListPermissions 返回用户 effective permission（含 super_admin 通配）。
func (r *Repo) ListPermissions(ctx context.Context, userID uuid.UUID) ([]string, error) {
	const q = `
SELECT DISTINCT p.code FROM permissions p
JOIN role_permissions rp ON rp.permission_id = p.id
JOIN user_roles ur ON ur.role_id = rp.role_id
WHERE ur.user_id = $1`
	rows, err := r.pool.Query(ctx, q, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
