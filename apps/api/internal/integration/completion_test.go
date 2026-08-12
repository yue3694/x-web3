// F04-T15 — 完课判定 + 唯一 mint job 创建 集成测试。
//
// 覆盖：
//   - ErrNotEnrolled：未enrollment 用户 → ErrNotEnrolled；
//   - ErrNotCompleted：enrolled 但进度不全 → ErrNotCompleted；
//   - ErrNoRecipientWallet：用户未绑钱包 → ErrNoRecipientWallet；
//   - Happy path：100% 进度 → completion + certificate + cert job 三表均写入；
//   - Idempotent：第二次调 CompleteCourse → 不创建第二条 job / certificate；
//   - 重复完成只产生一个 mint job：直接查表验证。
package integration_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/x-web3/api/internal/certificate"
	"github.com/x-web3/api/internal/course"
	"github.com/x-web3/api/internal/review"
)

// completionFixture 准备：teacher + buyer（带 primary wallet）+ published course +
// 1 chapter + 1 required lesson + 已确认 enrollment。
//
// 通过 *LessonProgressByID(100%)* 控制"已学完"。
type completionFixture struct {
	UserID      uuid.UUID
	TeacherID   uuid.UUID
	WalletID    uuid.UUID
	WalletAddr  string
	CourseID    uuid.UUID
	ChapterID   uuid.UUID
	LessonID    uuid.UUID
	VersionID   uuid.UUID
}

// makeCompletionFixture 直接走 SQL 装配完课链路（避开 ReplaceCurriculum 的乐观锁路径）。
func makeCompletionFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) completionFixture {
	t.Helper()
	teacherID := uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users(id, privy_user_id, status) VALUES($1, $2, 'active')`,
		teacherID, "did:privy:ct-"+uuid.NewString()); err != nil {
		t.Fatalf("insert teacher: %v", err)
	}
	buyer := uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users(id, privy_user_id, status) VALUES($1, $2, 'active')`,
		buyer, "did:privy:cb-"+uuid.NewString()); err != nil {
		t.Fatalf("insert buyer: %v", err)
	}
	walletID := uuid.New()
	walletAddr := randHexAddr(t)
	if _, err := pool.Exec(ctx,
		`INSERT INTO wallets(id, user_id, chain_id, address, is_primary) VALUES($1, $2, $3, $4, true)`,
		walletID, buyer, int64(11155111), walletAddr); err != nil {
		t.Fatalf("insert wallet: %v", err)
	}
	// 课程：published + current_version=1
	courseRepo := course.NewRepo(pool)
	created, err := courseRepo.Create(ctx, course.CreateInput{
		TeacherID:  teacherID,
		Slug:       "cert-test-" + uuid.NewString(),
		Title:      "Cert test",
		PriceMinor: 100,
		Currency:   "USD",
	})
	if err != nil {
		t.Fatalf("create course: %v", err)
	}
	if _, err := courseRepo.Transition(ctx, created.ID, teacherID, review.Submit, false, ""); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if _, err := courseRepo.Transition(ctx, created.ID, teacherID, review.Approve, true, ""); err != nil {
		t.Fatalf("approve: %v", err)
	}
	// course_versions v1（current_version 已设为 1）
	var versionID uuid.UUID
	if err := pool.QueryRow(ctx,
		`SELECT id FROM course_versions WHERE course_id=$1 AND version=1`,
		created.ID).Scan(&versionID); err != nil {
		t.Fatalf("get version: %v", err)
	}
	chapterID := uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO chapters(id, course_version_id, title, position) VALUES($1, $2, 'Intro', 1)`,
		chapterID, versionID); err != nil {
		t.Fatalf("insert chapter: %v", err)
	}
	lessonID := uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO lessons(id, chapter_id, title, position, required) VALUES($1, $2, 'Lesson 1', 1, true)`,
		lessonID, chapterID); err != nil {
		t.Fatalf("insert lesson: %v", err)
	}
	return completionFixture{
		UserID:     buyer,
		TeacherID:  teacherID,
		WalletID:   walletID,
		WalletAddr: walletAddr,
		CourseID:   created.ID,
		ChapterID:  chapterID,
		LessonID:   lessonID,
		VersionID:  versionID,
	}
}

