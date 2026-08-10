// F03-T17 (部分) — 订单 / 购买意图 集成测试。
//
// 覆盖：
//   - Idempotency：同一 (user_id, idempotency_key) 重复创建 → 返回同一 intent；
//   - 过期：expires_at 已过 → SubmitTransaction 返回 ErrIntentExpired；
//   - Price 版本：valid_to 非空（已下线）→ ErrPriceNotFound；
//   - 已购买：enrollments 已存在 → ErrAlreadyPurchased；
//   - Wallet mismatch：wallet.chain_id ≠ intent.chain_id → error；
//   - Wallet not owned：wallet 属于别人 → ErrWalletNotOwned；
//   - Tx hash 长度非法：ErrTxBadHash；
//   - Tx chain mismatch：SubmitTransaction 的 chain 与 intent 不匹配 → ErrTxChainMismatch；
//   - Tx already used：同 (chain_id, tx_hash) 被另一 order 占用 → ErrTxAlreadyUsed；
//   - Happy path：CreateIntent → SubmitTransaction → order.status=submitted。
//   - GetOrder：非 owner 拒绝；admin 允许。
//   - ListMyOrders：只返回自己；limit 兜底。
package integration_test

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/x-web3/api/internal/course"
	"github.com/x-web3/api/internal/order"
	"github.com/x-web3/api/internal/review"
)

// orderFixture 测试夹具：用户 / 钱包 / 课程 / 当前价格。
type orderFixture struct {
	UserID      uuid.UUID
	OtherUserID uuid.UUID
	WalletID    uuid.UUID
	OtherWallet uuid.UUID
	CourseID    uuid.UUID
	PriceID     uuid.UUID
	ChainID     int64
	MarketAddr  string
	TokenAddr   string
}

// makeOrderFixture 直接拿 *pgxpool.Pool 准备完整场景：
//  user + wallet（chain=11155111） + published course +
//  course_prices（valid_to=NULL）。
func makeOrderFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, chainID int64) orderFixture {
	t.Helper()
	teacherID := uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users(id, privy_user_id, status) VALUES($1, $2, 'active')`,
		teacherID, "did:privy:t-"+uuid.NewString()); err != nil {
		t.Fatalf("insert teacher: %v", err)
	}
	buyer := uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users(id, privy_user_id, status) VALUES($1, $2, 'active')`,
		buyer, "did:privy:b-"+uuid.NewString()); err != nil {
		t.Fatalf("insert buyer: %v", err)
	}
	other := uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users(id, privy_user_id, status) VALUES($1, $2, 'active')`,
		other, "did:privy:o-"+uuid.NewString()); err != nil {
		t.Fatalf("insert other: %v", err)
	}
	walletID := uuid.New()
	walletAddr := randHexAddr(t)
	if _, err := pool.Exec(ctx,
		`INSERT INTO wallets(id, user_id, chain_id, address, is_primary) VALUES($1, $2, $3, $4, true)`,
		walletID, buyer, chainID, walletAddr); err != nil {
		t.Fatalf("insert wallet: %v", err)
	}
	otherWalletID := uuid.New()
	otherWalletAddr := randHexAddr(t)
	if _, err := pool.Exec(ctx,
		`INSERT INTO wallets(id, user_id, chain_id, address, is_primary) VALUES($1, $2, $3, $4, true)`,
		otherWalletID, other, chainID, otherWalletAddr); err != nil {
		t.Fatalf("insert other wallet: %v", err)
	}
	courseRepo := course.NewRepo(pool)
	created, err := courseRepo.Create(ctx, course.CreateInput{
		TeacherID:  teacherID,
		Slug:       "order-test-" + uuid.NewString(),
		Title:      "Order test",
		PriceMinor: 1000,
		Currency:   "USD",
	})
	if err != nil {
		t.Fatalf("create course: %v", err)
	}
	if _, err := courseRepo.Transition(ctx, created.ID, teacherID, review.Submit, false, ""); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if _, err := courseRepo.Transition(ctx, created.ID, teacherID, review.Approve, true, ""); err != nil {
		t.Fatalf("approve: %v", err)
	}
	marketAddr := randHexAddr(t)
	tokenAddr := randHexAddr(t)
	var priceID uuid.UUID
	if err := pool.QueryRow(ctx,
		`INSERT INTO course_prices(course_id, version, chain_id, token_address, amount, decimals, market_address)
