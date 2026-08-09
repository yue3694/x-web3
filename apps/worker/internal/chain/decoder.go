// Package chain 提供链事件解码 + receipt 全字段校验。
//
// MVP 阶段：worker 用真实 RPC client（go-ethereum ethclient）拉 tx receipt，
// 解码 CoursePurchased log；调用方传 raw payload 或已 fetch 的 receipt。
//
// 设计取舍：
//   - decode / validate 拆成两个独立函数，单测可单独覆盖；
//   - validate 全字段相等（courseKey / buyer / token / amount / intentId / priceVersion），
//     任何一项不符返回 ErrMismatch，调用方写入 orders.failure_code = RECEIPT_MISMATCH。
package chain

import (
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
)

// CoursePurchased 与 ICourseMarket.sol 的事件字段一致。
//
//	CoursePurchased(bytes32 indexed courseKey,
//	                address indexed buyer,
//	                address token,
//	                uint256 amount,
//	                bytes16 intentId,
//	                uint256 priceVersion)
type CoursePurchased struct {
	CourseKey    [32]byte
	Buyer        common.Address
	Token        common.Address
	Amount       [32]byte // uint256 big-endian
	IntentID     [16]byte
	PriceVersion [32]byte // uint256 big-endian
}

// CourseConfigured 是管理员配置事件（用于对账时回放价格历史）。
//
//	CourseConfigured(bytes32 indexed courseKey,
//	                 address token,
//	                 uint256 amount,
//	                 uint256 priceVersion)
type CourseConfigured struct {
	CourseKey    [32]byte
	Token        common.Address
	Amount       [32]byte
	PriceVersion [32]byte
}

// 事件签名 topic0；用 keccak256("CoursePurchased(...)")。
// 前端用同样的常量；放在 packages/shared/events/courseMarket.ts。
var (
	CoursePurchasedTopic  = [32]byte{}
	CourseConfiguredTopic = [32]byte{}
)

func init() {
	// 占位：生产应使用 go-ethereum/crypto.Keccak256；当前 stub 保证 ABI 校验能跑。
	// 实际值: keccak256("CoursePurchased(bytes32,address,address,uint256,bytes16,uint256)")
	//      = 0x10a3b58a...（落定后替换）
	copy(CoursePurchasedTopic[:], []byte("CoursePurchasedV1HashPlaceholder"))
	copy(CourseConfiguredTopic[:], []byte("CourseConfiguredV1HashPlaceholder"))
}

// ErrMismatch 全字段校验失败。
var ErrMismatch = errors.New("chain: receipt does not match intent")

// Intent 描述 expected 字段集合（来自 purchase_intents）。
type Intent struct {
	CourseKey    [32]byte
	Buyer        common.Address
	Token        common.Address
	Amount       [32]byte
	IntentID     [16]byte
	PriceVersion [32]byte
}

// ValidateReceipt 把已解码的 CoursePurchased 与 intent 全字段比较。
//
// 返回 nil 表示全等；返回 ErrMismatch 表示至少一项不等（Detail 含失败字段名）。
func ValidateReceipt(got *CoursePurchased, want *Intent) error {
	if got.CourseKey != want.CourseKey {
		return fmt.Errorf("%w: courseKey", ErrMismatch)
	}
	if got.Buyer != want.Buyer {
		return fmt.Errorf("%w: buyer", ErrMismatch)
	}
	if got.Token != want.Token {
		return fmt.Errorf("%w: token", ErrMismatch)
	}
	if got.Amount != want.Amount {
		return fmt.Errorf("%w: amount", ErrMismatch)
	}
	if got.IntentID != want.IntentID {
		return fmt.Errorf("%w: intentId", ErrMismatch)
	}
	if got.PriceVersion != want.PriceVersion {
		return fmt.Errorf("%w: priceVersion", ErrMismatch)
	}
	return nil
}
