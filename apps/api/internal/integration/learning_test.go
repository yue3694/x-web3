// F02 DoD 集成测试：
//   - 未购买学生调 /lessons/{id}/playback → Service.Playback 返回 ErrNotEligible
//     （handler 层在 unit_test 覆盖映射到 403 NOT_ENROLLED）。
//   - media finalize 校验失败路径：客户端声明的 checksum 与 DB 已存的
//     checksum_sha256 不一致时，Repo.Finalize 返回 ErrChecksumBad。
//
// 这两条都是 F02 DoD 「未购买 / 校验失败」路径的端到端断言。
package integration_test

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/x-web3/api/internal/course"
	"github.com/x-web3/api/internal/learning"
	"github.com/x-web3/api/internal/media"
	"github.com/x-web3/api/internal/objectstore"
	"github.com/x-web3/api/internal/review"
)

// playbackFixture 准备：teacher + student（无 enrollment）+ published course +
// 1 chapter + 1 lesson（含已 finalize 的 media asset）。
type playbackFixture struct {
	TeacherID  uuid.UUID
	StudentID  uuid.UUID
	CourseID   uuid.UUID
	ChapterID  uuid.UUID
	LessonID   uuid.UUID
	VersionID  uuid.UUID
	MediaKey   string
}

func makePlaybackFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) playbackFixture {
	t.Helper()
	teacherID := uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users(id, privy_user_id, status) VALUES($1, $2, 'active')`,
		teacherID, "did:privy:lt-"+uuid.NewString()); err != nil {
		t.Fatalf("insert teacher: %v", err)
	}
	studentID := uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users(id, privy_user_id, status) VALUES($1, $2, 'active')`,
		studentID, "did:privy:ls-"+uuid.NewString()); err != nil {
		t.Fatalf("insert student: %v", err)
	}

	// 走 draft → published 全流程，让 current_version=1
	created, err := course.NewRepo(pool).Create(ctx, course.CreateInput{
		TeacherID: teacherID, Slug: "f02dod-" + uuid.NewString(),
		Title: "F02 DoD playback", PriceMinor: 1000, Currency: "USD",
	})
	if err != nil {
		t.Fatalf("create course: %v", err)
	}
	if _, err := course.NewRepo(pool).Transition(ctx, created.ID, teacherID, review.Submit, false, ""); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if _, err := course.NewRepo(pool).Transition(ctx, created.ID, teacherID, review.Approve, true, ""); err != nil {
		t.Fatalf("approve: %v", err)
	}

	var versionID uuid.UUID
	if err := pool.QueryRow(ctx,
		`SELECT id FROM course_versions WHERE course_id=$1 AND version=1`,
		created.ID).Scan(&versionID); err != nil {
		t.Fatalf("get version: %v", err)
	}

	chapterID := uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO chapters(id, course_version_id, title, position) VALUES($1, $2, 'Chapter 1', 1)`,
		chapterID, versionID); err != nil {
		t.Fatalf("insert chapter: %v", err)
	}

	// 先建一个 s3 key + size，再插 media_assets（status='ready'）
	s3Key := "f02dod/" + uuid.NewString() + ".mp4"
	mediaAssetID := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO media_assets(id, owner_user_id, s3_key, content_type, size_bytes, status, scan_status, checksum_sha256)
VALUES($1,$2,$3,'video/mp4',1024,'ready','clean','')`, mediaAssetID, teacherID, s3Key); err != nil {
		t.Fatalf("insert media: %v", err)
	}

	lessonID := uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO lessons(id, chapter_id, position, title, required, duration_seconds, media_asset_id)
VALUES($1,$2,1,'Lesson 1',true,60,$3)`,
		lessonID, chapterID, mediaAssetID); err != nil {
		t.Fatalf("insert lesson: %v", err)
	}

	return playbackFixture{
		TeacherID: teacherID, StudentID: studentID, CourseID: created.ID,
		ChapterID: chapterID, LessonID: lessonID, VersionID: versionID,
		MediaKey: s3Key,
	}
}

// TestPlayback_RejectsNotEnrolled 覆盖 F02 DoD:
// 「未购买学生调 `/lessons/{id}/playback` 返回 403」
//
// 走 learning.Service.Playback 直接断言语义错误 ErrNotEligible。
// handler → 403 NOT_ENROLLED 的映射在 handlers/learning_test.go 单测中覆盖。
func TestPlayback_RejectsNotEnrolled(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	fx := makePlaybackFixture(t, ctx, pool)

	store := objectstore.NewFakeStore()
	// Head 必须返回与 media_assets.size_bytes 一致的尺寸，否则 service 会先返 ErrMediaNotReady
	store.Put(fx.MediaKey, "video/mp4", 1024)

	svc := learning.NewService(pool, store)
	_, err := svc.Playback(ctx, fx.LessonID, fx.StudentID)
	if !errors.Is(err, learning.ErrNotEligible) {
		t.Fatalf("expected ErrNotEligible, got %v", err)
	}

	// 二次断言：student 在 enrollments 表里仍是 0 行（确保失败原因是「未购买」
	// 而不是 service 提前短路）
	var n int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM enrollments WHERE user_id=$1`, fx.StudentID,
	).Scan(&n); err != nil {
		t.Fatalf("count enrollments: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected zero enrollments, got %d", n)
	}
}

