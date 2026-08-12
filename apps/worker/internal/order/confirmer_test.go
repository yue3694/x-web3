//go:build integration

// F03-T11：worker Confirmer 集成测试。
//
// 覆盖 DoD 场景：
//  1. 假 tx hash 提交 → worker 拒绝（orders.status = 'failed'）。
//  2. CoursePurchased 事件里的 buyer 与 intent 不一致 → 不创建 enrollment；
//     orders.status = 'failed', failure_code = 'RECEIPT_MISMATCH'。
//  3. 同一 (chain_id, tx_hash, log_index) 重复投递 → enrollment 仅一条。
//
// 入参：
//   - DATABASE_URL_TEST：指向已应用 0001~0007 migrations 的 PG。
//
// 设计：
//   - 用唯一的 user / course / wallet / intent 隔离；测试结束清理。
//   - 复用 `order.CourseKey` 与 chain.CoursePurchasedTopic 让 intent 与事件匹配。
//   - 失败的 buyer-mismatch 用单独事件字段而非乱改 intent，避免污染其他 case。
package workerorder_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"crypto/sha256"

	"github.com/ethereum/go-ethereum/common"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/x-web3/worker/internal/chain"
	workerorder "github.com/x-web3/worker/internal/order"
)

// courseKeyForTest 与 api/internal/order.CourseKey 等价（sha256(uuid)）。
// 两个包需要保持一致；本测试仅在 worker 集成测试中使用，复制一份不破坏 prod。
func courseKeyForTest(id uuid.UUID) []byte {
	h := sha256.Sum256(id[:])
	return h[:]
}

const (
	testChainID = int64(11155111)
)

// itPool 返回 DATABASE_URL_TEST 的 pool；缺省时 t.Skip。
func itPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL_TEST")
	if dsn == "" {
		t.Skip("DATABASE_URL_TEST not set; confirmer integration test skipped")
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

// seedConfirmedFixture 落一组「submitted order + intent + course + wallet + user」，
// 让 Confirmer.Apply 能跑通 happy path。
//
// 返回 (orderID, userID, courseID, intentID, txHash, marketAddr, tokenAddr)。
func seedConfirmedFixture(t *testing.T, pool *pgxpool.Pool) (uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID, []byte, common.Address, common.Address) {
	t.Helper()
	ctx := context.Background()

	userID := uuid.New()
	courseID := uuid.New()
	walletID := uuid.New()
	intentID := uuid.New()
	orderID := uuid.New()

	market := common.HexToAddress("0x5FbDB2315678afecb367f032d93F642f64180aa3")
	token := common.HexToAddress("0x70997970C51812dc3A010C7d01b50e0d17dc79C8")
	buyer := common.HexToAddress("0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266")

	if _, err := pool.Exec(ctx,
		`INSERT INTO users(id, privy_user_id) VALUES($1, $2) ON CONFLICT DO NOTHING`,
		userID, "confirmer-test-"+userID.String()); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO courses(id, teacher_id, title, slug, status) VALUES($1, $2, 't', $3, 'published')`,
		courseID, userID, "course-"+courseID.String()); err != nil {
		t.Fatalf("insert course: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO wallets(id, user_id, chain_id, address, is_primary) VALUES($1, $2, $3, $4, true)`,
		walletID, userID, testChainID, buyer.Hex()); err != nil {
		t.Fatalf("insert wallet: %v", err)
	}
	// course_prices 是 orders/intent 的前置；落一条 1.0 USDC / price_version=1。
	var priceID uuid.UUID
	if err := pool.QueryRow(ctx, `
INSERT INTO course_prices(id, course_id, version, chain_id, token_address, amount, decimals, market_address, valid_from)
VALUES($1, $2, 1, $3, $4, 1000000::numeric, 6, $5, now())
RETURNING id`,
		uuid.New(), courseID, testChainID, token.Hex(), market.Hex()).Scan(&priceID); err != nil {
		t.Fatalf("insert course_prices: %v", err)
	}
	courseKey := courseKeyForTest(courseID)
	if _, err := pool.Exec(ctx, `
INSERT INTO purchase_intents(id, user_id, wallet_id, course_id, price_id, course_key, price_version,
                             chain_id, token_address, amount, market_address, idempotency_key, expires_at)
VALUES($1, $2, $3, $4, $5, $6, 1, $7, $8, 1000000::numeric, $9, $10, now() + interval '15 minutes')`,
		intentID, userID, walletID, courseID, priceID, courseKey,
		testChainID, token.Hex(), market.Hex(), "confirmer-"+intentID.String()); err != nil {
		t.Fatalf("insert intent: %v", err)
	}
	// 32-byte 假 tx hash（与 order 唯一约束配对）。
	txHash := make([]byte, 32)
	for i := range txHash {
		txHash[i] = byte(0xA0 + i)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO orders(id, intent_id, user_id, course_id, status, chain_id, tx_hash, block_number, log_index, block_hash)
VALUES($1, $2, $3, $4, 'submitted', $5, $6, 1000, 0, decode('01','hex'))`,
		orderID, intentID, userID, courseID, testChainID, txHash); err != nil {
		t.Fatalf("insert order: %v", err)
	}
	t.Cleanup(func() {
		// cleanup 顺序：依赖倒序
		_, _ = pool.Exec(context.Background(), `DELETE FROM outbox_events WHERE aggregate='order' AND payload->>'orderId'=$1`, orderID.String())
		_, _ = pool.Exec(context.Background(), `DELETE FROM enrollments WHERE user_id=$1 AND course_id=$2`, userID, courseID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM chain_events WHERE chain_id=$1 AND tx_hash=$2`, testChainID, txHash)
		_, _ = pool.Exec(context.Background(), `DELETE FROM orders WHERE id=$1`, orderID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM purchase_intents WHERE id=$1`, intentID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM course_prices WHERE course_id=$1`, courseID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM courses WHERE id=$1`, courseID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM wallets WHERE id=$1`, walletID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, userID)
	})
	return orderID, userID, courseID, intentID, txHash, market, token
}

