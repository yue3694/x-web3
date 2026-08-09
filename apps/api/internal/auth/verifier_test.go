package auth_test

import (
	"context"
	"testing"
	"time"

	"github.com/x-web3/api/internal/auth"
	"go.uber.org/zap"
)

// TestDevStubVerifier_Verify 验证 dev stub 在固定 subject 下放行任意 token。
func TestDevStubVerifier_Verify(t *testing.T) {
	logger := zap.NewNop()
	v, err := auth.NewVerifier(context.Background(), auth.Config{
		DevStub:    true,
		DevSubject: "did:privy:stub-123",
	}, logger)
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	c, err := v.Verify(context.Background(), "anything")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if c.Subject != "did:privy:stub-123" {
		t.Errorf("subject = %q, want did:privy:stub-123", c.Subject)
	}
	if time.Until(c.Expires) <= 0 {
		t.Errorf("expected expires in the future, got %v", c.Expires)
	}
}

// TestDevStub_MissingSubject 拒绝空 subject。
func TestDevStub_MissingSubject(t *testing.T) {
	logger := zap.NewNop()
	_, err := auth.NewVerifier(context.Background(), auth.Config{
		DevStub: true,
	}, logger)
	if err == nil {
		t.Fatal("expected error when DevSubject is empty")
	}
}

// TestJWKSVerifier_MissingConfig 拒绝缺少 Privy 配置（除非 dev stub）。
func TestJWKSVerifier_MissingConfig(t *testing.T) {
	logger := zap.NewNop()
	_, err := auth.NewVerifier(context.Background(), auth.Config{
		// 故意留空
	}, logger)
	if err == nil {
		t.Fatal("expected error when Privy config missing")
	}
}
