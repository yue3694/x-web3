package learning

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func skipIfNoPGQueries(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := strings.TrimSpace(os.Getenv("DATABASE_URL_TEST"))
	if url == "" {
		t.Skip("set DATABASE_URL_TEST to enable queries integration tests")
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

func bootstrapQueriesSchema(t *testing.T, pool *pgxpool.Pool) {
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
		`CREATE TABLE IF NOT EXISTS course_completions (
			id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			enrollment_id uuid NOT NULL REFERENCES enrollments(id) ON DELETE CASCADE,
			rule_version integer NOT NULL DEFAULT 1,
			completed_at timestamptz NOT NULL DEFAULT now(),
			revoked_at timestamptz,
			CONSTRAINT course_completions_rule_version_chk CHECK (rule_version > 0),
			UNIQUE (enrollment_id, rule_version)
		)`,
		`CREATE TABLE IF NOT EXISTS certificates (
			id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			completion_id uuid NOT NULL UNIQUE REFERENCES course_completions(id) ON DELETE RESTRICT,
			user_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
			course_id uuid NOT NULL REFERENCES courses(id) ON DELETE RESTRICT,
			cert_version integer NOT NULL DEFAULT 1,
			certificate_id numeric(78, 0) NOT NULL,
			recipient_wallet text NOT NULL,
			metadata_uri text NOT NULL,
			metadata_sha256 text NOT NULL,
			chain_id bigint NOT NULL,
			status text NOT NULL DEFAULT 'pending',
			tx_hash bytea,
			token_id numeric(78, 0),
			confirmed_block bigint,
			confirmed_at timestamptz,
			attempts integer NOT NULL DEFAULT 0,
			last_error text,
			created_at timestamptz NOT NULL DEFAULT now(),
			updated_at timestamptz NOT NULL DEFAULT now(),
			CONSTRAINT certificates_status_chk CHECK (status IN ('pending','minting','confirmed','failed','dead')),
			CONSTRAINT certificates_recipient_chk CHECK (recipient_wallet ~ '^0x[a-fA-F0-9]{40}$'),
			CONSTRAINT certificates_cert_id_chk CHECK (certificate_id >= 0),
			UNIQUE (user_id, course_id, cert_version)
		)`,
		`CREATE TABLE IF NOT EXISTS certificate_jobs (
			id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			certificate_id uuid NOT NULL UNIQUE REFERENCES certificates(id) ON DELETE CASCADE,
			status text NOT NULL DEFAULT 'pending',
			attempt integer NOT NULL DEFAULT 0,
			last_error text,
			next_retry_at timestamptz NOT NULL DEFAULT now(),
			started_at timestamptz,
			confirmed_at timestamptz,
			tx_hash bytea,
			created_at timestamptz NOT NULL DEFAULT now(),
			updated_at timestamptz NOT NULL DEFAULT now(),
			CONSTRAINT certificate_jobs_status_chk CHECK (status IN ('pending','minting','confirmed','failed','dead'))
		)`,
	}
	for _, s := range stmts {
		if _, err := pool.Exec(ctx, s); err != nil {
			t.Fatalf("schema bootstrap: %v\nSQL: %s", err, s)
		}
	}
}

// seedCourseWithLessons 为 userID 建一个 course，包含 requiredTotal 个必修课时。
func seedCourseWithLessons(t *testing.T, pool *pgxpool.Pool, userID uuid.UUID, requiredTotal int) (courseID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	teacherID := uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users(id, privy_user_id, status) VALUES ($1, $2, 'active')`,
		teacherID, "did:t-"+teacherID.String(),
	); err != nil {
		t.Fatalf("insert teacher: %v", err)
	}
	courseID = uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO courses(id, teacher_id, slug, title, status) VALUES ($1, $2, $3, 'Test', 'published')`,
		courseID, teacherID, "tc-"+courseID.String(),
	); err != nil {
		t.Fatalf("insert course: %v", err)
	}
	var versionID uuid.UUID
	if err := pool.QueryRow(ctx,
		`INSERT INTO course_versions(course_id, version) VALUES ($1, 1) RETURNING id`,
		courseID,
	).Scan(&versionID); err != nil {
		t.Fatalf("insert version: %v", err)
	}
	chapterID := uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO chapters(id, course_version_id, position, title) VALUES ($1, $2, 1, 'Chapter')`,
		chapterID, versionID,
	); err != nil {
		t.Fatalf("insert chapter: %v", err)
	}
	for i := 1; i <= requiredTotal; i++ {
		if _, err := pool.Exec(ctx,
			`INSERT INTO lessons(id, chapter_id, position, title, required) VALUES ($1, $2, $3, $4, true)`,
			uuid.New(), chapterID, i, "Lesson",
		); err != nil {
			t.Fatalf("insert lesson %d: %v", i, err)
		}
	}
	return courseID
}

// TestListEnrollments_Empty 当前 user 无 enrollment。
func TestListEnrollments_Empty(t *testing.T) {
	pool := skipIfNoPGQueries(t)
	bootstrapQueriesSchema(t, pool)

	svc := &Service{pool: pool}
	items, err := svc.ListEnrollments(context.Background(), uuid.New(), 50)
	if err != nil {
		t.Fatalf("ListEnrollments: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("expected 0 items, got %d", len(items))
	}
}

// TestListEnrollments_Multi 多 enrollment + 进度统计。
func TestListEnrollments_Multi(t *testing.T) {
	pool := skipIfNoPGQueries(t)
	bootstrapQueriesSchema(t, pool)
	ctx := context.Background()

	userID := uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users(id, privy_user_id, status) VALUES ($1, $2, 'active')`,
		userID, "did:u-"+userID.String(),
	); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	// 课程 A：3 必修，完成 2
	cA := seedCourseWithLessons(t, pool, userID, 3)
	if _, err := pool.Exec(ctx,
		`INSERT INTO enrollments(user_id, course_id, source) VALUES ($1, $2, 'seed')`,
		userID, cA,
	); err != nil {
		t.Fatalf("enroll A: %v", err)
	}
	// 给课程 A 前 2 个 lesson 写 100%
	rows, err := pool.Query(ctx, `SELECT id FROM lessons WHERE chapter_id IN
		(SELECT id FROM chapters WHERE course_version_id IN
		 (SELECT id FROM course_versions WHERE course_id = $1))`, cA)
	if err != nil {
		t.Fatalf("list lessons A: %v", err)
	}
	var lessonsA []uuid.UUID
	for rows.Next() {
		var l uuid.UUID
		if err := rows.Scan(&l); err != nil {
			t.Fatalf("scan lesson: %v", err)
		}
		lessonsA = append(lessonsA, l)
	}
	rows.Close()
	for i := 0; i < 2 && i < len(lessonsA); i++ {
		if _, err := pool.Exec(ctx,
			`INSERT INTO lesson_progress(user_id, lesson_id, pct) VALUES ($1, $2, 100)`,
			userID, lessonsA[i],
		); err != nil {
			t.Fatalf("lesson_progress: %v", err)
		}
	}

	// 课程 B：1 必修，未开始
	cB := seedCourseWithLessons(t, pool, userID, 1)
	if _, err := pool.Exec(ctx,
		`INSERT INTO enrollments(user_id, course_id, source) VALUES ($1, $2, 'seed')`,
		userID, cB,
	); err != nil {
		t.Fatalf("enroll B: %v", err)
	}

	svc := &Service{pool: pool}
	items, err := svc.ListEnrollments(ctx, userID, 50)
	if err != nil {
		t.Fatalf("ListEnrollments: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 enrollments, got %d", len(items))
	}

	// 顺序：enrolled_at DESC；cB 晚于 cA，应当在 items[0]。
	var gotA, gotB bool
	for _, it := range items {
		switch it.CourseID {
		case cA:
			gotA = true
			if it.RequiredTotal != 3 || it.CompletedTotal != 2 {
				t.Errorf("A: required=%d completed=%d, want 3/2", it.RequiredTotal, it.CompletedTotal)
			}
			if it.CompletionPct != 66 {
				t.Errorf("A: pct=%d, want 66", it.CompletionPct)
			}
		case cB:
			gotB = true
			if it.RequiredTotal != 1 || it.CompletedTotal != 0 {
				t.Errorf("B: required=%d completed=%d, want 1/0", it.RequiredTotal, it.CompletedTotal)
			}
			if it.CompletionPct != 0 {
				t.Errorf("B: pct=%d, want 0", it.CompletionPct)
			}
		}
	}
	if !gotA || !gotB {
		t.Errorf("missing A=%v B=%v", gotA, gotB)
	}
}

