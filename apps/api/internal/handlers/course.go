package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/x-web3/api/internal/course"
	"github.com/x-web3/api/internal/errcode"
	"github.com/x-web3/api/internal/httpkit"
	"github.com/x-web3/api/internal/review"
)

type CourseHandler struct{ repo *course.Repo }

func NewCourseHandler(repo *course.Repo) *CourseHandler { return &CourseHandler{repo: repo} }

type courseWriteRequest struct {
	Slug        string `json:"slug"`
	Title       string `json:"title" binding:"required"`
	Description string `json:"description"`
	PriceMinor  int64  `json:"priceMinor"`
	Currency    string `json:"currency"`
}

func (h *CourseHandler) Create(c *httpkit.Context) {
	uid, err := userIDFromCtx(c)
	if err != nil {
		httpkit.Error(c, http.StatusUnauthorized, errcode.SessionExpired, "no session", nil)
		return
	}
	var req courseWriteRequest
	if !c.MustJSON(&req) {
		return
	}
	if strings.TrimSpace(req.Slug) == "" || len(strings.TrimSpace(req.Title)) > 160 || req.PriceMinor < 0 {
		httpkit.Error(c, http.StatusBadRequest, errcode.BadRequest, "slug, title and non-negative priceMinor are required", nil)
		return
	}
	created, err := h.repo.Create(c.Request.Context(), course.CreateInput{TeacherID: uid, Slug: req.Slug, Title: req.Title, Description: req.Description, PriceMinor: req.PriceMinor, Currency: req.Currency})
	if err != nil {
		httpkit.Internal(c, err)
		return
	}
	c.Header("ETag", strconv.Itoa(created.CurrentVersion))
	c.JSON(http.StatusCreated, created)
}

func (h *CourseHandler) Update(c *httpkit.Context) {
	uid, err := userIDFromCtx(c)
	if err != nil {
		httpkit.Error(c, http.StatusUnauthorized, errcode.SessionExpired, "no session", nil)
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpkit.Error(c, http.StatusBadRequest, errcode.BadRequest, "invalid course id", nil)
		return
	}
	version, err := strconv.Atoi(strings.Trim(c.GetHeader("If-Match"), `"`))
	if err != nil || version <= 0 {
		httpkit.Error(c, http.StatusBadRequest, errcode.BadRequest, "If-Match course version is required", nil)
		return
	}
	var req courseWriteRequest
	if !c.MustJSON(&req) {
		return
	}
	if len(strings.TrimSpace(req.Title)) == 0 || len(strings.TrimSpace(req.Title)) > 160 || req.PriceMinor < 0 {
		httpkit.Error(c, http.StatusBadRequest, errcode.BadRequest, "invalid course fields", nil)
		return
	}
	updated, err := h.repo.UpdateDraft(c.Request.Context(), course.UpdateInput{ID: id, ActorID: uid, Version: version, Title: req.Title, Description: req.Description, PriceMinor: req.PriceMinor, Currency: req.Currency})
	if err != nil {
		mapCourseError(c, err)
		return
	}
	c.Header("ETag", strconv.Itoa(updated.CurrentVersion))
	c.JSON(http.StatusOK, updated)
}

type curriculumRequest struct {
	Chapters []struct {
		Title   string `json:"title" binding:"required"`
		Lessons []struct {
			Title           string     `json:"title" binding:"required"`
			Required        bool       `json:"required"`
			DurationSeconds int        `json:"durationSeconds"`
			MediaAssetID    *uuid.UUID `json:"mediaAssetId"`
		} `json:"lessons"`
	} `json:"chapters" binding:"required"`
}

func (h *CourseHandler) ReplaceCurriculum(c *httpkit.Context) {
	uid, err := userIDFromCtx(c)
	if err != nil {
		httpkit.Error(c, http.StatusUnauthorized, errcode.SessionExpired, "no session", nil)
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpkit.Error(c, http.StatusBadRequest, errcode.BadRequest, "invalid course id", nil)
		return
	}
	version, err := strconv.Atoi(strings.Trim(c.GetHeader("If-Match"), `"`))
	if err != nil || version <= 0 {
		httpkit.Error(c, http.StatusBadRequest, errcode.BadRequest, "If-Match course version is required", nil)
		return
	}
	var req curriculumRequest
	if !c.MustJSON(&req) {
		return
	}
	chapters := make([]course.ChapterInput, 0, len(req.Chapters))
	for _, chapter := range req.Chapters {
		lessons := make([]course.LessonInput, 0, len(chapter.Lessons))
		for _, lesson := range chapter.Lessons {
			lessons = append(lessons, course.LessonInput{Title: lesson.Title, Required: lesson.Required, DurationSeconds: lesson.DurationSeconds, MediaAssetID: lesson.MediaAssetID})
		}
		chapters = append(chapters, course.ChapterInput{Title: chapter.Title, Lessons: lessons})
	}
	nextVersion, err := h.repo.ReplaceCurriculum(c.Request.Context(), id, uid, version, chapters)
	if err != nil {
		mapCourseError(c, err)
		return
	}
	c.Header("ETag", strconv.Itoa(nextVersion))
	c.JSON(http.StatusOK, gin.H{"currentVersion": nextVersion, "chapters": req.Chapters})
}

