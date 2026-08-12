// Package admin — dlq_store.go 提供基于 pgxpool 的 DLQ store 实现，
// 让 admin handler 可以直接消费 dlq_events 表。
//
// 之所以不复用 apps/worker/reconcile：
//   - apps/api 与 apps/worker 是两个 Go module，跨 module 引用 internal 包违反 Go 习惯；
//   - admin 端只暴露 unresolved 列表 / retry / 解决标记，不需要 scanner；
//   - 把这部分放在 admin 包内，未来若提取出 apps/shared/dlq 公共包可平滑迁移。
package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PGDLQStore 是 *pgxpool.Pool 上 dlqStore 接口的实现。
type PGDLQStore struct {
	pool *pgxpool.Pool
}

// NewPGDLQStore ...
func NewPGDLQStore(pool *pgxpool.Pool) *PGDLQStore {
	return &PGDLQStore{pool: pool}
}

// ListUnresolved 拉最近 N 条未解决 DLQ。
func (s *PGDLQStore) ListUnresolved(ctx context.Context, limit int) ([]dlqRow, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `
SELECT id, consumer, chain_id, kind, severity, summary, payload, retry_count,
       resolved, created_at, resolution
FROM dlq_events
WHERE resolved = false
ORDER BY created_at DESC
LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("dlq list: %w", err)
	}
	defer rows.Close()
	out := make([]dlqRow, 0, limit)
	for rows.Next() {
		var r dlqRow
		var payloadRaw []byte
		if err := rows.Scan(&r.ID, &r.Consumer, &r.ChainID, &r.Kind, &r.Severity,
			&r.Summary, &payloadRaw, &r.RetryCount, &r.Resolved, &r.CreatedAt, &r.Resolution); err != nil {
			return nil, err
		}
		if len(payloadRaw) > 0 {
			_ = json.Unmarshal(payloadRaw, &r.Payload)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// IncrementRetry 把 retry_count + 1；返回最新值。
func (s *PGDLQStore) IncrementRetry(ctx context.Context, id int64) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `UPDATE dlq_events
SET retry_count = retry_count + 1
WHERE id=$1
RETURNING retry_count`, id).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("dlq inc: %w", err)
	}
	return n, nil
}

// MarkResolved 把 id 标记为已解决；记录 resolver + resolution。
//
// 重复 resolve 走 unique 路径：保持 SQL 干净。
func (s *PGDLQStore) MarkResolved(ctx context.Context, id int64, resolver uuid.UUID, resolution string) error {
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
		// 行不存在 / 已 resolved
		var exists bool
		_ = s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM dlq_events WHERE id=$1)`, id).Scan(&exists)
		if !exists {
			return pgx.ErrNoRows
		}
		return errAlreadyResolved
	}
	return nil
}

// ResolveAndIncrementRetry 原子地把「resolved=true, retry_count += 1」一起做。
//
// 设计动机：原 admin handler 顺序是 IncRetry → MarkResolved；当 MarkResolved 失败
//（行不存在 / 已 resolved / DB 抖动）时，retry_count 已被白白 +1，破坏了 DLQ 的
// 「retry 数 = 真正尝试次数」不变量。本函数把两件事合并到单条 UPDATE：在 WHERE
// 子句携带 resolved=false 锁，让 PG 自己保证「只有真正翻转状态的那一行」才递增
// retry_count。调用方根据 resolved 返回值区分三种结局：
//
//   - resolved=true,  err=nil  ：成功，retry 已 +1；
//   - resolved=false, err=errAlreadyResolved ：行已 resolved，不动 retry；
//   - resolved=false, err=ErrNoRows          ：行不存在，PG 函数里走 EXISTS 探测。
//
// Retry 计数永远跟在 resolve 后面，不会再出现「计数器 +1 但状态没变」的脏数据。
func (s *PGDLQStore) ResolveAndIncrementRetry(
	ctx context.Context, id int64, resolver uuid.UUID, resolution string,
) (bool, error) {
	if resolution == "" {
		return false, errors.New("dlq: resolution required")
	}
	// 关键：UPDATE 一次性翻 resolved + 累加 retry_count，并 RETURNING 影响行数 + 新 retry_count。
	tag, err := s.pool.Exec(ctx, `UPDATE dlq_events
SET resolved=true,
    resolved_at=now(),
    resolved_by=$2,
    resolution=$3,
    retry_count = retry_count + 1
WHERE id=$1 AND resolved=false`, id, resolver, resolution)
	if err != nil {
		return false, fmt.Errorf("dlq resolve+inc: %w", err)
	}
	if tag.RowsAffected() == 1 {
		return true, nil
	}
	// 0 行受影响：要么已 resolved，要么 id 不存在。用 EXISTS 区分两类错误。
	var exists bool
	probeErr := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM dlq_events WHERE id=$1)`, id).Scan(&exists)
	if probeErr != nil {
		return false, fmt.Errorf("dlq probe after noop: %w", probeErr)
	}
	if !exists {
		return false, pgx.ErrNoRows
	}
	return false, errAlreadyResolved
}
