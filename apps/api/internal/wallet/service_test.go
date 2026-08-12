package wallet

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/x-web3/api/internal/audit"
)

// testPool 跟 integration 包同名 helper 同等作用；在包内测试用于联调真实 DB。
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL := os.Getenv("DATABASE_URL_TEST")
	if databaseURL == "" {
		t.Skip("DATABASE_URL_TEST is not set")
	}
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(context.Background()); err != nil {
		t.Fatalf("database ping: %v", err)
	}
	return pool
}

// freshPriv 生成新 ECDSA 私钥。
func freshPriv(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	p, err := ecdsa.GenerateKey(crypto.S256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	return p
}

// newSvc 构造真实 Service：miniredis + 真实 DB + audit nopSink。
// 每个用例使用随机用户与钱包地址，避免清空共享测试表影响外键数据。
func newSvc(t *testing.T) (*Service, *miniredis.Miniredis) {
	t.Helper()
	pool := testPool(t)
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	nonces := NewNonceStore(rdb, 5*time.Minute)
	svc := NewService(pool, nonces, "localhost:8080",
		audit.NewWriterWithSink(nopSink{}, zap.NewNop()))
	return svc, mr
}

type nopSink struct{}

func (nopSink) Exec(_ context.Context, _ string, _ ...any) (any, error) { return nil, nil }

// makeUser 在 DB 插一个 active user。
func makeUser(t *testing.T, ctx context.Context) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if _, err := testPool(t).Exec(ctx,
		`INSERT INTO users(id, privy_user_id, status) VALUES($1, $2, 'active')`,
		id, "did:privy:w-"+id.String()); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	return id
}

