package wallet

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

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

const fiveMinutes = 5 * time.Minute