VALUES($1, 1, $2, $3, 100000000::numeric, 18, $4) RETURNING id`,
		created.ID, chainID, tokenAddr, marketAddr).Scan(&priceID); err != nil {
		t.Fatalf("insert price: %v", err)
	}
	return orderFixture{
		UserID:      buyer,
		OtherUserID: other,
		WalletID:    walletID,
		OtherWallet: otherWalletID,
		CourseID:    created.ID,
		PriceID:     priceID,
		ChainID:     chainID,
		MarketAddr:  marketAddr,
		TokenAddr:   tokenAddr,
	}
}

// makeTxHash 返回 32 字节 hash，第一个字节是 tag（方便调试时区分用例）。
// 同时用 nanosecond 压成 SHA256，保证相邻调用不同。
func makeTxHash(tag byte) []byte {
	seed := make([]byte, 16)
	binary.BigEndian.PutUint64(seed[:8], uint64(time.Now().UnixNano()))
	seed[8] = tag
	h := sha256.Sum256(seed)
	return h[:]
}

// randHexAddr 返回随机 0x 前缀的 40 字符 hex 地址（避免固定地址跨测试撞
// (chain_namespace, chain_id, address) 唯一约束）。
func randHexAddr(t *testing.T) string {
	t.Helper()
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return "0x" + hex.EncodeToString(b)
}

func TestOrder_CreateIntent_HappyPath(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	fx := makeOrderFixture(t, ctx, pool, 11155111)
	svc := order.NewService(pool, 15*time.Minute)
	key := "idem-" + uuid.NewString()
	intent, err := svc.CreateIntent(ctx, order.CreateIntentInput{
		UserID:         fx.UserID,
		CourseID:       fx.CourseID,
		ChainID:        fx.ChainID,
		WalletID:       fx.WalletID,
		IdempotencyKey: key,
	})
	if err != nil {
		t.Fatalf("create intent: %v", err)
	}
	if intent.Status != "created" {
		t.Fatalf("status = %s, want created", intent.Status)
	}
	if intent.PriceVersion != 1 {
		t.Fatalf("priceVersion = %d, want 1", intent.PriceVersion)
	}
	if intent.ChainID != fx.ChainID {
		t.Fatalf("chainID = %d, want %d", intent.ChainID, fx.ChainID)
	}
	if intent.CourseKeyHex == "" {
		t.Fatalf("courseKey empty")
	}
}

func TestOrder_CreateIntent_Idempotency(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	fx := makeOrderFixture(t, ctx, pool, 11155111)
	svc := order.NewService(pool, 15*time.Minute)
	key := "idem-" + uuid.NewString()
	in := order.CreateIntentInput{
		UserID:         fx.UserID,
		CourseID:       fx.CourseID,
		ChainID:        fx.ChainID,
		WalletID:       fx.WalletID,
		IdempotencyKey: key,
	}
	first, err := svc.CreateIntent(ctx, in)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := svc.CreateIntent(ctx, in)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("idempotency: ids differ: %s vs %s", first.ID, second.ID)
	}
	var count int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM purchase_intents WHERE user_id=$1 AND idempotency_key=$2`,
		fx.UserID, key).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("row count = %d, want 1", count)
	}
}

