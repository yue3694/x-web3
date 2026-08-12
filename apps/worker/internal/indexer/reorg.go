// Package indexer — reorg.go 检测并回滚被重组的事件 / 订单。
//
// 检测机制（按检测时点分类）：
//   1. 深度错位（reorg 跨越 ConfirmDepth）：
//      推进 checkpoint 时，若 next_block-1 的实际 hash 与 last_block_hash 不符
//      → 触发 HandleReorg。
//   2. WS 推送的 removed log：
//      ethclient 的 SubscriptionFilterLogs 在 reorg 时把 removed=true 的旧 log
//      重发；本模块会把它视为"撤销信号"。
//   3. 手动 rewind：admin POST /admin/chain/rewind {blockNumber} → ManualRewind。
//
// 副作用（按事务）：
//   BEGIN
//     SELECT ... FOR UPDATE on chain_checkpoints(chain_id, consumer)  -- 串行化
//     UPDATE chain_events SET canonical=false WHERE chain_id=$1 AND block_number >= $2
//     UPDATE orders      SET status='reorged', failure_code='EVENT_REORGED' ... (same filter)
//     INSERT INTO chain_reorgs(...)
//     UPDATE chain_checkpoints SET next_block=$2, last_block_hash=NULL
//   COMMIT
//
// 锁契约（critical）：
//   - HandleReorg / ManualRewind / chain_rewind.mirrorManualRewind 都必须在事务内
//     获取 chain_checkpoints(chain_id, consumer) 的行锁，才能写 chain_reorgs +
//     推进 / 回退 checkpoint。这样把同一 (chain_id, consumer) 的 rewind 操作
//     串行化，避开 worker 自动检测和 admin 手动 rewind 之间的 race。
//   - 锁在事务 commit/rollback 时释放；务必不要持锁发 RPC。
//
// enrollment 不动：reorg 后若同笔 tx 在新链上仍 confirmed，confirmer.Apply 走幂等路径会重新
// 把 orders 推到 confirmed；enrollment 唯一约束保证不会重复发证。
package indexer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ReorgInfo 是 Reorg 回调的载荷（runner → reorg → admin）。
type ReorgInfo struct {
	ChainID     int64
	CommonBlock int64
	NewHash     []byte
	Reason      string
}

// DefaultConsumer 是 worker 默认 consumer 标识；reorg/rewind 路径与
// runner.Config.Consumer 一致，确保锁住同一 (chain_id, consumer) 行。
const DefaultConsumer = "indexer"

// rewindConsumer 用于 admin 手动 rewind 的 consumer 标识 — 必须与
// runner / HandleReorg 一致才能拿到同一行锁。
const rewindConsumer = "indexer"

// ReorgError 包装回滚过程中产生的错误。
type ReorgError struct {
	ChainID     int64
	CommonBlock int64
	Reason      string
	Err         error
}

func (e *ReorgError) Error() string {
	return fmt.Sprintf("reorg: chain=%d block=%d reason=%s: %v",
		e.ChainID, e.CommonBlock, e.Reason, e.Err)
}

func (e *ReorgError) Unwrap() error { return e.Err }

// ReorgStore 是数据库操作的最小接口；生产 = pgxpool.Pool 包装。
type ReorgStore interface {
	// MarkOrphanedEvents 标记 >= fromBlock 的所有事件 canonical=false，返回影响行数。
	MarkOrphanedEvents(ctx context.Context, chainID, fromBlock int64) (int64, error)
	// RevertOrders 推动受影响 order 到 reorged；返回影响行数。
	RevertOrders(ctx context.Context, chainID, fromBlock int64) (int64, error)
	// RecordReorg 写一行 chain_reorgs。
	RecordReorg(ctx context.Context, info ReorgInfo, orphanedEvents, affectedOrders int64, payload map[string]any) error
	// ResetCheckpoint 同 checkpoint.go 的语义。
	ResetCheckpoint(ctx context.Context, chainID int64, consumer string, fromBlock int64) error
}

// pgReorgStore 是 *pgxpool.Pool 的实现。
type pgReorgStore struct{ pool *pgxpool.Pool }

