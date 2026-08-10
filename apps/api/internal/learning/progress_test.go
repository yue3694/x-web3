package learning

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// skipIfNoPG 跳过没 PG 的环境；测试逻辑只在 DATABASE_URL_TEST 存在时跑。
func skipIfNoPG(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := strings.TrimSpace(os.Getenv("DATABASE_URL_TEST"))
	if url == "" {
		t.Skip("set DATABASE_URL_TEST to enable progress integration tests")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Skipf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(context.Background()); err != nil {
		t.Skipf("ping: %v", err)
	}
	return pool
}

func bootstrapProgressSchema(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	stmts := []string{
		`CREATE EXTENSION IF NOT EXISTS pgcrypto`,
		`CREATE TABLE IF NOT EXISTS users (
			id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			privy_user_id text UNIQUE,
			display_name text,
			status text NOT NULL DEFAULT 'active',
			created_at timestamptz NOT NULL DEFAULT now(),
			updated_at timestamptz NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS courses (
			id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			teacher_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
			slug text NOT NULL UNIQUE,
			title text NOT NULL,
			status text NOT NULL DEFAULT 'draft',
			current_version integer NOT NULL DEFAULT 1,
			price_minor bigint NOT NULL DEFAULT 0,
			currency text NOT NULL DEFAULT 'USD',
			published_at timestamptz,
			deleted_at timestamptz,
			created_at timestamptz NOT NULL DEFAULT now(),
			updated_at timestamptz NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS course_versions (
			id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			course_id uuid NOT NULL REFERENCES courses(id) ON DELETE CASCADE,
			version integer NOT NULL,
			description text NOT NULL DEFAULT '',
			completion_rule jsonb NOT NULL DEFAULT '{}'::jsonb,
			created_at timestamptz NOT NULL DEFAULT now(),
			updated_at timestamptz NOT NULL DEFAULT now(),
			UNIQUE(course_id, version)
		)`,
		`CREATE TABLE IF NOT EXISTS chapters (
			id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			course_version_id uuid NOT NULL REFERENCES course_versions(id) ON DELETE CASCADE,
			position integer NOT NULL,
			title text NOT NULL,
			created_at timestamptz NOT NULL DEFAULT now(),
			updated_at timestamptz NOT NULL DEFAULT now(),
			UNIQUE(course_version_id, position)
		)`,
		`CREATE TABLE IF NOT EXISTS lessons (
			id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			chapter_id uuid NOT NULL REFERENCES chapters(id) ON DELETE CASCADE,
			position integer NOT NULL,
			title text NOT NULL,
			required boolean NOT NULL DEFAULT true,
			media_asset_id uuid,
			duration_seconds integer NOT NULL DEFAULT 0,
			created_at timestamptz NOT NULL DEFAULT now(),
			updated_at timestamptz NOT NULL DEFAULT now(),
			UNIQUE(chapter_id, position)
		)`,
		`CREATE TABLE IF NOT EXISTS enrollments (
			id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			course_id uuid NOT NULL REFERENCES courses(id) ON DELETE CASCADE,
			source text NOT NULL DEFAULT 'seed',
			enrolled_at timestamptz NOT NULL DEFAULT now(),
			UNIQUE (user_id, course_id)
		)`,
		`CREATE TABLE IF NOT EXISTS lesson_progress (
			user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			lesson_id uuid NOT NULL REFERENCES lessons(id) ON DELETE CASCADE,
			pct integer NOT NULL DEFAULT 0,
			updated_at timestamptz NOT NULL DEFAULT now(),
			PRIMARY KEY (user_id, lesson_id),
			CONSTRAINT lesson_progress_pct_chk CHECK (pct BETWEEN 0 AND 100)
		)`,
	}
	for _, s := range stmts {
		if _, err := pool.Exec(ctx, s); err != nil {
			t.Fatalf("schema bootstrap: %v\nSQL: %s", err, s)
		}
	}
}

// (progress_test.go no longer needs full 0009 schema — lesson_progress alone is enough
// for the ReportProgress service tests; completion tests live in completion_test.go.)

// seedLesson 创建一个 user + course + lesson + enrollment 场景；返回 lessonID。
func seedLesson(t *testing.T, pool *pgxpool.Pool, enroll bool) (userID, lessonID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	userID = uuid.New()
	teacherID := uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users(id, privy_user_id, status) VALUES ($1, $2, 'active'), ($3, $4, 'active')`,
		userID, "did:u-"+userID.String(),
		teacherID, "did:t-"+teacherID.String(),
	); err != nil {
		t.Fatalf("insert users: %v", err)
	}
	courseID := uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO courses(id, teacher_id, slug, title, status) VALUES ($1, $2, $3, 'Test Course', 'published')`,
		courseID, teacherID, "tc-"+courseID.String(),
	); err != nil {
		t.Fatalf("insert course: %v", err)
	}
	var versionID uuid.UUID
	if err := pool.QueryRow(ctx,
		`INSERT INTO course_versions(course_id, version) VALUES ($1, 1) RETURNING id`,
		courseID,
	).Scan(&versionID); err != nil {
		t.Fatalf("insert course_version: %v", err)
	}
	chapterID := uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO chapters(id, course_version_id, position, title) VALUES ($1, $2, 1, 'Chapter 1')`,
		chapterID, versionID,
	); err != nil {
		t.Fatalf("insert chapter: %v", err)
	}
	lessonID = uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO lessons(id, chapter_id, position, title, required) VALUES ($1, $2, 1, 'Lesson', true)`,
		lessonID, chapterID,
	); err != nil {
		t.Fatalf("insert lesson: %v", err)
	}
	if enroll {
		if _, err := pool.Exec(ctx,
			`INSERT INTO enrollments(user_id, course_id, source) VALUES ($1, $2, 'seed')`,
			userID, courseID,
		); err != nil {
			t.Fatalf("insert enrollment: %v", err)
		}
	}
	return userID, lessonID
}

