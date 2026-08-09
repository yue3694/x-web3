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
	ActionMediaIntentCreated Action = "media.intent_created"
	ActionMediaFinalized     Action = "media.finalized"
	ActionCommentCreated     Action = "comment.created"
	ActionCommentModerated   Action = "comment.moderated"
	ActionPlaybackIssued     Action = "playback.issued"
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

// Sink 抽象出底层的 SQL 执行；生产由 pgxpool.Pool 提供，
// 单测可注入 stub 以避免依赖真实 DB。返回值是 pgconn.CommandTag
// 或其测试替身，因此用 any 暴露。
type Sink interface {
	Exec(ctx context.Context, sql string, args ...any) (any, error)
}

// poolSink 把 *pgxpool.Pool 适配成 Sink。
type poolSink struct{ pool *pgxpool.Pool }

func (s *poolSink) Exec(ctx context.Context, sql string, args ...any) (any, error) {
	return s.pool.Exec(ctx, sql, args...)
}

// Writer append-only writer。
type Writer struct {
	sink   Sink
	logger *zap.Logger
}

// NewWriter 构造生产用 Writer；底层 Sink 走 pgxpool。
func NewWriter(pool *pgxpool.Pool, logger *zap.Logger) *Writer {
	return NewWriterWithSink(&poolSink{pool: pool}, logger)
}

// NewWriterWithSink 允许测试注入自定义 Sink（保持生产路径不变）。
func NewWriterWithSink(sink Sink, logger *zap.Logger) *Writer {
	return &Writer{sink: sink, logger: logger}
}

// FillCorrelationID 当 Entry.CorrelationID 为空时生成一个时间戳兜底值。
// 暴露为独立函数以便单测断言。
func FillCorrelationID(e *Entry) {
	if e.CorrelationID == "" {
		e.CorrelationID = time.Now().UTC().Format(time.RFC3339Nano)
	}
}

// Log 写入一条审计。必须在调用方的事务外执行（audit 落库不影响业务事务）。
//
// 如果你希望 audit 与业务同事务，传 tx 进来。
func (w *Writer) Log(ctx context.Context, e Entry) error {
	beforeB, _ := json.Marshal(e.Before)
	afterB, _ := json.Marshal(e.After)
	FillCorrelationID(&e)
	_, err := w.sink.Exec(ctx, auditInsertSQL,
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
	FillCorrelationID(&e)
	_, err := tx.Exec(ctx, auditInsertSQL,
		e.ActorUserID, string(e.Action), e.TargetType, e.TargetID,
		beforeB, afterB, e.CorrelationID, e.IP, e.UserAgent,
	)
	return err
}

// auditInsertSQL 集中维护 INSERT 语句。
const auditInsertSQL = `
INSERT INTO audit_logs
  (actor_user_id, action, target_type, target_id, before, after, correlation_id, ip, user_agent)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`

// AuditInsertSQLForTest 暴露 INSERT 语句供测试断言；
// 生产代码请走 Log / LogTx 入口，不要直接 Exec 此 SQL。
func AuditInsertSQLForTest() string { return auditInsertSQL }
