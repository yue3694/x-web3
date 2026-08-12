// Package reconcile 提供漏块扫描 + DLQ 写。
//
// 模块拆分：
//   - dlq.go：DLQ writer（写 + 重试 + 解决标记）。
//   - scanner.go：定时扫描器，检测 [lastConfirmed-ConfirmDepth, lastIndexed] 范围内的漏块。
//
// 触发源：
//   1. apps/worker 内部 ticker；
//   2. admin POST /admin/dlq/{id}/retry；
//   3. manual_scan flag for ops.
package reconcile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Entry 是写入 dlq_events 的入参。
type Entry struct {
	Consumer string         // 'indexer' | 'reconcile' | 'confirmer'
	ChainID  *int64         // 链上事件必须；其它场景可空
	Kind     string         // 'gap' | 'reorg' | 'decode' | 'persist'
	Severity string         // 'warn' | 'error'
	Summary  string
	Payload  map[string]any // 任意 JSON
}

// DLQStore 把 dlq_events 操作封装为接口。
type DLQStore interface {
	Write(ctx context.Context, e Entry) (int64, error)
	ListUnresolved(ctx context.Context, limit int) ([]DLQRow, error)
	Get(ctx context.Context, id int64) (*DLQRow, error)
	MarkResolved(ctx context.Context, id int64, resolver uuid.UUID, resolution string) error
	IncrementRetry(ctx context.Context, id int64) (int, error)
}

// DLQRow 是查询返回结构。
type DLQRow struct {
	ID          int64
	Consumer    string
	ChainID     *int64
	Kind        string
	Severity    string
	Summary     string
	Payload     map[string]any
	RetryCount  int
	Resolved    bool
	ResolvedAt  *time.Time
	ResolvedBy  *uuid.UUID
	Resolution  *string
	CreatedAt   time.Time
}

// Writer 是 DLQ 写入端的轻量包装。
//
// 写入失败不会 panic；返回 error 让调用方决定是否要再回退。
// 所有 error 应当 wrap 以保留 %w 链。
type Writer struct {
	store   DLQStore
	logger  *slog.Logger
}

// NewWriter 构造 DLQ writer。
func NewWriter(store DLQStore, logger *slog.Logger) *Writer {
	if logger == nil {
		logger = slog.Default()
	}
	return &Writer{store: store, logger: logger}
}

// Write 写入一条 DLQ。
//
// Consumer 为空时返回 ErrConsumerRequired；Kind / Summary 不能为空。
func (w *Writer) Write(ctx context.Context, e Entry) (int64, error) {
	if e.Consumer == "" {
		return 0, ErrConsumerRequired
	}
	if e.Kind == "" || e.Summary == "" {
		return 0, errors.New("dlq: kind and summary required")
	}
	if e.Severity == "" {
		e.Severity = "warn"
	}
	id, err := w.store.Write(ctx, e)
	if err != nil {
		w.logger.Error("dlq_write_failed", "consumer", e.Consumer, "kind", e.Kind, "err", err.Error())
		return 0, err
	}
	w.logger.Warn("dlq_recorded",
		"id", id, "consumer", e.Consumer, "kind", e.Kind, "summary", e.Summary,
		"chainId", deref(e.ChainID), "severity", e.Severity)
	return id, nil
}

// ErrConsumerRequired Consumer 字段是必填（避免误把任意错误归到 DLQ）。
var ErrConsumerRequired = errors.New("dlq: consumer required")

func deref(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

// pgDLQStore 是 *pgxpool.Pool 的实现。
type pgDLQStore struct{ pool *pgxpool.Pool }

// NewPGDLQStore 构造 PG 版 DLQStore。
func NewPGDLQStore(pool *pgxpool.Pool) DLQStore { return &pgDLQStore{pool: pool} }

func (s *pgDLQStore) Write(ctx context.Context, e Entry) (int64, error) {
	payload := e.Payload
	if payload == nil {
		payload = map[string]any{}
	}
	raw, _ := json.Marshal(payload)
	var id int64
	err := s.pool.QueryRow(ctx, `
INSERT INTO dlq_events(consumer, chain_id, kind, severity, summary, payload)
VALUES($1,$2,$3,$4,$5,$6::jsonb)
RETURNING id`,
		e.Consumer, e.ChainID, e.Kind, e.Severity, e.Summary, raw).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("dlq insert: %w", err)
	}
	return id, nil
}

func (s *pgDLQStore) ListUnresolved(ctx context.Context, limit int) ([]DLQRow, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `
SELECT id, consumer, chain_id, kind, severity, summary, payload, retry_count,
       resolved, resolved_at, resolved_by, resolution, created_at
FROM dlq_events
WHERE resolved = false
ORDER BY created_at DESC
LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("dlq list: %w", err)
	}
	defer rows.Close()
	out := make([]DLQRow, 0, limit)
	for rows.Next() {
		var r DLQRow
		var raw []byte
		if err := rows.Scan(&r.ID, &r.Consumer, &r.ChainID, &r.Kind, &r.Severity,
			&r.Summary, &raw, &r.RetryCount, &r.Resolved, &r.ResolvedAt,
			&r.ResolvedBy, &r.Resolution, &r.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(raw, &r.Payload)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *pgDLQStore) Get(ctx context.Context, id int64) (*DLQRow, error) {
	var r DLQRow
	var raw []byte
	err := s.pool.QueryRow(ctx, `
SELECT id, consumer, chain_id, kind, severity, summary, payload, retry_count,
       resolved, resolved_at, resolved_by, resolution, created_at
FROM dlq_events WHERE id=$1`, id).Scan(
		&r.ID, &r.Consumer, &r.ChainID, &r.Kind, &r.Severity, &r.Summary, &raw,
		&r.RetryCount, &r.Resolved, &r.ResolvedAt, &r.ResolvedBy, &r.Resolution, &r.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(raw, &r.Payload)
	return &r, nil
}

func (s *pgDLQStore) MarkResolved(ctx context.Context, id int64, resolver uuid.UUID, resolution string) error {
	if resolution == "" {
		return errors.New("dlq: resolution required")
	}
	tag, err := s.pool.Exec(ctx, `UPDATE dlq_events
SET resolved=true, resolved_at=now(), resolved_by=$2, resolution=$3
WHERE id=$1 AND resolved=false`, id, resolver, resolution)
	if err != nil {
		return fmt.Errorf("dlq resolve: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrAlreadyResolved
	}
	return nil
}

// ErrAlreadyResolved 重复 resolve 返回。
var ErrAlreadyResolved = errors.New("dlq: already resolved")

func (s *pgDLQStore) IncrementRetry(ctx context.Context, id int64) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `UPDATE dlq_events SET retry_count = retry_count + 1
WHERE id=$1 RETURNING retry_count`, id).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("dlq retry: %w", err)
	}
	return n, nil
}
