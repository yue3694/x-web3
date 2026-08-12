package integration_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/x-web3/api/internal/audit"
	"github.com/x-web3/api/internal/wallet"
)

// newWalletService 构造真实的 wallet.Service：miniredis + 真实 DB + audit spy。
//
// 每个测试用例前先 TRUNCATE wallets，避免跨用例地址残留导致 "already bound"。
func newWalletService(t *testing.T) (*wallet.Service, *miniredis.Miniredis) {
	t.Helper()
	pool := testPool(t)
	if _, err := pool.Exec(context.Background(), `TRUNCATE wallets`); err != nil {
		t.Fatalf("truncate wallets: %v", err)
	}

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	nonces := wallet.NewNonceStore(rdb, 5*time.Minute)
	svc := wallet.NewService(pool, nonces, "localhost:8080",
		audit.NewWriterWithSink(nopSink{}, zap.NewNop()))
	return svc, mr
}

// nopSink 是 audit.Sink 的最小实现，写入永远成功但不真的落库。
// audit_logs 由真业务 tx 外调用，单元/集成测试中可丢弃。
type nopSink struct{}

func (nopSink) Exec(_ context.Context, _ string, _ ...any) (any, error) { return nil, nil }

// makeUser 直接在 DB 插一个 active user 并返回 id。
func makeUser(t *testing.T, ctx context.Context) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if _, err := testPool(t).Exec(ctx,
		`INSERT INTO users(id, privy_user_id, status) VALUES($1, $2, 'active')`,
		id, "did:privy:wallet-"+id.String()); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	return id
}

// makeBoundAddress 创建一个用户并绑定一个**新生成**的地址；返回 (userID, walletID, address)。
//
// 流程必须按生产 API 走：先 IssueNonce → 再签名 → 再 Bind。
// 每次调用都生成新私钥，避免跨测试相互污染（同一地址已被他人绑定会触发 already-bound）。
func makeBoundAddress(t *testing.T, ctx context.Context, svc *wallet.Service, chainID int64) (uuid.UUID, uuid.UUID, string, *ecdsa.PrivateKey) {
	t.Helper()
	uid := makeUser(t, ctx)
	priv := freshPriv(t)
	addr := crypto.PubkeyToAddress(priv.PublicKey).Hex()

	// 1. 走真实 API 流程：先发 nonce
	nonce, exp, err := svc.IssueNonce(ctx, uid)
	if err != nil {
		t.Fatalf("IssueNonce: %v", err)
	}
	// 2. 用发来的 nonce 重新签名（exp 由发 nonce 时决定，不可改）
	msg := wallet.CanonicalMessage(nonce, chainID, strings.ToLower(addr), "localhost:8080", exp.Format(time.RFC3339))
	raw, err := crypto.Sign(wallet.PrefixedHashForTest(msg), priv)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	raw[64] += 27
	sig := common.Bytes2Hex(raw)

	if err := svc.Bind(ctx, wallet.BindRequest{
		UserID: uid, ChainID: chainID, Address: addr,
		Nonce: nonce, Expiry: exp, Signature: sig, Domain: "localhost:8080",
	}); err != nil {
		t.Fatalf("bind: %v", err)
	}
	var wid uuid.UUID
	if err := testPool(t).QueryRow(ctx, `SELECT id FROM wallets WHERE user_id=$1 AND address=$2`,
		uid, strings.ToLower(addr)).Scan(&wid); err != nil {
		t.Fatalf("select wallet: %v", err)
	}
	return uid, wid, addr, priv
}

// signBind 走真实 EIP-191 personal_sign 签名；返回 (nonce, expiry, sigHex)。
func signBind(t *testing.T, priv *ecdsa.PrivateKey, address, domain string, chainID int64) (string, time.Time, string) {
	t.Helper()
	nonce := uuid.NewString()
	expiry := time.Now().UTC().Add(5 * time.Minute)
	msg := wallet.CanonicalMessage(nonce, chainID, strings.ToLower(address), domain, expiry.Format(time.RFC3339))
	raw, err := crypto.Sign(wallet.PrefixedHashForTest(msg), priv)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	raw[64] += 27
	return nonce, expiry, common.Bytes2Hex(raw)
}