// makeBound 完整流程：IssueNonce → 签名 → Bind → 返回 (uid, wid, addr, priv)。
func makeBound(t *testing.T, ctx context.Context, svc *Service, chainID int64) (uuid.UUID, uuid.UUID, string, *ecdsa.PrivateKey) {
	t.Helper()
	uid := makeUser(t, ctx)
	priv := freshPriv(t)
	addr := crypto.PubkeyToAddress(priv.PublicKey).Hex()
	nonce, exp, err := svc.IssueNonce(ctx, uid)
	if err != nil {
		t.Fatalf("IssueNonce: %v", err)
	}
	msg := CanonicalMessage(nonce, chainID, strings.ToLower(addr), "localhost:8080", exp.Format(time.RFC3339))
	raw, _ := crypto.Sign(PrefixedHashForTest(msg), priv)
	raw[64] += 27
	if err := svc.Bind(ctx, BindRequest{
		UserID: uid, ChainID: chainID, Address: addr,
		Nonce: nonce, Expiry: exp, Signature: common.Bytes2Hex(raw), Domain: "localhost:8080",
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

// TestService_IssueNonce 转发到 NonceStore。
func TestService_IssueNonce(t *testing.T) {
	svc, _ := newSvc(t)
	ctx := context.Background()
	uid := makeUser(t, ctx)
	nonce, exp, err := svc.IssueNonce(ctx, uid)
	if err != nil || nonce == "" || time.Until(exp) <= 0 {
		t.Fatalf("IssueNonce: nonce=%q exp=%v err=%v", nonce, exp, err)
	}
}

func signLoginRequest(t *testing.T, svc *Service, priv *ecdsa.PrivateKey, chainID int64, displayName string) LoginRequest {
	t.Helper()
	ctx := context.Background()
	addr := crypto.PubkeyToAddress(priv.PublicKey).Hex()
	nonce, exp, _, _, err := svc.IssueLoginNonce(ctx, chainID, addr)
	if err != nil {
		t.Fatalf("IssueLoginNonce: %v", err)
	}
	msg := CanonicalLoginMessage(nonce, chainID, addr, "localhost:8080", exp.UTC().Format(time.RFC3339))
	raw, err := crypto.Sign(PrefixedHashForTest(msg), priv)
	if err != nil {
		t.Fatalf("sign login: %v", err)
	}
	raw[64] += 27
	return LoginRequest{
		ChainID: chainID, Address: addr, Nonce: nonce, Expiry: exp,
		Signature: common.Bytes2Hex(raw), Domain: "localhost:8080", DisplayName: displayName,
	}
}

func TestService_Login_RegistersThenLogsInExistingWallet(t *testing.T) {
	svc, _ := newSvc(t)
	ctx := context.Background()
	priv := freshPriv(t)

	registered, created, err := svc.Login(ctx, signLoginRequest(t, svc, priv, 31337, "Wallet Student"))
	if err != nil || !created {
		t.Fatalf("register: user=%v created=%v err=%v", registered, created, err)
	}
	if registered.DisplayName != "Wallet Student" {
		t.Fatalf("display name=%q", registered.DisplayName)
	}

	existing, created, err := svc.Login(ctx, signLoginRequest(t, svc, priv, 31337, "Ignored Name"))
	if err != nil || created {
		t.Fatalf("existing login: user=%v created=%v err=%v", existing, created, err)
	}
	if existing.ID != registered.ID || existing.DisplayName != "Wallet Student" {
		t.Fatalf("existing wallet resolved to wrong user: %#v", existing)
	}
}

func TestService_Login_RequiresNicknameForNewWallet(t *testing.T) {
	svc, _ := newSvc(t)
	_, _, err := svc.Login(context.Background(), signLoginRequest(t, svc, freshPriv(t), 31337, "x"))
	if err == nil || !strings.Contains(err.Error(), "display name") {
		t.Fatalf("expected display name validation, got %v", err)
	}
}

// TestService_Bind_HappyPath 完整路径。
func TestService_Bind_HappyPath(t *testing.T) {
	svc, _ := newSvc(t)
	ctx := context.Background()
	uid, _, _, _ := makeBound(t, ctx, svc, 11155111)
	var got string
	if err := testPool(t).QueryRow(ctx, `SELECT address FROM wallets WHERE user_id=$1`, uid).Scan(&got); err != nil {
		t.Fatalf("query: %v", err)
	}
	if got == "" {
		t.Fatal("empty address")
	}
}

// TestService_Bind_RejectsDomainMismatch
func TestService_Bind_RejectsDomainMismatch(t *testing.T) {
	svc, _ := newSvc(t)
	ctx := context.Background()
	priv := freshPriv(t)
	uid := makeUser(t, ctx)
	addr := crypto.PubkeyToAddress(priv.PublicKey).Hex()
	nonce, exp, err := svc.IssueNonce(ctx, uid)
	if err != nil {
		t.Fatalf("IssueNonce: %v", err)
	}
	msg := CanonicalMessage(nonce, 11155111, strings.ToLower(addr), "evil.com", exp.Format(time.RFC3339))
	raw, _ := crypto.Sign(PrefixedHashForTest(msg), priv)
	raw[64] += 27
	err = svc.Bind(ctx, BindRequest{
		UserID: uid, ChainID: 11155111, Address: addr,
		Nonce: nonce, Expiry: exp, Signature: common.Bytes2Hex(raw), Domain: "evil.com",
	})
	if err == nil || !strings.Contains(err.Error(), "domain") {
		t.Fatalf("expected domain mismatch, got %v", err)
	}
}

// TestService_Bind_RejectsExpiredSignature
func TestService_Bind_RejectsExpiredSignature(t *testing.T) {
	svc, _ := newSvc(t)
	ctx := context.Background()
	priv := freshPriv(t)
	uid := makeUser(t, ctx)
	addr := crypto.PubkeyToAddress(priv.PublicKey).Hex()
	nonce, exp, _ := svc.IssueNonce(ctx, uid)
	msg := CanonicalMessage(nonce, 11155111, strings.ToLower(addr), "localhost:8080", exp.Format(time.RFC3339))
	raw, _ := crypto.Sign(PrefixedHashForTest(msg), priv)
	raw[64] += 27
	err := svc.Bind(ctx, BindRequest{
		UserID: uid, ChainID: 11155111, Address: addr,
		Nonce: nonce, Expiry: time.Now().Add(-time.Minute), Signature: common.Bytes2Hex(raw), Domain: "localhost:8080",
	})
	if err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expected expired, got %v", err)
	}
}

// TestService_Bind_RejectsBadAddress
func TestService_Bind_RejectsBadAddress(t *testing.T) {
	svc, _ := newSvc(t)
	ctx := context.Background()
	uid := makeUser(t, ctx)
	err := svc.Bind(ctx, BindRequest{
		UserID: uid, ChainID: 1, Address: "not-an-address",
		Nonce: "n", Expiry: time.Now().Add(time.Minute), Signature: strings.Repeat("ab", 65), Domain: "localhost:8080",
	})
	if err == nil || !strings.Contains(err.Error(), "bad address") {
		t.Fatalf("expected bad address, got %v", err)
	}
}

// TestService_Bind_RejectsSecondUserSameWallet
func TestService_Bind_RejectsSecondUserSameWallet(t *testing.T) {
	svc, _ := newSvc(t)
	ctx := context.Background()
	_, _, addr, priv := makeBound(t, ctx, svc, 11155111)
	other := makeUser(t, ctx)
	nonce, exp, _ := svc.IssueNonce(ctx, other)
	msg := CanonicalMessage(nonce, 11155111, strings.ToLower(addr), "localhost:8080", exp.Format(time.RFC3339))
	raw, _ := crypto.Sign(PrefixedHashForTest(msg), priv)
	raw[64] += 27
	err := svc.Bind(ctx, BindRequest{
		UserID: other, ChainID: 11155111, Address: addr,
		Nonce: nonce, Expiry: exp, Signature: common.Bytes2Hex(raw), Domain: "localhost:8080",
	})
	if err == nil || !strings.Contains(err.Error(), "already bound") {
		t.Fatalf("expected already-bound, got %v", err)
	}
}

// TestService_Bind_RejectsNonceReuse
func TestService_Bind_RejectsNonceReuse(t *testing.T) {
	svc, _ := newSvc(t)
	ctx := context.Background()
	uid := makeUser(t, ctx)
	priv := freshPriv(t)
	addr := crypto.PubkeyToAddress(priv.PublicKey).Hex()
	nonce, exp, _ := svc.IssueNonce(ctx, uid)
	msg := CanonicalMessage(nonce, 11155111, strings.ToLower(addr), "localhost:8080", exp.Format(time.RFC3339))
	raw, _ := crypto.Sign(PrefixedHashForTest(msg), priv)
	raw[64] += 27
	req := BindRequest{
		UserID: uid, ChainID: 11155111, Address: addr,
		Nonce: nonce, Expiry: exp, Signature: common.Bytes2Hex(raw), Domain: "localhost:8080",
	}
	if err := svc.Bind(ctx, req); err != nil {
		t.Fatalf("first bind: %v", err)
	}
	if err := svc.Bind(ctx, req); err == nil {
		t.Fatal("expected nonce reuse to fail")
	}
}

// TestService_Bind_RejectsBadSignature 签名不匹配。
func TestService_Bind_RejectsBadSignature(t *testing.T) {
	svc, _ := newSvc(t)
	ctx := context.Background()
	uid := makeUser(t, ctx)
	priv := freshPriv(t)
	addr := crypto.PubkeyToAddress(priv.PublicKey).Hex()
	nonce, exp, _ := svc.IssueNonce(ctx, uid)
	// 用别的私钥签
	wrongPriv := freshPriv(t)
	msg := CanonicalMessage(nonce, 11155111, strings.ToLower(addr), "localhost:8080", exp.Format(time.RFC3339))
	raw, _ := crypto.Sign(PrefixedHashForTest(msg), wrongPriv)
	raw[64] += 27
	err := svc.Bind(ctx, BindRequest{
		UserID: uid, ChainID: 11155111, Address: addr,
		Nonce: nonce, Expiry: exp, Signature: common.Bytes2Hex(raw), Domain: "localhost:8080",
	})
	if err == nil {
		t.Fatal("expected bad signature")
	}
}

// TestService_Unbind_RejectsLastWallet
func TestService_Unbind_RejectsLastWallet(t *testing.T) {
	svc, _ := newSvc(t)
	ctx := context.Background()
	uid, wid, _, _ := makeBound(t, ctx, svc, 11155111)
	err := svc.Unbind(ctx, uid, wid, "127.0.0.1", "ua")
	if err == nil || !strings.Contains(err.Error(), "last wallet") {
		t.Fatalf("expected last-wallet, got %v", err)
	}
}

// TestService_Unbind_RejectsNotOwner
func TestService_Unbind_RejectsNotOwner(t *testing.T) {
	svc, _ := newSvc(t)
	ctx := context.Background()
	_, wid, _, _ := makeBound(t, ctx, svc, 11155111)
	other := makeUser(t, ctx)
	err := svc.Unbind(ctx, other, wid, "127.0.0.1", "ua")
	if err == nil || !strings.Contains(err.Error(), "forbidden") {
		t.Fatalf("expected forbidden, got %v", err)
	}
}

// TestService_Unbind_RejectsUnknownWallet
func TestService_Unbind_RejectsUnknownWallet(t *testing.T) {
	svc, _ := newSvc(t)
	ctx := context.Background()
	uid := makeUser(t, ctx)
	if err := svc.Unbind(ctx, uid, uuid.New(), "127.0.0.1", "ua"); err == nil {
		t.Fatal("expected error for unknown wallet")
	}
}

// TestService_Unbind_HappyPath
func TestService_Unbind_HappyPath(t *testing.T) {
	svc, _ := newSvc(t)
	ctx := context.Background()
	uid := makeUser(t, ctx)
	// 同 user 绑两个地址
	privA := freshPriv(t)
	privB := freshPriv(t)
	addrA := crypto.PubkeyToAddress(privA.PublicKey).Hex()
	addrB := crypto.PubkeyToAddress(privB.PublicKey).Hex()
	if addrA == addrB {
		t.Fatal("addrA == addrB")
	}
	bindOne := func(priv *ecdsa.PrivateKey, addr string) {
		nonce, exp, _ := svc.IssueNonce(ctx, uid)
		msg := CanonicalMessage(nonce, 11155111, strings.ToLower(addr), "localhost:8080", exp.Format(time.RFC3339))
		raw, _ := crypto.Sign(PrefixedHashForTest(msg), priv)
		raw[64] += 27
		if err := svc.Bind(ctx, BindRequest{
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
