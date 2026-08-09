package wallet

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

const fiveMinutes = 5 * time.Minute

// TestNonceIsBoundAndConsumedOnce 是最早一批用例：正向路径 + 跨用户 + 重用。
func TestNonceIsBoundAndConsumedOnce(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	store := NewNonceStore(client, fiveMinutes)
	ctx := context.Background()

	nonce, expiresAt, err := store.Issue(ctx, "user-a")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if time.Until(expiresAt) <= 0 {
		t.Fatal("expected a future expiry")
	}
	if err := store.Consume(ctx, nonce, "user-b"); err == nil {
		t.Fatal("expected another user to be rejected")
	}
	if err := store.Consume(ctx, nonce, "user-a"); err != nil {
		t.Fatalf("Consume: %v", err)
	}
	if err := store.Consume(ctx, nonce, "user-a"); err == nil {
		t.Fatal("expected nonce reuse to be rejected")
	}
}

// TestNonceStore_IssueRejectsEmptyUserID 边界：空 userID 不发 nonce。
func TestNonceStore_IssueRejectsEmptyUserID(t *testing.T) {
	srv := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: srv.Addr()})
	store := NewNonceStore(rdb, time.Minute)
	if _, _, err := store.Issue(context.Background(), ""); err == nil {
		t.Fatal("expected error for empty user id")
	}
}

// TestNonceStore_ConsumeRejectsEmptyArgs 边界：空 nonce / userID 必须报错。
func TestNonceStore_ConsumeRejectsEmptyArgs(t *testing.T) {
	srv := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: srv.Addr()})
	store := NewNonceStore(rdb, time.Minute)
	ctx := context.Background()

	if err := store.Consume(ctx, "", "user-x"); err == nil {
		t.Error("empty nonce should fail")
	}
	if err := store.Consume(ctx, "n", ""); err == nil {
		t.Error("empty userID should fail")
	}
}

// TestNonceStore_ConsumeUnknownNonce 已过期或不存在。
func TestNonceStore_ConsumeUnknownNonce(t *testing.T) {
	srv := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: srv.Addr()})
	store := NewNonceStore(rdb, time.Minute)
	if err := store.Consume(context.Background(), "never-issued", "user-a"); err == nil {
		t.Fatal("unknown nonce must be rejected")
	}
}

// TestNonceStore_ConsumeAfterTTL 过期后 Consume 必须失败。
func TestNonceStore_ConsumeAfterTTL(t *testing.T) {
	srv := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: srv.Addr()})
	store := NewNonceStore(rdb, 100*time.Millisecond)
	ctx := context.Background()
	nonce, _, err := store.Issue(ctx, "user-ttl")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	srv.FastForward(200 * time.Millisecond) // 触发 key 过期
	if err := store.Consume(ctx, nonce, "user-ttl"); err == nil {
		t.Fatal("expired nonce must be rejected")
	}
}

// TestNonceStore_KeyFormat key 必须稳定 `wallet:nonce:<value>`，客户端审计有依赖。
func TestNonceStore_KeyFormat(t *testing.T) {
	srv := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: srv.Addr()})
	store := NewNonceStore(rdb, time.Minute)
	want := "wallet:nonce:abc"
	if got := store.key("abc"); got != want {
		t.Errorf("key = %q, want %q", got, want)
	}
}