const testPriv = "4af1bceebf7f3634ec3cff8a2c38e51178d5d4ce585c52d6043cfe7f3b25d4e1"

// freshPriv 返回一个新生成的 ECDSA 私钥；用于同测试函数内做多钱包绑定。
func freshPriv(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	p, err := ecdsa.GenerateKey(crypto.S256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	return p
}

// TestWalletService_IssueNonce 转发到 NonceStore。
func TestWalletService_IssueNonce(t *testing.T) {
	svc, _ := newWalletService(t)
	ctx := context.Background()
	uid := makeUser(t, ctx)

	nonce, exp, err := svc.IssueNonce(ctx, uid)
	if err != nil {
		t.Fatalf("IssueNonce: %v", err)
	}
	if nonce == "" {
		t.Fatal("empty nonce")
	}
	if time.Until(exp) <= 0 {
		t.Fatal("expiry in the past")
	}
}

// TestWalletService_Bind_HappyPath 完整签名 → 落库。
func TestWalletService_Bind_HappyPath(t *testing.T) {
	svc, _ := newWalletService(t)
	ctx := context.Background()
	priv, err := crypto.HexToECDSA(testPriv)
	if err != nil {
		t.Fatalf("HexToECDSA: %v", err)
	}
	uid := makeUser(t, ctx)
	addr := crypto.PubkeyToAddress(priv.PublicKey).Hex()

	// 走真实流程：先 IssueNonce，再用返回的 nonce 签名
	nonce, exp, err := svc.IssueNonce(ctx, uid)
	if err != nil {
		t.Fatalf("IssueNonce: %v", err)
	}
	msg := wallet.CanonicalMessage(nonce, 11155111, strings.ToLower(addr), "localhost:8080", exp.Format(time.RFC3339))
	raw, _ := crypto.Sign(wallet.PrefixedHashForTest(msg), priv)
	raw[64] += 27
	sig := common.Bytes2Hex(raw)

	if err := svc.Bind(ctx, wallet.BindRequest{
		UserID: uid, ChainID: 11155111, Address: addr,
		Nonce: nonce, Expiry: exp, Signature: sig, Domain: "localhost:8080",
	}); err != nil {
		t.Fatalf("Bind: %v", err)
	}

	var got string
	if err := testPool(t).QueryRow(ctx, `SELECT address FROM wallets WHERE user_id=$1`, uid).Scan(&got); err != nil {
		t.Fatalf("query wallet: %v", err)
	}
	if !strings.EqualFold(got, addr) {
		t.Errorf("address = %q, want %q", got, addr)
	}
}

// TestWalletService_Bind_RejectsDomainMismatch domain 不一致直接拒绝（在 nonce 校验之前）。
func TestWalletService_Bind_RejectsDomainMismatch(t *testing.T) {
	svc, _ := newWalletService(t)
	ctx := context.Background()
	priv, _ := crypto.HexToECDSA(testPriv)
	uid := makeUser(t, ctx)
	addr := crypto.PubkeyToAddress(priv.PublicKey).Hex()
	// 即使 nonce 没经过 Issue，domain 不匹配也必须先拒
	nonce, exp, sig := signBind(t, priv, addr, "evil.com", 11155111)
	err := svc.Bind(ctx, wallet.BindRequest{
		UserID: uid, ChainID: 11155111, Address: addr,
		Nonce: nonce, Expiry: exp, Signature: sig, Domain: "evil.com",
	})
	if err == nil || !strings.Contains(err.Error(), "domain") {
		t.Fatalf("expected domain mismatch, got %v", err)
	}
}

// TestWalletService_Bind_RejectsExpiredSignature 过期签名必须拒绝（在 nonce 校验之前）。
func TestWalletService_Bind_RejectsExpiredSignature(t *testing.T) {
	svc, _ := newWalletService(t)
	ctx := context.Background()
	priv, _ := crypto.HexToECDSA(testPriv)
	uid := makeUser(t, ctx)
	addr := crypto.PubkeyToAddress(priv.PublicKey).Hex()
	nonce, _, sig := signBind(t, priv, addr, "localhost:8080", 11155111)

	err := svc.Bind(ctx, wallet.BindRequest{
		UserID: uid, ChainID: 11155111, Address: addr,
		Nonce: nonce, Expiry: time.Now().Add(-time.Minute), Signature: sig, Domain: "localhost:8080",
	})
	if err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expected expired, got %v", err)
	}
}

