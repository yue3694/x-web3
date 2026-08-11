package chain_test

import (
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/google/uuid"

	"github.com/x-web3/worker/internal/chain"
)

// fakeLog 适配 chain.LogRecordLike；测试无需引入 indexer 包。
type fakeLog struct {
	topics  []common.Hash
	data    []byte
	removed bool
}

func (f *fakeLog) TopicsList() []common.Hash { return f.topics }
func (f *fakeLog) DataBytes() []byte         { return f.data }
func (f *fakeLog) IsRemoved() bool           { return f.removed }

// buildCoursePurchasedLog 用合约端 ABI 规则构造一条合法 log，测试 Decode 是否能解。
//
// 入参任意：courseKey（32 字节）、buyer 地址、token 地址、amount big.Int、intentUUID、priceVersion。
// 返回 fakeLog，topics[0] = 真 CoursePurchasedTopic。
func buildCoursePurchasedLog(
	t *testing.T,
	courseKey [32]byte,
	buyer common.Address,
	token common.Address,
	amount *big.Int,
	intent uuid.UUID,
	priceVersion *big.Int,
) *fakeLog {
	t.Helper()

	topics := []common.Hash{
		chain.CoursePurchasedTopic,
		common.Hash(courseKey),
		common.BytesToHash(buyer.Bytes()), // address 在 32 字节 word 里右对齐
	}

	args := abi.Arguments{
		{Type: mustType("address")},
		{Type: mustType("uint256")},
		{Type: mustType("bytes16")},
		{Type: mustType("uint256")},
	}
	var intentBytes [16]byte
	copy(intentBytes[:], intent[:])
	data, err := args.Pack(token, amount, intentBytes, priceVersion)
	if err != nil {
		t.Fatalf("pack data: %v", err)
	}
	return &fakeLog{topics: topics, data: data}
}

func mustType(s string) abi.Type {
	t, err := abi.NewType(s, "", nil)
	if err != nil {
		panic(err)
	}
	return t
}

// TestCoursePurchasedTopicMatchesKeccak256 锁定 topic0 SSOT：必须等于 keccak256("CoursePurchased(bytes32,address,address,uint256,bytes16,uint256)").
//
// 历史教训：曾用字面量字符串占位导致与链上 topic 不一致；这里直接 hash 一遍。
func TestCoursePurchasedTopicMatchesKeccak256(t *testing.T) {
	want := crypto.Keccak256Hash([]byte("CoursePurchased(bytes32,address,address,uint256,bytes16,uint256)"))
	if chain.CoursePurchasedTopic != want {
		t.Fatalf("CoursePurchasedTopic drift:\n  got  %x\n  want %x", chain.CoursePurchasedTopic, want)
	}
}

func TestCourseConfiguredTopicMatchesKeccak256(t *testing.T) {
	want := crypto.Keccak256Hash([]byte("CourseConfigured(bytes32,address,uint256,uint256)"))
	if chain.CourseConfiguredTopic != want {
		t.Fatalf("CourseConfiguredTopic drift:\n  got  %x\n  want %x", chain.CourseConfiguredTopic, want)
	}
}

func TestDecode_Happy(t *testing.T) {
	var courseKey [32]byte
	copy(courseKey[:], []byte("course-key-fixture-00000000-aaaa"))
	buyer := common.HexToAddress("0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266")
	token := common.HexToAddress("0x70997970C51812dc3A010C7d01b50e0d17dc79C8")
	amount := big.NewInt(1_000_000)
	intent := uuid.MustParse("01020304-0506-0708-0910-111213141516")
	priceVersion := big.NewInt(42)

	log := buildCoursePurchasedLog(t, courseKey, buyer, token, amount, intent, priceVersion)

	got, err := chain.Decode(log)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.CourseKey != courseKey {
		t.Errorf("CourseKey mismatch: got %x want %x", got.CourseKey, courseKey)
	}
	if got.Buyer != buyer {
		t.Errorf("Buyer mismatch: got %s want %s", got.Buyer.Hex(), buyer.Hex())
	}
	if got.Token != token {
		t.Errorf("Token mismatch: got %s want %s", got.Token.Hex(), token.Hex())
	}
	if new(big.Int).SetBytes(got.Amount[:]).Cmp(amount) != 0 {
		t.Errorf("Amount mismatch: got %s want %s", new(big.Int).SetBytes(got.Amount[:]), amount)
	}
	if got.IntentID != chain.Bytes16FromUUID(intent) {
		t.Errorf("IntentID mismatch: got %x want %x", got.IntentID, chain.Bytes16FromUUID(intent))
	}
	if new(big.Int).SetBytes(got.PriceVersion[:]).Cmp(priceVersion) != 0 {
		t.Errorf("PriceVersion mismatch: got %s want %s", new(big.Int).SetBytes(got.PriceVersion[:]), priceVersion)
	}
}

func TestDecode_Removed(t *testing.T) {
	log := &fakeLog{removed: true, topics: []common.Hash{chain.CoursePurchasedTopic}}
	_, err := chain.Decode(log)
	if !errors.Is(err, chain.ErrLogRemoved) {
		t.Fatalf("expected ErrLogRemoved, got %v", err)
	}
}