// TestReportProgress_HappyPath 首次写入 → 200/OK 等价语义（service 返回新 pct）。
func TestReportProgress_HappyPath(t *testing.T) {
	pool := skipIfNoPG(t)
	bootstrapProgressSchema(t, pool)
	userID, lessonID := seedLesson(t, pool, true)

	svc := &Service{pool: pool}
	got, err := svc.ReportProgress(context.Background(), userID, lessonID, 60)
	if err != nil {
		t.Fatalf("ReportProgress: %v", err)
	}
	if got != 60 {
		t.Errorf("got = %d, want 60", got)
	}

	// 再调用 100 → 递增
	got, err = svc.ReportProgress(context.Background(), userID, lessonID, 100)
	if err != nil {
		t.Fatalf("ReportProgress(100): %v", err)
	}
	if got != 100 {
		t.Errorf("got = %d, want 100", got)
	}

	// 同值 → 幂等返回
	got, err = svc.ReportProgress(context.Background(), userID, lessonID, 100)
	if err != nil {
		t.Fatalf("ReportProgress(same): %v", err)
	}
	if got != 100 {
		t.Errorf("got = %d, want 100", got)
	}
}

// TestReportProgress_Regression 倒退拒绝。
func TestReportProgress_Regression(t *testing.T) {
	pool := skipIfNoPG(t)
	bootstrapProgressSchema(t, pool)
	userID, lessonID := seedLesson(t, pool, true)

	svc := &Service{pool: pool}
	if _, err := svc.ReportProgress(context.Background(), userID, lessonID, 80); err != nil {
		t.Fatalf("ReportProgress(80): %v", err)
	}
	_, err := svc.ReportProgress(context.Background(), userID, lessonID, 50)
	if !errors.Is(err, ErrProgressRegression) {
		t.Fatalf("expected ErrProgressRegression, got %v", err)
	}

	// 表里 pct 应仍为 80
	var pct int
	if err := pool.QueryRow(context.Background(),
		`SELECT pct FROM lesson_progress WHERE user_id = $1 AND lesson_id = $2`,
		userID, lessonID,
	).Scan(&pct); err != nil {
		t.Fatalf("select: %v", err)
	}
	if pct != 80 {
		t.Errorf("pct = %d, want 80", pct)
	}
}

