package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/x-web3/api/internal/audit"
	"github.com/x-web3/api/internal/errcode"
	"github.com/x-web3/api/internal/httpkit"
	"github.com/x-web3/api/internal/learning"
)

// LearningHandler 播放凭证签发。
type LearningHandler struct {
	svc     *learning.Service
	auditor *audit.Writer
	logger  *zap.Logger
}

func NewLearningHandler(svc *learning.Service, auditor *audit.Writer, logger *zap.Logger) *LearningHandler {
	return &LearningHandler{svc: svc, auditor: auditor, logger: logger}
}

// GetPlayback 已购买学生获取正式播放凭证。
//
// 未登录返回 401；未购买 / 未发布媒体返回 403。
func (h *LearningHandler) GetPlayback(c *httpkit.Context) {
	uid, err := userIDFromCtx(c)
	if err != nil {
		httpkit.Error(c, http.StatusUnauthorized, errcode.SessionExpired, "no session", nil)
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpkit.Error(c, http.StatusBadRequest, errcode.BadRequest, "invalid lesson id", nil)
		return
	}
	cred, err := h.svc.Playback(c.Request.Context(), id, uid)
	if err != nil {
		mapLearningErr(c, err)
		return
	}
	_ = h.auditor.Log(c.Request.Context(), audit.Entry{
		ActorUserID: &uid,
		Action:      audit.ActionPlaybackIssued,
		TargetType:  "lesson",
		TargetID:    id.String(),
		After: map[string]any{
			"purpose":   string(cred.Purpose),
			"expiresAt": cred.ExpiresAt.UTC(),
		},
		IP:        c.ClientIP(),
		UserAgent: c.Request.UserAgent(),
	})
	c.JSON(http.StatusOK, cred)
}

// GetPreview 教师预览自己 draft 课程的 lesson；返回带 purpose=preview 的凭证。
func (h *LearningHandler) GetPreview(c *httpkit.Context) {
	uid, err := userIDFromCtx(c)
	if err != nil {
		httpkit.Error(c, http.StatusUnauthorized, errcode.SessionExpired, "no session", nil)
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpkit.Error(c, http.StatusBadRequest, errcode.BadRequest, "invalid lesson id", nil)
		return
	}
	cred, err := h.svc.Preview(c.Request.Context(), id, uid)
	if err != nil {
		mapLearningErr(c, err)
		return
	}
	c.JSON(http.StatusOK, cred)
}

// progressRequest POST /lessons/{id}/progress 请求体。
type progressRequest struct {
	Pct int `json:"pct" binding:"required"`
}

// PostProgress 写课时进度（F04-T08）。
//
// 200 OK：新 pct（首次写或递增）；
// 200 OK + 与原值相同：幂等；
// 403：未 enrollment；
// 404：lesson 不存在；
// 409：倒退拒绝。
func (h *LearningHandler) PostProgress(c *httpkit.Context) {
	uid, err := userIDFromCtx(c)
	if err != nil {
		httpkit.Error(c, http.StatusUnauthorized, errcode.SessionExpired, "no session", nil)
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpkit.Error(c, http.StatusBadRequest, errcode.BadRequest, "invalid lesson id", nil)
		return
	}
	var body progressRequest
	if !c.MustJSON(&body) {
		return
	}
	got, err := h.svc.ReportProgress(c.Request.Context(), uid, id, body.Pct)
	if err != nil {
		mapLearningErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"lessonId": id, "pct": got})
}

func mapLearningErr(c *httpkit.Context, err error) {
	switch {
	case errors.Is(err, learning.ErrLessonNotFound), errors.Is(err, learning.ErrLessonMissing):
		httpkit.Error(c, http.StatusNotFound, errcode.NotFound, "lesson not found", nil)
	case errors.Is(err, learning.ErrNotEligible), errors.Is(err, learning.ErrLessonAccessDenied):
		httpkit.Error(c, http.StatusForbidden, errcode.NotEnrolled, "not enrolled or not the author", nil)
	case errors.Is(err, learning.ErrMediaNotReady):
		httpkit.Error(c, http.StatusConflict, errcode.MediaNotReady, "media not ready", nil)
	case errors.Is(err, learning.ErrProgressRegression):
		httpkit.Error(c, http.StatusConflict, errcode.ProgressRegression, "progress cannot regress", nil)
	default:
		httpkit.Internal(c, err)
	}
}

// LearningEndpoints 装配路由，便于 main.go 调用。
func LearningEndpoints(r *gin.RouterGroup, h *LearningHandler) {
	r.GET("/:id/playback", httpkit.Wrap(h.GetPlayback))
	r.GET("/:id/preview", httpkit.Wrap(h.GetPreview))
	r.POST("/:id/progress", httpkit.Wrap(h.PostProgress))
}
