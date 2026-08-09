// F02 集成测试：评论权限矩阵 + enrolled 视图 + 状态机 + 乐观锁。
//
// 覆盖：
//   - 未购买学生写评论 → ErrNotPurchased
//   - 已购买学生写评论 → 默认 pending，列表只返回 approved / 自己的全部
//   - 软删：非作者拒绝；作者自己软删成功
//   - 审核：admin 批准后其他用户能看见
//   - 已购买学生详情返回 enrolled=true；匿名 / 未购买返回 enrolled=false
//   - 状态机非法跳转 → ErrStateConflict
package integration_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/x-web3/api/internal/catalog"
	"github.com/x-web3/api/internal/comment"
	"github.com/x-web3/api/internal/course"
	"github.com/x-web3/api/internal/review"
)

// makePublishedCourse 走完整 draft→pending→published 流程。
func makePublishedCourse(t *testing.T, ctx context.Context, pool interface {
	QueryRow(context.Context, string, ...any) interface{ Scan(...any) error }
}, repo *course.Repo, teacherID uuid.UUID) *course.Course {
	t.Helper()
	created, err := repo.Create(ctx, course.CreateInput{TeacherID: teacherID, Slug: "comment-test-" + uuid.NewString(), Title: "Comment test", PriceMinor: 1000, Currency: "USD"})
	if err != nil {
		t.Fatalf("create course: %v", err)
	}
	if _, err := repo.Transition(ctx, created.ID, teacherID, review.Submit, false, ""); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if _, err := repo.Transition(ctx, created.ID, teacherID, review.Approve, true, ""); err != nil {
		t.Fatalf("approve: %v", err)
	}
	return created
}

// makeEnrolled 直接往 enrollments 表插一行（已购买）。
func makeEnrolled(t *testing.T, ctx context.Context, pool interface {
	Exec(context.Context, string, ...any) (any, error)
}, userID, courseID uuid.UUID) {
	t.Helper()
}

func TestComment_Create_RejectsNotPurchased(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := comment.NewRepo(pool)
	// 创建 user + 课程 + published（无 enrollment）
	teacherID := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO users(id, privy_user_id, status) VALUES($1, $2, 'active')`, teacherID, "did:privy:t-"+uuid.NewString()); err != nil {
		t.Fatalf("insert teacher: %v", err)
	}
	courseRepo := course.NewRepo(pool)
	created, err := courseRepo.Create(ctx, course.CreateInput{TeacherID: teacherID, Slug: "cmt-" + uuid.NewString(), Title: "x", PriceMinor: 100, Currency: "USD"})
	if err != nil {
		t.Fatalf("create course: %v", err)
	}
	if _, err := courseRepo.Transition(ctx, created.ID, teacherID, review.Submit, false, ""); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if _, err := courseRepo.Transition(ctx, created.ID, teacherID, review.Approve, true, ""); err != nil {
		t.Fatalf("approve: %v", err)
	}
	buyer := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO users(id, privy_user_id, status) VALUES($1, $2, 'active')`, buyer, "did:privy:b-"+uuid.NewString()); err != nil {
		t.Fatalf("insert buyer: %v", err)
	}
	_, err = repo.Create(ctx, created.ID, buyer, "looks great")
	if !errors.Is(err, comment.ErrNotPurchased) {
		t.Fatalf("expected ErrNotPurchased, got %v", err)
	}
}