// TestListEnrollments_WithCompletion 有 completion 行时填充 CompletedAt。
func TestListEnrollments_WithCompletion(t *testing.T) {
	pool := skipIfNoPGQueries(t)
	bootstrapQueriesSchema(t, pool)
	ctx := context.Background()

	userID := uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users(id, privy_user_id, status) VALUES ($1, $2, 'active')`,
		userID, "did:u-"+userID.String(),
	); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	c := seedCourseWithLessons(t, pool, userID, 1)
	enrollmentID := uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO enrollments(id, user_id, course_id, source) VALUES ($1, $2, $3, 'seed')`,
		enrollmentID, userID, c,
	); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO course_completions(enrollment_id, rule_version) VALUES ($1, 1)`,
		enrollmentID,
	); err != nil {
		t.Fatalf("completion: %v", err)
	}
	svc := &Service{pool: pool}
	items, err := svc.ListEnrollments(ctx, userID, 50)
	if err != nil {
		t.Fatalf("ListEnrollments: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1, got %d", len(items))
	}
	if !items[0].HasCompletion || items[0].CompletedAt == nil {
		t.Errorf("expected completion set, got %+v", items[0])
	}
}

// TestListCertificates_Empty 当前 user 无 job。
func TestListCertificates_Empty(t *testing.T) {
	pool := skipIfNoPGQueries(t)
	bootstrapQueriesSchema(t, pool)

	svc := &Service{pool: pool}
	items, err := svc.ListCertificates(context.Background(), uuid.New(), 50)
	if err != nil {
		t.Fatalf("ListCertificates: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("expected 0, got %d", len(items))
	}
}

// TestListCertificates_Single 一条 cert + job。
func TestListCertificates_Single(t *testing.T) {
	pool := skipIfNoPGQueries(t)
	bootstrapQueriesSchema(t, pool)
	ctx := context.Background()

	userID := uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users(id, privy_user_id, status) VALUES ($1, $2, 'active')`,
		userID, "did:u-"+userID.String(),
	); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	c := seedCourseWithLessons(t, pool, userID, 1)
	certRowID := uuid.New()
	if _, err := pool.Exec(ctx, `
INSERT INTO certificates(id, completion_id, user_id, course_id, cert_version,
                         certificate_id, recipient_wallet, metadata_uri, metadata_sha256,
                         chain_id, status)
SELECT $1, cc.id, $2, $3, 1, 12345, '0x70997970C51812dc3A010C7d01b50e0d17dc79C8',
       'ipfs://x', 'deadbeef', 11155111, 'pending'
  FROM course_completions cc
  JOIN enrollments e ON e.id = cc.enrollment_id
 WHERE e.user_id = $2 AND e.course_id = $3`,
		certRowID, userID, c,
	); err != nil {
		t.Fatalf("insert certificate: %v", err)
	}
	// 准备 course_completion 行（completion_id 必须存在）
	enrollmentID := uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO enrollments(id, user_id, course_id, source) VALUES ($1, $2, $3, 'seed')
		 ON CONFLICT (user_id, course_id) DO UPDATE SET source = EXCLUDED.source`,
		enrollmentID, userID, c,
	); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	completionID := uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO course_completions(id, enrollment_id, rule_version) VALUES ($1, $2, 1)`,
		completionID, enrollmentID,
	); err != nil {
		t.Fatalf("insert completion: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE certificates SET completion_id = $1 WHERE id = $2`,
		completionID, certRowID,
	); err != nil {
		t.Fatalf("link cert to completion: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO certificate_jobs(certificate_id, status, attempt) VALUES ($1, 'pending', 0)`,
		certRowID,
	); err != nil {
		t.Fatalf("insert job: %v", err)
	}
	svc := &Service{pool: pool}
	items, err := svc.ListCertificates(ctx, userID, 50)
	if err != nil {
		t.Fatalf("ListCertificates: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1, got %d", len(items))
	}
	if items[0].Status != "pending" {
		t.Errorf("status = %s, want pending", items[0].Status)
	}
	if items[0].CourseID != c {
		t.Errorf("course = %s, want %s", items[0].CourseID, c)
	}
}

// TestListEnrollments_LimitClamp limit 越界被钳制到 50（无报错）。
func TestListEnrollments_LimitClamp(t *testing.T) {
	pool := skipIfNoPGQueries(t)
	bootstrapQueriesSchema(t, pool)
	ctx := context.Background()
	userID := uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users(id, privy_user_id, status) VALUES ($1, $2, 'active')`,
		userID, "did:u-"+userID.String(),
	); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	svc := &Service{pool: pool}
	// limit=0 → 回退 50；limit=999 → 回退 50
	if _, err := svc.ListEnrollments(ctx, userID, 0); err != nil {
		t.Fatalf("ListEnrollments(0): %v", err)
	}
	if _, err := svc.ListEnrollments(ctx, userID, 999); err != nil {
		t.Fatalf("ListEnrollments(999): %v", err)
	}
}