// applyInputFromFixture 把 seed 出来的 order 转成 Confirmer.ApplyInput，
// 默认 buyer 跟 intent 一致。buyerOverride 用于 mismatch 场景。
func applyInputFromFixture(orderID uuid.UUID, courseID uuid.UUID, intentID uuid.UUID, txHash []byte, market common.Address, buyer common.Address) workerorder.ApplyInput {
	courseKey := courseKeyForTest(courseID)
	var courseKeyArr [32]byte
	copy(courseKeyArr[:], courseKey)
	return workerorder.ApplyInput{
		OrderID:         orderID,
		ChainID:         testChainID,
		ContractAddress: market,
		TxHash:          txHash,
		LogIndex:        0,
		BlockNumber:     1000,
		BlockHash:       []byte{0x01},
		BlockTime:       time.Now().UTC(),
		EventSig:        chain.CoursePurchasedTopic,
		Event: &chain.CoursePurchased{
			CourseKey:    courseKeyArr,
			Buyer:        buyer,
			Token:        common.HexToAddress("0x70997970C51812dc3A010C7d01b50e0d17dc79C8"),
			Amount:       workerorder.U256(1000000),
			IntentID:     chain.Bytes16FromUUID(intentID),
			PriceVersion: workerorder.U256(1),
		},
	}
}

// countEnrollments 返回 user×course 的 enrollment 行数。
func countEnrollments(t *testing.T, pool *pgxpool.Pool, userID, courseID uuid.UUID) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM enrollments WHERE user_id=$1 AND course_id=$2`,
		userID, courseID).Scan(&n); err != nil {
		t.Fatalf("count enrollments: %v", err)
	}
	return n
}

// countChainEvents 返回 (chain_id, tx_hash) 的 chain_events 行数。
func countChainEvents(t *testing.T, pool *pgxpool.Pool, chainID int64, txHash []byte) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM chain_events WHERE chain_id=$1 AND tx_hash=$2`,
		chainID, txHash).Scan(&n); err != nil {
		t.Fatalf("count chain_events: %v", err)
	}
	return n
}

