// Package chain 提供链事件解码 + receipt 全字段校验。
//
// worker 用真实 RPC client（go-ethereum ethclient）拉 tx receipt 或订阅 log，
// 通过 Decode(LogRecordLike) 把 raw log 翻译成 CoursePurchased 结构；
// ValidateReceipt 把解码结果与 DB intent 全字段比对，任何一项不符返回
// ErrMismatch，调用方写入 orders.failure_code = RECEIPT_MISMATCH。
//
// 设计取舍：
//   - decode / validate 拆成两个独立函数，单测可单独覆盖；
//   - event topic0 用 crypto.Keccak256 在 init() 计算（SSOT 来自
//     packages/shared/src/events/courseMarket.ts 的 COURSE_MARKET_EVENT_SIGNATURES，
//     两端对齐由合约端 test/CourseMarket.t.sol 锁定）。
package chain

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
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

// Event signature strings — 与 packages/shared/src/events/courseMarket.ts 锁定的 SSOT 对齐。
// 任何事件签名变更需同步：
//   - Solidity 合约（自动，编译器算 topic0）
//   - packages/shared/src/events/courseMarket.ts（前端 log 解析与单测）
//   - 这里（worker decoder；courseMarket.t.sol 锁定 selector 兼容）
const (
	sigCoursePurchased  = "CoursePurchased(bytes32,address,address,uint256,bytes16,uint256)"
	sigCourseConfigured = "CourseConfigured(bytes32,address,uint256,uint256)"
)

// 事件签名 topic0；用 keccak256(event_signature) 在 init() 算。
//
// 历史背景：早期版本用字面量字符串 "CoursePurchasedV1HashPlaceholder" 占位，
// 导致 chain_events.event_signature 写入的 topic 与真链上 topic 不一致。
// 现阶段已经用真 keccak256 计算，与合约端 compiler 输出对齐。
var (
	CoursePurchasedTopic  common.Hash
	CourseConfiguredTopic common.Hash
)

// coursePurchasedUnindexedABI 是 CoursePurchased 事件去掉 indexed 字段后的
// 参数 schema（token / amount / intentId / priceVersion），用于 log.Data 解码。
//
// 直接传 4 个独立 Arguments 而不是嵌套 tuple：解包后是 []interface{}，比
// tuple 包装少一层 indirection。
var coursePurchasedUnindexedABI abi.Arguments

func init() {
	CoursePurchasedTopic = crypto.Keccak256Hash([]byte(sigCoursePurchased))
	CourseConfiguredTopic = crypto.Keccak256Hash([]byte(sigCourseConfigured))

	// ABI 解码 log.Data 不需要 indexed 标记 — schema 顺序对应 data 里 token/amount/intentId/priceVersion。
	coursePurchasedUnindexedABI = abi.Arguments{
		{Name: "token", Type: mustNewType("address")},
		{Name: "amount", Type: mustNewType("uint256")},
		{Name: "intentId", Type: mustNewType("bytes16")},
		{Name: "priceVersion", Type: mustNewType("uint256")},
	}
}

// Sentinel errors。
var (
	// ErrMismatch 全字段校验失败。
	ErrMismatch = errors.New("chain: receipt does not match intent")
	// ErrLogRemoved 表示 log 被 reorg 移除（Removed=true）；调用方应走 reorged 路径。
	ErrLogRemoved = errors.New("chain: log removed by reorg")
	// ErrTopicMismatch 表示 topic0 不是 CoursePurchased；indexer 调用方应忽略。
	ErrTopicMismatch = errors.New("chain: topic0 does not match CoursePurchased")
	// ErrTooFewTopics 表示 topics 长度不足以覆盖 indexed 字段（courseKey + buyer）。
	ErrTooFewTopics = errors.New("chain: topics missing indexed fields")
	// ErrDecodeData 表示 abi 解码 Data 失败（事件参数类型不匹配）。
	ErrDecodeData = errors.New("chain: failed to decode event data")
)