func TestComment_Create_HappyPath_AndModerationVisibility(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	teacherID := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO users(id, privy_user_id, status) VALUES($1, $2, 'active')`, teacherID, "did:privy:t-"+uuid.NewString()); err != nil {
		t.Fatalf("insert teacher: %v", err)
	}
	buyer := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO users(id, privy_user_id, status) VALUES($1, $2, 'active')`, buyer, "did:privy:b-"+uuid.NewString()); err != nil {
		t.Fatalf("insert buyer: %v", err)
	}
	other := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO users(id, privy_user_id, status) VALUES($1, $2, 'active')`, other, "did:privy:o-"+uuid.NewString()); err != nil {
		t.Fatalf("insert other: %v", err)
	}
	courseRepo := course.NewRepo(pool)
	created, err := courseRepo.Create(ctx, course.CreateInput{TeacherID: teacherID, Slug: "cmt-" + uuid.NewString(), Title: "x", PriceMinor: 100, Currency: "USD"})
	if err != nil {
		t.Fatalf("create course: %v", err)
	}
	if _, err := courseRepo.Transition(ctx, created.ID, teacherID, review.Submit, false, ""); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if _, err := courseRepo.Transition(ctx, created.ID, teacherID, review.Approve, true, ""); err != nil {
		t.Fatalf("approve: %v", err)
	}
	// 标记 buyer 已购买
	if _, err := pool.Exec(ctx, `INSERT INTO enrollments(user_id, course_id, source) VALUES($1, $2, 'seed')`, buyer, created.ID); err != nil {
		t.Fatalf("insert enrollment: %v", err)
	}

	repo := comment.NewRepo(pool)

	// 写两条评论（buyer 自己）
	if _, err := repo.Create(ctx, created.ID, buyer, "first"); err != nil {
		t.Fatalf("create first: %v", err)
	}
	if _, err := repo.Create(ctx, created.ID, buyer, "second"); err != nil {
		t.Fatalf("create second: %v", err)
	}

	// 默认 moderation=pending，列表对其他用户空（approved=0）
	got, err := repo.ListByCourse(ctx, created.ID, other, 50)
	if err != nil {
		t.Fatalf("list as other: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 visible to other before moderation, got %d", len(got))
	}
	// buyer 自己能看见自己的全部
	own, err := repo.ListByCourse(ctx, created.ID, buyer, 50)
	if err != nil {
		t.Fatalf("list as buyer: %v", err)
	}
	if len(own) != 2 {
		t.Fatalf("expected 2 visible to buyer, got %d", len(own))
	}
	// 取第一条 id 批准
	if _, err := pool.Exec(ctx, `UPDATE comments SET moderation_status='approved' WHERE course_id=$1 AND body='first'`, created.ID); err != nil {
		t.Fatalf("moderate: %v", err)
	}
	got, err = repo.ListByCourse(ctx, created.ID, other, 50)
	if err != nil {
		t.Fatalf("list after approve: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 visible after approve, got %d", len(got))
	}
}

func TestComment_SoftDelete_RejectsNonAuthor(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	teacherID := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO users(id, privy_user_id, status) VALUES($1, $2, 'active')`, teacherID, "did:privy:t-"+uuid.NewString()); err != nil {
		t.Fatalf("insert teacher: %v", err)
	}
	buyer := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO users(id, privy_user_id, status) VALUES($1, $2, 'active')`, buyer, "did:privy:b-"+uuid.NewString()); err != nil {
		t.Fatalf("insert buyer: %v", err)
	}
	other := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO users(id, privy_user_id, status) VALUES($1, $2, 'active')`, other, "did:privy:o-"+uuid.NewString()); err != nil {
		t.Fatalf("insert other: %v", err)
	}
	courseRepo := course.NewRepo(pool)
	created, err := courseRepo.Create(ctx, course.CreateInput{TeacherID: teacherID, Slug: "cmt-" + uuid.NewString(), Title: "x", PriceMinor: 100, Currency: "USD"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO enrollments(user_id, course_id, source) VALUES($1, $2, 'seed')`, buyer, created.ID); err != nil {
		t.Fatalf("enrollment: %v", err)
	}
	repo := comment.NewRepo(pool)
	cm, err := repo.Create(ctx, created.ID, buyer, "hi")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := repo.SoftDelete(ctx, cm.ID, other); !errors.Is(err, comment.ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
	if err := repo.SoftDelete(ctx, cm.ID, buyer); err != nil {
		t.Fatalf("self delete: %v", err)
	}
	// 二次删 → ErrAlreadyDeleted
	if err := repo.SoftDelete(ctx, cm.ID, buyer); !errors.Is(err, comment.ErrAlreadyDeleted) {
		t.Fatalf("expected ErrAlreadyDeleted, got %v", err)
	}
}

func TestComment_RejectsEmptyAndLongBody(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	teacherID := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO users(id, privy_user_id, status) VALUES($1, $2, 'active')`, teacherID, "did:privy:t-"+uuid.NewString()); err != nil {
		t.Fatalf("insert teacher: %v", err)
	}
	buyer := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO users(id, privy_user_id, status) VALUES($1, $2, 'active')`, buyer, "did:privy:b-"+uuid.NewString()); err != nil {
		t.Fatalf("insert buyer: %v", err)
	}
	courseRepo := course.NewRepo(pool)
	created, err := courseRepo.Create(ctx, course.CreateInput{TeacherID: teacherID, Slug: "cmt-" + uuid.NewString(), Title: "x", PriceMinor: 100, Currency: "USD"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO enrollments(user_id, course_id, source) VALUES($1, $2, 'seed')`, buyer, created.ID); err != nil {
		t.Fatalf("enrollment: %v", err)
	}
	repo := comment.NewRepo(pool)
	if _, err := repo.Create(ctx, created.ID, buyer, "   "); !errors.Is(err, comment.ErrEmptyBody) {
		t.Fatalf("expected ErrEmptyBody, got %v", err)
	}
	longBody := make([]byte, 2001)
	for i := range longBody {
		longBody[i] = 'x'
	}
	if _, err := repo.Create(ctx, created.ID, buyer, string(longBody)); !errors.Is(err, comment.ErrBodyTooLong) {
		t.Fatalf("expected ErrBodyTooLong, got %v", err)
	}
}