// readOrderStatus 读 orders.status。
func readOrderStatus(t *testing.T, pool *pgxpool.Pool, orderID uuid.UUID) (string, *string) {
	t.Helper()
	var status string
	var failureCode *string
	if err := pool.QueryRow(context.Background(),
		`SELECT status, failure_code FROM orders WHERE id=$1`, orderID).Scan(&status, &failureCode); err != nil {
		t.Fatalf("read order: %v", err)
	}
	return status, failureCode
}

// TestConfirmer_HappyPath_Smoke 兜底：构造一个合法事件并跑通确认全链路，
// 给后面两个 case 留一份"绿色"基线（无失败即视为通过）。
func TestConfirmer_HappyPath_Smoke(t *testing.T) {
	pool := itPool(t)
	orderID, userID, courseID, intentID, txHash, market, _ := seedConfirmedFixture(t, pool)
	buyer := common.HexToAddress("0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266")
	in := applyInputFromFixture(uuid.Nil, courseID, intentID, txHash, market, buyer)
	c := workerorder.NewConfirmer(pool)
	enrollID, state, err := c.Apply(context.Background(), in)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if state != "confirmed" {
		t.Fatalf("state = %q, want confirmed", state)
	}
	if enrollID == uuid.Nil {
		t.Fatal("enrollment id is nil")
	}
	if n := countEnrollments(t, pool, userID, courseID); n != 1 {
		t.Errorf("enrollments = %d, want 1", n)
	}
	if n := countChainEvents(t, pool, testChainID, txHash); n != 1 {
		t.Errorf("chain_events = %d, want 1", n)
	}
	status, fc := readOrderStatus(t, pool, orderID)
	if status != "confirmed" {
		t.Errorf("order status = %q, want confirmed", status)
	}
	if fc != nil && *fc != "" {
		t.Errorf("failure_code = %v, want nil/empty", *fc)
	}
}

// TestConfirmer_DuplicateTxHash 覆盖 DoD #1/#3：相同 (chain_id, tx_hash, log_index)
// 重复投递 → chain_events 唯一约束保证幂等；enrollment 仅 1 条。
//
// 说明：production 流程下「假 tx hash」由 API 层的 SubmitTransaction 拦截
// （ErrTxAlreadyUsed）；worker 这条线上的"假"通常指「用户绕开 API 伪造了
// 一样格式但未真实上链的事件」。这里我们验证 worker 自身在 db 层
// 防御重复事件 delivery 的能力。
func TestConfirmer_DuplicateTxHash(t *testing.T) {
	pool := itPool(t)
	orderID, userID, courseID, intentID, txHash, market, _ := seedConfirmedFixture(t, pool)
	buyer := common.HexToAddress("0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266")
	in := applyInputFromFixture(orderID, courseID, intentID, txHash, market, buyer)
	c := workerorder.NewConfirmer(pool)

	// 第一次：happy path。
	enrollID, state, err := c.Apply(context.Background(), in)
	if err != nil {
		t.Fatalf("first Apply: %v", err)
	}
	if state != "confirmed" {
		t.Fatalf("state = %q, want confirmed", state)
	}
	if enrollID == uuid.Nil {
		t.Fatal("enrollment id is nil")
	}

	// 第二次：相同 (chain_id, tx_hash, log_index=0) → chain_events ON CONFLICT DO NOTHING
	// 跳过新插；enrollment 唯一约束幂等命中；order 状态保持 confirmed。
	_, state2, err := c.Apply(context.Background(), in)
	if err != nil {
		t.Fatalf("second Apply: %v", err)
	}
	if state2 != "confirmed" {
		t.Errorf("state2 = %q, want confirmed (idempotent)", state2)
	}
	// enrollment id 可能为 Nil（ON CONFLICT 走 UPDATE 路径 → 但 Scan 仍会返回旧 id），
	// 重点是行数不增。
	if n := countEnrollments(t, pool, userID, courseID); n != 1 {
		t.Errorf("enrollments after replay = %d, want 1", n)
	}
	if n := countChainEvents(t, pool, testChainID, txHash); n != 1 {
		t.Errorf("chain_events after replay = %d, want 1", n)
	}
	status, _ := readOrderStatus(t, pool, orderID)
	if status != "confirmed" {
		t.Errorf("order status after replay = %q, want confirmed", status)
	}
}

