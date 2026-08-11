package course

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/x-web3/api/internal/review"
)

var (
	ErrNotFound      = errors.New("course not found")
	ErrForbidden     = errors.New("course author required")
	ErrStaleVersion  = errors.New("stale course version")
	ErrStateConflict = errors.New("course state conflict")
)

type Course struct {
	ID             uuid.UUID     `json:"id"`
	TeacherID      uuid.UUID     `json:"teacherId"`
	TeacherName    string        `json:"teacherName"`
	Slug           string        `json:"slug"`
	Title          string        `json:"title"`
	Description    string        `json:"description"`
	Status         review.Status `json:"status"`
	CurrentVersion int           `json:"currentVersion"`
	PriceMinor     int64         `json:"priceMinor"`
	Currency       string        `json:"currency"`
	PublishedAt    *time.Time    `json:"publishedAt,omitempty"`
	CreatedAt      time.Time     `json:"createdAt"`
	UpdatedAt      time.Time     `json:"updatedAt"`
}

type CreateInput struct {
	TeacherID   uuid.UUID
	Slug        string
	Title       string
	Description string
	PriceMinor  int64
	Currency    string
}

type UpdateInput struct {
	ID          uuid.UUID
	ActorID     uuid.UUID
	Version     int
	Title       string
	Description string
	PriceMinor  int64
	Currency    string
}

type ListFilter struct {
	Query     string
	TeacherID *uuid.UUID
	PriceMin  *int64
	PriceMax  *int64
	BeforeAt  *time.Time
	BeforeID  *uuid.UUID
	Limit     int
}

// SettlementPrice is the chain configuration recorded when an admin publishes
// a paid course. The admin UI writes the same values to CourseMarket first.
type SettlementPrice struct {
	ChainID       int64
	TokenAddress  string
	MarketAddress string
	Decimals      int
}

type LessonInput struct {
	Title           string
	Required        bool
	DurationSeconds int
	MediaAssetID    *uuid.UUID
}

type ChapterInput struct {
	Title   string
	Lessons []LessonInput
}

type Lesson struct {
	ID              uuid.UUID  `json:"id"`
	Position        int        `json:"position"`
	Title           string     `json:"title"`
	Required        bool       `json:"required"`
	DurationSeconds int        `json:"durationSeconds"`
	MediaAssetID    *uuid.UUID `json:"mediaAssetId,omitempty"`
}

type Chapter struct {
	ID       uuid.UUID `json:"id"`
	Position int       `json:"position"`
	Title    string    `json:"title"`
	Lessons  []Lesson  `json:"lessons"`
}

type Repo struct{ pool *pgxpool.Pool }

func NewRepo(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }

// Pool 暴露底层 pool 给 catalog 等需要跨包查询的子系统。
// 避免给 Repo 加一堆透传方法；调用方应只做只读查询。
func (r *Repo) Pool() *pgxpool.Pool { return r.pool }

func (r *Repo) Create(ctx context.Context, in CreateInput) (*Course, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if in.Currency == "" {
		in.Currency = "USD"
	}
	var c Course
	err = tx.QueryRow(ctx, `INSERT INTO courses(teacher_id,slug,title,price_minor,currency)
VALUES($1,$2,$3,$4,$5) RETURNING id,teacher_id,slug,title,status,current_version,price_minor,currency,published_at,created_at,updated_at`,
		in.TeacherID, strings.TrimSpace(in.Slug), strings.TrimSpace(in.Title), in.PriceMinor, strings.ToUpper(in.Currency)).Scan(
		&c.ID, &c.TeacherID, &c.Slug, &c.Title, &c.Status, &c.CurrentVersion, &c.PriceMinor, &c.Currency, &c.PublishedAt, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO course_versions(course_id,version,description) VALUES($1,1,$2)`, c.ID, in.Description)
	if err != nil {
		return nil, err
	}
	c.Description = in.Description
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *Repo) UpdateDraft(ctx context.Context, in UpdateInput) (*Course, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if in.Currency == "" {
		in.Currency = "USD"
	}
	var c Course
	err = tx.QueryRow(ctx, `UPDATE courses SET title=$1,price_minor=$2,currency=$3,current_version=current_version+1
WHERE id=$4 AND teacher_id=$5 AND status='draft' AND current_version=$6
RETURNING id,teacher_id,slug,title,status,current_version,price_minor,currency,published_at,created_at,updated_at`,
		strings.TrimSpace(in.Title), in.PriceMinor, strings.ToUpper(in.Currency), in.ID, in.ActorID, in.Version).Scan(
		&c.ID, &c.TeacherID, &c.Slug, &c.Title, &c.Status, &c.CurrentVersion, &c.PriceMinor, &c.Currency, &c.PublishedAt, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		var teacher uuid.UUID
		var status review.Status
		var version int
		checkErr := tx.QueryRow(ctx, `SELECT teacher_id,status,current_version FROM courses WHERE id=$1 AND deleted_at IS NULL`, in.ID).Scan(&teacher, &status, &version)
		switch {
		case errors.Is(checkErr, pgx.ErrNoRows):
			return nil, ErrNotFound
		case checkErr != nil:
			return nil, checkErr
		case teacher != in.ActorID:
			return nil, ErrForbidden
		case status != review.Draft:
			return nil, ErrStateConflict
		default:
			return nil, ErrStaleVersion
		}
	}
	if err != nil {
		return nil, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO course_versions(course_id,version,description) VALUES($1,$2,$3)`, c.ID, c.CurrentVersion, in.Description)
	if err != nil {
		return nil, err
	}
	c.Description = in.Description
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *Repo) Transition(ctx context.Context, courseID, actorID uuid.UUID, action review.Action, admin bool, reason string) (*Course, error) {
	return r.transition(ctx, courseID, actorID, action, admin, reason, nil)
}

