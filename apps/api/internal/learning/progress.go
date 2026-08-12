// Package learning — progress 进度上报（F04-T08）。
//
// 关键不变量：
//   - 进度只能**不倒退**：写入前读当前 pct；新值 < 当前值 → 409 ProgressRegression；
//     新值 == 当前值 → 幂等返回，不变更 updated_at（避免无谓的写）；
//   - 必须在 lesson 所属课程的 enrollment 校验通过后写，否则视为越权 → 403；
//   - lesson_progress 表的 (user_id, lesson_id) UNIQUE 约束由 UPSERT 兜底，
//     并发同 lesson 上报不会出现重复行。
package learning

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// 错误哨兵：handler 层用 errors.Is 分类到 errcode。
var (
	// ErrProgressRegression 试图写入比当前值更低的 pct。
	ErrProgressRegression = errors.New("learning: progress regression")
	// ErrLessonAccessDenied 用户未购买此 lesson 所属课程。
	ErrLessonAccessDenied = errors.New("learning: lesson not accessible")
	// ErrLessonMissing lesson 不存在。
	ErrLessonMissing = errors.New("learning: lesson missing")
)

// progressUpsertSQL 通过 PostgreSQL 的 ON CONFLICT + WHERE 子句实现原子非递减：
//
//	INSERT ... ON CONFLICT (user_id, lesson_id) DO UPDATE
//	  SET pct = EXCLUDED.pct, updated_at = now()
//	  WHERE EXCLUDED.pct >= lesson_progress.pct
//
// WHERE 不命中时，UPDATE 返回 0 行；service 层据此判定 regression 并翻译为 409。
// 同值（pct == current）时 UPDATE 会命中 WHERE 并刷新时间戳；
// service 层预先短路同值，走轻量 touch（避免 ON CONFLICT 路径下不必要的触发器开销）。
const progressUpsertSQL = `
INSERT INTO lesson_progress (user_id, lesson_id, pct, updated_at)
VALUES ($1, $2, $3, now())
ON CONFLICT (user_id, lesson_id) DO UPDATE
  SET pct = EXCLUDED.pct, updated_at = now()
  WHERE EXCLUDED.pct >= lesson_progress.pct
`

// progressSelectSQL 取当前 pct（不存在则返回 pgx.ErrNoRows）。
const progressSelectSQL = `
SELECT pct
  FROM lesson_progress
 WHERE user_id = $1 AND lesson_id = $2
`

// progressTouchSQL 同值幂等：仅刷新 updated_at，不变更 pct。
const progressTouchSQL = `
UPDATE lesson_progress
   SET updated_at = now()
 WHERE user_id = $1 AND lesson_id = $2 AND pct = $3
`

// ReportProgress 在 service 层封装：
//  1. 校验 lesson 存在且用户已 enrollment
//  2. SELECT 当前 pct；与新值比较
//  3. UPSERT；UPDATE 0 行 → 409 翻译
//  4. 返回新 pct 给 handler
func (s *Service) ReportProgress(ctx context.Context, userID, lessonID uuid.UUID, pct int) (int, error) {
	if pct < 0 || pct > 100 {
		return 0, fmt.Errorf("learning: pct must be in [0,100], got %d", pct)
	}

	// 1. lesson 存在
	var lessonExists bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM lessons WHERE id = $1)`, lessonID,
	).Scan(&lessonExists); err != nil {
		return 0, err
	}
	if !lessonExists {
		return 0, ErrLessonMissing
	}

	// 2. 解析 lesson → course 并校验 enrollment
	var courseID uuid.UUID
	err := s.pool.QueryRow(ctx, `
SELECT v.course_id
  FROM lessons l
  JOIN chapters ch ON ch.id = l.chapter_id
  JOIN course_versions v ON v.id = ch.course_version_id
  JOIN courses c ON c.id = v.course_id AND c.current_version = v.version
 WHERE l.id = $1`, lessonID).Scan(&courseID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, ErrLessonMissing
		}
		return 0, err
	}

	enrolled, err := s.userEnrolled(ctx, userID, courseID)
	if err != nil {
		return 0, err
	}
	if !enrolled {
		return 0, ErrLessonAccessDenied
	}

	// 3. 取当前 pct（无行 = 首次写入）
	var current int
	row := s.pool.QueryRow(ctx, progressSelectSQL, userID, lessonID)
	if err := row.Scan(&current); err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return 0, err
		}
		// 首次：直接 UPSERT 即可
		if _, err := s.pool.Exec(ctx, progressUpsertSQL, userID, lessonID, pct); err != nil {
			return 0, err
		}
		return pct, nil
	}

	// 倒退判定（短路，避免无谓写）
	if pct < current {
		return current, ErrProgressRegression
	}

	// 同值：幂等，touch updated_at
	if pct == current {
		if _, err := s.pool.Exec(ctx, progressTouchSQL, userID, lessonID, current); err != nil {
			return 0, err
		}
		return current, nil
	}

	// 4. 递增：UPSERT；WHERE 兜底并发 regression
	tag, err := s.pool.Exec(ctx, progressUpsertSQL, userID, lessonID, pct)
	if err != nil {
		return 0, err
	}
	if tag.RowsAffected() == 0 {
		return current, ErrProgressRegression
	}
	return pct, nil
}

// userEnrolled 是 enrolled 的简化版：直接给定 (user_id, course_id) 查询 enrollments 表。
//
// 进度接口不需要 lesson → course 解析（ReportProgress 已在上层解析过），
// 因此提供一个直查版本更清晰。
func (s *Service) userEnrolled(ctx context.Context, userID, courseID uuid.UUID) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM enrollments WHERE user_id = $1 AND course_id = $2)`,
		userID, courseID).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}

// ProgressEntry 是查询接口（/me/progress）要返回的行结构。
type ProgressEntry struct {
	LessonID  uuid.UUID `json:"lessonId"`
	Pct       int       `json:"pct"`
	UpdatedAt time.Time `json:"updatedAt"`
}