func TestOrder_CreateIntent_RejectsEmptyIdempotencyKey(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	fx := makeOrderFixture(t, ctx, pool, 11155111)
	svc := order.NewService(pool, 15*time.Minute)
	_, err := svc.CreateIntent(ctx, order.CreateIntentInput{
		UserID:         fx.UserID,
		CourseID:       fx.CourseID,
		ChainID:        fx.ChainID,
		WalletID:       fx.WalletID,
		IdempotencyKey: "   ",
	})
	if err == nil {
		t.Fatalf("expected error for empty key")
	}
}

func TestOrder_CreateIntent_RejectsWalletNotOwned(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	fx := makeOrderFixture(t, ctx, pool, 11155111)
	svc := order.NewService(pool, 15*time.Minute)
	_, err := svc.CreateIntent(ctx, order.CreateIntentInput{
		UserID:         fx.UserID,
		CourseID:       fx.CourseID,
		ChainID:        fx.ChainID,
		WalletID:       fx.OtherWallet, // belongs to other
		IdempotencyKey: "idem-" + uuid.NewString(),
	})
	if !errors.Is(err, order.ErrWalletNotOwned) {
		t.Fatalf("expected ErrWalletNotOwned, got %v", err)
	}
}

func TestOrder_CreateIntent_RejectsWalletChainMismatch(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	fx := makeOrderFixture(t, ctx, pool, 11155111)
	// 给 buyer 塞一个 chain=1 的钱包
	var wrongChainWallet uuid.UUID
	if err := pool.QueryRow(ctx,
		`INSERT INTO wallets(id, user_id, chain_id, address, is_primary) VALUES($1, $2, $3, $4, true)
RETURNING id`,
		uuid.New(), fx.UserID, int64(1), randHexAddr(t)).Scan(&wrongChainWallet); err != nil {
		t.Fatalf("insert chain1 wallet: %v", err)
	}
	svc := order.NewService(pool, 15*time.Minute)
	_, err := svc.CreateIntent(ctx, order.CreateIntentInput{
		UserID:         fx.UserID,
		CourseID:       fx.CourseID,
		ChainID:        fx.ChainID, // 11155111
		WalletID:       wrongChainWallet,
		IdempotencyKey: "idem-" + uuid.NewString(),
	})
	if err == nil {
		t.Fatalf("expected chain mismatch error")
	}
}

func TestOrder_CreateIntent_RejectsAlreadyPurchased(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	fx := makeOrderFixture(t, ctx, pool, 11155111)
	if _, err := pool.Exec(ctx,
		`INSERT INTO enrollments(user_id, course_id, source) VALUES($1, $2, 'seed')`,
		fx.UserID, fx.CourseID); err != nil {
		t.Fatalf("enrollment: %v", err)
	}
	svc := order.NewService(pool, 15*time.Minute)
	_, err := svc.CreateIntent(ctx, order.CreateIntentInput{
		UserID:         fx.UserID,
		CourseID:       fx.CourseID,
		ChainID:        fx.ChainID,
		WalletID:       fx.WalletID,
		IdempotencyKey: "idem-" + uuid.NewString(),
	})
	if !errors.Is(err, order.ErrAlreadyPurchased) {
		t.Fatalf("expected ErrAlreadyPurchased, got %v", err)
	}
}

func TestOrder_CreateIntent_RejectsPriceMissing(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	fx := makeOrderFixture(t, ctx, pool, 11155111)
	if _, err := pool.Exec(ctx,
		`UPDATE course_prices SET valid_to = now() WHERE id = $1`, fx.PriceID); err != nil {
		t.Fatalf("close price: %v", err)
	}
	svc := order.NewService(pool, 15*time.Minute)
	_, err := svc.CreateIntent(ctx, order.CreateIntentInput{
		UserID:         fx.UserID,
		CourseID:       fx.CourseID,
		ChainID:        fx.ChainID,
		WalletID:       fx.WalletID,
		IdempotencyKey: "idem-" + uuid.NewString(),
	})
	if !errors.Is(err, order.ErrPriceNotFound) {
		t.Fatalf("expected ErrPriceNotFound, got %v", err)
	}
}

