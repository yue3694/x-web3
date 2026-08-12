package handlers

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/x-web3/api/internal/audit"
	"github.com/x-web3/api/internal/comment"
	"github.com/x-web3/api/internal/errcode"
	"github.com/x-web3/api/internal/httpkit"
)

// CommentHandler 课程评论。
type CommentHandler struct {
	repo    *comment.Repo
	auditor *audit.Writer
	logger  *zap.Logger
}

func NewCommentHandler(repo *comment.Repo, auditor *audit.Writer, logger *zap.Logger) *CommentHandler {
	return &CommentHandler{repo: repo, auditor: auditor, logger: logger}
}

type createCommentReq struct {
	Body string `json:"body" binding:"required"`
}

// PostCreate 学生在已购买课程下写评论；moderation 状态由后端决定（默认 pending）。
//
// 公开课程才能评论；权限校验已下放到 repo.userHasPurchased。
func (h *CommentHandler) PostCreate(c *httpkit.Context) {
	uid, err := userIDFromCtx(c)
	if err != nil {
		httpkit.Error(c, http.StatusUnauthorized, errcode.SessionExpired, "no session", nil)
		return
	}
	courseID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpkit.Error(c, http.StatusBadRequest, errcode.BadRequest, "invalid course id", nil)
		return
	}
	var req createCommentReq
	if !c.MustJSON(&req) {
		return
	}
	cm, err := h.repo.Create(c.Request.Context(), courseID, uid, req.Body)
	if err != nil {
		mapCommentErr(c, err)
		return
	}
	_ = h.auditor.Log(c.Request.Context(), audit.Entry{
		ActorUserID: &uid,
		Action:      audit.ActionCommentCreated,
		TargetType:  "comment",
		TargetID:    cm.ID.String(),
		IP:          c.ClientIP(),
		UserAgent:   c.Request.UserAgent(),
	})
	c.JSON(http.StatusCreated, cm)
}

// GetCourseComments 课程的公开评论流；已登录用户能看自己的全部状态。
//
// 未登录 viewer 走空 uuid：列表自然只返回 approved。
func (h *CommentHandler) GetCourseComments(c *httpkit.Context) {
	courseID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpkit.Error(c, http.StatusBadRequest, errcode.BadRequest, "invalid course id", nil)
		return
	}
	var viewer uuid.UUID
	if raw := c.UserID(); raw != "" {
		viewer, _ = uuid.Parse(raw)
	}
	limit := 50
	if raw := c.Query("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	items, err := h.repo.ListByCourse(c.Request.Context(), courseID, viewer, limit)
	if err != nil {
		httpkit.Internal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// DeleteMine 软删自己的评论。
func (h *CommentHandler) DeleteMine(c *httpkit.Context) {
	uid, err := userIDFromCtx(c)
	if err != nil {
		httpkit.Error(c, http.StatusUnauthorized, errcode.SessionExpired, "no session", nil)
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpkit.Error(c, http.StatusBadRequest, errcode.BadRequest, "invalid comment id", nil)
		return
	}
	if err := h.repo.SoftDelete(c.Request.Context(), id, uid); err != nil {
		mapCommentErr(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// GetMyComments 当前用户的所有评论（含 pending / approved / rejected）。
// 软删的行不再返回。
//
// 对应 OpenAPI：GET /me/comments?limit=
func (h *CommentHandler) GetMyComments(c *httpkit.Context) {
	uid, err := userIDFromCtx(c)
	if err != nil {
		httpkit.Error(c, http.StatusUnauthorized, errcode.SessionExpired, "no session", nil)
		return
	}
	limit := 50
	if raw := c.Query("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	items, err := h.repo.ListMyByUser(c.Request.Context(), uid, limit)
	if err != nil {
		httpkit.Internal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

type moderateReq struct {
	Status string `json:"status" binding:"required"`
}

// PatchModerate 管理员审批；权限由路由层 rbac.Middleware 保证。
func (h *CommentHandler) PatchModerate(c *httpkit.Context) {
	uid, err := userIDFromCtx(c)
	if err != nil {
		httpkit.Error(c, http.StatusUnauthorized, errcode.SessionExpired, "no session", nil)
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpkit.Error(c, http.StatusBadRequest, errcode.BadRequest, "invalid comment id", nil)
		return
	}
	var req moderateReq
	if !c.MustJSON(&req) {
		return
	}
	cm, err := h.repo.Moderate(c.Request.Context(), id, req.Status)
	if err != nil {
		mapCommentErr(c, err)
		return
	}
	_ = h.auditor.Log(c.Request.Context(), audit.Entry{
		ActorUserID: &uid,
		Action:      audit.ActionCommentModerated,
		TargetType:  "comment",
		TargetID:    id.String(),
		After:       map[string]any{"status": req.Status},
		IP:          c.ClientIP(),
		UserAgent:   c.Request.UserAgent(),
	})
	c.JSON(http.StatusOK, cm)
}

func mapCommentErr(c *httpkit.Context, err error) {
	switch {
	case errors.Is(err, comment.ErrNotFound):
		httpkit.Error(c, http.StatusNotFound, errcode.NotFound, "comment not found", nil)
	case errors.Is(err, comment.ErrForbidden):
		httpkit.Error(c, http.StatusForbidden, errcode.Forbidden, "not the author", nil)
	case errors.Is(err, comment.ErrEmptyBody), errors.Is(err, comment.ErrBodyTooLong):
		httpkit.Error(c, http.StatusBadRequest, errcode.BadRequest, err.Error(), nil)
	case errors.Is(err, comment.ErrNotPurchased):
		httpkit.Error(c, http.StatusForbidden, errcode.CommentNotPurchased, "only purchased users may comment", nil)
	case errors.Is(err, comment.ErrAlreadyDeleted):
		httpkit.Error(c, http.StatusConflict, errcode.Conflict, "already deleted", nil)
	default:
		httpkit.Internal(c, err)
	}
}
