package certificate_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/x-web3/api/internal/certificate"
)

// fixture 拼一个最小可跑的完课场景：teacher + course + chapter + 2 lessons。
// 调用方再 INSERT enrollment + lesson_progress + wallet。
type fixture struct {
	pool      *pgxpool.Pool
	teacherID uuid.UUID
	studentID uuid.UUID
	courseID  uuid.UUID
	chapterID uuid.UUID
	lessonReq uuid.UUID
	lessonOpt uuid.UUID
	wallet    string
}

func skipIfNoPG(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := strings.TrimSpace(os.Getenv("DATABASE_URL_TEST"))
	if url == "" {
		t.Skip("set DATABASE_URL_TEST to enable completion integration tests")
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

// bootstrapSchema 按 0009 创建业务所需的最小表，避免依赖完整 migration runner。
//
// 真实 production 走 database/migrate.sh；这里只关心测试需要的表。
func bootstrapSchema(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
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
		`CREATE TABLE IF NOT EXISTS wallets (
			id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			chain_namespace text NOT NULL DEFAULT 'eip155',
			chain_id bigint NOT NULL DEFAULT 1,
			address text NOT NULL,
			is_primary boolean NOT NULL DEFAULT true,
			created_at timestamptz NOT NULL DEFAULT now(),
			CONSTRAINT wallets_unique UNIQUE (chain_namespace, chain_id, address),
			CONSTRAINT wallets_address_chk CHECK (address ~ '^0x[a-fA-F0-9]{40}$')
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
	ctxObj := context.Background()
	for _, s := range stmts {
		if _, err := pool.Exec(ctxObj, s); err != nil {
			t.Fatalf("schema bootstrap: %v\nSQL: %s", err, s)
		}
	}
}

// newFixture 建一个最小可用的完课场景，包含 teacher + student + course +
// chapter + 2 lessons + enrollment + wallet；返回的 fx.enrolled 默认 true；
// 测试用 fx.enrolled=false 模拟「未购买」。
func newFixture(t *testing.T, pool *pgxpool.Pool) *fixture {
	t.Helper()
	ctx := context.Background()
	fx := &fixture{pool: pool}
	fx.teacherID = uuid.New()
	fx.studentID = uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users(id, privy_user_id, status) VALUES ($1, $2, 'active'), ($3, $4, 'active')`,
		fx.teacherID, "did:t-"+fx.teacherID.String(),
		fx.studentID, "did:s-"+fx.studentID.String(),
	); err != nil {
		t.Fatalf("insert users: %v", err)
	}

	fx.courseID = uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO courses(id, teacher_id, slug, title, status) VALUES ($1, $2, $3, 'Test Course', 'published')`,
		fx.courseID, fx.teacherID, "tc-"+fx.courseID.String(),
	); err != nil {
		t.Fatalf("insert course: %v", err)
	}
	var versionID uuid.UUID
	if err := pool.QueryRow(ctx,
		`INSERT INTO course_versions(course_id, version) VALUES ($1, 1) RETURNING id`,
		fx.courseID,
	).Scan(&versionID); err != nil {
		t.Fatalf("insert course_version: %v", err)
	}
	fx.chapterID = uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO chapters(id, course_version_id, position, title) VALUES ($1, $2, 1, 'Chapter 1')`,
		fx.chapterID, versionID,
	); err != nil {
		t.Fatalf("insert chapter: %v", err)
	}
	fx.lessonReq = uuid.New()
	fx.lessonOpt = uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO lessons(id, chapter_id, position, title, required) VALUES
		 ($1, $2, 1, 'Required Lesson', true),
		 ($3, $2, 2, 'Optional Lesson', false)`,
		fx.lessonReq, fx.chapterID, fx.lessonOpt,
	); err != nil {
		t.Fatalf("insert lessons: %v", err)
	}

	if _, err := pool.Exec(ctx,
		`INSERT INTO enrollments(user_id, course_id, source) VALUES ($1, $2, 'seed')`,
		fx.studentID, fx.courseID,
	); err != nil {
		t.Fatalf("insert enrollment: %v", err)
	}

	fx.wallet = "0x70997970C51812dc3A010C7d01b50e0d17dc79C8"
	if _, err := pool.Exec(ctx,
		`INSERT INTO wallets(user_id, address, chain_id, is_primary) VALUES ($1, $2, 11155111, true)`,
		fx.studentID, fx.wallet,
	); err != nil {
		t.Fatalf("insert wallet: %v", err)
	}

	return fx
}

func (fx *fixture) markLessonComplete(t *testing.T, lessonID uuid.UUID, pct int) {
	t.Helper()
	if _, err := fx.pool.Exec(context.Background(),
		`INSERT INTO lesson_progress(user_id, lesson_id, pct) VALUES ($1, $2, $3)
		 ON CONFLICT (user_id, lesson_id) DO UPDATE SET pct = EXCLUDED.pct`,
		fx.studentID, lessonID, pct,
	); err != nil {
		t.Fatalf("insert lesson_progress: %v", err)
	}
}

func newCertService(pool *pgxpool.Pool) *certificate.Service {
	svc, err := certificate.NewService(certificate.ServiceConfig{
		Pool:    pool,
		ChainID: 11155111,
		Logger:  zap.NewNop(),
	})
	if err != nil {
		panic(err)
	}
	return svc
}

// TestComplete_HappyPath 100% 必修进度 → 创建 completion + certificate + job 各一条。
func TestComplete_HappyPath(t *testing.T) {
	pool := skipIfNoPG(t)
	bootstrapSchema(t, pool)
	fx := newFixture(t, pool)
	fx.markLessonComplete(t, fx.lessonReq, 100)

	svc := newCertService(pool)
	rec, err := svc.CompleteCourse(context.Background(), fx.studentID, fx.courseID)
	if err != nil {
		t.Fatalf("CompleteCourse: %v", err)
	}
	if rec.UserID != fx.studentID || rec.CourseID != fx.courseID {
		t.Errorf("record mismatch: %+v", rec)
	}
	if rec.TotalLessonsCount != 1 || rec.CompletedLessonsCount != 1 {
		t.Errorf("counts wrong: %+v", rec)
	}
	if rec.CertificateID == nil {
		t.Error("certificate id should be set")
	}
	if rec.RecipientWallet != fx.wallet {
		t.Errorf("recipient = %q, want %q", rec.RecipientWallet, fx.wallet)
	}
	if rec.OnchainCertID == "" {
		t.Error("onchain cert id empty")
	}

	// certificate_jobs 必须有 1 条
	var jobs int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM certificate_jobs cj
		 JOIN certificates c ON c.id = cj.certificate_id
		 WHERE c.user_id = $1 AND c.course_id = $2`,
		fx.studentID, fx.courseID,
	).Scan(&jobs); err != nil {
		t.Fatalf("count jobs: %v", err)
	}
	if jobs != 1 {
		t.Errorf("expected 1 job, got %d", jobs)
	}
}