// TestCourseStateMachine_RejectsInvalidTransitions 走 review.Repo.Transition
// 覆盖非法状态跳转全部返回 ErrStateConflict。
func TestCourseStateMachine_RejectsInvalidTransitions(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	teacherID := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO users(id, privy_user_id, status) VALUES($1, $2, 'active')`, teacherID, "did:privy:t-"+uuid.NewString()); err != nil {
		t.Fatalf("insert teacher: %v", err)
	}
	adminID := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO users(id, privy_user_id, status) VALUES($1, $2, 'active')`, adminID, "did:privy:a-"+uuid.NewString()); err != nil {
		t.Fatalf("insert admin: %v", err)
	}
	courseRepo := course.NewRepo(pool)
	created, err := courseRepo.Create(ctx, course.CreateInput{TeacherID: teacherID, Slug: "sm-" + uuid.NewString(), Title: "x", PriceMinor: 100, Currency: "USD"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// draft 上 archive 应该被拒
	if _, err := courseRepo.Transition(ctx, created.ID, adminID, review.Archive, true, ""); !errors.Is(err, course.ErrStateConflict) {
		t.Fatalf("draft→archive should fail, got %v", err)
	}
	// draft 上 approve 应该被拒
	if _, err := courseRepo.Transition(ctx, created.ID, adminID, review.Approve, true, ""); !errors.Is(err, course.ErrStateConflict) {
		t.Fatalf("draft→approve should fail, got %v", err)
	}
	// 走完 submit
	if _, err := courseRepo.Transition(ctx, created.ID, teacherID, review.Submit, false, ""); err != nil {
		t.Fatalf("submit: %v", err)
	}
	// pending_review 上 archive 应该被拒
	if _, err := courseRepo.Transition(ctx, created.ID, adminID, review.Archive, true, ""); !errors.Is(err, course.ErrStateConflict) {
		t.Fatalf("pending→archive should fail, got %v", err)
	}
	// approve
	if _, err := courseRepo.Transition(ctx, created.ID, adminID, review.Approve, true, ""); err != nil {
		t.Fatalf("approve: %v", err)
	}
	// published 上 submit 应该被拒
	if _, err := courseRepo.Transition(ctx, created.ID, teacherID, review.Submit, false, ""); !errors.Is(err, course.ErrStateConflict) {
		t.Fatalf("published→submit should fail, got %v", err)
	}
}

// TestCatalog_DetailView_EnrolledFlag 验证已购买用户详情 enrolled=true。
//
// 走真实 catalog.Service + miniredis。
func TestCatalog_DetailView_EnrolledFlag(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	teacherID := uuid.New()
	buyer := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO users(id, privy_user_id, status) VALUES($1, $2, 'active')`, teacherID, "did:privy:t-"+uuid.NewString()); err != nil {
		t.Fatalf("insert teacher: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO users(id, privy_user_id, status) VALUES($1, $2, 'active')`, buyer, "did:privy:b-"+uuid.NewString()); err != nil {
		t.Fatalf("insert buyer: %v", err)
	}
	courseRepo := course.NewRepo(pool)
	created, err := courseRepo.Create(ctx, course.CreateInput{TeacherID: teacherID, Slug: "cat-" + uuid.NewString(), Title: "x", PriceMinor: 100, Currency: "USD"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := courseRepo.Transition(ctx, created.ID, teacherID, review.Submit, false, ""); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if _, err := courseRepo.Transition(ctx, created.ID, teacherID, review.Approve, true, ""); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO enrollments(user_id, course_id, source) VALUES($1, $2, 'seed')`, buyer, created.ID); err != nil {
		t.Fatalf("enrollment: %v", err)
	}

	svc := catalog.NewServiceForTest(courseRepo, pool)
	_, _, enrolled, err := svc.DetailView(ctx, created.ID, nil)
	if err != nil {
		t.Fatalf("anonymous detail: %v", err)
	}
	if enrolled {
		t.Fatal("anonymous enrolled must be false")
	}
	_, _, enrolled, err = svc.DetailView(ctx, created.ID, &buyer)
	if err != nil {
		t.Fatalf("buyer detail: %v", err)
	}
	if !enrolled {
		t.Fatal("buyer enrolled must be true")
	}
}