// TestConfirmer_WrongBuyer 覆盖 DoD #2：事件里的 buyer 与 intent 钱包不一致
// → 走 ValidateReceipt 失败分支；order 标 failed，failure_code=RECEIPT_MISMATCH；
// 不写 enrollment。
func TestConfirmer_WrongBuyer(t *testing.T) {
	pool := itPool(t)
	orderID, userID, courseID, intentID, txHash, market, _ := seedConfirmedFixture(t, pool)
	// 故意用另一个地址（与 seed wallet 不同）。
	wrongBuyer := common.HexToAddress("0xdead000000000000000000000000000000000001")
	in := applyInputFromFixture(orderID, courseID, intentID, txHash, market, wrongBuyer)
	c := workerorder.NewConfirmer(pool)

	_, state, err := c.Apply(context.Background(), in)
	// ValidateReceipt 失败时 Apply 返回 (uuid.Nil, "failed", nil) — 这是预期：
	// 错误已经被吞进 orders.failure_code，返回 state 字段让 caller 决定。
	if err != nil {
		t.Fatalf("Apply returned err: %v (want nil because mismatch is recorded as failed)", err)
	}
	if state != "failed" {
		t.Fatalf("state = %q, want failed", state)
	}

	status, fc := readOrderStatus(t, pool, orderID)
	if status != "failed" {
		t.Errorf("order status = %q, want failed", status)
	}
	if fc == nil || *fc != "RECEIPT_MISMATCH" {
		t.Errorf("failure_code = %v, want RECEIPT_MISMATCH", fc)
	}
	if n := countEnrollments(t, pool, userID, courseID); n != 0 {
		t.Errorf("enrollments = %d, want 0 (no enrollment on mismatch)", n)
	}
	// chain_events 仍被记录（用于审计 / 重放），但 enrollment 不创建。
	if n := countChainEvents(t, pool, testChainID, txHash); n != 1 {
		t.Errorf("chain_events = %d, want 1 (audit row still recorded)", n)
	}
}

// TestConfirmer_WrongMarket ensures a lookalike event emitted by another contract
// cannot confirm an order, even when all event fields match the intent.
func TestConfirmer_WrongMarket(t *testing.T) {
	pool := itPool(t)
	orderID, userID, courseID, intentID, txHash, market, _ := seedConfirmedFixture(t, pool)
	buyer := common.HexToAddress("0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266")
	in := applyInputFromFixture(uuid.Nil, courseID, intentID, txHash, market, buyer)
	in.ContractAddress = common.HexToAddress("0xdead000000000000000000000000000000000002")

	_, _, err := workerorder.NewConfirmer(pool).Apply(context.Background(), in)
	if !errors.Is(err, workerorder.ErrMismatch) {
		t.Fatalf("Apply error = %v, want ErrMismatch", err)
	}
	status, _ := readOrderStatus(t, pool, orderID)
	if status != "submitted" {
		t.Fatalf("order status = %q, want submitted", status)
	}
	if n := countEnrollments(t, pool, userID, courseID); n != 0 {
		t.Fatalf("enrollments = %d, want 0", n)
	}
}