// TestReportProgress_NotEnrolled 未购买 → ErrLessonAccessDenied。
func TestReportProgress_NotEnrolled(t *testing.T) {
	pool := skipIfNoPG(t)
	bootstrapProgressSchema(t, pool)
	userID, lessonID := seedLesson(t, pool, false)

	svc := &Service{pool: pool}
	_, err := svc.ReportProgress(context.Background(), userID, lessonID, 50)
	if !errors.Is(err, ErrLessonAccessDenied) {
		t.Fatalf("expected ErrLessonAccessDenied, got %v", err)
	}
}

// TestReportProgress_LessonMissing lesson 不存在 → ErrLessonMissing。
func TestReportProgress_LessonMissing(t *testing.T) {
	pool := skipIfNoPG(t)
	bootstrapProgressSchema(t, pool)

	svc := &Service{pool: pool}
	_, err := svc.ReportProgress(context.Background(), uuid.New(), uuid.New(), 50)
	if !errors.Is(err, ErrLessonMissing) {
		t.Fatalf("expected ErrLessonMissing, got %v", err)
	}
}

// TestReportProgress_InvalidPct 越界值直接被 service 拦截。
func TestReportProgress_InvalidPct(t *testing.T) {
	pool := skipIfNoPG(t)
	bootstrapProgressSchema(t, pool)
	userID, lessonID := seedLesson(t, pool, true)

	svc := &Service{pool: pool}
	if _, err := svc.ReportProgress(context.Background(), userID, lessonID, -1); err == nil {
		t.Fatal("expected error for -1")
	}
	if _, err := svc.ReportProgress(context.Background(), userID, lessonID, 101); err == nil {
		t.Fatal("expected error for 101")
	}
}

// TestProgress_FirstWrite 用例任务里明确指定的「50% 从 0%」首次上报：
// lesson_progress 在调用前为空 → 写完之后恰好 1 行、pct==50。
func TestProgress_FirstWrite(t *testing.T) {
	pool := skipIfNoPG(t)
	bootstrapProgressSchema(t, pool)
	userID, lessonID := seedLesson(t, pool, true)

	// 前置：表里没有任何 lesson_progress 行
	var preCount int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM lesson_progress WHERE user_id = $1 AND lesson_id = $2`,
		userID, lessonID,
	).Scan(&preCount); err != nil {
		t.Fatalf("pre-check: %v", err)
	}
	if preCount != 0 {
		t.Fatalf("expected 0 rows before first write, got %d", preCount)
	}

	svc := &Service{pool: pool}
	got, err := svc.ReportProgress(context.Background(), userID, lessonID, 50)
	if err != nil {
		t.Fatalf("ReportProgress(50): %v", err)
	}
	if got != 50 {
		t.Errorf("returned pct = %d, want 50", got)
	}

	// 表里恰好 1 行 + pct==50
	var (
		rows int
		pct  int
	)
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*), COALESCE(MAX(pct), 0) FROM lesson_progress
		  WHERE user_id = $1 AND lesson_id = $2`,
		userID, lessonID,
	).Scan(&rows, &pct); err != nil {
		t.Fatalf("post-check: %v", err)
	}
	if rows != 1 {
		t.Errorf("expected 1 row after first write, got %d", rows)
	}
	if pct != 50 {
		t.Errorf("stored pct = %d, want 50", pct)
	}
}

// TestProgress_FirstWrite_ThenHigher 首次写 50% → 紧接着上报 80% 成功（递增路径）。
func TestProgress_FirstWrite_ThenHigher(t *testing.T) {
	pool := skipIfNoPG(t)
	bootstrapProgressSchema(t, pool)
	userID, lessonID := seedLesson(t, pool, true)

	svc := &Service{pool: pool}
	if _, err := svc.ReportProgress(context.Background(), userID, lessonID, 50); err != nil {
		t.Fatalf("first write 50: %v", err)
	}
	got, err := svc.ReportProgress(context.Background(), userID, lessonID, 80)
	if err != nil {
		t.Fatalf("second write 80: %v", err)
	}
	if got != 80 {
		t.Errorf("got = %d, want 80", got)
	}
	var pct int
	if err := pool.QueryRow(context.Background(),
		`SELECT pct FROM lesson_progress WHERE user_id = $1 AND lesson_id = $2`,
		userID, lessonID,
	).Scan(&pct); err != nil {
		t.Fatalf("select: %v", err)
	}
	if pct != 80 {
		t.Errorf("stored pct = %d, want 80", pct)
	}
}