func (h *CourseHandler) Submit(c *httpkit.Context) { h.transition(c, review.Submit, false, "") }

type reviewRequest struct {
	Action review.Action `json:"action" binding:"required"`
	Reason string        `json:"reason"`
}

func (h *CourseHandler) Review(c *httpkit.Context) {
	var req reviewRequest
	if !c.MustJSON(&req) {
		return
	}
	if req.Action != review.Approve && req.Action != review.Reject {
		httpkit.Error(c, http.StatusBadRequest, errcode.BadRequest, "action must be approve or reject", nil)
		return
	}
	h.transition(c, req.Action, true, req.Reason)
}

func (h *CourseHandler) Archive(c *httpkit.Context) { h.transition(c, review.Archive, true, "") }

func (h *CourseHandler) transition(c *httpkit.Context, action review.Action, admin bool, reason string) {
	uid, err := userIDFromCtx(c)
	if err != nil {
		httpkit.Error(c, http.StatusUnauthorized, errcode.SessionExpired, "no session", nil)
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpkit.Error(c, http.StatusBadRequest, errcode.BadRequest, "invalid course id", nil)
		return
	}
	updated, err := h.repo.Transition(c.Request.Context(), id, uid, action, admin, reason)
	if err != nil {
		mapCourseError(c, err)
		return
	}
	c.JSON(http.StatusOK, updated)
}

func (h *CourseHandler) Get(c *httpkit.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpkit.Error(c, http.StatusBadRequest, errcode.BadRequest, "invalid course id", nil)
		return
	}
	result, err := h.repo.GetPublished(c.Request.Context(), id)
	if err != nil {
		mapCourseError(c, err)
		return
	}
	chapters, err := h.repo.Curriculum(c.Request.Context(), id, true)
	if err != nil {
		httpkit.Internal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"course": result, "chapters": chapters})
}

func (h *CourseHandler) List(c *httpkit.Context) {
	f := course.ListFilter{Query: c.Query("q"), Limit: 20}
	if raw := c.Query("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 50 {
			httpkit.Error(c, http.StatusBadRequest, errcode.BadRequest, "limit must be between 1 and 50", nil)
			return
		}
		f.Limit = n
	}
	if raw := c.Query("teacherId"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			httpkit.Error(c, http.StatusBadRequest, errcode.BadRequest, "invalid teacherId", nil)
			return
		}
		f.TeacherID = &id
	}
	if raw := c.Query("priceMin"); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || n < 0 {
			httpkit.Error(c, http.StatusBadRequest, errcode.BadRequest, "invalid priceMin", nil)
			return
		}
		f.PriceMin = &n
	}
	if raw := c.Query("priceMax"); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || n < 0 {
			httpkit.Error(c, http.StatusBadRequest, errcode.BadRequest, "invalid priceMax", nil)
			return
		}
		f.PriceMax = &n
	}
	if raw := c.Query("before"); raw != "" {
		parts := strings.Split(raw, ",")
		if len(parts) != 2 {
			httpkit.Error(c, http.StatusBadRequest, errcode.BadRequest, "invalid cursor", nil)
			return
		}
		at, err1 := time.Parse(time.RFC3339Nano, parts[0])
		id, err2 := uuid.Parse(parts[1])
		if err1 != nil || err2 != nil {
			httpkit.Error(c, http.StatusBadRequest, errcode.BadRequest, "invalid cursor", nil)
			return
		}
		f.BeforeAt = &at
		f.BeforeID = &id
	}
	items, err := h.repo.ListPublished(c.Request.Context(), f)
	if err != nil {
		httpkit.Internal(c, err)
		return
	}
	var next string
	if len(items) == f.Limit {
		last := items[len(items)-1]
		if last.PublishedAt != nil {
			next = last.PublishedAt.Format(time.RFC3339Nano) + "," + last.ID.String()
		}
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "nextCursor": next})
}

func mapCourseError(c *httpkit.Context, err error) {
	switch {
	case errors.Is(err, course.ErrNotFound):
		httpkit.Error(c, http.StatusNotFound, errcode.NotFound, "course not found", nil)
	case errors.Is(err, course.ErrForbidden):
		httpkit.Error(c, http.StatusForbidden, errcode.Forbidden, "course access denied", nil)
	case errors.Is(err, course.ErrStaleVersion):
		httpkit.Error(c, http.StatusConflict, errcode.StaleVersion, "course was updated; reload and retry", nil)
	case errors.Is(err, course.ErrStateConflict):
		httpkit.Error(c, http.StatusConflict, errcode.CourseStateConflict, "invalid course state transition", nil)
	default:
		httpkit.Internal(c, err)
	}
}
