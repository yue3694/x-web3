package auth_test

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/x-web3/api/internal/auth"
)

const testSessionSecret = "0123456789abcdef0123456789abcdef"

// newTestStore 启动一个内存 redis + 1 小时 TTL 的 SessionStore。
func newTestStore(t *testing.T) (*auth.SessionStore, *miniredis.Miniredis, *redis.Client) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		t.Fatalf("redis ping: %v", err)
	}
	return auth.NewSessionStore(rdb, []byte(testSessionSecret), time.Hour), mr, rdb
}

func TestSessionStore_IssueAndRead(t *testing.T) {
	store, _, _ := newTestStore(t)
	ctx := context.Background()

	sid, data, err := store.Issue(ctx, "did:privy:abc", "ua-fp")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if sid == "" {
		t.Fatal("expected sid")
	}
	if data.Subject != "did:privy:abc" {
		t.Errorf("subject = %q", data.Subject)
	}
	if data.Fingerprint != "ua-fp" {
		t.Errorf("fingerprint = %q", data.Fingerprint)
	}
	if time.Until(data.ExpiresAt) <= 0 {
		t.Errorf("expected future expiry, got %v", data.ExpiresAt)
	}

	got, err := store.Read(ctx, sid)
	if err != nil || got == nil {
		t.Fatalf("Read after issue: %v %v", err, got)
	}
	if got.Subject != "did:privy:abc" {
		t.Errorf("read subject = %q", got.Subject)
	}
}

func TestSessionStore_RefreshRotatesSIDAndRevokesOld(t *testing.T) {
	store, _, _ := newTestStore(t)
	ctx := context.Background()

	sid, _, err := store.Issue(ctx, "did:privy:rotate", "ua")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	newSID, data, err := store.Refresh(ctx, sid, "ua-new")
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if newSID == "" || newSID == sid {
		t.Fatalf("expected rotated sid, got new=%q old=%q", newSID, sid)
	}
	if data.Subject != "did:privy:rotate" {
		t.Errorf("subject lost on refresh: %q", data.Subject)
	}
	if data.Fingerprint != "ua-new" {
		t.Errorf("fingerprint not updated: %q", data.Fingerprint)
	}

	// 旧 sid 必须被撤销
	if got, err := store.Read(ctx, sid); err != nil || got != nil {
		t.Fatalf("old sid must be revoked, got=%v err=%v", got, err)
	}
	// 新 sid 必须可读
	if got, err := store.Read(ctx, newSID); err != nil || got == nil {
		t.Fatalf("new sid must be readable, got=%v err=%v", got, err)
	}
}

func TestSessionStore_RefreshRejectsUnknownSID(t *testing.T) {
	store, _, _ := newTestStore(t)
	if _, _, err := store.Refresh(context.Background(), "deadbeef", ""); err == nil {
		t.Fatal("expected error refreshing unknown sid")
	}
}

func TestSessionStore_DestroyIdempotent(t *testing.T) {
	store, _, _ := newTestStore(t)
	ctx := context.Background()
	if err := store.Destroy(ctx, "no-such-sid"); err != nil {
		t.Fatalf("Destroy on missing sid should be no-op, got %v", err)
	}
	sid, _, err := store.Issue(ctx, "did:privy:logout", "")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if err := store.Destroy(ctx, sid); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if got, _ := store.Read(ctx, sid); got != nil {
		t.Errorf("expected nil after destroy, got %+v", got)
	}
}

func TestSessionStore_TamperedSignatureRejected(t *testing.T) {
	store, mr, rdb := newTestStore(t)
	ctx := context.Background()
	sid, _, err := store.Issue(ctx, "did:privy:tamper", "")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	// 改坏 redis 里的最后 4 字节，模拟签名被破坏
	raw, _ := rdb.Get(ctx, "session:"+sid).Result()
	if len(raw) < 8 {
		t.Fatalf("unexpected session payload: %q", raw)
	}
	bad := raw[:len(raw)-4] + "AAAA"
	mr.Set("session:"+sid, bad)

	if _, err := store.Read(ctx, sid); err == nil {
		t.Fatal("expected error on tampered session")
	}
}
