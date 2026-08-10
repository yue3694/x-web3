//go:build integration

// F03-T17 续：worker Confirmer + manual rewind 端到端集成测试。
//
// 覆盖 DoD「Worker reorg / 重复消费 / 漏块全部测试覆盖」里的剩余两条：
//   1. admin 手动 rewind → 受影响 order 标 reorged + failure_code=EVENT_REORGED
//      + chain_reorgs 新增一行 + enrollment 仍唯一；
//   2. rewind 之后再次投递同一 (chain_id, tx_hash, log_index) 事件：
//      - chain_events 唯一约束幂等命中；
//      - order 状态保持 reorged（不再被 Apply 推回 confirmed），
//        因为 Apply 的 WHERE 仅匹配 status IN ('submitted','confirming')。
//
// 跑法：go test -tags integration -run TestApply_AfterRewind ./internal/order/
package workerorder_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	workerorder "github.com/x-web3/worker/internal/order"
)

// mirrorManualRewind 镜像 apps/api/internal/admin/handlers/chain_rewind.go 中
// 的 mirrorManualRewind 逻辑（跨 module 不 import）。两个 module 必须保持
// 一致；任意一边变更时同步这一份。
//
// 锁契约：事务内锁 chain_checkpoints(chain_id, consumer='indexer')，
// 然后把 ≥ fromBlock 的 canonical chain_events 翻 false + 把 ≥ fromBlock
// 的 order 翻 reorged + 写 chain_reorgs 行 + 复位 checkpoint。
func mirrorManualRewind(
	ctx context.Context,
	t *testing.T,
	pool poolLike,
	chainID, fromBlock int64,
	reason string,
	actor uuid.UUID,
) (orphaned, affected int64) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	const consumer = "indexer"
	// 0) 锁 chain_checkpoints 行
	if _, err := tx.Exec(ctx, `
INSERT INTO chain_checkpoints(chain_id, consumer, next_block, last_block_hash, updated_at)
VALUES($1, $2, 0, NULL, now())
ON CONFLICT (chain_id, consumer) DO NOTHING`, chainID, consumer); err != nil {
		t.Fatalf("ensure cp: %v", err)
	}
	var one int
	if err := tx.QueryRow(ctx, `
SELECT 1 FROM chain_checkpoints WHERE chain_id=$1 AND consumer=$2 FOR UPDATE`,
		chainID, consumer).Scan(&one); err != nil {
		t.Fatalf("lock cp: %v", err)
	}

	tag, err := tx.Exec(ctx, `UPDATE chain_events SET canonical=false
WHERE chain_id=$1 AND block_number >= $2 AND canonical=true`, chainID, fromBlock)
	if err != nil {
		t.Fatalf("orphan events: %v", err)
	}
	orphaned = tag.RowsAffected()

	tag, err = tx.Exec(ctx, `UPDATE orders
SET status='reorged', failure_code='EVENT_REORGED', updated_at=now()
WHERE chain_id=$1 AND block_number >= $2
  AND status IN ('submitted','confirming','confirmed')`, chainID, fromBlock)
	if err != nil {
		t.Fatalf("reorg orders: %v", err)
	}
	affected = tag.RowsAffected()

	payload, _ := json.Marshal(map[string]any{
		"actorUserId": actor.String(),
		"userReason":  reason,
		"reason":      "manual_rewind",
		"at":          time.Now().UTC().Format(time.RFC3339Nano),
	})
	if _, err := tx.Exec(ctx, `
INSERT INTO chain_reorgs(chain_id, common_block, reason, orphaned_events, affected_orders, payload)
VALUES($1,$2,$3,$4,$5,$6::jsonb)`,
		chainID, fromBlock, "manual_rewind", orphaned, affected, payload); err != nil {
		t.Fatalf("insert reorg: %v", err)
	}

	if _, err := tx.Exec(ctx, `UPDATE chain_checkpoints
SET next_block=$3, last_block_hash=NULL, updated_at=now()
WHERE chain_id=$1 AND consumer=$2 AND (next_block > $3 OR last_block_hash IS NOT NULL)`,
		chainID, consumer, fromBlock); err != nil {
		t.Fatalf("rewind cp: %v", err)
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	return orphaned, affected
}

