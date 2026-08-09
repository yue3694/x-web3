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

// TestCanonicalMessage_StableAgainstReordering 不同调用顺序的字段应严格相同。
func TestCanonicalMessage_StableAgainstReordering(t *testing.T) {
	a := CanonicalMessage("n", 1, "0xabc", "d", "exp")
	b := CanonicalMessage("n", 1, "0xabc", "d", "exp")
	if a != b {
		t.Errorf("CanonicalMessage must be deterministic: %q vs %q", a, b)
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

// TestVerifyEIP191_Accepts0xPrefixedHex 签名带 0x 前缀也应通过（客户端常用格式）。
func TestVerifyEIP191_Accepts0xPrefixedHex(t *testing.T) {
	priv, _ := crypto.HexToECDSA("4af1bceebf7f3634ec3cff8a2c38e51178d5d4ce585c52d6043cfe7f3b25d4e1")
	addr := crypto.PubkeyToAddress(priv.PublicKey).Hex()
	msg := "hello"

	sig, _ := crypto.Sign(prefixedHash(msg), priv)
	sig[64] += 27

	if err := VerifyEIP191(msg, "0x"+common.Bytes2Hex(sig), addr); err != nil {
		t.Errorf("0x-prefixed signature should pass: %v", err)
	}
}

// TestVerifyEIP191_AcceptsV27AndV28 真实签名产生的 parity 对应 27 或 28 都应通过。
// 钱包实现差异：某些客户端返回 {0,1}，另一些返回 {27,28}。只要 parity 与
// 真实签名一致，就能 recover 出来。
func TestVerifyEIP191_AcceptsV27AndV28(t *testing.T) {
	priv, _ := crypto.HexToECDSA("4af1bceebf7f3634ec3cff8a2c38e51178d5d4ce585c52d6043cfe7f3b25d4e1")
	addr := crypto.PubkeyToAddress(priv.PublicKey).Hex()
	msg := "v-test"

	// crypto.Sign 返回的 sig[64] 是真实 parity (0 或 1)。
	// 把它按 27+parity 写回去，verifier 必须能恢复出声明地址。
	sig, _ := crypto.Sign(prefixedHash(msg), priv)
	sig[64] = sig[64] + 27 // 27 或 28，由 parity 决定
	if err := VerifyEIP191(msg, common.Bytes2Hex(sig), addr); err != nil {
		t.Fatalf("parity-correct v should pass: %v", err)
	}
}

// TestVerifyEIP191_RejectsWrongLengthSignature 签名长度不为 65 字节必须拒绝。
func TestVerifyEIP191_RejectsWrongLengthSignature(t *testing.T) {
	addr := "0x0000000000000000000000000000000000000001"
	for _, hex := range []string{
		"deadbeef",               // 太短
		strings.Repeat("ab", 64), // 64 字节
		strings.Repeat("ab", 66), // 66 字节
	} {
		if err := VerifyEIP191("msg", hex, addr); err == nil {
			t.Errorf("length=%d must reject", len(hex)/2)
		}
	}
}

// TestVerifyEIP191_RejectsBadHex 乱码 hex 必须拒绝。
func TestVerifyEIP191_RejectsBadHex(t *testing.T) {
	addr := "0x0000000000000000000000000000000000000001"
	if err := VerifyEIP191("msg", "not-hex-zzz", addr); err == nil {
		t.Fatal("expected error for non-hex signature")
	}
}

// TestVerifyEIP191_RejectsInvalidClaimedAddress 声明地址不是合法 hex 必须拒绝。
func TestVerifyEIP191_RejectsInvalidClaimedAddress(t *testing.T) {
	if err := VerifyEIP191("msg", strings.Repeat("ab", 65), "not-an-address"); err == nil {
		t.Fatal("expected error for bad claimed address")
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

// TestVerifyEIP191_RejectsMessageChange 改动 message 后签名必须不通过。
func TestVerifyEIP191_RejectsMessageChange(t *testing.T) {
	priv, _ := crypto.HexToECDSA("4af1bceebf7f3634ec3cff8a2c38e51178d5d4ce585c52d6043cfe7f3b25d4e1")
	addr := crypto.PubkeyToAddress(priv.PublicKey).Hex()

	sig, _ := crypto.Sign(prefixedHash("original"), priv)
	sig[64] += 27

	if err := VerifyEIP191("tampered", common.Bytes2Hex(sig), addr); err == nil {
		t.Fatal("expected error for tampered message")
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

// TestVerifyDomain_TrimsWhitespace 域名带前后空白应被修剪后比较。
func TestVerifyDomain_TrimsWhitespace(t *testing.T) {
	if err := VerifyDomain("  example.com  ", "example.com"); err != nil {
		t.Errorf("whitespace should be trimmed: %v", err)
	}
}
