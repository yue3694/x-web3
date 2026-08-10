// Package indexer — checkpoint.go 提供 chain_checkpoints 表的 read/write。
//
// 数据模型（见 0004_order.up.sql）：
//
//	chain_checkpoints(chain_id, consumer, next_block, last_block_hash, updated_at,
//	                  primary key(chain_id, consumer))
//
// 并发契约（非常重要）：
//   - 同一 (chain_id, consumer) 的所有写入必须串行化。
//     indexer runner 是单一所有者，其它写入（reorg / manual rewind）必须
//     通过 SELECT ... FOR UPDATE 在事务内获取 row lock 后才能推进。
//   - Load (CheckpointStore.Load) 仅做非加锁 SELECT，用于启动期 / 调试读取。
//     任何想要"读 + 写"一致性的代码，必须走 pgStore.LoadForUpdate 拿到
//     pgx.Tx，再走 tx-scoped Save / Reset。
//   - Save 走 UPSERT：INSERT ... ON CONFLICT DO UPDATE。
//     WHERE EXCLUDED.next_block >= chain_checkpoints.next_block
//     保证 next_block 单调不减（旧值覆盖 → lost-update 被禁止）。
//   - 多读 OK；多写必须 serialize。
package indexer

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Checkpoint 是表 chain_checkpoints 的内存表示。
type Checkpoint struct {
	ChainID       int64
	Consumer      string
	NextBlock     int64
	LastBlockHash []byte // may be nil on first run
	UpdatedAt     int64  // unix nano；测试断言用
}

// ErrCheckpointNotFound 启动时表里没有记录：indexer 应当用 --from-block 或
// 0 兜底；上层决定是否报错。
var ErrCheckpointNotFound = errors.New("indexer: checkpoint not found")

// CheckpointStore 把 chain_checkpoints 操作封装成可注入的接口；
// 生产用 pgStore，测试用 fakeStore（in-memory map）。
//
// Load 是 non-locking：仅用于启动 / 监控 / 调试。任何"读 + 写"路径必须走
// pgxpool + BeginTx + SELECT ... FOR UPDATE 再做 Save / Reset。
type CheckpointStore interface {
	Load(ctx context.Context, chainID int64, consumer string) (*Checkpoint, error)
	Save(ctx context.Context, cp *Checkpoint) error
	// Reset 把 next_block 退回到 fromBlock，并把 last_block_hash 清空。
	// 用于 admin manual rewind。
	Reset(ctx context.Context, chainID int64, consumer string, fromBlock int64) error
}

// pgStore 是 *pgxpool.Pool 上的 CheckpointStore 实现。
type pgStore struct{ pool *pgxpool.Pool }

// NewPGCheckpointStore ...
func NewPGCheckpointStore(pool *pgxpool.Pool) CheckpointStore {
	return &pgStore{pool: pool}
}

func (s *pgStore) Load(ctx context.Context, chainID int64, consumer string) (*Checkpoint, error) {
	const q = `
SELECT next_block, last_block_hash, extract(epoch from updated_at)*1e9
FROM chain_checkpoints
WHERE chain_id=$1 AND consumer=$2`
	var (
		next  int64
		hash  []byte
		updNs float64
	)
	err := s.pool.QueryRow(ctx, q, chainID, consumer).Scan(&next, &hash, &updNs)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCheckpointNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("checkpoint load: %w", err)
	}
	cp := &Checkpoint{
		ChainID:       chainID,
		Consumer:      consumer,
		NextBlock:     next,
		LastBlockHash: hash,
		UpdatedAt:     int64(updNs),
	}
	return cp, nil
}

// Save UPSERT，next_block 单调不减（防 lost-update 竞态）。
//
// 旧值覆盖被禁止：EXCLUDED.next_block 必须 >= 已存在的 next_block 才更新，
// 否则保留旧值。这样即使两个 writer 同时 Save，DB 也会以较大者为最终值。
func (s *pgStore) Save(ctx context.Context, cp *Checkpoint) error {
	if cp == nil {
		return errors.New("indexer: nil checkpoint")
	}
	if cp.Consumer == "" {
		return errors.New("indexer: consumer required")
	}
	const q = `
INSERT INTO chain_checkpoints(chain_id, consumer, next_block, last_block_hash, updated_at)
VALUES($1,$2,$3,$4, now())
ON CONFLICT (chain_id, consumer) DO UPDATE
  SET next_block     = EXCLUDED.next_block,
      last_block_hash = EXCLUDED.last_block_hash,
      updated_at     = now()
  WHERE EXCLUDED.next_block >= chain_checkpoints.next_block`
	if _, err := s.pool.Exec(ctx, q, cp.ChainID, cp.Consumer, cp.NextBlock, cp.LastBlockHash); err != nil {
		return fmt.Errorf("checkpoint save: %w", err)
	}
	return nil
}