// poolLike 抽象 pool / tx 共有方法，便于测试代码使用。
type poolLike interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// TestApply_AfterRewind_OrderStaysReorged 覆盖 DoD「重复消费」与「reorg 链路」：
//   1) seed 完整 fixture，order 落到 'submitted'；
//   2) Apply 一次 → confirmed，enrollment 写入；
//   3) 模拟 admin 手动 rewind 到 block 1000（含该 order）；
//   4) 验证 orders.status='reorged' / failure_code='EVENT_REORGED'；
//   5) 验证 chain_reorgs 有 1 行；
//   6) 再次投递相同 ApplyInput → 状态保持 reorged（不被推回 confirmed）；
//   7) 验证 enrollment 行数仍为 1（enrollment 唯一约束）。
func TestApply_AfterRewind_OrderStaysReorged(t *testing.T) {
	pool := itPool(t)
	orderID, userID, courseID, _, txHash, _, _ := seedConfirmedFixture(t, pool)
	buyer := common.HexToAddress("0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266")
	in := applyInputFromFixture(orderID, courseID, txHash, buyer)

	// 1) 第一次 Apply → confirmed
	confirmer := workerorder.NewConfirmer(pool)
	enrollID, state, err := confirmer.Apply(context.Background(), in)
	if err != nil {
		t.Fatalf("first Apply: %v", err)
	}
	if state != "confirmed" {
		t.Fatalf("first state = %q, want confirmed", state)
	}
	if enrollID == uuid.Nil {
		t.Fatal("first enrollment id is nil")
	}
	if n := countEnrollments(t, pool, userID, courseID); n != 1 {
		t.Fatalf("enrollments after first apply = %d, want 1", n)
	}

	// 2) 手动 rewind 到 block 1000（含该 order 的 block_number）
	actor := uuid.New()
	orphaned, affected := mirrorManualRewind(context.Background(), t, pool, testChainID, 1000, "drill reorg", actor)
	if orphaned == 0 {
		t.Errorf("orphaned events = 0, want > 0 (chain_event from happy path)")
	}
	if affected == 0 {
		t.Errorf("affected orders = 0, want 1 (the just-confirmed order)")
	}

	// 3) 验证 orders 状态
	status, fc := readOrderStatus(t, pool, orderID)
	if status != "reorged" {
		t.Errorf("order status after rewind = %q, want reorged", status)
	}
	if fc == nil || *fc != "EVENT_REORGED" {
		t.Errorf("failure_code = %v, want EVENT_REORGED", fc)
	}

	// 4) 验证 chain_reorgs 行
	var reorgCount int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM chain_reorgs
WHERE chain_id=$1 AND common_block=1000 AND reason='manual_rewind'`,
		testChainID).Scan(&reorgCount); err != nil {
		t.Fatalf("count reorgs: %v", err)
	}
	if reorgCount != 1 {
		t.Errorf("chain_reorgs rows = %d, want 1", reorgCount)
	}

	// 5) 再次投递同一事件 → Apply 看到 order 不再是 submitted/confirming，
	//    不会翻回 confirmed；enrollment 仍唯一。
	enrollID2, state2, err := confirmer.Apply(context.Background(), in)
	if err != nil {
		t.Fatalf("re-Apply after rewind: %v", err)
	}
	// state 字段语义：Apply 返回的 state 是 orders.status 实际写入后的值。
	// 因为 WHERE status IN ('submitted','confirming') 命中 0 行 → UPDATE 不动；
	// 仍返回 "reorged"（读出来的当前状态）。
	if state2 != "reorged" {
		t.Errorf("re-Apply state = %q, want reorged (idempotent)", state2)
	}
	// enrollment 行数仍为 1（enrollment ON CONFLICT 幂等）
	if n := countEnrollments(t, pool, userID, courseID); n != 1 {
		t.Errorf("enrollments after re-apply = %d, want 1 (no double enrollment)", n)
	}
	_ = enrollID2
}

// TestApply_ReplayedEventFromDifferentLogIndex_NotBlocked 覆盖 DoD 边界：
// 同一 tx_hash 但 log_index 不同 → 视作独立事件；chain_events 唯一约束
// 仅在 (chain_id, tx_hash, log_index) 三元组上；replay 不应被吞。
//
// 这个 case 主要用于「同一笔交易触发多个 log」（multi-log transaction），
// 不会被 ON CONFLICT DO NOTHING 误杀。
//
// 流程：先 Apply 一次（log_index=0），再 Apply 一次（log_index=1，同 tx_hash）→
// chain_events 应有 2 行；order 仍 confirmed；enrollment 仅 1 条。
func TestApply_ReplayedEventFromDifferentLogIndex_NotBlocked(t *testing.T) {
	pool := itPool(t)
	orderID, userID, courseID, _, txHash, _, _ := seedConfirmedFixture(t, pool)
	buyer := common.HexToAddress("0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266")

	confirmer := workerorder.NewConfirmer(pool)

	// 第一次 Apply：log_index=0
	in0 := applyInputFromFixture(orderID, courseID, txHash, buyer)
	enrollID, state, err := confirmer.Apply(context.Background(), in0)
	if err != nil {
		t.Fatalf("first Apply (log_index=0): %v", err)
	}
	if state != "confirmed" {
		t.Fatalf("first state = %q, want confirmed", state)
	}
	if enrollID == uuid.Nil {
		t.Fatal("first enrollment id is nil")
	}

	// 第二次 Apply：同一 tx_hash，log_index=1 → 应被视作新事件
	in1 := applyInputFromFixture(orderID, courseID, txHash, buyer)
	in1.LogIndex = 1
	_, state2, err := confirmer.Apply(context.Background(), in1)
	if err != nil {
		t.Fatalf("second Apply (log_index=1): %v", err)
	}
	if state2 != "confirmed" {
		t.Fatalf("second state = %q, want confirmed", state2)
	}

	// 验证：chain_events 同 tx_hash 但不同 log_index 都应存在
	if n := countChainEvents(t, pool, testChainID, txHash); n != 2 {
		t.Errorf("chain_events with same tx_hash = %d, want 2 (log_index=0 and 1)", n)
	}
	// 验证：enrollment 仅 1（unique 约束幂等）
	if n := countEnrollments(t, pool, userID, courseID); n != 1 {
		t.Errorf("enrollments = %d, want 1 (no double enrollment)", n)
	}
}
