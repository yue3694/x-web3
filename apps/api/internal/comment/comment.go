// Package comment 实现课程评论的 CRUD + 审核 + 软删除。
//
// 业务规则：
//   - 只有已购买用户可写评论（通过 enrollments 或 paid orders 校验）
//   - 评论默认 moderation_status='pending'；管理员审核后 'approved' / 'rejected'
//   - 软删除：deleted_at 非空视为不存在
//   - 列表查询只返回 'approved' + 自己的全部状态（让学生看自己的 pending）
package comment

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound      = errors.New("comment: not found")
	ErrNotPurchased  = errors.New("comment: only purchased users may comment")
	ErrEmptyBody     = errors.New("comment: body required")
	ErrBodyTooLong   = errors.New("comment: body too long")
	ErrForbidden     = errors.New("comment: not the author")
	ErrAlreadyDeleted = errors.New("comment: already deleted")
)

// Comment 是 comments 表的镜像。
type Comment struct {
	ID               uuid.UUID `json:"id"`
	CourseID         uuid.UUID `json:"courseId"`
	UserID           uuid.UUID `json:"userId"`
	UserDisplayName  string    `json:"userDisplayName,omitempty"`
	Body             string    `json:"body"`
	ModerationStatus string    `json:"moderationStatus"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

// Repo comments 数据访问 + 业务规则。
type Repo struct{ pool *pgxpool.Pool }

func NewRepo(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }

// Pool 暴露底层 pool。
func (r *Repo) Pool() *pgxpool.Pool { return r.pool }

// Create 由用户写入评论；返回的 comment 不一定审核通过。
//
// 校验顺序：body 长度 → 是否购买 → INSERT。
func (r *Repo) Create(ctx context.Context, courseID, userID uuid.UUID, body string) (*Comment, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, ErrEmptyBody
	}
	if len(body) > 2000 {
		return nil, ErrBodyTooLong
	}
	ok, err := r.userHasPurchased(ctx, userID, courseID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNotPurchased
	}
	var c Comment
	err = r.pool.QueryRow(ctx, `INSERT INTO comments(course_id,user_id,body)
VALUES($1,$2,$3)
RETURNING id,course_id,user_id,body,moderation_status,created_at,updated_at`,
		courseID, userID, body).Scan(&c.ID, &c.CourseID, &c.UserID, &c.Body, &c.ModerationStatus, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// ListByCourse 列出课程的评论；viewer 自己写的评论无论状态都返回，
// 其他人只返回 approved。
func (r *Repo) ListByCourse(ctx context.Context, courseID, viewerID uuid.UUID, limit int) ([]Comment, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx, `SELECT c.id,c.course_id,c.user_id,COALESCE(u.display_name,''),c.body,c.moderation_status,c.created_at,c.updated_at
FROM comments c LEFT JOIN users u ON u.id = c.user_id
WHERE c.course_id=$1 AND c.deleted_at IS NULL
  AND (c.moderation_status='approved' OR c.user_id=$2)
ORDER BY c.created_at DESC LIMIT $3`, courseID, viewerID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Comment, 0, limit)
	for rows.Next() {
		var c Comment
		if err := rows.Scan(&c.ID, &c.CourseID, &c.UserID, &c.UserDisplayName, &c.Body, &c.ModerationStatus, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ListMyByUser 列出用户的全部评论（含 pending / rejected），用于"我的评论"。
func (r *Repo) ListMyByUser(ctx context.Context, userID uuid.UUID, limit int) ([]Comment, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx, `SELECT c.id,c.course_id,c.user_id,COALESCE(u.display_name,''),c.body,c.moderation_status,c.created_at,c.updated_at
FROM comments c LEFT JOIN users u ON u.id = c.user_id
WHERE c.user_id=$1 AND c.deleted_at IS NULL
ORDER BY c.created_at DESC LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Comment, 0, limit)
	for rows.Next() {
		var c Comment
		if err := rows.Scan(&c.ID, &c.CourseID, &c.UserID, &c.UserDisplayName, &c.Body, &c.ModerationStatus, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// SoftDelete 用户自己软删自己的评论；非作者返回 ErrForbidden。
func (r *Repo) SoftDelete(ctx context.Context, commentID, userID uuid.UUID) error {
	var owner uuid.UUID
	var deleted *time.Time
	err := r.pool.QueryRow(ctx, `SELECT user_id,deleted_at FROM comments WHERE id=$1`, commentID).Scan(&owner, &deleted)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if deleted != nil {
		return ErrAlreadyDeleted
	}
	if owner != userID {
		return ErrForbidden
	}
	_, err = r.pool.Exec(ctx, `UPDATE comments SET deleted_at=now(), updated_at=now() WHERE id=$1`, commentID)
	return err
}

// Moderate 管理员审批 / 驳回；新 status 必须为 'approved' 或 'rejected'。
func (r *Repo) Moderate(ctx context.Context, commentID uuid.UUID, status string) (*Comment, error) {
	if status != "approved" && status != "rejected" {
		return nil, errors.New("comment: status must be approved or rejected")
	}
	var c Comment
	err := r.pool.QueryRow(ctx, `UPDATE comments SET moderation_status=$2, updated_at=now() WHERE id=$1 AND deleted_at IS NULL
RETURNING id,course_id,user_id,body,moderation_status,created_at,updated_at`,
		commentID, status).Scan(&c.ID, &c.CourseID, &c.UserID, &c.Body, &c.ModerationStatus, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &c, err
}

// userHasPurchased 用户是否已购买这门课。
//
// 优先 enrollments 表（F04）；缺失时退回 orders.paid 路径（F03）。
// 都缺失 → false，公开路径不受影响。
func (r *Repo) userHasPurchased(ctx context.Context, userID, courseID uuid.UUID) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM enrollments WHERE user_id=$1 AND course_id=$2)`,
		userID, courseID).Scan(&exists)
	if err == nil {
		return exists, nil
	}
	if !isMissingRelation(err) {
		return false, err
	}
	err = r.pool.QueryRow(ctx, `SELECT EXISTS (
SELECT 1 FROM orders o
JOIN purchase_intents pi ON pi.id=o.intent_id
WHERE pi.user_id=$1 AND o.course_id=$2 AND o.status='paid')`,
		userID, courseID).Scan(&exists)
	if err != nil {
		if isMissingRelation(err) {
			return false, nil
		}
		return false, err
	}
	return exists, nil
}

func isMissingRelation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "does not exist") || strings.Contains(msg, "undefined_table")
}
