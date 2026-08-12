package handlers

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/x-web3/api/internal/audit"
	"github.com/x-web3/api/internal/certificate"
	"github.com/x-web3/api/internal/errcode"
	"github.com/x-web3/api/internal/httpkit"
	"github.com/x-web3/api/internal/learning"
)

// CertificateHandler 完课判定 + 证书查询（F04-T07/T08）。
type CertificateHandler struct {
	svc         *certificate.Service
	learningSvc *learning.Service
	auditor     *audit.Writer
	logger      *zap.Logger
}

func NewCertificateHandler(
	svc *certificate.Service,
	learningSvc *learning.Service,
	auditor *audit.Writer,
	logger *zap.Logger,
) *CertificateHandler {
	return &CertificateHandler{svc: svc, learningSvc: learningSvc, auditor: auditor, logger: logger}
}

// CompleteCourse POST /courses/{id}/complete — 评估完课并原子地写 mint job。
//
// 鉴权：必须已 enrollment。
// 状态：
//
//	200 OK + completion record — 已完成 / 新完成（幂等）
//	403 — 未 enrollment / 用户未绑钱包
//	404 — 课程不存在
//	422 — 进度未达 100%（partial completion）
func (h *CertificateHandler) CompleteCourse(c *httpkit.Context) {
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
	rec, err := h.svc.CompleteCourse(c.Request.Context(), uid, courseID)
	if err != nil {
		switch {
		case errors.Is(err, certificate.ErrNotEnrolled):
			httpkit.Error(c, http.StatusForbidden, errcode.NotEnrolled, "not enrolled", nil)
		case errors.Is(err, certificate.ErrNoRecipientWallet):
			httpkit.Error(c, http.StatusForbidden, errcode.NotEnrolled, "no recipient wallet", nil)
		case errors.Is(err, certificate.ErrCourseNotFound):
			httpkit.Error(c, http.StatusNotFound, errcode.NotFound, "course not found", nil)
		case errors.Is(err, certificate.ErrNotCompleted):
			// 422：与「已 completed」的 200 幂等返回明确区分。
			httpkit.Error(c, http.StatusUnprocessableEntity, errcode.BadRequest, "not all required lessons completed", nil)
		default:
			httpkit.Internal(c, err)
		}
		return
	}
	_ = h.auditor.Log(c.Request.Context(), audit.Entry{
		ActorUserID: &uid,
		Action:      audit.ActionCourseCompleted,
		TargetType:  "course",
		TargetID:    courseID.String(),
		After: map[string]any{
			"completionId":           rec.ID.String(),
			"enrollmentId":           rec.EnrollmentID.String(),
			"completedLessonsCount":  rec.CompletedLessonsCount,
			"totalLessonsCount":      rec.TotalLessonsCount,
			"certificateId":          stringPtr(rec.CertificateID),
			"onchainCertId":          rec.OnchainCertID,
			"recipientWallet":        rec.RecipientWallet,
		},
		IP:        c.ClientIP(),
		UserAgent: c.Request.UserAgent(),
	})
	c.JSON(http.StatusOK, rec)
}

func stringPtr(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}

// ListMineEnrollments GET /me/enrollments — 当前用户的 enrollment 列表 + 进度统计。
func (h *CertificateHandler) ListMineEnrollments(c *httpkit.Context) {
	uid, err := userIDFromCtx(c)
	if err != nil {
		httpkit.Error(c, http.StatusUnauthorized, errcode.SessionExpired, "no session", nil)
		return
	}
	limit := 50
	if raw := c.Query("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			limit = n
		}
	}
	// 复用 learning.Service 的 ListEnrollments；handler 持有 learningSvc。
	items, err := h.learningSvc.ListEnrollments(c.Request.Context(), uid, limit)
	if err != nil {
		httpkit.Internal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// ListMineCertificates GET /me/certificates — 当前用户的证书 job 列表。
func (h *CertificateHandler) ListMineCertificates(c *httpkit.Context) {
	uid, err := userIDFromCtx(c)
	if err != nil {
		httpkit.Error(c, http.StatusUnauthorized, errcode.SessionExpired, "no session", nil)
		return
	}
	limit := 50
	if raw := c.Query("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			limit = n
		}
	}
	items, err := h.learningSvc.ListCertificates(c.Request.Context(), uid, limit)
	if err != nil {
		httpkit.Internal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// CertificateEndpoints 注册证书相关路由。
//
// 注意：/me/enrollments 与 /me/certificates 也由本 handler 提供，
// 因为 ListEnrollments/ListCertificates 的 SQL 在 learning.Service 实现，
// 共享 handler 避免再开一个 meHandler。
func CertificateEndpoints(
	r *gin.RouterGroup,
	h *CertificateHandler,
) {
	r.POST("/:id/complete", httpkit.Wrap(h.CompleteCourse))
}