// TestWalletService_Bind_RejectsBadAddress 非 hex 地址。
func TestWalletService_Bind_RejectsBadAddress(t *testing.T) {
	svc, _ := newWalletService(t)
	ctx := context.Background()
	uid := makeUser(t, ctx)
	err := svc.Bind(ctx, wallet.BindRequest{
		UserID: uid, ChainID: 1, Address: "not-an-address",
		Nonce: "n", Expiry: time.Now().Add(time.Minute), Signature: strings.Repeat("ab", 65), Domain: "localhost:8080",
	})
	if err == nil || !strings.Contains(err.Error(), "bad address") {
		t.Fatalf("expected bad address, got %v", err)
	}
}

// TestWalletService_Bind_RejectsSecondUserSameWallet 同地址第二次绑定（不同 user）必须拒绝。
func TestWalletService_Bind_RejectsSecondUserSameWallet(t *testing.T) {
	svc, _ := newWalletService(t)
	ctx := context.Background()
	_, _, addr, priv := makeBoundAddress(t, ctx, svc, 11155111)
	other := makeUser(t, ctx)

	nonce, exp, err := svc.IssueNonce(ctx, other)
	if err != nil {
		t.Fatalf("IssueNonce: %v", err)
	}
	msg := wallet.CanonicalMessage(nonce, 11155111, strings.ToLower(addr), "localhost:8080", exp.Format(time.RFC3339))
	raw, _ := crypto.Sign(wallet.PrefixedHashForTest(msg), priv)
	raw[64] += 27
	sig := common.Bytes2Hex(raw)

	err = svc.Bind(ctx, wallet.BindRequest{
		UserID: other, ChainID: 11155111, Address: addr,
		Nonce: nonce, Expiry: exp, Signature: sig, Domain: "localhost:8080",
	})
	if err == nil {
		t.Fatal("expected second bind to fail")
	}
	if !strings.Contains(err.Error(), "already bound") {
		t.Errorf("expected already-bound, got %v", err)
	}
}

// TestWalletService_Bind_RejectsNonceReuse 同一 nonce 第二次使用必须拒绝。
func TestWalletService_Bind_RejectsNonceReuse(t *testing.T) {
	svc, _ := newWalletService(t)
	ctx := context.Background()
	priv, _ := crypto.HexToECDSA(testPriv)
	uid := makeUser(t, ctx)
	addr := crypto.PubkeyToAddress(priv.PublicKey).Hex()
	nonce, exp, err := svc.IssueNonce(ctx, uid)
	if err != nil {
		t.Fatalf("IssueNonce: %v", err)
	}
	msg := wallet.CanonicalMessage(nonce, 11155111, strings.ToLower(addr), "localhost:8080", exp.Format(time.RFC3339))
	raw, _ := crypto.Sign(wallet.PrefixedHashForTest(msg), priv)
	raw[64] += 27
	sig := common.Bytes2Hex(raw)

	req := wallet.BindRequest{
		UserID: uid, ChainID: 11155111, Address: addr,
		Nonce: nonce, Expiry: exp, Signature: sig, Domain: "localhost:8080",
	}
	if err := svc.Bind(ctx, req); err != nil {
		t.Fatalf("first bind: %v", err)
	}
	if err := svc.Bind(ctx, req); err == nil {
		t.Fatal("expected nonce reuse to fail")
	}
}

