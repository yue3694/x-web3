// Package learning — 用户维度的读路径：/me/enrollments 与 /me/certificates（F04-T08）。
//
// 设计要点：
//   - 列表分页走 cursor（enrolled_at DESC + id DESC），与课程公开列表同款；
//   - 进度统计：required lessons 总数 / pct=100 数 / 整体百分比（0-100）；
//   - certificate 视图走 certificates + certificate_jobs（0009 模型），状态字段、tx_hash
//     与链上 metadata 都能直接 JOIN。
package learning

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// 错误哨兵：handler 层用 errors.Is 分类到 errcode。
var (
	// ErrEmptyUserID 调用方传入空 user_id（路由层前置防御）。
	ErrEmptyUserID = errors.New("learning: empty user id")
)

// EnrollmentItem /me/enrollments 单行。
type EnrollmentItem struct {
	EnrollmentID   uuid.UUID  `json:"enrollmentId"`
	CourseID       uuid.UUID  `json:"courseId"`
	CourseSlug     string     `json:"courseSlug"`
	CourseTitle    string     `json:"courseTitle"`
	EnrolledAt     time.Time  `json:"enrolledAt"`
	RequiredTotal  int        `json:"requiredLessonsTotal"`
	CompletedTotal int        `json:"completedLessonsTotal"`
	CompletionPct  int        `json:"completionPct"`
	HasCompletion  bool       `json:"hasCompletion"`
	CompletedAt    *time.Time `json:"completedAt,omitempty"`
}

// ListEnrollments 拉当前 user 的 enrollments + 课程基础信息 + 进度统计。
//
// 入参 limit 必须 1..50；超过限制时回退到 50，避免爆量查询拖垮 PG。
func (s *Service) ListEnrollments(ctx context.Context, userID uuid.UUID, limit int) ([]EnrollmentItem, error) {
	if userID == uuid.Nil {
		return nil, ErrEmptyUserID
	}
	if limit <= 0 || limit > 50 {
		limit = 50
	}
	const q = `
SELECT
  e.id              AS enrollment_id,
  c.id              AS course_id,
  c.slug            AS course_slug,
  c.title           AS course_title,
  e.enrolled_at     AS enrolled_at,
  COALESCE(req.required_total, 0)    AS required_total,
  COALESCE(req.completed_total, 0)   AS completed_total,
  cc.completed_at   AS completed_at
FROM enrollments e
JOIN courses c ON c.id = e.course_id AND c.deleted_at IS NULL
LEFT JOIN (
  SELECT
    v.course_id,
    COUNT(*) FILTER (WHERE l.required) AS required_total,
    COUNT(*) FILTER (WHERE l.required AND COALESCE(lp.pct, 0) = 100) AS completed_total
  FROM course_versions v
  JOIN courses c ON c.id = v.course_id AND c.current_version = v.version
  JOIN chapters ch ON ch.course_version_id = v.id
  JOIN lessons l ON l.chapter_id = ch.id
  LEFT JOIN lesson_progress lp ON lp.lesson_id = l.id AND lp.user_id = $1
  GROUP BY v.course_id
) req ON req.course_id = e.course_id
LEFT JOIN course_completions cc
  ON cc.enrollment_id = e.id
WHERE e.user_id = $1
ORDER BY e.enrolled_at DESC, e.id DESC
LIMIT $2
`
	rows, err := s.pool.Query(ctx, q, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]EnrollmentItem, 0)
	for rows.Next() {
		var it EnrollmentItem
		var completedAt *time.Time
		if err := rows.Scan(
			&it.EnrollmentID, &it.CourseID, &it.CourseSlug, &it.CourseTitle,
			&it.EnrolledAt,
			&it.RequiredTotal, &it.CompletedTotal,
			&completedAt,
		); err != nil {
			return nil, err
		}
		if completedAt != nil {
			it.HasCompletion = true
			it.CompletedAt = completedAt
		}
		if it.RequiredTotal > 0 {
			it.CompletionPct = (it.CompletedTotal * 100) / it.RequiredTotal
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

// CertificateItem /me/certificates 单行（0009 schema 视图：certificates + certificate_jobs）。
type CertificateItem struct {
	JobID         uuid.UUID  `json:"jobId"`
	CertificateID uuid.UUID  `json:"certificateId"`
	UserID        uuid.UUID  `json:"userId"`
	CourseID      uuid.UUID  `json:"courseId"`
	CourseTitle   string     `json:"courseTitle"`
	OnchainCertID string     `json:"onchainCertId"`
	Status        string     `json:"status"`
	Attempt       int        `json:"attempt"`
	LastError     *string    `json:"lastError,omitempty"`
	TxHash        []byte     `json:"txHash,omitempty"`
	Recipient     string     `json:"recipientWallet"`
	MetadataURI   string     `json:"metadataUri"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
}

// ListCertificates 拉当前 user 的所有证书记录（含 worker job 状态）。
//
// 入参 limit 必须 1..50；未传或越界时回退 50。
func (s *Service) ListCertificates(ctx context.Context, userID uuid.UUID, limit int) ([]CertificateItem, error) {
	if userID == uuid.Nil {
		return nil, ErrEmptyUserID
	}
	if limit <= 0 || limit > 50 {
		limit = 50
	}
	const q = `
SELECT
  cj.id              AS job_id,
  c.id               AS certificate_id,
  c.user_id          AS user_id,
  c.course_id        AS course_id,
  co.title           AS course_title,
  c.certificate_id::text AS onchain_cert_id,
  cj.status          AS status,
  cj.attempt         AS attempt,
  cj.last_error      AS last_error,
  cj.tx_hash         AS tx_hash,
  c.recipient_wallet AS recipient_wallet,
  c.metadata_uri     AS metadata_uri,
  cj.created_at      AS created_at,
  cj.updated_at      AS updated_at
FROM certificates c
JOIN certificate_jobs cj ON cj.certificate_id = c.id
JOIN courses co ON co.id = c.course_id AND co.deleted_at IS NULL
WHERE c.user_id = $1
ORDER BY cj.created_at DESC, cj.id DESC
LIMIT $2
`
	rows, err := s.pool.Query(ctx, q, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]CertificateItem, 0)
	for rows.Next() {
		var it CertificateItem
		var lastErr *string
		var txHash []byte
		if err := rows.Scan(
			&it.JobID, &it.CertificateID, &it.UserID, &it.CourseID, &it.CourseTitle,
			&it.OnchainCertID, &it.Status, &it.Attempt, &lastErr, &txHash,
			&it.Recipient, &it.MetadataURI,
			&it.CreatedAt, &it.UpdatedAt,
		); err != nil {
			return nil, err
		}
		it.LastError = lastErr
		it.TxHash = txHash
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

// ensure sql imports are used (pgx.ErrNoRows used elsewhere; pgx is also referenced
// through Service.pool which is *pgxpool.Pool).
var _ = pgx.ErrNoRows