// TestProgress_RegressionRejected 任务里明确指定的「倒退拒绝」：
// 两次显式断言——错误哨兵 + 数据库里 pct 不变。
func TestProgress_RegressionRejected(t *testing.T) {
	pool := skipIfNoPG(t)
	bootstrapProgressSchema(t, pool)
	userID, lessonID := seedLesson(t, pool, true)

	svc := &Service{pool: pool}
	if _, err := svc.ReportProgress(context.Background(), userID, lessonID, 80); err != nil {
		t.Fatalf("seed write 80: %v", err)
	}

	// 倒退 → 必须 ErrProgressRegression
	got, err := svc.ReportProgress(context.Background(), userID, lessonID, 50)
	if !errors.Is(err, ErrProgressRegression) {
		t.Fatalf("expected ErrProgressRegression, got %v (pct=%d)", err, got)
	}
	// service 约定：回归时返回 current pct（便于前端回显最新值）
	if got != 80 {
		t.Errorf("regression returned pct = %d, want 80 (current)", got)
	}

	// 数据库里 pct 仍为 80（rollback 等价语义）
	var pct int
	if err := pool.QueryRow(context.Background(),
		`SELECT pct FROM lesson_progress WHERE user_id = $1 AND lesson_id = $2`,
		userID, lessonID,
	).Scan(&pct); err != nil {
		t.Fatalf("select: %v", err)
	}
	if pct != 80 {
		t.Errorf("pct after regression = %d, want 80 (unchanged)", pct)
	}
}

// TestProgress_IdempotentSameValue 任务里明确指定的「同值幂等」：
// 第二次写同 pct → 200 (无报错) + pct 不变 + updated_at 刷新（轻量 touch）。
func TestProgress_IdempotentSameValue(t *testing.T) {
	pool := skipIfNoPG(t)
	bootstrapProgressSchema(t, pool)
	userID, lessonID := seedLesson(t, pool, true)

	svc := &Service{pool: pool}
	if _, err := svc.ReportProgress(context.Background(), userID, lessonID, 50); err != nil {
		t.Fatalf("first write 50: %v", err)
	}

	// 取第一次写后的 updated_at
	var firstUpdatedAt time.Time
	if err := pool.QueryRow(context.Background(),
		`SELECT updated_at FROM lesson_progress WHERE user_id = $1 AND lesson_id = $2`,
		userID, lessonID,
	).Scan(&firstUpdatedAt); err != nil {
		t.Fatalf("select first updated_at: %v", err)
	}

	// 睡 5ms 让时间戳自然前进（now() 通常 μs 精度，等 1 tick 一般足够）
	time.Sleep(5 * time.Millisecond)

	got, err := svc.ReportProgress(context.Background(), userID, lessonID, 50)
	if err != nil {
		t.Fatalf("idempotent same-value write: %v", err)
	}
	if got != 50 {
		t.Errorf("got = %d, want 50 (idempotent)", got)
	}

	var (
		rows      int
		pct       int
		updatedAt time.Time
	)
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*), MAX(pct), MAX(updated_at) FROM lesson_progress
		  WHERE user_id = $1 AND lesson_id = $2`,
		userID, lessonID,
	).Scan(&rows, &pct, &updatedAt); err != nil {
		t.Fatalf("post-check: %v", err)
	}
	if rows != 1 {
		t.Errorf("expected 1 row, got %d", rows)
	}
	if pct != 50 {
		t.Errorf("pct = %d, want 50", pct)
	}
	if !updatedAt.After(firstUpdatedAt) {
		t.Errorf("updated_at must advance on same-value write (pre=%v post=%v)",
			firstUpdatedAt, updatedAt)
	}
}