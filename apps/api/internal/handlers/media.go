package handlers

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/x-web3/api/internal/audit"
	"github.com/x-web3/api/internal/errcode"
	"github.com/x-web3/api/internal/httpkit"
	"github.com/x-web3/api/internal/media"
)

// MediaHandler 上传意图 + finalize + 列表。
type MediaHandler struct {
	repo    *media.Repo
	store   media.ObjectStore
	auditor *audit.Writer
	logger  *zap.Logger
}

func NewMediaHandler(repo *media.Repo, store media.ObjectStore, auditor *audit.Writer, logger *zap.Logger) *MediaHandler {
	return &MediaHandler{repo: repo, store: store, auditor: auditor, logger: logger}
}

type uploadIntentReq struct {
	FileName    string `json:"fileName"    binding:"required"`
	ContentType string `json:"contentType" binding:"required"`
	SizeBytes   int64  `json:"sizeBytes"   binding:"required"`
}

// PostUploadIntent 创建 media_assets(draft) + 返回 presigned PUT URL。
//
// 需要登录；目前不强制 MEDIA_UPLOAD 权限（编辑器是老师唯一入口，路由已经
// 受 teacher group 保护），handler 不再加 rbac 中间件。
func (h *MediaHandler) PostUploadIntent(c *httpkit.Context) {
	uid, err := userIDFromCtx(c)
	if err != nil {
		httpkit.Error(c, http.StatusUnauthorized, errcode.SessionExpired, "no session", nil)
		return
	}
	var req uploadIntentReq
	if !c.MustJSON(&req) {
		return
	}
	asset, url, exp, err := h.repo.CreateIntent(c.Request.Context(), uid, req.FileName, req.ContentType, req.SizeBytes, h.store, media.DefaultMaxBytes)
	if err != nil {
		mapMediaErr(c, err)
		return
	}
	_ = h.auditor.Log(c.Request.Context(), audit.Entry{
		ActorUserID: &uid,
		Action:      audit.ActionMediaIntentCreated,
		TargetType:  "media_asset",
		TargetID:    asset.ID.String(),
		IP:          c.ClientIP(),
		UserAgent:   c.Request.UserAgent(),
	})
	c.JSON(http.StatusCreated, gin.H{
		"mediaAssetId": asset.ID,
		"s3Key":        asset.S3Key,
		"uploadUrl":    url,
		"expiresAt":    exp.UTC(),
	})
}

type finalizeReq struct {
	ChecksumSHA256 string `json:"checksumSha256"`
}

// PostFinalize HeadObject + 可选 checksum 校验 → status='ready'。
func (h *MediaHandler) PostFinalize(c *httpkit.Context) {
	uid, err := userIDFromCtx(c)
	if err != nil {
		httpkit.Error(c, http.StatusUnauthorized, errcode.SessionExpired, "no session", nil)
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpkit.Error(c, http.StatusBadRequest, errcode.BadRequest, "invalid media id", nil)
		return
	}
	var req finalizeReq
	_ = c.ShouldBindJSON(&req)
	asset, err := h.repo.Finalize(c.Request.Context(), id, uid, req.ChecksumSHA256, h.store)
	if err != nil {
		mapMediaErr(c, err)
		return
	}
	c.JSON(http.StatusOK, asset)
}

// GetMine 列出当前用户上传的 asset。
func (h *MediaHandler) GetMine(c *httpkit.Context) {
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
	items, err := h.repo.ListByOwner(c.Request.Context(), uid, limit)
	if err != nil {
		httpkit.Internal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func mapMediaErr(c *httpkit.Context, err error) {
	switch {
	case errors.Is(err, media.ErrNotFound):
		httpkit.Error(c, http.StatusNotFound, errcode.NotFound, "media not found", nil)
	case errors.Is(err, media.ErrForbidden):
		httpkit.Error(c, http.StatusForbidden, errcode.Forbidden, "not the owner", nil)
	case errors.Is(err, media.ErrBadMIME):
		httpkit.Error(c, http.StatusBadRequest, errcode.BadRequest, "content-type not allowed", nil)
	case errors.Is(err, media.ErrSizeTooLarge):
		httpkit.Error(c, http.StatusRequestEntityTooLarge, errcode.BadRequest, "media too large", nil)
	case errors.Is(err, media.ErrAlreadyReady):
		httpkit.Error(c, http.StatusConflict, errcode.Conflict, "already finalized", nil)
	case errors.Is(err, media.ErrChecksumBad):
		httpkit.Error(c, http.StatusUnprocessableEntity, errcode.MediaChecksumMismatch, err.Error(), nil)
	case errors.Is(err, media.ErrObjectMissing):
		httpkit.Error(c, http.StatusBadRequest, errcode.MediaNotReady, "object not uploaded yet", nil)
	default:
		httpkit.Internal(c, err)
	}
}

// MediaEndpoints 装配路由，便于 main.go 调用。
func MediaEndpoints(r *gin.RouterGroup, h *MediaHandler) {
	r.POST("/upload-intent", httpkit.Wrap(h.PostUploadIntent))
	r.POST("/:id/finalize", httpkit.Wrap(h.PostFinalize))
	r.GET("", httpkit.Wrap(h.GetMine))
}