// TransitionWithSettlement publishes a course and records the matching active
// chain price in the same database transaction.
func (r *Repo) TransitionWithSettlement(ctx context.Context, courseID, actorID uuid.UUID, action review.Action, admin bool, reason string, settlement *SettlementPrice) (*Course, error) {
	return r.transition(ctx, courseID, actorID, action, admin, reason, settlement)
}

func (r *Repo) transition(ctx context.Context, courseID, actorID uuid.UUID, action review.Action, admin bool, reason string, settlement *SettlementPrice) (*Course, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var from review.Status
	var teacher uuid.UUID
	err = tx.QueryRow(ctx, `SELECT status,teacher_id FROM courses WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, courseID).Scan(&from, &teacher)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if action == review.Submit && teacher != actorID {
		return nil, ErrForbidden
	}
	if action != review.Submit && !admin {
		return nil, ErrForbidden
	}
	if action == review.Submit || action == review.Approve {
		var hasLesson bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (
SELECT 1 FROM course_versions v JOIN chapters ch ON ch.course_version_id=v.id JOIN lessons l ON l.chapter_id=ch.id
WHERE v.course_id=$1 AND v.version=(SELECT current_version FROM courses WHERE id=$1))`, courseID).Scan(&hasLesson); err != nil {
			return nil, err
		}
		if !hasLesson {
			return nil, fmt.Errorf("%w: at least one lesson is required", ErrStateConflict)
		}
	}
	to, err := review.Next(from, action)
	if err != nil {
		return nil, ErrStateConflict
	}
	if action == review.Reject && strings.TrimSpace(reason) == "" {
		return nil, fmt.Errorf("%w: rejection reason required", ErrStateConflict)
	}
	var c Course
	err = tx.QueryRow(ctx, `UPDATE courses SET status=$1,published_at=CASE WHEN $1='published' THEN now() ELSE published_at END,
current_version=current_version + CASE WHEN $2='unarchive' THEN 1 ELSE 0 END WHERE id=$3
RETURNING id,teacher_id,slug,title,status,current_version,price_minor,currency,published_at,created_at,updated_at`, to, action, courseID).Scan(
		&c.ID, &c.TeacherID, &c.Slug, &c.Title, &c.Status, &c.CurrentVersion, &c.PriceMinor, &c.Currency, &c.PublishedAt, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO course_audit_logs(course_id,from_status,to_status,actor_user_id,reason) VALUES($1,$2,$3,$4,NULLIF($5,''))`, courseID, from, to, actorID, strings.TrimSpace(reason))
	if err != nil {
		return nil, err
	}
	if action == review.Approve && c.PriceMinor > 0 {
		if settlement == nil || settlement.ChainID <= 0 || settlement.TokenAddress == "" || settlement.MarketAddress == "" {
			return nil, fmt.Errorf("%w: settlement configuration required", ErrStateConflict)
		}
		amount := new(big.Int).Mul(big.NewInt(c.PriceMinor), new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(settlement.Decimals-2)), nil))
		if _, err = tx.Exec(ctx, `UPDATE course_prices SET valid_to=now() WHERE course_id=$1 AND chain_id=$2 AND valid_to IS NULL`, c.ID, settlement.ChainID); err != nil {
			return nil, err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO course_prices(course_id,version,chain_id,token_address,amount,decimals,market_address)
VALUES($1,$2,$3,$4,$5,$6,$7)`, c.ID, c.CurrentVersion, settlement.ChainID, settlement.TokenAddress, amount.String(), settlement.Decimals, settlement.MarketAddress); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &c, nil
}

