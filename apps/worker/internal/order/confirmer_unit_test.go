package workerorder_test

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/google/uuid"

	"github.com/x-web3/worker/internal/chain"
	workerorder "github.com/x-web3/worker/internal/order"
)

// TestBuildIntentFromDB_Happy 是 PR-A2 #4 的单测对应：DB → Intent 全字段对齐。
//
// 不依赖 PG：BuildIntentFromDBForTest（来自 export_test.go）是 buildIntentFromDB
// 的导出 alias；integration test 仍需 DATABASE_URL_TEST。
func TestBuildIntentFromDB_Happy(t *testing.T) {
	intentID := uuid.MustParse("01020304-0506-0708-0910-111213141516")
	courseKeyHex := "1111111111111111111111111111111111111111111111111111111111111111"
	tokenAddr := "0x70997970C51812dc3A010C7d01b50e0d17dc79C8"
	walletAddr := "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266"

	got, err := workerorder.BuildIntentFromDBForTest(
		[]byte(courseKeyHex),
		tokenAddr,
		"1000000",
		7,
		intentID,
		walletAddr,
	)
	if err != nil {
		t.Fatalf("BuildIntentFromDBForTest: %v", err)
	}

	if got.Token != common.HexToAddress(tokenAddr) {
		t.Errorf("Token: got %s want %s", got.Token.Hex(), tokenAddr)
	}
	if got.Buyer != common.HexToAddress(walletAddr) {
		t.Errorf("Buyer: got %s want %s", got.Buyer.Hex(), walletAddr)
	}
	if amount := decodeU256BE(got.Amount); amount != "1000000" {
		t.Errorf("Amount: got %s want 1000000", amount)
	}
	if pv := decodeU256BE(got.PriceVersion); pv != "7" {
		t.Errorf("PriceVersion: got %s want 7", pv)
	}
}

// TestBuildIntentFromDB_MismatchFields 表驱动：把每个 event 字段单独改掉，
// ValidateReceipt 必须报 ErrMismatch。这是 PR-A2 #5 的核心断言：
// 即使 event 字段有 1 个不对，校验也必须捕获——而不是「形同自比」全过。
func TestBuildIntentFromDB_MismatchFields(t *testing.T) {
	intentID := uuid.MustParse("01020304-0506-0708-0910-111213141516")
	courseKeyHex := "1111111111111111111111111111111111111111111111111111111111111111"
	want, err := workerorder.BuildIntentFromDBForTest(
		[]byte(courseKeyHex),
		"0x70997970C51812dc3A010C7d01b50e0d17dc79C8",
		"1000000",
		7,
		intentID,
		"0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266",
	)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	cases := []struct {
		name   string
		mutate func(*chain.CoursePurchased)
	}{
		{"courseKey", func(p *chain.CoursePurchased) { p.CourseKey = [32]byte{9} }},
		{"token", func(p *chain.CoursePurchased) {
			p.Token = common.HexToAddress("0x0000000000000000000000000000000000000001")
		}},
		{"amount", func(p *chain.CoursePurchased) { p.Amount = workerorder.U256(2_000_000) }},
		{"intentId", func(p *chain.CoursePurchased) { p.IntentID = [16]byte{} }},
		{"priceVersion", func(p *chain.CoursePurchased) { p.PriceVersion = workerorder.U256(99) }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := chain.CoursePurchased{
				CourseKey:    want.CourseKey,
				Buyer:        want.Buyer,
				Token:        want.Token,
				Amount:       want.Amount,
				IntentID:     want.IntentID,
				PriceVersion: want.PriceVersion,
			}
			tc.mutate(&got)
			if err := chain.ValidateReceipt(&got, &want); err == nil {
				t.Fatalf("%s mismatch should fail ValidateReceipt, got nil", tc.name)
			}
		})
	}
}

// TestBuildIntentFromDB_InvalidInputs 表驱动：bad hex / bad amount / zero addr。
func TestBuildIntentFromDB_InvalidInputs(t *testing.T) {
	intentID := uuid.New()
	good := func() (string, string, string) {
		return "1111111111111111111111111111111111111111111111111111111111111111",
			"0x70997970C51812dc3A010C7d01b50e0d17dc79C8",
			"0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266"
	}
	cases := []struct {
		name        string
		ck, tok, wl string
		amount      string
	}{
		{"short courseKey", "abcd", "0x70997970C51812dc3A010C7d01b50e0d17dc79C8", "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266", "1"},
		{"zero token", "1111111111111111111111111111111111111111111111111111111111111111", "0x0000000000000000000000000000000000000000", "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266", "1"},
		{"zero wallet", "1111111111111111111111111111111111111111111111111111111111111111", "0x70997970C51812dc3A010C7d01b50e0d17dc79C8", "0x0000000000000000000000000000000000000000", "1"},
		{"non-numeric amount", "1111111111111111111111111111111111111111111111111111111111111111", "0x70997970C51812dc3A010C7d01b50e0d17dc79C8", "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266", "not-a-number"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := workerorder.BuildIntentFromDBForTest([]byte(tc.ck), tc.tok, tc.amount, 1, intentID, tc.wl); err == nil {
				_ = good // 抑制 unused；good 用于校对 happy path 不在 cases
				t.Fatalf("%s should fail", tc.name)
			}
		})
	}
}

// decodeU256BE 把 [32]byte big-endian 转成 decimal 字符串（测试断言用）。
func decodeU256BE(b [32]byte) string {
	v := new(big.Int).SetBytes(b[:])
	return v.String()
}
