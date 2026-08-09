// Package audit 实现 append-only 审计写入。
//
// 设计原则：
//   - service 层调 Audit.Log(ctx, ...)；
//   - 表上无 UPDATE/DELETE 权限（DB role 限制）；
//   - 写失败：blocking error 还是 non-blocking？
//     当前选择 blocking（返回 error），由 service 层决定是否 wrap 成 500。
package audit

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

// Action 是审计动词（动词过去式）。
type Action string

const (
	ActionUserCreated   Action = "user.created"
	ActionUserLoggedIn  Action = "user.logged_in"
	ActionWalletLinked  Action = "wallet.linked"
	ActionWalletUnbound Action = "wallet.unbound"
	ActionRoleGranted   Action = "role.granted"
	ActionRoleRevoked   Action = "role.revoked"
	ActionCourseCreated Action = "course.created"
	ActionCourseReview  Action = "course.review"
	ActionOrderCreated  Action = "order.created"
	ActionChainReplayed Action = "chain.replayed"
)

// Entry 是单条审计写入参数。
type Entry struct {
	ActorUserID *uuid.UUID
	Action      Action
	TargetType  string
	TargetID    string
	Before      any
	After       any
	IP          string
	UserAgent   string
	// CorrelationID 从 context 注入；默认从 gin ctx 取 "request_id"。
	CorrelationID string
}

// Writer append-only writer。
type Writer struct {
	pool   *pgxpool.Pool
	logger *zap.Logger
}

func NewWriter(pool *pgxpool.Pool, logger *zap.Logger) *Writer {
	return &Writer{pool: pool, logger: logger}
}

// Log 写入一条审计。必须在调用方的事务外执行（audit 落库不影响业务事务）。
//
// 如果你希望 audit 与业务同事务，传 tx 进来。
func (w *Writer) Log(ctx context.Context, e Entry) error {
	beforeB, _ := json.Marshal(e.Before)
	afterB, _ := json.Marshal(e.After)
	if e.CorrelationID == "" {
		// 兜底：调用方未注入；middleware 一般已注入
		e.CorrelationID = time.Now().UTC().Format(time.RFC3339Nano)
	}
	const q = `
INSERT INTO audit_logs
  (actor_user_id, action, target_type, target_id, before, after, correlation_id, ip, user_agent)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`
	_, err := w.pool.Exec(ctx, q,
		e.ActorUserID, string(e.Action), e.TargetType, e.TargetID,
		beforeB, afterB, e.CorrelationID, e.IP, e.UserAgent,
	)
	if err != nil {
		w.logger.Error("audit_write_failed",
			zap.String("action", string(e.Action)),
			zap.Error(err),
		)
	}
	return err
}

// LogTx 与业务事务同 commit；用于"做这件事 → 留痕"必须同生共死。
func (w *Writer) LogTx(ctx context.Context, tx pgx.Tx, e Entry) error {
	beforeB, _ := json.Marshal(e.Before)
	afterB, _ := json.Marshal(e.After)
	if e.CorrelationID == "" {
		e.CorrelationID = time.Now().UTC().Format(time.RFC3339Nano)
	}
	const q = `
INSERT INTO audit_logs
  (actor_user_id, action, target_type, target_id, before, after, correlation_id, ip, user_agent)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`
	_, err := tx.Exec(ctx, q,
		e.ActorUserID, string(e.Action), e.TargetType, e.TargetID,
		beforeB, afterB, e.CorrelationID, e.IP, e.UserAgent,
	)
	return err
}