// TestPlayback_HappyPath_WhenEnrolled 顺便覆盖：写入 enrollment 后 service
// 应该返回 *Credential（非 nil），确证上一步的 ErrNotEligible 真的是由
// enrollment 缺失引起，而不是更早的短路。
func TestPlayback_HappyPath_WhenEnrolled(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	fx := makePlaybackFixture(t, ctx, pool)

	if _, err := pool.Exec(ctx,
		`INSERT INTO enrollments(user_id, course_id, source) VALUES($1, $2, 'seed')`,
		fx.StudentID, fx.CourseID); err != nil {
		t.Fatalf("insert enrollment: %v", err)
	}

	store := objectstore.NewFakeStore()
	store.Put(fx.MediaKey, "video/mp4", 1024)

	svc := learning.NewService(pool, store)
	cred, err := svc.Playback(ctx, fx.LessonID, fx.StudentID)
	if err != nil {
		t.Fatalf("expected happy path, got %v", err)
	}
	if cred == nil || cred.Purpose != learning.PurposePlayback {
		t.Fatalf("credential mismatch: %+v", cred)
	}
	if cred.ExpiresAt.IsZero() {
		t.Fatal("credential must carry ExpiresAt")
	}
}

// TestMedia_Finalize_RejectsChecksumMismatch 覆盖 F02 DoD:
// 「媒体上传 finalize 校验失败路径」
//
// 客户端传入的 checksum 与服务端 DB 记录的 checksum_sha256 不一致时，
// media.Repo.Finalize 必须返回 ErrChecksumBad（即 HTTP 400 VALIDATION）。
func TestMedia_Finalize_RejectsChecksumMismatch(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	owner := uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users(id, privy_user_id, status) VALUES($1, $2, 'active')`,
		owner, "did:privy:mt-"+uuid.NewString()); err != nil {
		t.Fatalf("insert owner: %v", err)
	}

	// 真实 hash 与 DB 里存的 hash 必须不同 → 故意分两次哈希
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		t.Fatalf("rand: %v", err)
	}
	storedSum := sha256.Sum256(random)                    // 服务端记录的（真实上传后）
	clientSum := sha256.Sum256(append(random, byte(0)))  // 客户端声称的（错配）

	assetID := uuid.New()
	key := "f02dod/" + uuid.NewString() + ".mp4"
	if _, err := pool.Exec(ctx, `INSERT INTO media_assets(id, owner_user_id, s3_key, content_type, size_bytes, status, scan_status, checksum_sha256)
VALUES($1,$2,$3,'video/mp4',1024,'draft','pending',$4)`,
		assetID, owner, key, hex.EncodeToString(storedSum[:]),
	); err != nil {
		t.Fatalf("insert media: %v", err)
	}

	store := objectstore.NewFakeStore()
	store.Put(key, "video/mp4", 1024)

	repo := media.NewRepo(pool)
	_, err := repo.Finalize(ctx, assetID, owner, hex.EncodeToString(clientSum[:]), store)
	if !errors.Is(err, media.ErrChecksumBad) {
		t.Fatalf("expected ErrChecksumBad, got %v", err)
	}

	// 同时确认：失败后 asset.status 应保持 draft（事务回滚）
	var status string
	if err := pool.QueryRow(ctx,
		`SELECT status FROM media_assets WHERE id=$1`, assetID,
	).Scan(&status); err != nil {
		t.Fatalf("select status: %v", err)
	}
	if status != "draft" {
		t.Fatalf("status must remain draft on mismatch, got %q", status)
	}
}

// （学习测试只接受 *pgxpool.Pool；保持和 completion_test.go / comment_test.go
// 同样的具体类型约束——避免引入匿名 interface 兼容写法。）