func TestOrder_CreateIntent_FreezesPriceVersion(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	fx := makeOrderFixture(t, ctx, pool, 11155111)
	svc := order.NewService(pool, 15*time.Minute)

	intent, err := svc.CreateIntent(ctx, order.CreateIntentInput{
		UserID:         fx.UserID,
		CourseID:       fx.CourseID,
		ChainID:        fx.ChainID,
		WalletID:       fx.WalletID,
		IdempotencyKey: "idem-" + uuid.NewString(),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if intent.PriceVersion != 1 {
		t.Fatalf("frozen priceVersion = %d, want 1", intent.PriceVersion)
	}

	// 把 v1 关掉，再插 v2
	if _, err := pool.Exec(ctx,
		`UPDATE course_prices SET valid_to=now() WHERE id=$1`, fx.PriceID); err != nil {
		t.Fatalf("close v1: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO course_prices(course_id, version, chain_id, token_address, amount, decimals, market_address)
VALUES($1, 2, $2, $3, 200000000::numeric, 18, $4)`,
		fx.CourseID, fx.ChainID, fx.TokenAddr, fx.MarketAddr); err != nil {
		t.Fatalf("insert v2: %v", err)
	}
	// 新 intent（other 用户）应拿到 v2
	intent2, err := svc.CreateIntent(ctx, order.CreateIntentInput{
		UserID:         fx.OtherUserID,
		CourseID:       fx.CourseID,
		ChainID:        fx.ChainID,
		WalletID:       fx.OtherWallet,
		IdempotencyKey: "idem-" + uuid.NewString(),
	})
	if err != nil {
		t.Fatalf("create v2: %v", err)
	}
	if intent2.PriceVersion != 2 {
		t.Fatalf("new intent priceVersion = %d, want 2", intent2.PriceVersion)
	}
	// 原 intent 仍指向 v1
	got, err := svc.GetIntent(ctx, intent.ID)
	if err != nil {
		t.Fatalf("get intent: %v", err)
	}
	if got.PriceVersion != 1 {
		t.Fatalf("original intent priceVersion drifted to %d", got.PriceVersion)
	}
}

func TestOrder_Submit_HappyPath(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	fx := makeOrderFixture(t, ctx, pool, 11155111)
	svc := order.NewService(pool, 15*time.Minute)
	intent, err := svc.CreateIntent(ctx, order.CreateIntentInput{
		UserID:         fx.UserID,
		CourseID:       fx.CourseID,
		ChainID:        fx.ChainID,
		WalletID:       fx.WalletID,
		IdempotencyKey: "idem-" + uuid.NewString(),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	ord, err := svc.SubmitTransaction(ctx, intent.ID, fx.UserID, fx.ChainID, makeTxHash(0xAA))
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if ord.Status != "submitted" {
		t.Fatalf("status = %s, want submitted", ord.Status)
	}
	if ord.TxHashHex == "" {
		t.Fatalf("txHashHex empty")
	}
	refreshed, err := svc.GetIntent(ctx, intent.ID)
	if err != nil {
		t.Fatalf("get intent: %v", err)
	}
	if refreshed.Status != "submitted" {
		t.Fatalf("intent status = %s, want submitted", refreshed.Status)
	}
}

func TestOrder_Submit_RejectsExpiredIntent(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	fx := makeOrderFixture(t, ctx, pool, 11155111)
	svc := order.NewService(pool, 15*time.Minute)
	intent, err := svc.CreateIntent(ctx, order.CreateIntentInput{
		UserID:         fx.UserID,
		CourseID:       fx.CourseID,
		ChainID:        fx.ChainID,
		WalletID:       fx.WalletID,
		IdempotencyKey: "idem-" + uuid.NewString(),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE purchase_intents SET expires_at = now() - interval '1 minute' WHERE id=$1`,
		intent.ID); err != nil {
		t.Fatalf("expire: %v", err)
	}
	_, err = svc.SubmitTransaction(ctx, intent.ID, fx.UserID, fx.ChainID, makeTxHash(0xBB))
	if !errors.Is(err, order.ErrIntentExpired) {
		t.Fatalf("expected ErrIntentExpired, got %v", err)
	}
	got, _ := svc.GetIntent(ctx, intent.ID)
	if got.Status != "expired" {
		t.Fatalf("post-expire status = %s, want expired", got.Status)
	}
}

func TestOrder_Submit_RejectsChainMismatch(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	fx := makeOrderFixture(t, ctx, pool, 11155111)
	svc := order.NewService(pool, 15*time.Minute)
	intent, err := svc.CreateIntent(ctx, order.CreateIntentInput{
		UserID:         fx.UserID,
		CourseID:       fx.CourseID,
		ChainID:        fx.ChainID,
		WalletID:       fx.WalletID,
		IdempotencyKey: "idem-" + uuid.NewString(),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	_, err = svc.SubmitTransaction(ctx, intent.ID, fx.UserID, 1, makeTxHash(0xCC))
	if !errors.Is(err, order.ErrTxChainMismatch) {
		t.Fatalf("expected ErrTxChainMismatch, got %v", err)
	}
}

func TestOrder_Submit_RejectsBadHashLength(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	fx := makeOrderFixture(t, ctx, pool, 11155111)
	svc := order.NewService(pool, 15*time.Minute)
	intent, err := svc.CreateIntent(ctx, order.CreateIntentInput{
		UserID:         fx.UserID,
		CourseID:       fx.CourseID,
		ChainID:        fx.ChainID,
		WalletID:       fx.WalletID,
		IdempotencyKey: "idem-" + uuid.NewString(),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	_, err = svc.SubmitTransaction(ctx, intent.ID, fx.UserID, fx.ChainID, []byte{1, 2, 3})
	if !errors.Is(err, order.ErrTxBadHash) {
		t.Fatalf("expected ErrTxBadHash, got %v", err)
	}
}

func TestOrder_Submit_RejectsNotOwnedIntent(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	fx := makeOrderFixture(t, ctx, pool, 11155111)
	svc := order.NewService(pool, 15*time.Minute)
	intent, err := svc.CreateIntent(ctx, order.CreateIntentInput{
		UserID:         fx.UserID,
		CourseID:       fx.CourseID,
		ChainID:        fx.ChainID,
		WalletID:       fx.WalletID,
		IdempotencyKey: "idem-" + uuid.NewString(),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	_, err = svc.SubmitTransaction(ctx, intent.ID, fx.OtherUserID, fx.ChainID, makeTxHash(0xDD))
	if !errors.Is(err, order.ErrIntentNotOwned) {
		t.Fatalf("expected ErrIntentNotOwned, got %v", err)
	}
}

func TestOrder_Submit_RejectsAlreadySubmittedIntent(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	fx := makeOrderFixture(t, ctx, pool, 11155111)
	svc := order.NewService(pool, 15*time.Minute)
	intent, err := svc.CreateIntent(ctx, order.CreateIntentInput{
		UserID:         fx.UserID,
		CourseID:       fx.CourseID,
		ChainID:        fx.ChainID,
		WalletID:       fx.WalletID,
		IdempotencyKey: "idem-" + uuid.NewString(),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := svc.SubmitTransaction(ctx, intent.ID, fx.UserID, fx.ChainID, makeTxHash(0xEE)); err != nil {
		t.Fatalf("first submit: %v", err)
	}
	_, err = svc.SubmitTransaction(ctx, intent.ID, fx.UserID, fx.ChainID, makeTxHash(0xEF))
	if !errors.Is(err, order.ErrIntentBadState) {
		t.Fatalf("expected ErrIntentBadState, got %v", err)
	}
}

func TestOrder_Submit_RejectsTxAlreadyUsed(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	fx := makeOrderFixture(t, ctx, pool, 11155111)
	svc := order.NewService(pool, 15*time.Minute)

	intent1, err := svc.CreateIntent(ctx, order.CreateIntentInput{
		UserID:         fx.UserID,
		CourseID:       fx.CourseID,
		ChainID:        fx.ChainID,
		WalletID:       fx.WalletID,
		IdempotencyKey: "idem-" + uuid.NewString(),
	})
	if err != nil {
		t.Fatalf("create intent1: %v", err)
	}
	txHash := makeTxHash(0xF1)
	if _, err := svc.SubmitTransaction(ctx, intent1.ID, fx.UserID, fx.ChainID, txHash); err != nil {
		t.Fatalf("submit 1: %v", err)
	}

	// 第二个用户尝试复用同一 tx_hash
	intent2, err := svc.CreateIntent(ctx, order.CreateIntentInput{
		UserID:         fx.OtherUserID,
		CourseID:       fx.CourseID,
		ChainID:        fx.ChainID,
		WalletID:       fx.OtherWallet,
		IdempotencyKey: "idem-" + uuid.NewString(),
	})
	if err != nil {
		t.Fatalf("create intent2: %v", err)
	}
	_, err = svc.SubmitTransaction(ctx, intent2.ID, fx.OtherUserID, fx.ChainID, txHash)
	if !errors.Is(err, order.ErrTxAlreadyUsed) {
		t.Fatalf("expected ErrTxAlreadyUsed, got %v", err)
	}
}

func TestOrder_GetOrder_OwnerAndAdmin(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	fx := makeOrderFixture(t, ctx, pool, 11155111)
	svc := order.NewService(pool, 15*time.Minute)
	intent, err := svc.CreateIntent(ctx, order.CreateIntentInput{
		UserID:         fx.UserID,
		CourseID:       fx.CourseID,
		ChainID:        fx.ChainID,
		WalletID:       fx.WalletID,
		IdempotencyKey: "idem-" + uuid.NewString(),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	ord, err := svc.SubmitTransaction(ctx, intent.ID, fx.UserID, fx.ChainID, makeTxHash(0xF2))
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if _, err := svc.GetOrder(ctx, ord.ID, fx.OtherUserID, false); !errors.Is(err, order.ErrOrderNotOwned) {
		t.Fatalf("expected ErrOrderNotOwned, got %v", err)
	}
	if _, err := svc.GetOrder(ctx, ord.ID, fx.OtherUserID, true); err != nil {
		t.Fatalf("admin read: %v", err)
	}
	if _, err := svc.GetOrder(ctx, ord.ID, fx.UserID, false); err != nil {
		t.Fatalf("owner read: %v", err)
	}
}

func TestOrder_ListMyOrders_ScopedToOwner(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	fx := makeOrderFixture(t, ctx, pool, 11155111)
	svc := order.NewService(pool, 15*time.Minute)
	intent, err := svc.CreateIntent(ctx, order.CreateIntentInput{
		UserID:         fx.UserID,
		CourseID:       fx.CourseID,
		ChainID:        fx.ChainID,
		WalletID:       fx.WalletID,
		IdempotencyKey: "idem-" + uuid.NewString(),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := svc.SubmitTransaction(ctx, intent.ID, fx.UserID, fx.ChainID, makeTxHash(0xF3)); err != nil {
		t.Fatalf("submit: %v", err)
	}
	orders, err := svc.ListMyOrders(ctx, fx.UserID, 50)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(orders) != 1 {
		t.Fatalf("count = %d, want 1", len(orders))
	}
	others, err := svc.ListMyOrders(ctx, fx.OtherUserID, 50)
	if err != nil {
		t.Fatalf("list other: %v", err)
	}
	if len(others) != 0 {
		t.Fatalf("other count = %d, want 0", len(others))
	}
}