// TestWalletService_Unbind_RejectsLastWallet 用户只剩一个钱包时禁止解绑。
func TestWalletService_Unbind_RejectsLastWallet(t *testing.T) {
	svc, _ := newWalletService(t)
	ctx := context.Background()
	uid, wid, _, _ := makeBoundAddress(t, ctx, svc, 11155111)

	err := svc.Unbind(ctx, uid, wid, "127.0.0.1", "ua")
	if err == nil || !strings.Contains(err.Error(), "last wallet") {
		t.Fatalf("expected last-wallet error, got %v", err)
	}
}

// TestWalletService_Unbind_RejectsNotOwner 别人的钱包不能解绑（IDOR 防护）。
func TestWalletService_Unbind_RejectsNotOwner(t *testing.T) {
	svc, _ := newWalletService(t)
	ctx := context.Background()
	owner, wid, _, _ := makeBoundAddress(t, ctx, svc, 11155111)
	_ = owner
	other := makeUser(t, ctx)

	err := svc.Unbind(ctx, other, wid, "127.0.0.1", "ua")
	if err == nil || !strings.Contains(err.Error(), "forbidden") {
		t.Fatalf("expected forbidden, got %v", err)
	}
}

// TestWalletService_Unbind_RejectsUnknownWallet 不存在的 wallet id。
func TestWalletService_Unbind_RejectsUnknownWallet(t *testing.T) {
	svc, _ := newWalletService(t)
	ctx := context.Background()
	uid := makeUser(t, ctx)
	if err := svc.Unbind(ctx, uid, uuid.New(), "127.0.0.1", "ua"); err == nil {
		t.Fatal("expected error for unknown wallet")
	}
}

// TestWalletService_Unbind_HappyPath 多钱包时其中一个可解绑，剩余计数 = 1。
func TestWalletService_Unbind_HappyPath(t *testing.T) {
	svc, _ := newWalletService(t)
	ctx := context.Background()
	uid := makeUser(t, ctx)

	// 同一 user 绑两个不同地址
	privA := freshPriv(t)
	privB := freshPriv(t)
	addrA := crypto.PubkeyToAddress(privA.PublicKey).Hex()
	addrB := crypto.PubkeyToAddress(privB.PublicKey).Hex()
	if addrA == addrB {
		t.Fatal("addrA == addrB (test bug)")
	}

	bindOne := func(priv *ecdsa.PrivateKey, addr string) {
		nonce, exp, err := svc.IssueNonce(ctx, uid)
		if err != nil {
			t.Fatalf("IssueNonce: %v", err)
		}
		msg := wallet.CanonicalMessage(nonce, 11155111, strings.ToLower(addr), "localhost:8080", exp.Format(time.RFC3339))
		raw, _ := crypto.Sign(wallet.PrefixedHashForTest(msg), priv)
		raw[64] += 27
		if err := svc.Bind(ctx, wallet.BindRequest{
			UserID: uid, ChainID: 11155111, Address: addr,
			Nonce: nonce, Expiry: exp, Signature: common.Bytes2Hex(raw), Domain: "localhost:8080",
		}); err != nil {
			t.Fatalf("bind %s: %v", addr, err)
		}
	}
	bindOne(privA, addrA)
	bindOne(privB, addrB)

	var widB uuid.UUID
	if err := testPool(t).QueryRow(ctx, `SELECT id FROM wallets WHERE user_id=$1 AND address=$2`, uid, strings.ToLower(addrB)).Scan(&widB); err != nil {
		t.Fatalf("select B: %v", err)
	}
	if err := svc.Unbind(ctx, uid, widB, "127.0.0.1", "ua"); err != nil {
		t.Fatalf("Unbind B: %v", err)
	}
	var remaining int
	if err := testPool(t).QueryRow(ctx, `SELECT COUNT(*) FROM wallets WHERE user_id=$1`, uid).Scan(&remaining); err != nil {
		t.Fatalf("count: %v", err)
	}
	if remaining != 1 {
		t.Errorf("remaining = %d, want 1", remaining)
	}
}
