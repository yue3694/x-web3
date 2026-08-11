package certificate

import (
	"context"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// fakeOps 实现 rpcOps；每个方法独立控制，便于表驱动覆盖 fallback 链。
type fakeOps struct {
	nonce       uint64
	nonceErr    error
	tip         *big.Int
	tipErr      error
	gasPrice    *big.Int
	gasPriceErr error
}

func (f *fakeOps) PendingNonceAt(context.Context, common.Address) (uint64, error) {
	return f.nonce, f.nonceErr
}
func (f *fakeOps) SuggestGasTipCap(context.Context) (*big.Int, error) {
	return f.tip, f.tipErr
}
func (f *fakeOps) SuggestGasPrice(context.Context) (*big.Int, error) {
	return f.gasPrice, f.gasPriceErr
}

func TestChainTxParams_1559Path(t *testing.T) {
	c := &ChainTxParams{gasLimit: 200_000}
	ops := &fakeOps{
		nonce:    7,
		tip:      big.NewInt(2_000_000_000),
		gasPrice: big.NewInt(50_000_000_000),
	}
	got, err := c.txParamsFromClient(context.Background(), ops, common.Address{})
	if err != nil {
		t.Fatalf("txParamsFromClient: %v", err)
	}
	if got.Nonce != 7 {
		t.Errorf("Nonce: got %d want 7", got.Nonce)
	}
	if got.GasLimit != 200_000 {
		t.Errorf("GasLimit: got %d want 200000", got.GasLimit)
	}
	if got.GasFeeCap.Cmp(big.NewInt(50_000_000_000)) != 0 {
		t.Errorf("GasFeeCap: got %s want 50 gwei", got.GasFeeCap)
	}
	if got.GasTipCap.Cmp(big.NewInt(2_000_000_000)) != 0 {
		t.Errorf("GasTipCap: got %s want 2 gwei", got.GasTipCap)
	}
}

// TestChainTxParams_TotalFallback：tip + gasPrice 都失败 → 1 gwei 兜底。
func TestChainTxParams_TotalFallback(t *testing.T) {
	c := &ChainTxParams{gasLimit: 300_000}
	ops := &fakeOps{
		nonce:       5,
		tipErr:      errors.New("rpc dead"),
		gasPriceErr: errors.New("rpc dead"),
	}
	got, err := c.txParamsFromClient(context.Background(), ops, common.Address{})
	if err != nil {
		t.Fatalf("txParamsFromClient: %v", err)
	}
	want := big.NewInt(1_000_000_000)
	if got.GasFeeCap.Cmp(want) != 0 {
		t.Errorf("GasFeeCap total fallback: got %s want 1 gwei", got.GasFeeCap)
	}
	if got.GasTipCap.Cmp(want) != 0 {
		t.Errorf("GasTipCap total fallback: got %s want 1 gwei", got.GasTipCap)
	}
}

func TestChainTxParams_NonceError(t *testing.T) {
	c := &ChainTxParams{gasLimit: 300_000}
	ops := &fakeOps{nonceErr: errors.New("nonce: connection reset")}
	_, err := c.txParamsFromClient(context.Background(), ops, common.Address{})
	if err == nil {
		t.Fatal("expected error from PendingNonceAt failure")
	}
	if !contains(err.Error(), "PendingNonceAt") {
		t.Errorf("error should mention PendingNonceAt: %v", err)
	}
}

// TestChainTxParams_ThreeCallsMonotonicNonce 模拟「连续铸 3 张 cert」场景，
// nonce 必须严格递增 1, 2, 3 — 否则 signer 会出现链上 nonce 冲突。
func TestChainTxParams_ThreeCallsMonotonicNonce(t *testing.T) {
	c := &ChainTxParams{gasLimit: 300_000}
	ops := &fakeOps{
		tip:      big.NewInt(1_000_000_000),
		gasPrice: big.NewInt(30_000_000_000),
	}

	for i := uint64(1); i <= 3; i++ {
		ops.nonce = i
		got, err := c.txParamsFromClient(context.Background(), ops, common.Address{})
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		if got.Nonce != i {
			t.Errorf("call %d: nonce got %d want %d", i, got.Nonce, i)
		}
	}
}

func TestChainTxParams_NilReceiver(t *testing.T) {
	var c *ChainTxParams
	_, err := c.txParamsFromClient(context.Background(), &fakeOps{nonce: 1}, common.Address{})
	if err == nil {
		t.Fatal("expected nil receiver to fail")
	}
}

func contains(haystack, needle string) bool {
	if len(haystack) < len(needle) {
		return false
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