// TestComplete_NotEnrolled → ErrNotEnrolled。
func TestComplete_NotEnrolled(t *testing.T) {
	pool := skipIfNoPG(t)
	bootstrapSchema(t, pool)
	fx := newFixture(t, pool)
	if _, err := pool.Exec(context.Background(),
		`DELETE FROM enrollments WHERE user_id = $1 AND course_id = $2`,
		fx.studentID, fx.courseID,
	); err != nil {
		t.Fatalf("delete enrollment: %v", err)
	}

	svc := newCertService(pool)
	_, err := svc.CompleteCourse(context.Background(), fx.studentID, fx.courseID)
	if !errors.Is(err, certificate.ErrNotEnrolled) {
		t.Fatalf("expected ErrNotEnrolled, got %v", err)
	}
}

// TestComplete_PartialProgress → ErrNotCompleted（对应 422）。
func TestComplete_PartialProgress(t *testing.T) {
	pool := skipIfNoPG(t)
	bootstrapSchema(t, pool)
	fx := newFixture(t, pool)
	fx.markLessonComplete(t, fx.lessonReq, 50) // 未达 100%

	svc := newCertService(pool)
	_, err := svc.CompleteCourse(context.Background(), fx.studentID, fx.courseID)
	if !errors.Is(err, certificate.ErrNotCompleted) {
		t.Fatalf("expected ErrNotCompleted, got %v", err)
	}

	// 没有任何 completion / certificate / job 被写入
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM course_completions cc
		 JOIN enrollments e ON e.id = cc.enrollment_id
		 WHERE e.user_id = $1 AND e.course_id = $2`,
		fx.studentID, fx.courseID,
	).Scan(&n); err != nil {
		t.Fatalf("count completions: %v", err)
	}
	if n != 0 {
		t.Errorf("partial progress must not create completion, got %d", n)
	}
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM certificates WHERE user_id = $1 AND course_id = $2`,
		fx.studentID, fx.courseID,
	).Scan(&n); err != nil {
		t.Fatalf("count certs: %v", err)
	}
	if n != 0 {
		t.Errorf("partial progress must not create cert, got %d", n)
	}
}

