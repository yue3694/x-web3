package wallet

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// VerifyEIP191 验证 personal_sign 风格签名：
//
//	sign(keccak256("\x19Ethereum Signed Message:\n" + len(message) + message))
//
// 返回恢复地址；必须等于声明的 address（不区分大小写）。
func VerifyEIP191(message string, signatureHex string, claimed string) error {
	if !common.IsHexAddress(claimed) {
		return errors.New("wallet: bad claimed address")
	}
	claimed = strings.ToLower(common.HexToAddress(claimed).Hex())
	sig, err := hex.DecodeString(strings.TrimPrefix(signatureHex, "0x"))
	if err != nil {
		return fmt.Errorf("wallet: bad signature hex: %w", err)
	}
	if len(sig) != 65 {
		return fmt.Errorf("wallet: signature length %d != 65", len(sig))
	}
	// 调整 v：crypto.SigToPub 期望 v ∈ {0, 1}
	if sig[64] >= 27 {
		sig[64] -= 27
	}
	digest := prefixedHash(message)
	pub, err := crypto.SigToPub(digest, sig)
	if err != nil {
		return fmt.Errorf("wallet: recover: %w", err)
	}
	recovered := strings.ToLower(crypto.PubkeyToAddress(*pub).Hex())
	if recovered != claimed {
		return fmt.Errorf("wallet: recovered %s != claimed %s", recovered, claimed)
	}
	return nil
}

// prefixedHash 实现 EIP-191 personal_sign 的摘要。
func prefixedHash(message string) []byte {
	prefix := fmt.Sprintf("\x19Ethereum Signed Message:\n%d%s", len(message), message)
	return crypto.Keccak256([]byte(prefix))
}

// PrefixedHashForTest 暴露给测试包用于本地签名生成；生产代码请勿调用。
func PrefixedHashForTest(message string) []byte { return prefixedHash(message) }

// CanonicalMessage 构造绑定场景下要签名的字符串。客户端必须用同样模板签名。
// 模板与 F01 design 文档 "钱包绑定 nonce 签名" 一节一致：
//
//	x-web3 bind wallet
//	nonce: <nonce>
//	chainId: <chainId>
//	address: <address>
//	domain: <domain>
//	expiry: <expiry RFC3339>
func CanonicalMessage(nonce string, chainID int64, address, domain, expiry string) string {
	return strings.Join([]string{
		"x-web3 bind wallet",
		"nonce: " + nonce,
		fmt.Sprintf("chainId: %d", chainID),
		"address: " + strings.ToLower(address),
		"domain: " + domain,
		"expiry: " + expiry,
	}, "\n")
}
