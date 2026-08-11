package certificate

import (
	"context"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

func TestToCertReceipt(t *testing.T) {
	r := &types.Receipt{
		TxHash:      common.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111"),
		BlockNumber: big.NewInt(42),
		BlockHash:   common.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222"),
		Status:      1,
	}
	got := toCertReceipt(r)
	if got == nil {
		t.Fatal("toCertReceipt returned nil for non-nil input")
	}
	if got.TxHash != r.TxHash {
		t.Errorf("TxHash: got %s want %s", got.TxHash.Hex(), r.TxHash.Hex())
	}
	if got.BlockNumber != 42 {
		t.Errorf("BlockNumber: got %d want 42", got.BlockNumber)
	}
	if got.Status != 1 {
		t.Errorf("Status: got %d want 1", got.Status)
	}
}

func TestToCertReceiptNil(t *testing.T) {
	if got := toCertReceipt(nil); got != nil {
		t.Errorf("toCertReceipt(nil) should return nil, got %+v", got)
	}
}

func TestIsNotFoundErr(t *testing.T) {
	cases := []struct {
		name string
		in   error
		want bool
	}{
		{"exact match", errors.New("not found"), true},
		{"connection refused", errors.New("connection refused"), false},
		{"wrong case", errors.New("Not Found"), false}, // 大小写敏感 — ethereum.NotFound 不走这里
		{"nil", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isNotFoundErr(tc.in); got != tc.want {
				t.Errorf("isNotFoundErr(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// fakePassThroughEthClient 实现 EthClient，用于单元测试纯函数与默认值。
// 与 consumer_test.go::fakeEthClient（按 round 序列）不冲突，名字显式区分。
type fakePassThroughEthClient struct {
	nonce    uint64
	nonceErr error
	sendErr  error
	mined    *Receipt
}

func (f *fakePassThroughEthClient) PendingNonceAt(context.Context, common.Address) (uint64, error) {
	return f.nonce, f.nonceErr
}
func (f *fakePassThroughEthClient) SendTransaction(context.Context, *types.Transaction) error {
	return f.sendErr
}
func (f *fakePassThroughEthClient) WaitMined(_ context.Context, hash common.Hash) (*Receipt, error) {
	if f.mined != nil {
		return f.mined, nil
	}
	return &Receipt{TxHash: hash, BlockNumber: 1, Status: 1}, nil
}

func TestNewEthClientAdapter_Defaults(t *testing.T) {
	// nil ec + 0 depth + 0 poll：构造不应 panic；运行时 SendTransaction 会因 nil ec 报错
	a := NewEthClientAdapter(nil, 0, 0)
	if a == nil {
		t.Fatal("NewEthClientAdapter returned nil")
	}
	if a.pollInterval <= 0 {
		t.Errorf("pollInterval should default to > 0, got %v", a.pollInterval)
	}
	if a.confirmDepth != 0 {
		t.Errorf("confirmDepth: got %d want 0", a.confirmDepth)
	}
}

func TestNewEthClientAdapter_NegativeDepthClamped(t *testing.T) {
	a := NewEthClientAdapter(nil, -5, 0)
	if a.confirmDepth != 0 {
		t.Errorf("negative confirmDepth should clamp to 0, got %d", a.confirmDepth)
	}
}

func TestEthClientAdapter_ImplementsInterface(t *testing.T) {
	// 静态断言：*ethClientAdapter 必须实现 EthClient；编译期保证。
	var _ EthClient = (*ethClientAdapter)(nil)
}

func TestFakePassThrough_Nonce(t *testing.T) {
	from := common.HexToAddress("0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266")
	f := &fakePassThroughEthClient{nonce: 17}
	got, err := f.PendingNonceAt(context.Background(), from)
	if err != nil {
		t.Fatalf("nonce: %v", err)
	}
	if got != 17 {
		t.Errorf("nonce got %d want 17", got)
	}
}
