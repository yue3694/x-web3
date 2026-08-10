package learning

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

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