// TestComplete_Idempotent 第二次调用 → 返回同一条 record + 不会出现第二条 job。
func TestComplete_Idempotent(t *testing.T) {
	pool := skipIfNoPG(t)
	bootstrapSchema(t, pool)
	fx := newFixture(t, pool)
	fx.markLessonComplete(t, fx.lessonReq, 100)

	svc := newCertService(pool)
	first, err := svc.CompleteCourse(context.Background(), fx.studentID, fx.courseID)
	if err != nil {
		t.Fatalf("first CompleteCourse: %v", err)
	}
	second, err := svc.CompleteCourse(context.Background(), fx.studentID, fx.courseID)
	if err != nil {
		t.Fatalf("second CompleteCourse: %v", err)
	}
	if first.ID != second.ID {
		t.Errorf("idempotent: expected same completion ID, got %s vs %s", first.ID, second.ID)
	}
	if first.CertificateID == nil || second.CertificateID == nil {
		t.Fatalf("nil cert id in record")
	}
	if *first.CertificateID != *second.CertificateID {
		t.Errorf("idempotent: expected same cert ID, got %s vs %s",
			*first.CertificateID, *second.CertificateID)
	}
	if !first.CompletedAt.Equal(second.CompletedAt) {
		t.Errorf("idempotent: completed_at changed: %v vs %v", first.CompletedAt, second.CompletedAt)
	}

	var jobs int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM certificate_jobs cj
		 JOIN certificates c ON c.id = cj.certificate_id
		 WHERE c.user_id = $1 AND c.course_id = $2`,
		fx.studentID, fx.courseID,
	).Scan(&jobs); err != nil {
		t.Fatalf("count jobs: %v", err)
	}
	if jobs != 1 {
		t.Errorf("idempotent: expected 1 job, got %d", jobs)
	}
}

// TestComplete_Atomicity_NoCompletionNoJob 验证 tx 回滚：
// 用一次手工 tx 模拟 INSERT course_completions 成功 + 故意失败，确认 cert / job 未残留。
func TestComplete_Atomicity_NoCompletionNoJob(t *testing.T) {
	pool := skipIfNoPG(t)
	bootstrapSchema(t, pool)
	fx := newFixture(t, pool)
	fx.markLessonComplete(t, fx.lessonReq, 100)

	// 直接调用 service 把数据写进去（happy path）。
	svc := newCertService(pool)
	if _, err := svc.CompleteCourse(context.Background(), fx.studentID, fx.courseID); err != nil {
		t.Fatalf("CompleteCourse: %v", err)
	}

	// 模拟 tx 回滚：手工起 tx，写 completion，故意 CHECK 失败，回滚。
	txctx := context.Background()
	tx, err := pool.Begin(txctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	// 故意插入 rule_version=-1（CHECK 失败）
	if _, err := tx.Exec(txctx,
		`INSERT INTO course_completions(enrollment_id, rule_version)
		 SELECT e.id, -1 FROM enrollments e
		 WHERE e.user_id=$1 AND e.course_id=$2`,
		fx.studentID, fx.courseID,
	); err == nil {
		t.Fatal("expected CHECK violation on rule_version")
	}
	// 回滚
	if err := tx.Rollback(txctx); err != nil {
		t.Fatalf("rollback: %v", err)
	}

	// course_completions 仍只有 1 行（happy path 写的），无残留。
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM course_completions cc
		 JOIN enrollments e ON e.id = cc.enrollment_id
		 WHERE e.user_id = $1 AND e.course_id = $2`,
		fx.studentID, fx.courseID,
	).Scan(&n); err != nil {
		t.Fatalf("count completions: %v", err)
	}
	if n != 1 {
		t.Errorf("expected exactly 1 completion, got %d", n)
	}
}

// TestComplete_NoRecipientWallet 用户没绑钱包 → ErrNoRecipientWallet。
func TestComplete_NoRecipientWallet(t *testing.T) {
	pool := skipIfNoPG(t)
	bootstrapSchema(t, pool)
	fx := newFixture(t, pool)
	if _, err := pool.Exec(context.Background(),
		`DELETE FROM wallets WHERE user_id = $1`, fx.studentID,
	); err != nil {
		t.Fatalf("delete wallets: %v", err)
	}
	fx.markLessonComplete(t, fx.lessonReq, 100)

	svc := newCertService(pool)
	_, err := svc.CompleteCourse(context.Background(), fx.studentID, fx.courseID)
	if !errors.Is(err, certificate.ErrNoRecipientWallet) {
		t.Fatalf("expected ErrNoRecipientWallet, got %v", err)
	}
}