// Intent 描述 expected 字段集合（来自 purchase_intents + wallets 表）。
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
//
// 重要：want 必须从 DB intent + wallet 派生，不能从事件字段拷出，否则形同自比。
// 调用方（confirmer.go）有责任保证 Intent 来自数据库而不是 event payload。
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

// LogRecordLike 是 indexer.LogRecord 的最小依赖接口，避免 chain 包反向依赖 indexer。
//
// indexer.LogRecord 通过 TopicsList / DataBytes / IsRemoved 实现这个接口
// （见 apps/worker/internal/indexer/client.go）。Go 不允许 struct field 与
// 同名 method 共存，所以接口方法名避开 Topics / Data / Removed。
type LogRecordLike interface {
	TopicsList() []common.Hash
	DataBytes() []byte
	IsRemoved() bool
}

// Decode 把 raw log（来自 eth_getLogs 或 WS 订阅）解码为 CoursePurchased。
//
// 约定 log.Removed=true → 返回 ErrLogRemoved（reorged 路径应丢弃，不入库）。
// topic0 不匹配 → 返回 ErrTopicMismatch（indexer 调用方应忽略；不计入 Apply）。
// topics 长度 < 3 → 返回 ErrTooFewTopics。
//
// indexed 字段布局（ABI 规则）：
//   - topics[0] = event sig（已外部匹配）
//   - topics[1] = courseKey（bytes32 直接 = topics[1][0:32]）
//   - topics[2] = buyer（address 右对齐到 32 字节 = topics[2][12:32]）
//
// 非 indexed 字段在 log.Data 里按 ABI 编码：token / amount / intentId / priceVersion。
func Decode(rec LogRecordLike) (*CoursePurchased, error) {
	if rec.IsRemoved() {
		return nil, ErrLogRemoved
	}
	topics := rec.TopicsList()
	data := rec.DataBytes()
	if len(topics) < 3 {
		return nil, ErrTooFewTopics
	}
	if topics[0] != CoursePurchasedTopic {
		return nil, ErrTopicMismatch
	}

	var out CoursePurchased
	copy(out.CourseKey[:], topics[1][:])
	copy(out.Buyer[:], topics[2][12:32]) // address 在 32-byte word 里右对齐

	unindexed, err := coursePurchasedUnindexedABI.Unpack(data)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDecodeData, err)
	}
	if len(unindexed) != 4 {
		return nil, fmt.Errorf("%w: expected 4 unindexed fields, got %d", ErrDecodeData, len(unindexed))
	}

	addr, ok := unindexed[0].(common.Address)
	if !ok {
		return nil, fmt.Errorf("%w: token type %T", ErrDecodeData, unindexed[0])
	}
	out.Token = addr

	amount, ok := unindexed[1].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("%w: amount type %T", ErrDecodeData, unindexed[1])
	}
	amountBytes, err := BigIntToU256(amount)
	if err != nil {
		return nil, fmt.Errorf("%w: amount: %v", ErrDecodeData, err)
	}
	out.Amount = amountBytes

	intent, ok := unindexed[2].([16]byte)
	if !ok {
		return nil, fmt.Errorf("%w: intentId type %T", ErrDecodeData, unindexed[2])
	}
	out.IntentID = intent

	pv, ok := unindexed[3].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("%w: priceVersion type %T", ErrDecodeData, unindexed[3])
	}
	pvBytes, err := BigIntToU256(pv)
	if err != nil {
		return nil, fmt.Errorf("%w: priceVersion: %v", ErrDecodeData, err)
	}
	out.PriceVersion = pvBytes

	return &out, nil
}

// mustNewType 解析 ABI 类型；失败 panic（启动期即崩，不留运行时错误）。
func mustNewType(s string) abi.Type {
	t, err := abi.NewType(s, "", nil)
	if err != nil {
		panic(fmt.Sprintf("chain: bad ABI type %q: %v", s, err))
	}
	return t
}