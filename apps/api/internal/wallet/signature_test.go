package wallet

import (
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// TestCanonicalMessage_Structure 保证模板字段顺序固定（影响客户端签名一致性）。
func TestCanonicalMessage_Structure(t *testing.T) {
	msg := CanonicalMessage("nonce-x", 11155111, "0xAbC...", "localhost:8080", "2030-01-01T00:00:00Z")
	if !strings.Contains(msg, "x-web3 bind wallet") {
		t.Errorf("missing header: %q", msg)
	}
	if !strings.Contains(msg, "nonce: nonce-x") {
		t.Errorf("missing nonce line: %q", msg)
	}
	if !strings.Contains(msg, "chainId: 11155111") {
		t.Errorf("missing chainId: %q", msg)
	}
	// address 应被规范化小写
	if !strings.Contains(msg, "address: 0xabc...") {
		t.Errorf("expected lowercase address in canonical: %q", msg)
	}
}

// TestVerifyEIP191_RecoversAddress 用确定性签名验证 ecrecover。
func TestVerifyEIP191_RecoversAddress(t *testing.T) {
	priv, err := crypto.HexToECDSA("4af1bceebf7f3634ec3cff8a2c38e51178d5d4ce585c52d6043cfe7f3b25d4e1")
	if err != nil {
		t.Fatalf("HexToECDSA: %v", err)
	}
	addr := crypto.PubkeyToAddress(priv.PublicKey).Hex()
	msg := CanonicalMessage("n1", 11155111, addr, "localhost:8080", "2030-01-01T00:00:00Z")

	// 用 go-ethereum 的 SignHash 走 personal_sign 路径
	prefixed := prefixedHash(msg)
	sig, err := crypto.Sign(prefixed, priv)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	// 转成 RSV + v ∈ {27, 28}
	sig[64] += 27

	if err := VerifyEIP191(msg, common.Bytes2Hex(sig), addr); err != nil {
		t.Fatalf("VerifyEIP191: %v", err)
	}
}

// TestVerifyEIP191_BadAddress 错误地址应被拒绝。
func TestVerifyEIP191_BadAddress(t *testing.T) {
	priv, _ := crypto.HexToECDSA("4af1bceebf7f3634ec3cff8a2c38e51178d5d4ce585c52d6043cfe7f3b25d4e1")
	msg := "hello"
	prefixed := prefixedHash(msg)
	sig, _ := crypto.Sign(prefixed, priv)
	sig[64] += 27

	wrong := "0x0000000000000000000000000000000000000001"
	if err := VerifyEIP191(msg, common.Bytes2Hex(sig), wrong); err == nil {
		t.Fatal("expected error for mismatched claimed address")
	}
}

// TestVerifyDomain 域名校验。
func TestVerifyDomain(t *testing.T) {
	if err := VerifyDomain("example.com", "example.com"); err != nil {
		t.Errorf("same domain should pass: %v", err)
	}
	if err := VerifyDomain("Example.COM", "example.com"); err != nil {
		t.Errorf("case-insensitive: %v", err)
	}
	if err := VerifyDomain("evil.com", "example.com"); err == nil {
		t.Error("mismatch should fail")
	}
	if err := VerifyDomain("", "example.com"); err == nil {
		t.Error("empty should fail")
	}
}
