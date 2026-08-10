// Package admin — cert_retry.go 提供 POST /admin/certificates/{id}/retry。
//
// 行为：
//   - 把指定 certificate 关联的 certificate_jobs 重新置为可被 worker 抢单的状态：
//       status='pending', next_retry_at=now(), last_error=NULL
//     attempt 保持不变（worker 端 backoff 计数仍能反映历史尝试次数）。
//   - 同时把 certificates.status 同步回 'pending'，避免「jobs 是 pending 但
//     certificates 仍是 dead」导致 me/certificates 列表视图与底层状态漂移。
//   - 目标 certificates.id 不存在 → 404；
//   - 当前 status 已为 'confirmed' → 409（已上链的证书不能被 admin retry）；
//   - audit 留痕（ActionCertificateMintJob）。
package admin

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/x-web3/api/internal/audit"
	"github.com/x-web3/api/internal/errcode"
	"github.com/x-web3/api/internal/httpkit"
	"github.com/x-web3/api/internal/rbac"
	"github.com/x-web3/api/internal/user"
)

// CertRetryHandler 暴露 admin 端的证书 mint 重试入口。
type CertRetryHandler struct {
	pool    *pgxpool.Pool
	auditor *audit.Writer
	rbac    *rbac.Engine
	logger  *zap.Logger
}

// NewCertRetryHandler 构造 handler。
func NewCertRetryHandler(pool *pgxpool.Pool, auditor *audit.Writer, rbac *rbac.Engine, logger *zap.Logger) *CertRetryHandler {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &CertRetryHandler{pool: pool, auditor: auditor, rbac: rbac, logger: logger}
}

// Retry POST /admin/certificates/{id}/retry。
//
//   - id: certificates.id（uuid）；
//   - 仅在 status='dead' 或 'failed' 时允许 retry；其它状态返回 409；
//   - 实际更新走事务：同时翻 certificates.status 与 certificate_jobs.status。
func (h *CertRetryHandler) Retry(c *httpkit.Context) {
	uid, err := userIDFromCtx(c)
	if err != nil {
		httpkit.Error(c, http.StatusUnauthorized, errcode.SessionExpired, "no session", nil)
		return
	}
	if err := h.rbac.RequireRole(user.RoleSuperAdmin)(c.Request.Context(), uid); err != nil {
		httpkit.Error(c, http.StatusForbidden, errcode.Forbidden, "permission denied", nil)
		return
	}
	certID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpkit.Error(c, http.StatusBadRequest, errcode.BadRequest, "id must be a uuid", nil)
		return
	}

	updated, prevStatus, attempt, err := h.retryCert(c.Request.Context(), certID)
	if err != nil {
		switch {
		case errors.Is(err, errCertNotFound):
			httpkit.Error(c, http.StatusNotFound, errcode.NotFound, "certificate not found", nil)
			return
		case errors.Is(err, errCertNotRetryable):
			httpkit.Error(c, http.StatusConflict, errcode.Conflict,
				"certificate is not in a retryable state (only 'dead' / 'failed' are retryable)", nil)
			return
		}
		h.logger.Error("admin_retry_cert_failed",
			zap.String("certificateId", certID.String()), zap.Error(err))
		httpkit.Internal(c, err)
		return
	}

	_ = h.auditor.Log(c.Request.Context(), audit.Entry{
		ActorUserID:   &uid,
		Action:        audit.ActionCertificateMintJob,
		TargetType:    "certificate",
		TargetID:      certID.String(),
		Before:        map[string]any{"status": prevStatus, "attempt": attempt},
		After:         map[string]any{"status": "pending", "nextRetryAt": time.Now().UTC().Format(time.RFC3339)},
		IP:            c.ClientIP(),
		UserAgent:     c.Request.UserAgent(),
		CorrelationID: c.RequestID(),
	})

	c.JSON(http.StatusOK, gin.H{
		"certificateId": certID,
		"previousStatus": prevStatus,
		"attempt":       attempt,
		"nextRetryAt":   time.Now().UTC().Format(time.RFC3339),
		"requeuedAt":    time.Now().UTC().Format(time.RFC3339),
		"requeued":      updated,
	})
}

// errCertNotFound / errCertNotRetryable 哨兵错误。
var (
	errCertNotFound     = errors.New("cert retry: certificate not found")
	errCertNotRetryable = errors.New("cert retry: certificate not in retryable state")
)

// retryCert 在事务内：
//   - 取 certificates.status；
//   - 若 status='dead' 或 'failed'：把 certificates.status='pending' 并把
//     certificate_jobs.status='pending', next_retry_at=now(), last_error=NULL；
//   - 若 status='confirmed' / 'minting' / 'pending'：返回 errCertNotRetryable
//     （已上链 / 已在跑 / 已经是 pending 都不应再被 admin 触发 retry）；
//   - 其它 → 同样按 errCertNotRetryable 处理（保守拒绝）。
func (h *CertRetryHandler) retryCert(ctx context.Context, certID uuid.UUID) (bool, string, int, error) {
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		return false, "", 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var status string
	var attempt int
	err = tx.QueryRow(ctx, `
SELECT status, attempts
FROM certificates
WHERE id = $1
FOR UPDATE`, certID).Scan(&status, &attempt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, "", 0, errCertNotFound
		}
		return false, "", 0, err
	}
	switch status {
	case "dead", "failed":
		// 允许 retry
	default:
		return false, status, attempt, errCertNotRetryable
	}

	// 翻 certificates.status。
	if _, err := tx.Exec(ctx, `
UPDATE certificates
SET status = 'pending', updated_at = now()
WHERE id = $1`, certID); err != nil {
		return false, status, attempt, err
	}
	// 翻 certificate_jobs：worker hot-path 索引是 (status='pending', next_retry_at, created_at)，
	// next_retry_at 设为 now() 让 worker 立即可消费。
	if _, err := tx.Exec(ctx, `
UPDATE certificate_jobs
SET status = 'pending', next_retry_at = now(), last_error = NULL, updated_at = now()
WHERE certificate_id = $1`, certID); err != nil {
		return false, status, attempt, err
	}

	if err := tx.Commit(ctx); err != nil {
		return false, status, attempt, err
	}
	return true, status, attempt, nil
}