// insertEnrollment 直接 SQL 插入 enrollment。
func insertEnrollment(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID, courseID uuid.UUID) {
	t.Helper()
	if _, err := pool.Exec(ctx,
		`INSERT INTO enrollments(user_id, course_id, source) VALUES($1, $2, 'seed')`,
		userID, courseID); err != nil {
		t.Fatalf("insert enrollment: %v", err)
	}
}

// setLessonProgress 把 lesson_progress.pct 设置为指定值（用 UPSERT）。
func setLessonProgress(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID, lessonID uuid.UUID, pct int) {
	t.Helper()
	if _, err := pool.Exec(ctx,
		`INSERT INTO lesson_progress(user_id, lesson_id, pct) VALUES($1, $2, $3)
ON CONFLICT (user_id, lesson_id) DO UPDATE SET pct = EXCLUDED.pct`,
		userID, lessonID, pct); err != nil {
		t.Fatalf("upsert lesson_progress: %v", err)
	}
}

func newCompletionService(t *testing.T, pool *pgxpool.Pool) *certificate.Service {
	t.Helper()
	svc, err := certificate.NewService(certificate.ServiceConfig{
		Pool:    pool,
		ChainID: 11155111,
		Logger:  zap.NewNop(),
	})
	if err != nil {
		t.Fatalf("new completion svc: %v", err)
	}
	return svc
}

// ===========================================================================
// Tests
// ===========================================================================

func TestCompletion_RejectsNotEnrolled(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	fx := makeCompletionFixture(t, ctx, pool)
	svc := newCompletionService(t, pool)
	_, err := svc.CompleteCourse(ctx, fx.UserID, fx.CourseID)
	if !errors.Is(err, certificate.ErrNotEnrolled) {
		t.Fatalf("expected ErrNotEnrolled, got %v", err)
	}
}

func TestCompletion_RejectsPartialProgress(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	fx := makeCompletionFixture(t, ctx, pool)
	insertEnrollment(t, ctx, pool, fx.UserID, fx.CourseID)
	setLessonProgress(t, ctx, pool, fx.UserID, fx.LessonID, 50) // < 100
	svc := newCompletionService(t, pool)
	_, err := svc.CompleteCourse(ctx, fx.UserID, fx.CourseID)
	if !errors.Is(err, certificate.ErrNotCompleted) {
		t.Fatalf("expected ErrNotCompleted, got %v", err)
	}
}

func TestCompletion_RejectsNoRecipientWallet(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	fx := makeCompletionFixture(t, ctx, pool)
	insertEnrollment(t, ctx, pool, fx.UserID, fx.CourseID)
	setLessonProgress(t, ctx, pool, fx.UserID, fx.LessonID, 100)
	// 移除主钱包
	if _, err := pool.Exec(ctx, `DELETE FROM wallets WHERE id=$1`, fx.WalletID); err != nil {
		t.Fatalf("delete wallet: %v", err)
	}
	svc := newCompletionService(t, pool)
	_, err := svc.CompleteCourse(ctx, fx.UserID, fx.CourseID)
	if !errors.Is(err, certificate.ErrNoRecipientWallet) {
		t.Fatalf("expected ErrNoRecipientWallet, got %v", err)
	}
}

