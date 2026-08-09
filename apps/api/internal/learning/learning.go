// Package learning 实现播放凭证签发。
//
// 流程：student → GET /lessons/{id}/playback → 检查 enrollment → 返回
// 短期 presigned URL 或 CloudFront signed cookie（TTL ≤ 5 min）。
//
// 关键不变量：
//   - 凭证 TTL ≤ 5 分钟（5 * 60s 硬上限）
//   - 教师预览 draft 时返回同样凭证，但带 purpose=preview 审计标记
//   - 未购买、未登录、lesson 不存在一律返回 403/404（不区分场景，防止枚举）
//
// ObjectStore 接口复用 media.ObjectStore（仅多一个 PresignGet）。
package learning

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/x-web3/api/internal/media"
)

var (
	ErrLessonNotFound = errors.New("learning: lesson not found")
	ErrNotEligible    = errors.New("learning: not enrolled")
	ErrMediaNotReady  = errors.New("learning: media not ready")
)

// 5 分钟硬上限。Config 可下调但不能超过。
const maxTTL = 5 * time.Minute

// ObjectStore 扩展 media.ObjectStore，多一个 PresignGet。
// 在 cmd/api 由同一 AWS SDK 适配器实现。
type ObjectStore interface {
	media.ObjectStore
	PresignGet(ctx context.Context, key string, ttl time.Duration) (url string, expiresAt time.Time, err error)
}

// Purpose 标记用于审计：preview 是老师预览自己 draft，playback 是学生正式播放。
type Purpose string

const (
	PurposePlayback Purpose = "playback"
	PurposePreview  Purpose = "preview"
)

// Credential 返回给客户端的播放凭证。
type Credential struct {
	LessonID  uuid.UUID `json:"lessonId"`
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expiresAt"`
	Purpose   Purpose   `json:"purpose"`
}

// Service 学习子系统入口。
type Service struct {
	pool  *pgxpool.Pool
	store ObjectStore
	ttl   time.Duration
}

// NewService 默认 ttl=2min，调用方可通过 SetTTL 调，但不能超过 maxTTL。
func NewService(pool *pgxpool.Pool, store ObjectStore) *Service {
	return &Service{pool: pool, store: store, ttl: 2 * time.Minute}
}

// SetTTL 设凭证 TTL；超过 maxTTL 自动钳制。
func (s *Service) SetTTL(d time.Duration) {
	if d <= 0 || d > maxTTL {
		d = maxTTL
	}
	s.ttl = d
}

// Playback 返回学生正式播放凭证；未购买返回 ErrNotEligible。
func (s *Service) Playback(ctx context.Context, lessonID, viewerID uuid.UUID) (*Credential, error) {
	return s.issue(ctx, lessonID, viewerID, PurposePlayback)
}

// Preview 返回教师预览 draft 课程章节的凭证；必须是 lesson 所属课程的 teacher。
func (s *Service) Preview(ctx context.Context, lessonID, teacherID uuid.UUID) (*Credential, error) {
	return s.issue(ctx, lessonID, teacherID, PurposePreview)
}

func (s *Service) issue(ctx context.Context, lessonID, viewerID uuid.UUID, purpose Purpose) (*Credential, error) {
	var (
		mediaID  *uuid.UUID
		teacher  uuid.UUID
		status   string
		key      string
	)
	err := s.pool.QueryRow(ctx, `
SELECT l.media_asset_id, c.teacher_id, m.status, COALESCE(m.s3_key,'')
FROM lessons l
JOIN chapters ch ON ch.id = l.chapter_id
JOIN course_versions v ON v.id = ch.course_version_id
JOIN courses c ON c.id = v.course_id AND c.deleted_at IS NULL
LEFT JOIN media_assets m ON m.id = l.media_asset_id
WHERE l.id = $1`, lessonID).Scan(&mediaID, &teacher, &status, &key)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrLessonNotFound
	}
	if err != nil {
		return nil, err
	}
	if mediaID == nil {
		return nil, fmt.Errorf("%w: lesson has no media_asset", ErrMediaNotReady)
	}
	if status != "ready" {
		return nil, ErrMediaNotReady
	}
	switch purpose {
	case PurposePlayback:
		ok, err := s.enrolled(ctx, viewerID, lessonID)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, ErrNotEligible
		}
	case PurposePreview:
		if teacher != viewerID {
			return nil, ErrNotEligible
		}
	}
	url, exp, err := s.store.PresignGet(ctx, key, s.ttl)
	if err != nil {
		return nil, fmt.Errorf("presign: %w", err)
	}
	return &Credential{LessonID: lessonID, URL: url, ExpiresAt: exp, Purpose: purpose}, nil
}

// enrolled 判断 viewer 是否已购买这门课。当前走 orders + purchase_intents 的 paid 状态；
// F04 引入 enrollments 后会切换成 enrollments 查询。
//
// 注意：MVP 阶段 orders / purchase_intents 还未落（F03），所以查不到 → false，
// 公开路径不受影响。
func (s *Service) enrolled(ctx context.Context, userID, lessonID uuid.UUID) (bool, error) {
	// 先查 enrollments（F04 落表后即生效）
	var exists bool
	err := s.pool.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1 FROM enrollments e
  JOIN lessons l ON l.id = $2
  JOIN chapters ch ON ch.id = l.chapter_id
  JOIN course_versions v ON v.id = ch.course_version_id
  WHERE e.user_id = $1 AND e.course_id = v.course_id
)`, userID, lessonID).Scan(&exists)
	if err == nil {
		return exists, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		// 可能是表缺失；尝试老路径
		if !isMissingRelation(err) {
			return false, err
		}
	}
	// 兜底查 orders（paid 状态）— F03 落表后生效
	err = s.pool.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1 FROM orders o
  JOIN purchase_intents pi ON pi.id = o.intent_id
  JOIN lessons l ON l.id = $2
  JOIN chapters ch ON ch.id = l.chapter_id
  JOIN course_versions v ON v.id = ch.course_version_id
  WHERE pi.user_id = $1 AND o.course_id = v.course_id AND o.status = 'paid'
)`, userID, lessonID).Scan(&exists)
	if err != nil {
		if isMissingRelation(err) {
			return false, nil
		}
		return false, err
	}
	return exists, nil
}

// isMissingRelation 兼容表未创建时的查询；用错误信息粗判（生产有 sqlstate 25P02）。
func isMissingRelation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return contains(msg, "does not exist") || contains(msg, "undefined_table")
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// Compile-time check.
var _ = pgx.ErrNoRows
