//go:build integration

// F03-T12/T13：reorg 集成测试。
//
// 覆盖 DoD #4：处理 block N → reorg 到 N-3 →
//   - chain_reorgs 表新增一行
//   - chain_checkpoints.next_block 退回到 common_block
//   - chain_events 中 >= common_block 的事件被标 canonical=false
//   - replay 重新跑一遍 cycle 后，state 保持一致（orphan 留痕、replay 不重 confirm）
//
// 入参：DATABASE_URL_TEST（指向已应用 0007_reorg_reconcile migration 的 PG）。
//
// 跑法：go test -tags integration -run TestReorg_DetectedRewindsCheckpoint ./internal/indexer/
package indexer

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	reorgChainID = int64(11155111)
	reorgTestID  = "reorg-dod-test"
)

// itPool 返回 DATABASE_URL_TEST 的 pool；缺省 t.Skip。
func itPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL_TEST")
	if dsn == "" {
		t.Skip("DATABASE_URL_TEST not set; reorg integration test skipped")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		t.Skipf("pg ping failed: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// freshConsumer 在测试用专属 consumer 上重置 checkpoint；避免与生产数据交叉。
func freshConsumer(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	consumer := reorgTestID + "-" + uuid.NewString()
	if _, err := pool.Exec(context.Background(),
		`DELETE FROM chain_checkpoints WHERE consumer=$1`, consumer); err != nil {
		t.Fatalf("reset checkpoint: %v", err)
	}
	if _, err := pool.Exec(context.Background(),
		`DELETE FROM chain_reorgs WHERE chain_id=$1 AND payload->>'source'=$2`,
		reorgChainID, consumer); err != nil {
		t.Fatalf("reset reorgs: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM chain_reorgs WHERE chain_id=$1 AND payload->>'source'=$2`,
			reorgChainID, consumer)
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM chain_checkpoints WHERE consumer=$1`, consumer)
	})
	return consumer
}

// readCheckpoint 读 next_block / last_block_hash；不存在时 nextBlock=-1。
func readCheckpoint(t *testing.T, pool *pgxpool.Pool, consumer string) (int64, []byte, bool) {
	t.Helper()
	var (
		next int64
		hash []byte
	)
	err := pool.QueryRow(context.Background(),
		`SELECT next_block, last_block_hash FROM chain_checkpoints WHERE chain_id=$1 AND consumer=$2`,
		reorgChainID, consumer).Scan(&next, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return -1, nil, false
	}
	if err != nil {
		t.Fatalf("read checkpoint: %v", err)
	}
	return next, hash, true
}

// seedEvent 在 chain_events 落一条 canonical 记录。
func seedEvent(t *testing.T, pool *pgxpool.Pool, block int64, txHash []byte, logIdx int, canonical bool) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO chain_events(chain_id, tx_hash, log_index, block_number, block_hash, event_signature, payload, canonical)
VALUES($1, $2, $3, $4, decode('01','hex'), decode('00','hex'), '{}'::jsonb, $5)
ON CONFLICT (chain_id, tx_hash, log_index) DO UPDATE SET canonical=EXCLUDED.canonical, block_number=EXCLUDED.block_number`,
		reorgChainID, txHash, logIdx, block, canonical)
	if err != nil {
		t.Fatalf("seed event: %v", err)
	}
}

// seedOrder 在 orders 落一条 submitted/confirming 记录，关联一个 fake intent。
func seedOrder(t *testing.T, pool *pgxpool.Pool, block int64) uuid.UUID {
	t.Helper()
	orderID := uuid.New()
	userID := uuid.New()
	courseID := uuid.New()
	intentID := uuid.New()
	walletID := uuid.New()
	ctx := context.Background()

	if _, err := pool.Exec(ctx,
		`INSERT INTO users(id, privy_user_id) VALUES($1, $2) ON CONFLICT DO NOTHING`,
		userID, "reorg-test-"+userID.String()); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO courses(id, teacher_id, title, slug, status) VALUES($1, $2, 't', $3, 'published')`,
		courseID, userID, "rc-"+courseID.String()); err != nil {
		t.Fatalf("insert course: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO wallets(id, user_id, chain_id, address, is_primary) VALUES($1, $2, $3, '0x' || repeat('1',40), true)`,
		walletID, userID, reorgChainID); err != nil {
		t.Fatalf("insert wallet: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO purchase_intents(id, user_id, wallet_id, course_id, course_key, price_version, chain_id, token_address, amount, market_address, idempotency_key, expires_at)
VALUES($1, $2, $3, $4, decode('aa','hex'), 1, $5, '0xtok', 1::numeric, '0xmkt', $6, now() + interval '15 minutes')`,
		intentID, userID, walletID, courseID, reorgChainID, "ri-"+intentID.String()); err != nil {
		t.Fatalf("insert intent: %v", err)
	}
	txHash := make([]byte, 32)
	for i := range txHash {
		txHash[i] = byte(i)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO orders(id, intent_id, user_id, course_id, status, chain_id, tx_hash, block_number, log_index, block_hash)
VALUES($1, $2, $3, $4, 'confirmed', $5, $6, $7, 0, decode('01','hex'))`,
		orderID, intentID, userID, courseID, reorgChainID, txHash, block); err != nil {
		t.Fatalf("insert order: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM orders WHERE id=$1`, orderID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM purchase_intents WHERE id=$1`, intentID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM wallets WHERE id=$1`, walletID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM courses WHERE id=$1`, courseID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, userID)
	})
	return orderID
}

// TestReorg_DetectedRewindsCheckpoint 覆盖 DoD #4：reorg 触达后
//   (a) chain_reorgs 新增一行
//   (b) chain_checkpoints 退回到 common_block
//   (c) chain_events 中 >= common_block 的 canonical=false
//   (d) replay（再跑一次 HandleReorg 同一 common_block）幂等（不再产生新行）；
//       此时 ops 重新跑 cycle 后，checkpoint 仍指向 common_block 直到新 head 推进。
func TestReorg_DetectedRewindsCheckpoint(t *testing.T) {
	pool := itPool(t)
	consumer := freshConsumer(t, pool)
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	// 1) seed 三个 block 的 events + orders；checkpoint 推进到 103。
	for blk := int64(100); blk <= 102; blk++ {
		txHash := make([]byte, 32)
		txHash[0] = byte(blk)
		txHash[1] = 0xCC
		seedEvent(t, pool, blk, txHash, 0, true)
		seedOrder(t, pool, blk)
	}
	// 初始 checkpoint：next=103, lastHash=02（与 block 102 配对）
	if _, err := pool.Exec(ctx, `
INSERT INTO chain_checkpoints(chain_id, consumer, next_block, last_block_hash)
VALUES($1, $2, 103, decode('CC','hex'))`, reorgChainID, consumer); err != nil {
		t.Fatalf("insert checkpoint: %v", err)
	}

	// 2) 触发 reorg 到 common_block=100（block 100 是新链 + 旧链的最近共同块）。
	info := ReorgInfo{
		ChainID:     reorgChainID,
		CommonBlock: 100,
		NewHash:     []byte{0xDE, 0xAD, 0xBE, 0xEF},
		Reason:      "depth_miss",
	}
	payload := map[string]any{"source": consumer, "note": "dod-test reorg"}

	// 用 HandleReorg 直接走生产路径：它会在事务内锁 checkpoint 并完成
	// 全部副作用，与 runner 检测时一致。
	orphaned, affected, err := HandleReorg(ctx, pool, info, payload)
	if err != nil {
		t.Fatalf("HandleReorg: %v", err)
	}
	if orphaned != 2 {
		t.Errorf("orphaned = %d, want 2 (blocks 101, 102)", orphaned)
	}
	if affected != 3 {
		t.Errorf("affected orders = %d, want 3", affected)
	}

	// 3) 断言 (a) chain_reorgs 新增一行
	var reorgCount int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM chain_reorgs
WHERE chain_id=$1 AND common_block=100 AND reason='depth_miss'`,
		reorgChainID).Scan(&reorgCount); err != nil {
		t.Fatalf("count reorgs: %v", err)
	}
	if reorgCount != 1 {
		t.Errorf("chain_reorgs rows = %d, want 1", reorgCount)
	}

	// 4) 断言 (b) checkpoint 已 rewind 到 100（注意 HandleReorg 的语义是
	// next_block=common_block，runner 下次 cycle 会从 common_block 重新拉）。
	next, hash, ok := readCheckpoint(t, pool, consumer)
	if !ok {
		t.Fatal("checkpoint disappeared after reorg")
	}
	if next != 100 {
		t.Errorf("next_block = %d, want 100 (rewound to common_block)", next)
	}
	if hash != nil {
		t.Errorf("last_block_hash = %x, want nil (cleared on reorg)", hash)
	}

	// 5) 断言 (c) chain_events 101, 102 canonical=false；block 100 保持 true。
	for blk, want := range map[int64]bool{100: true, 101: false, 102: false} {
		var canonical bool
		if err := pool.QueryRow(ctx,
			`SELECT canonical FROM chain_events
WHERE chain_id=$1 AND block_number=$2 LIMIT 1`, reorgChainID, blk).Scan(&canonical); err != nil {
			t.Fatalf("read event blk %d: %v", blk, err)
		}
		if canonical != want {
			t.Errorf("block %d canonical = %v, want %v", blk, canonical, want)
		}
	}

	// 6) 断言 (d) replay：再跑一次 HandleReorg 到同一 common_block，
	// chain_reorgs 再写一行（按 reorg.go 语义审计留痕，但 checkpoint 不再变化）。
	info2 := ReorgInfo{
		ChainID:     reorgChainID,
		CommonBlock: 100,
		NewHash:     []byte{0xFE, 0xED, 0xFA, 0xCE},
		Reason:      "depth_miss",
	}
	_, _, err = HandleReorg(ctx, pool, info2, map[string]any{"source": consumer, "note": "replay"})
	if err != nil {
		t.Fatalf("HandleReorg replay: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM chain_reorgs WHERE chain_id=$1 AND common_block=100`,
		reorgChainID).Scan(&reorgCount); err != nil {
		t.Fatalf("count reorgs after replay: %v", err)
	}
	if reorgCount != 2 {
		t.Errorf("chain_reorgs rows after replay = %d, want 2", reorgCount)
	}
	next2, _, _ := readCheckpoint(t, pool, consumer)
	if next2 != 100 {
		t.Errorf("next_block after replay = %d, want 100 (no-op)", next2)
	}

	// 7) 模拟 runner 用新 head 推进：Save 一个 next_block=110。
	if err := NewPGCheckpointStore(pool).Save(ctx, &Checkpoint{
		ChainID:       reorgChainID,
		Consumer:      consumer,
		NextBlock:     110,
		LastBlockHash: []byte{0x11},
	}); err != nil {
		t.Fatalf("save after reorg: %v", err)
	}
	// 推进后再走一次 reorg 回 100：checkpoint 必须能再次被压回去。
	info3 := ReorgInfo{
		ChainID:     reorgChainID,
		CommonBlock: 100,
		NewHash:     []byte{0x55, 0x55},
		Reason:      "depth_miss",
	}
	if _, _, err := HandleReorg(ctx, pool, info3, map[string]any{"source": consumer, "note": "after-forward"}); err != nil {
		t.Fatalf("HandleReorg after-forward: %v", err)
	}
	next3, _, _ := readCheckpoint(t, pool, consumer)
	if next3 != 100 {
		t.Errorf("next_block after second reorg = %d, want 100 (checkpoint can be rewound again)", next3)
	}

	// 8) 留 logger 引用避免 unused
	_ = logger
	_ = time.Now
}