// Reset 仅在 reorg / manual rewind 路径调用；其它场景应走 Save。
func (s *pgStore) Reset(ctx context.Context, chainID int64, consumer string, fromBlock int64) error {
	if fromBlock < 0 {
		return errors.New("indexer: fromBlock must be non-negative")
	}
	const q = `
UPDATE chain_checkpoints
SET next_block = $3, last_block_hash = NULL, updated_at = now()
WHERE chain_id = $1 AND consumer = $2`
	tag, err := s.pool.Exec(ctx, q, chainID, consumer, fromBlock)
	if err != nil {
		return fmt.Errorf("checkpoint reset: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// 没记录：插入一条 "from scratch" 的 checkpoint。
		const ins = `
INSERT INTO chain_checkpoints(chain_id, consumer, next_block, last_block_hash, updated_at)
VALUES($1, $2, $3, NULL, now())
ON CONFLICT (chain_id, consumer) DO NOTHING`
		if _, err := s.pool.Exec(ctx, ins, chainID, consumer, fromBlock); err != nil {
			return fmt.Errorf("checkpoint reset insert: %w", err)
		}
	}
	return nil
}

// LoadForUpdate 在给定事务内以 SELECT ... FOR UPDATE 锁定 (chain_id, consumer) 行。
//
// 调用方必须在事务内继续走 UPDATE / INSERT；事务结束（commit/rollback）即释放锁。
// 这把 checkpoint 写入串行化为 (chain_id, consumer) 维度，避免
// runner Save 和 reorg Reset 之间互相覆盖。
func (s *pgStore) LoadForUpdate(ctx context.Context, tx pgx.Tx, chainID int64, consumer string) (*Checkpoint, error) {
	const q = `
SELECT next_block, last_block_hash, extract(epoch from updated_at)*1e9
FROM chain_checkpoints
WHERE chain_id=$1 AND consumer=$2
FOR UPDATE`
	var (
		next  int64
		hash  []byte
		updNs float64
	)
	err := tx.QueryRow(ctx, q, chainID, consumer).Scan(&next, &hash, &updNs)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCheckpointNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("checkpoint load for update: %w", err)
	}
	return &Checkpoint{
		ChainID:       chainID,
		Consumer:      consumer,
		NextBlock:     next,
		LastBlockHash: hash,
		UpdatedAt:     int64(updNs),
	}, nil
}

// SaveTx 在事务内执行 Save（单调不减 next_block）。
func (s *pgStore) SaveTx(ctx context.Context, tx pgx.Tx, cp *Checkpoint) error {
	if cp == nil {
		return errors.New("indexer: nil checkpoint")
	}
	if cp.Consumer == "" {
		return errors.New("indexer: consumer required")
	}
	const q = `
INSERT INTO chain_checkpoints(chain_id, consumer, next_block, last_block_hash, updated_at)
VALUES($1,$2,$3,$4, now())
ON CONFLICT (chain_id, consumer) DO UPDATE
  SET next_block     = EXCLUDED.next_block,
      last_block_hash = EXCLUDED.last_block_hash,
      updated_at     = now()
  WHERE EXCLUDED.next_block >= chain_checkpoints.next_block`
	if _, err := tx.Exec(ctx, q, cp.ChainID, cp.Consumer, cp.NextBlock, cp.LastBlockHash); err != nil {
		return fmt.Errorf("checkpoint save tx: %w", err)
	}
	return nil
}

// ResetTx 在事务内执行 Reset（rewind）。
func (s *pgStore) ResetTx(ctx context.Context, tx pgx.Tx, chainID int64, consumer string, fromBlock int64) error {
	if fromBlock < 0 {
		return errors.New("indexer: fromBlock must be non-negative")
	}
	const q = `
UPDATE chain_checkpoints
SET next_block = $3, last_block_hash = NULL, updated_at = now()
WHERE chain_id = $1 AND consumer = $2`
	if _, err := tx.Exec(ctx, q, chainID, consumer, fromBlock); err != nil {
		return fmt.Errorf("checkpoint reset tx: %w", err)
	}
	return nil
}

// EnsureCheckpointLocked 在事务内确保 (chain_id, consumer) 行存在并被锁定。
//
// 行为：
//   - 若行已存在：SELECT ... FOR UPDATE 返回现有记录；
//   - 若不存在：INSERT ... ON CONFLICT DO NOTHING（不覆盖已存在行），
//     再 SELECT ... FOR UPDATE 拿到锁。
//
// 用于 reorg / manual rewind 这类"必须持锁但行可能尚未初始化"的场景。
func EnsureCheckpointLocked(ctx context.Context, tx pgx.Tx, chainID int64, consumer string) error {
	const upsert = `
INSERT INTO chain_checkpoints(chain_id, consumer, next_block, last_block_hash, updated_at)
VALUES($1, $2, 0, NULL, now())
ON CONFLICT (chain_id, consumer) DO NOTHING`
	if _, err := tx.Exec(ctx, upsert, chainID, consumer); err != nil {
		return fmt.Errorf("ensure checkpoint: %w", err)
	}
	const lockQ = `
SELECT 1 FROM chain_checkpoints
WHERE chain_id=$1 AND consumer=$2
FOR UPDATE`
	var one int
	if err := tx.QueryRow(ctx, lockQ, chainID, consumer).Scan(&one); err != nil {
		return fmt.Errorf("lock checkpoint: %w", err)
	}
	return nil
}