// NewPGReorgStore ...
func NewPGReorgStore(pool *pgxpool.Pool) ReorgStore {
	return &pgReorgStore{pool: pool}
}

func (s *pgReorgStore) MarkOrphanedEvents(ctx context.Context, chainID, fromBlock int64) (int64, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE chain_events SET canonical=false
WHERE chain_id=$1 AND block_number >= $2 AND canonical=true`, chainID, fromBlock)
	if err != nil {
		return 0, fmt.Errorf("mark orphaned: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (s *pgReorgStore) RevertOrders(ctx context.Context, chainID, fromBlock int64) (int64, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE orders
SET status='reorged', failure_code='EVENT_REORGED', updated_at=now()
WHERE chain_id=$1
  AND block_number >= $2
  AND status IN ('confirming','confirmed')`, chainID, fromBlock)
	if err != nil {
		return 0, fmt.Errorf("revert orders: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (s *pgReorgStore) RecordReorg(ctx context.Context, info ReorgInfo, orphanedEvents, affectedOrders int64, payload map[string]any) error {
	if payload == nil {
		payload = map[string]any{}
	}
	payload["reason"] = info.Reason
	payload["at"] = time.Now().UTC().Format(time.RFC3339Nano)
	raw, _ := json.Marshal(payload)
	_, err := s.pool.Exec(ctx, `INSERT INTO chain_reorgs(chain_id, common_block, new_block_hash, orphaned_events, affected_orders, reason, payload)
VALUES($1,$2,$3,$4,$5,$6,$7::jsonb)`,
		info.ChainID, info.CommonBlock, info.NewHash,
		orphanedEvents, affectedOrders, info.Reason, raw)
	if err != nil {
		return fmt.Errorf("record reorg: %w", err)
	}
	return nil
}

func (s *pgReorgStore) ResetCheckpoint(ctx context.Context, chainID int64, consumer string, fromBlock int64) error {
	const q = `
UPDATE chain_checkpoints
SET next_block=$3, last_block_hash=NULL, updated_at=now()
WHERE chain_id=$1 AND consumer=$2`
	tag, err := s.pool.Exec(ctx, q, chainID, consumer, fromBlock)
	if err != nil {
		return fmt.Errorf("reset checkpoint: %w", err)
	}
	if tag.RowsAffected() == 0 {
		const ins = `
INSERT INTO chain_checkpoints(chain_id, consumer, next_block, last_block_hash, updated_at)
VALUES($1,$2,$3,NULL,now())
ON CONFLICT (chain_id, consumer) DO NOTHING`
		if _, err := s.pool.Exec(ctx, ins, chainID, consumer, fromBlock); err != nil {
			return fmt.Errorf("reset checkpoint insert: %w", err)
		}
	}
	return nil
}

// HandleReorg 在单事务里完成事件 / 订单回滚 + reorg 留痕 + checkpoint 复位。
//
// 事务边界（critical）：
//   0) SELECT ... FOR UPDATE on chain_checkpoints(chain_id, 'indexer')
//      → 与 admin ManualRewind / mirrorManualRewind 串行化；
//   1) UPDATE chain_events canonical=false
//   2) UPDATE orders → reorged
//   3) INSERT chain_reorgs
//   4) UPDATE chain_checkpoints (next_block=common_block, last_block_hash=NULL)
//   5) COMMIT（释放锁）
//
// 锁契约：consumer="indexer" 是 worker 侧默认 consumer 标识；admin 路径会
// 使用 consumer="admin_rewind" 或同一 consumer — 由于 (chain_id, consumer)
// 主键相同，admin 也必须用相同 consumer 拿锁，否则锁不到同一行。
// 当前实现：admin 路径把 checkpoint 复位锁在与 worker 同 consumer 上（见
// ManualRewind），保证两者互斥。
func HandleReorg(ctx context.Context, pool *pgxpool.Pool, info ReorgInfo, payload map[string]any) (orphaned, affected int64, err error) {
	if info.ChainID <= 0 {
		return 0, 0, errors.New("reorg: chain_id required")
	}
	if info.CommonBlock < 0 {
		return 0, 0, errors.New("reorg: common_block must be non-negative")
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, 0, &ReorgError{ChainID: info.ChainID, CommonBlock: info.CommonBlock, Reason: info.Reason, Err: err}
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// 0) 锁定 chain_checkpoints 行，避免与 admin rewind 路径竞态。
	//    consumer 与 runner / admin rewind 一致，保证锁住同一行。
	if err := EnsureCheckpointLocked(ctx, tx, info.ChainID, DefaultConsumer); err != nil {
		return 0, 0, &ReorgError{ChainID: info.ChainID, CommonBlock: info.CommonBlock, Reason: info.Reason, Err: err}
	}

	// 1) chain_events
	tag, err := tx.Exec(ctx, `UPDATE chain_events SET canonical=false
WHERE chain_id=$1 AND block_number >= $2 AND canonical=true`, info.ChainID, info.CommonBlock)
	if err != nil {
		return 0, 0, &ReorgError{ChainID: info.ChainID, CommonBlock: info.CommonBlock, Reason: info.Reason, Err: fmt.Errorf("mark orphaned: %w", err)}
	}
	orphaned = tag.RowsAffected()

	// 2) orders
	tag, err = tx.Exec(ctx, `UPDATE orders
SET status='reorged', failure_code='EVENT_REORGED', updated_at=now()
WHERE chain_id=$1
  AND block_number >= $2
  AND status IN ('confirming','confirmed')`, info.ChainID, info.CommonBlock)
	if err != nil {
		return 0, 0, &ReorgError{ChainID: info.ChainID, CommonBlock: info.CommonBlock, Reason: info.Reason, Err: fmt.Errorf("revert orders: %w", err)}
	}
	affected = tag.RowsAffected()

	// 3) chain_reorgs
	if payload == nil {
		payload = map[string]any{}
	}
	payload["reason"] = info.Reason
	payload["at"] = time.Now().UTC().Format(time.RFC3339Nano)
	raw, _ := json.Marshal(payload)
	if _, err := tx.Exec(ctx, `INSERT INTO chain_reorgs(chain_id, common_block, new_block_hash, orphaned_events, affected_orders, reason, payload)
VALUES($1,$2,$3,$4,$5,$6,$7::jsonb)`,
		info.ChainID, info.CommonBlock, info.NewHash, orphaned, affected, info.Reason, raw); err != nil {
		return 0, 0, &ReorgError{ChainID: info.ChainID, CommonBlock: info.CommonBlock, Reason: info.Reason, Err: fmt.Errorf("record reorg: %w", err)}
	}

	// 4) checkpoint 复位在事务内：next_block=common_block, last_block_hash=NULL。
	//    consumer 与锁定行一致；WITH next_block > $2 守卫避免 no-op 时清掉已有 hash。
	if _, err := tx.Exec(ctx, `UPDATE chain_checkpoints
SET next_block=$3, last_block_hash=NULL, updated_at=now()
WHERE chain_id=$1 AND consumer=$2 AND (next_block > $3 OR last_block_hash IS NOT NULL)`,
		info.ChainID, DefaultConsumer, info.CommonBlock); err != nil {
		return 0, 0, &ReorgError{ChainID: info.ChainID, CommonBlock: info.CommonBlock, Reason: info.Reason, Err: fmt.Errorf("reset checkpoint: %w", err)}
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, 0, &ReorgError{ChainID: info.ChainID, CommonBlock: info.CommonBlock, Reason: info.Reason, Err: err}
	}
	return orphaned, affected, nil
}

// ManualRewind 实现 admin API: 强制回退到 fromBlock（含）之前。
//
// 语义（与 HandleReorg 共享锁契约）：
//   - 单事务内：先锁 chain_checkpoints(chain_id, 'indexer') →
//     再 mark orphaned / revert orders / 写 chain_reorgs / reset checkpoint。
//   - 这样保证与 HandleReorg（worker 自动检测 reorg）互斥，不会出现
//     "admin rewind 复位 checkpoint 同时 worker 锁住并覆盖"的竞态。
//
// 幂等性：
//   - UPDATE chain_checkpoints WHERE (next_block > $3 OR last_block_hash IS NOT NULL)
//     守卫：仅当存在差异（next_block 更大 或 last_block_hash 不为空）才复位。
//     二次进入同一 fromBlock → 行为 no-op，chain_reorgs 仍写一行（审计留痕）。
func ManualRewind(ctx context.Context, pool *pgxpool.Pool, chainID int64, fromBlock int64, actorUserID []byte, payload map[string]any) (orphaned, affected int64, err error) {
	if chainID <= 0 {
		return 0, 0, errors.New("rewind: chain_id required")
	}
	if fromBlock < 0 {
		return 0, 0, errors.New("rewind: from_block must be non-negative")
	}
	if payload == nil {
		payload = map[string]any{}
	}
	if len(actorUserID) > 0 {
		payload["actorUserId"] = string(actorUserID)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("rewind begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// 0) 锁 chain_checkpoints 行（与 HandleReorg 同 consumer）。
	if err := EnsureCheckpointLocked(ctx, tx, chainID, DefaultConsumer); err != nil {
		return 0, 0, fmt.Errorf("rewind lock checkpoint: %w", err)
	}

	// 1) chain_events
	tag, err := tx.Exec(ctx, `UPDATE chain_events SET canonical=false
WHERE chain_id=$1 AND block_number >= $2 AND canonical=true`, chainID, fromBlock)
	if err != nil {
		return 0, 0, fmt.Errorf("rewind mark orphaned: %w", err)
	}
	orphaned = tag.RowsAffected()

	// 2) orders
	tag, err = tx.Exec(ctx, `UPDATE orders
SET status='reorged', failure_code='EVENT_REORGED', updated_at=now()
WHERE chain_id=$1
  AND block_number >= $2
  AND status IN ('confirming','confirmed')`, chainID, fromBlock)
	if err != nil {
		return 0, 0, fmt.Errorf("rewind revert orders: %w", err)
	}
	affected = tag.RowsAffected()

	// 3) chain_reorgs — reason='manual_rewind'
	if _, err := tx.Exec(ctx, `INSERT INTO chain_reorgs(chain_id, common_block, reason, orphaned_events, affected_orders, payload)
VALUES($1,$2,$3,$4,$5,$6::jsonb)`,
		chainID, fromBlock, "manual_rewind", orphaned, affected,
		jsonPayload(payload, "manual_rewind")); err != nil {
		return 0, 0, fmt.Errorf("rewind record reorg: %w", err)
	}

	// 4) checkpoint 复位：守卫 (next_block > $3 OR last_block_hash IS NOT NULL)
	//    → rewind 到等于 current next_block（且 hash 已为 NULL）→ no-op，避免
	//    chain_reorgs 已留痕但 checkpoint 未被更新造成语义不一致。
	if _, err := tx.Exec(ctx, `UPDATE chain_checkpoints
SET next_block=$3, last_block_hash=NULL, updated_at=now()
WHERE chain_id=$1 AND consumer=$2 AND (next_block > $3 OR last_block_hash IS NOT NULL)`,
		chainID, DefaultConsumer, fromBlock); err != nil {
		return 0, 0, fmt.Errorf("rewind reset checkpoint: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, 0, fmt.Errorf("rewind commit: %w", err)
	}
	return orphaned, affected, nil
}

// jsonPayload 把 payload + reason + 时间戳打包为 JSON 字符串。
// 复用 HandleReorg 同款 payload 格式以保持 chain_reorgs.payload 一致。
func jsonPayload(payload map[string]any, reason string) []byte {
	if payload == nil {
		payload = map[string]any{}
	}
	payload["reason"] = reason
	payload["at"] = time.Now().UTC().Format(time.RFC3339Nano)
	raw, _ := json.Marshal(payload)
	return raw
}

// Ensure pgx.ErrNoRows is referenced to avoid unused import in build variants.
var _ = pgx.ErrNoRows

// NewReorgFromPool 直接基于 pool 构造 ReorgStore；方便 admin handler 注入。
func NewReorgFromPool(pool *pgxpool.Pool) ReorgStore { return NewPGReorgStore(pool) }

// safeLogger 防止 nil 引用。
func safeLogger(l *slog.Logger) *slog.Logger {
	if l == nil {
		return slog.New(slog.NewTextHandler(discardWriter{}, nil))
	}
	return l
}