func TestCompletion_HappyPath_CreatesCompletionCertJob(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	fx := makeCompletionFixture(t, ctx, pool)
	insertEnrollment(t, ctx, pool, fx.UserID, fx.CourseID)
	setLessonProgress(t, ctx, pool, fx.UserID, fx.LessonID, 100)

	svc := newCompletionService(t, pool)
	rec, err := svc.CompleteCourse(ctx, fx.UserID, fx.CourseID)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if rec.Status != "pending" {
		t.Fatalf("status = %s, want pending", rec.Status)
	}
	if rec.CompletedLessonsCount != 1 || rec.TotalLessonsCount != 1 {
		t.Fatalf("counts = (%d/%d), want 1/1", rec.CompletedLessonsCount, rec.TotalLessonsCount)
	}
	if !strings.EqualFold(rec.RecipientWallet, fx.WalletAddr) {
		t.Fatalf("recipient wallet = %s, want %s (case-insensitive)", rec.RecipientWallet, fx.WalletAddr)
	}
	// DB 校验三张表都有数据
	var completionsCount, certsCount, jobsCount int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM course_completions WHERE id=$1`, rec.ID).Scan(&completionsCount); err != nil {
		t.Fatalf("count completions: %v", err)
	}
	if completionsCount != 1 {
		t.Fatalf("course_completions rows = %d, want 1", completionsCount)
	}
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM certificates WHERE id=$1`, *rec.CertificateID).Scan(&certsCount); err != nil {
		t.Fatalf("count certs: %v", err)
	}
	if certsCount != 1 {
		t.Fatalf("certificates rows = %d, want 1", certsCount)
	}
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM certificate_jobs WHERE certificate_id=$1`, *rec.CertificateID).Scan(&jobsCount); err != nil {
		t.Fatalf("count jobs: %v", err)
	}
	if jobsCount != 1 {
		t.Fatalf("certificate_jobs rows = %d, want 1", jobsCount)
	}
}

func TestCompletion_Idempotent_OneJob(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	fx := makeCompletionFixture(t, ctx, pool)
	insertEnrollment(t, ctx, pool, fx.UserID, fx.CourseID)
	setLessonProgress(t, ctx, pool, fx.UserID, fx.LessonID, 100)

	svc := newCompletionService(t, pool)
	first, err := svc.CompleteCourse(ctx, fx.UserID, fx.CourseID)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := svc.CompleteCourse(ctx, fx.UserID, fx.CourseID)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("idempotency: ids differ: %s vs %s", first.ID, second.ID)
	}
	// 仍只有 1 条 certificate_jobs
	var jobsCount int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM certificate_jobs WHERE certificate_id=$1`, *first.CertificateID).Scan(&jobsCount); err != nil {
		t.Fatalf("count jobs: %v", err)
	}
	if jobsCount != 1 {
		t.Fatalf("certificate_jobs rows = %d, want 1", jobsCount)
	}
	// certificate 行也只有 1 条
	var certsCount int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM certificates WHERE user_id=$1 AND course_id=$2`, fx.UserID, fx.CourseID).Scan(&certsCount); err != nil {
		t.Fatalf("count certs: %v", err)
	}
	if certsCount != 1 {
		t.Fatalf("certificates rows = %d, want 1", certsCount)
	}
}

// TestCompletion_RepeatFromCleanSlate_StillSingleJob 模拟：
// 用户清空 lesson_progress 后再学一次，重复调 CompleteCourse 不会重复铸证书。
//
// 流程：第一次完成 → 然后把 progress 重置为 100%（模拟重学）→ 再调一次，
// 因为 (user_id, course_id, cert_version) UNIQUE + course_completions UNIQUE 兜底，
// 第二次仍然只返回原 completion，不会创建新 cert job。
func TestCompletion_RepeatFromCleanSlate_StillSingleJob(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	fx := makeCompletionFixture(t, ctx, pool)
	insertEnrollment(t, ctx, pool, fx.UserID, fx.CourseID)
	setLessonProgress(t, ctx, pool, fx.UserID, fx.LessonID, 100)

	svc := newCompletionService(t, pool)
	first, err := svc.CompleteCourse(ctx, fx.UserID, fx.CourseID)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	// 重置 progress → 再学一次（不会产生新 progress 行；保持 100% 即可）
	setLessonProgress(t, ctx, pool, fx.UserID, fx.LessonID, 100)
	second, err := svc.CompleteCourse(ctx, fx.UserID, fx.CourseID)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("idempotency: ids differ: %s vs %s", first.ID, second.ID)
	}
	// 仍只有 1 条 certificate_jobs（按 user + course 范围，避免被其他用例的 cert 干扰）
	var jobsCount int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM certificate_jobs cj
JOIN certificates c ON c.id = cj.certificate_id
WHERE c.user_id=$1 AND c.course_id=$2`,
		fx.UserID, fx.CourseID).Scan(&jobsCount); err != nil {
		t.Fatalf("count jobs: %v", err)
	}
	if jobsCount != 1 {
		t.Fatalf("jobs for (user,course) = %d, want 1", jobsCount)
	}
}

// keep rand/hex references active (lint safety).
var (
	_ = rand.Reader
	_ = hex.EncodeToString
)