func TestDecode_TopicMismatch(t *testing.T) {
	other := crypto.Keccak256Hash([]byte("OtherEvent()"))
	log := &fakeLog{topics: []common.Hash{other, {}, {}}}
	_, err := chain.Decode(log)
	if !errors.Is(err, chain.ErrTopicMismatch) {
		t.Fatalf("expected ErrTopicMismatch, got %v", err)
	}
}

func TestDecode_TooFewTopics(t *testing.T) {
	// 只有 topic[0]，缺 indexed 字段
	log := &fakeLog{topics: []common.Hash{chain.CoursePurchasedTopic}}
	_, err := chain.Decode(log)
	if !errors.Is(err, chain.ErrTooFewTopics) {
		t.Fatalf("expected ErrTooFewTopics, got %v", err)
	}
}

func TestDecode_BadData(t *testing.T) {
	// topics 全合法，但 data 是 garbage
	topics := []common.Hash{
		chain.CoursePurchasedTopic,
		common.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111"),
		common.BytesToHash(common.HexToAddress("0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266").Bytes()),
	}
	log := &fakeLog{topics: topics, data: []byte("not abi encoded")}
	_, err := chain.Decode(log)
	if err == nil {
		t.Fatal("expected error from garbage data")
	}
	if !errors.Is(err, chain.ErrDecodeData) {
		t.Fatalf("expected ErrDecodeData, got %v", err)
	}
}

func TestValidateReceipt_AllFields(t *testing.T) {
	courseKey := [32]byte{1, 2, 3}
	buyer := common.HexToAddress("0xAbCdEf0123456789aBcDeF0123456789AbCdEf01")
	token := common.HexToAddress("0x70997970C51812dc3A010C7d01b50e0d17dc79C8")
	amount := chain.U256FromUint64(1_000_000)
	intent := chain.Bytes16FromUUID(uuid.New())
	priceVersion := chain.U256FromUint64(7)

	got := &chain.CoursePurchased{
		CourseKey: courseKey, Buyer: buyer, Token: token,
		Amount: amount, IntentID: intent, PriceVersion: priceVersion,
	}
	want := &chain.Intent{
		CourseKey: courseKey, Buyer: buyer, Token: token,
		Amount: amount, IntentID: intent, PriceVersion: priceVersion,
	}
	if err := chain.ValidateReceipt(got, want); err != nil {
		t.Fatalf("ValidateReceipt equal: %v", err)
	}
}

func TestValidateReceipt_FieldMismatches(t *testing.T) {
	courseKey := [32]byte{1}
	buyer := common.HexToAddress("0xAbCdEf0123456789aBcDeF0123456789AbCdEf01")
	token := common.HexToAddress("0x70997970C51812dc3A010C7d01b50e0d17dc79C8")
	amount := chain.U256FromUint64(1_000_000)
	intent := chain.Bytes16FromUUID(uuid.New())
	priceVersion := chain.U256FromUint64(7)

	base := &chain.CoursePurchased{
		CourseKey: courseKey, Buyer: buyer, Token: token,
		Amount: amount, IntentID: intent, PriceVersion: priceVersion,
	}
	baseIntent := &chain.Intent{
		CourseKey: courseKey, Buyer: buyer, Token: token,
		Amount: amount, IntentID: intent, PriceVersion: priceVersion,
	}

	cases := []struct {
		name     string
		mutate   func(*chain.CoursePurchased)
		wantHint string
	}{
		{
			name:     "courseKey",
			mutate:   func(p *chain.CoursePurchased) { p.CourseKey = [32]byte{9} },
			wantHint: "courseKey",
		},
		{
			name:     "buyer",
			mutate:   func(p *chain.CoursePurchased) { p.Buyer = common.HexToAddress("0x0000000000000000000000000000000000000001") },
			wantHint: "buyer",
		},
		{
			name:     "token",
			mutate:   func(p *chain.CoursePurchased) { p.Token = common.HexToAddress("0x0000000000000000000000000000000000000002") },
			wantHint: "token",
		},
		{
			name:     "amount",
			mutate:   func(p *chain.CoursePurchased) { p.Amount = chain.U256FromUint64(2_000_000) },
			wantHint: "amount",
		},
		{
			name:     "intentId",
			mutate:   func(p *chain.CoursePurchased) { p.IntentID = [16]byte{} },
			wantHint: "intentId",
		},
		{
			name:     "priceVersion",
			mutate:   func(p *chain.CoursePurchased) { p.PriceVersion = chain.U256FromUint64(8) },
			wantHint: "priceVersion",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := *base // copy
			tc.mutate(&got)
			err := chain.ValidateReceipt(&got, baseIntent)
			if err == nil {
				t.Fatal("expected error")
			}
			if !errors.Is(err, chain.ErrMismatch) {
				t.Fatalf("expected ErrMismatch, got %v", err)
			}
			// 错误信息应含字段名（便于日志 grep）
			if !contains(err.Error(), tc.wantHint) {
				t.Errorf("error %q does not mention field %q", err.Error(), tc.wantHint)
			}
		})
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle ||
		indexOf(haystack, needle) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
