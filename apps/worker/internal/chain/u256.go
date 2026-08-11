package chain

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"math/big"

	"github.com/google/uuid"
)

// ErrBigIntTooLarge 当 *big.Int 超过 256-bit（32 字节）时返回。
// 用于 u256 编码前的安全检查；abi-encoded uint256 不可溢出。
var ErrBigIntTooLarge = errors.New("chain: bigint exceeds 256 bits")

// BigIntToU256 把 *big.Int 编码为 32 字节 big-endian（ABI uint256 表示）。
//
// 负数 / 超过 2^256-1 报错；nil 视为 0。用于把 DB numeric / intent_id 派生字段
// 填到 chain.Intent / CoursePurchased 的定长数组。
func BigIntToU256(v *big.Int) ([32]byte, error) {
	var out [32]byte
	if v == nil {
		return out, nil
	}
	if v.Sign() < 0 {
		return out, errors.New("chain: negative bigint not allowed in u256")
	}
	// 256-bit 上限 = (1 << 256) - 1。
	if v.BitLen() > 256 {
		return out, ErrBigIntTooLarge
	}
	b := v.Bytes()
	// big.Int.Bytes 返回无符号 big-endian，长度 ≤ 32。
	copy(out[32-len(b):], b)
	return out, nil
}

// U256FromUint64 helper：与 workerorder.U256 等价；这里保留独立实现以避免
// internal/chain 反向依赖 internal/order。
func U256FromUint64(v uint64) [32]byte {
	var b [32]byte
	binary.BigEndian.PutUint64(b[24:], v)
	return b
}

// Bytes16FromUUID 把 UUID 的全部 128 位转成 bytes16 大端数组。
//
// UUID v4 = 128 bits；与合约 `bytes16 intentId` 字段一致（高 128 位 = 全部）。
// 与 apps/web/src/features/checkout/derive.ts::uuidToBytes16 对齐。
func Bytes16FromUUID(id uuid.UUID) [16]byte {
	var out [16]byte
	copy(out[:], id[:])
	return out
}

// Bytes16ToUUID 反向操作；主要用于测试与反查（worker 拿 event.bytes16 →
// 查 purchase_intents.id 的 UUID）。
func Bytes16ToUUID(b [16]byte) uuid.UUID {
	var u uuid.UUID
	copy(u[:], b[:])
	return u
}

// HexToBytes32 把 64-char hex 字符串解码为 [32]byte（DB intent.course_key 用 hex 存）。
// 不接受 0x 前缀、不接受短/长 hex —— 严格 64 字符（32 字节），避免 silent zero padding。
func HexToBytes32(s string) ([32]byte, error) {
	var out [32]byte
	if len(s) != 64 {
		return out, errors.New("chain: course_key hex must be 64 chars (32 bytes)")
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return out, err
	}
	copy(out[:], b)
	return out, nil
}