// ListPendingReview returns the admin publication queue.
func (r *Repo) ListPendingReview(ctx context.Context) ([]Course, error) {
	rows, err := r.pool.Query(ctx, `SELECT c.id,c.teacher_id,u.display_name,c.slug,c.title,v.description,c.status,c.current_version,c.price_minor,c.currency,c.published_at,c.created_at,c.updated_at
FROM courses c JOIN users u ON u.id=c.teacher_id JOIN course_versions v ON v.course_id=c.id AND v.version=c.current_version
WHERE c.status='pending_review' AND c.deleted_at IS NULL ORDER BY c.updated_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Course, 0)
	for rows.Next() {
		var c Course
		if err := rows.Scan(&c.ID, &c.TeacherID, &c.TeacherName, &c.Slug, &c.Title, &c.Description, &c.Status, &c.CurrentVersion, &c.PriceMinor, &c.Currency, &c.PublishedAt, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ListByTeacher returns all non-deleted courses so Studio can resume work after
// a refresh or an admin rejection.
func (r *Repo) ListByTeacher(ctx context.Context, teacherID uuid.UUID) ([]Course, error) {
	rows, err := r.pool.Query(ctx, `SELECT c.id,c.teacher_id,u.display_name,c.slug,c.title,v.description,c.status,c.current_version,c.price_minor,c.currency,c.published_at,c.created_at,c.updated_at
FROM courses c JOIN users u ON u.id=c.teacher_id JOIN course_versions v ON v.course_id=c.id AND v.version=c.current_version
WHERE c.teacher_id=$1 AND c.deleted_at IS NULL ORDER BY c.updated_at DESC`, teacherID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Course, 0)
	for rows.Next() {
		var c Course
		if err := rows.Scan(&c.ID, &c.TeacherID, &c.TeacherName, &c.Slug, &c.Title, &c.Description, &c.Status, &c.CurrentVersion, &c.PriceMinor, &c.Currency, &c.PublishedAt, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *Repo) GetPublished(ctx context.Context, id uuid.UUID) (*Course, error) {
	var c Course
	err := r.pool.QueryRow(ctx, `SELECT c.id,c.teacher_id,u.display_name,c.slug,c.title,v.description,c.status,c.current_version,c.price_minor,c.currency,c.published_at,c.created_at,c.updated_at
FROM courses c JOIN users u ON u.id=c.teacher_id JOIN course_versions v ON v.course_id=c.id AND v.version=c.current_version
WHERE c.id=$1 AND c.status='published' AND c.deleted_at IS NULL`, id).Scan(&c.ID, &c.TeacherID, &c.TeacherName, &c.Slug, &c.Title, &c.Description, &c.Status, &c.CurrentVersion, &c.PriceMinor, &c.Currency, &c.PublishedAt, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &c, err
}

func (r *Repo) ListPublished(ctx context.Context, f ListFilter) ([]Course, error) {
	if f.Limit <= 0 || f.Limit > 50 {
		f.Limit = 20
	}
	rows, err := r.pool.Query(ctx, `SELECT c.id,c.teacher_id,u.display_name,c.slug,c.title,v.description,c.status,c.current_version,c.price_minor,c.currency,c.published_at,c.created_at,c.updated_at
FROM courses c JOIN users u ON u.id=c.teacher_id JOIN course_versions v ON v.course_id=c.id AND v.version=c.current_version
WHERE c.status='published' AND c.deleted_at IS NULL
AND ($1='' OR c.title ILIKE '%'||$1||'%' OR v.description ILIKE '%'||$1||'%')
AND ($2::uuid IS NULL OR c.teacher_id=$2) AND ($3::bigint IS NULL OR c.price_minor >= $3) AND ($4::bigint IS NULL OR c.price_minor <= $4)
AND ($5::timestamptz IS NULL OR (c.published_at,c.id) < ($5,$6::uuid))
ORDER BY c.published_at DESC,c.id DESC LIMIT $7`, strings.TrimSpace(f.Query), f.TeacherID, f.PriceMin, f.PriceMax, f.BeforeAt, f.BeforeID, f.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Course, 0, f.Limit)
	for rows.Next() {
		var c Course
		if err := rows.Scan(&c.ID, &c.TeacherID, &c.TeacherName, &c.Slug, &c.Title, &c.Description, &c.Status, &c.CurrentVersion, &c.PriceMinor, &c.Currency, &c.PublishedAt, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ReplaceCurriculum atomically replaces the ordered curriculum for the current draft version.
// The course optimistic-lock version is incremented in the same transaction.
func (r *Repo) ReplaceCurriculum(ctx context.Context, courseID, actorID uuid.UUID, version int, chapters []ChapterInput) (int, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var versionID uuid.UUID
	var nextVersion int
	err = tx.QueryRow(ctx, `UPDATE courses SET current_version=current_version+1
WHERE id=$1 AND teacher_id=$2 AND status='draft' AND current_version=$3
RETURNING current_version`, courseID, actorID, version).Scan(&nextVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		var teacher uuid.UUID
		var status review.Status
		var actual int
		checkErr := tx.QueryRow(ctx, `SELECT teacher_id,status,current_version FROM courses WHERE id=$1 AND deleted_at IS NULL`, courseID).Scan(&teacher, &status, &actual)
		switch {
		case errors.Is(checkErr, pgx.ErrNoRows):
			return 0, ErrNotFound
		case checkErr != nil:
			return 0, checkErr
		case teacher != actorID:
			return 0, ErrForbidden
		case status != review.Draft:
			return 0, ErrStateConflict
		default:
			return 0, ErrStaleVersion
		}
	}
	if err != nil {
		return 0, err
	}
	var description string
	if err := tx.QueryRow(ctx, `SELECT description FROM course_versions WHERE course_id=$1 AND version=$2`, courseID, version).Scan(&description); err != nil {
		return 0, err
	}
	if err := tx.QueryRow(ctx, `INSERT INTO course_versions(course_id,version,description) VALUES($1,$2,$3) RETURNING id`, courseID, nextVersion, description).Scan(&versionID); err != nil {
		return 0, err
	}
	for chapterPosition, chapter := range chapters {
		if strings.TrimSpace(chapter.Title) == "" {
			return 0, fmt.Errorf("chapter %d title required", chapterPosition)
		}
		var chapterID uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO chapters(course_version_id,position,title) VALUES($1,$2,$3) RETURNING id`, versionID, chapterPosition, strings.TrimSpace(chapter.Title)).Scan(&chapterID); err != nil {
			return 0, err
		}
		for lessonPosition, lesson := range chapter.Lessons {
			if strings.TrimSpace(lesson.Title) == "" || lesson.DurationSeconds < 0 {
				return 0, fmt.Errorf("invalid lesson at %d/%d", chapterPosition, lessonPosition)
			}
			if _, err := tx.Exec(ctx, `INSERT INTO lessons(chapter_id,position,title,required,media_asset_id,duration_seconds) VALUES($1,$2,$3,$4,$5,$6)`, chapterID, lessonPosition, strings.TrimSpace(lesson.Title), lesson.Required, lesson.MediaAssetID, lesson.DurationSeconds); err != nil {
				return 0, err
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return nextVersion, nil
}

func (r *Repo) Curriculum(ctx context.Context, courseID uuid.UUID, publishedOnly bool) ([]Chapter, error) {
	statusClause := ""
	if publishedOnly {
		statusClause = " AND c.status='published'"
	}
	rows, err := r.pool.Query(ctx, `SELECT ch.id,ch.position,ch.title,l.id,l.position,l.title,l.required,l.duration_seconds,l.media_asset_id
FROM courses c JOIN course_versions v ON v.course_id=c.id AND v.version=c.current_version
JOIN chapters ch ON ch.course_version_id=v.id LEFT JOIN lessons l ON l.chapter_id=ch.id
WHERE c.id=$1 AND c.deleted_at IS NULL`+statusClause+` ORDER BY ch.position,l.position`, courseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	chapters := make([]Chapter, 0)
	chapterIndex := map[uuid.UUID]int{}
	for rows.Next() {
		var chapter Chapter
		var lessonID, mediaID *uuid.UUID
		var lessonPosition, duration *int
		var lessonTitle *string
		var required *bool
		if err := rows.Scan(&chapter.ID, &chapter.Position, &chapter.Title, &lessonID, &lessonPosition, &lessonTitle, &required, &duration, &mediaID); err != nil {
			return nil, err
		}
		idx, ok := chapterIndex[chapter.ID]
		if !ok {
			chapter.Lessons = []Lesson{}
			chapters = append(chapters, chapter)
			idx = len(chapters) - 1
			chapterIndex[chapter.ID] = idx
		}
		if lessonID != nil {
			chapters[idx].Lessons = append(chapters[idx].Lessons, Lesson{ID: *lessonID, Position: *lessonPosition, Title: *lessonTitle, Required: *required, DurationSeconds: *duration, MediaAssetID: mediaID})
		}
	}
	return chapters, rows.Err()
}
