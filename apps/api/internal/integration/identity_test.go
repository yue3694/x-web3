package integration_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/x-web3/api/internal/auth"
	"github.com/x-web3/api/internal/course"
	"github.com/x-web3/api/internal/review"
	"github.com/x-web3/api/internal/user"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL := os.Getenv("DATABASE_URL_TEST")
	if databaseURL == "" {
		t.Skip("DATABASE_URL_TEST is not set")
	}
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(context.Background()); err != nil {
		t.Fatalf("database ping: %v", err)
	}
	return pool
}

func TestCourseLifecycleOptimisticLockAndCatalog(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	var teacherID, adminID uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO users(privy_user_id,display_name) VALUES($1,'Teacher') RETURNING id`, "did:privy:teacher-"+uuid.NewString()).Scan(&teacherID); err != nil {
		t.Fatalf("insert teacher: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO users(privy_user_id,display_name) VALUES($1,'Admin') RETURNING id`, "did:privy:admin-"+uuid.NewString()).Scan(&adminID); err != nil {
		t.Fatalf("insert admin: %v", err)
	}
	repo := course.NewRepo(pool)
	created, err := repo.Create(ctx, course.CreateInput{TeacherID: teacherID, Slug: "integration-" + uuid.NewString(), Title: "Course draft", Description: "v1", PriceMinor: 1200, Currency: "usd"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	updated, err := repo.UpdateDraft(ctx, course.UpdateInput{ID: created.ID, ActorID: teacherID, Version: 1, Title: "Course updated", Description: "v2", PriceMinor: 1500, Currency: "USD"})
	if err != nil || updated.CurrentVersion != 2 {
		t.Fatalf("update: course=%v err=%v", updated, err)
	}
	if _, err := repo.UpdateDraft(ctx, course.UpdateInput{ID: created.ID, ActorID: teacherID, Version: 1, Title: "stale", Currency: "USD"}); !errors.Is(err, course.ErrStaleVersion) {
		t.Fatalf("stale update error = %v", err)
	}
	nextVersion, err := repo.ReplaceCurriculum(ctx, created.ID, teacherID, 2, []course.ChapterInput{{Title: "Foundations", Lessons: []course.LessonInput{{Title: "Wallet safety", Required: true, DurationSeconds: 420}, {Title: "Threat models", Required: true, DurationSeconds: 600}}}})
	if err != nil || nextVersion != 3 {
		t.Fatalf("replace curriculum: version=%d err=%v", nextVersion, err)
	}
	chapters, err := repo.Curriculum(ctx, created.ID, false)
	if err != nil || len(chapters) != 1 || len(chapters[0].Lessons) != 2 || chapters[0].Lessons[1].Position != 1 {
		t.Fatalf("curriculum ordering: chapters=%v err=%v", chapters, err)
	}
	if _, err := repo.Transition(ctx, created.ID, teacherID, review.Submit, false, ""); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if _, err := repo.Transition(ctx, created.ID, adminID, review.Approve, true, ""); err != nil {
		t.Fatalf("approve: %v", err)
	}
	got, err := repo.GetPublished(ctx, created.ID)
	if err != nil || got.Status != review.Published || got.Description != "v2" {
		t.Fatalf("get published: course=%v err=%v", got, err)
	}
	items, err := repo.ListPublished(ctx, course.ListFilter{Query: "updated", Limit: 10})
	if err != nil || len(items) == 0 {
		t.Fatalf("list published: count=%d err=%v", len(items), err)
	}
}

func TestRepeatedPrivyLoginUpsertCreatesOneUser(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	subject := "did:privy:integration-" + uuid.NewString()
	repo := user.NewRepo(pool)

	for i := 0; i < 2; i++ {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("Begin: %v", err)
		}
		u, err := repo.UpsertByPrivySubject(ctx, tx, subject, "Integration User")
		if err == nil {
			err = repo.GrantDefaultRole(ctx, tx, u.ID)
		}
		if err == nil {
			err = tx.Commit(ctx)
		} else {
			_ = tx.Rollback(ctx)
		}
		if err != nil {
			t.Fatalf("upsert iteration %d: %v", i, err)
		}
	}

	var users, studentRoles int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE privy_user_id=$1`, subject).Scan(&users); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM user_roles ur JOIN users u ON u.id=ur.user_id JOIN roles r ON r.id=ur.role_id WHERE u.privy_user_id=$1 AND r.code='student'`, subject).Scan(&studentRoles); err != nil {
		t.Fatalf("count roles: %v", err)
	}
	if users != 1 || studentRoles != 1 {
		t.Fatalf("users=%d studentRoles=%d, want 1/1", users, studentRoles)
	}
}

func TestSuspendedUserSessionIsRejectedAndDestroyed(t *testing.T) {
	pool := testPool(t)
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		t.Skip("REDIS_URL is not set")
	}
	redisOptions, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatalf("redis URL: %v", err)
	}
	redisOptions.DB = 15
	rdb := redis.NewClient(redisOptions)
	t.Cleanup(func() { _ = rdb.Close() })
	ctx := context.Background()
	if err := rdb.FlushDB(ctx).Err(); err != nil {
		t.Fatalf("redis flush: %v", err)
	}

	subject := "did:privy:suspended-" + uuid.NewString()
	var userID uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO users(privy_user_id,status) VALUES($1,'suspended') RETURNING id`, subject).Scan(&userID); err != nil {
		t.Fatalf("insert suspended user: %v", err)
	}
	store := auth.NewSessionStore(rdb, []byte("0123456789abcdef0123456789abcdef"), time.Hour)
	sid, _, err := store.Issue(ctx, subject, "test")
	if err != nil {
		t.Fatalf("issue session: %v", err)
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(auth.Middleware(nil, store, pool))
	router.GET("/protected", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: sid})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
	data, err := store.Read(ctx, sid)
	if err != nil || data != nil {
		t.Fatalf("session should be destroyed, data=%v err=%v", data, err)
	}
}
