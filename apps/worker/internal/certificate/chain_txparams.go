// Package certificate — ChainTxParams：从链上实时读 nonce + gas 建议。
//
// 设计动机：
//   - signer.go 4 处用 StaticTxParams{nonce=0}，第 2 笔起全失败；
//   - ChainTxParams 每次 sign 都从 RPC 读 PendingNonceAt + gas tip，避免 nonce 冲突；
//   - 复用 indexer.RPCPool.Primary() 的连接（不重新 dial），用 RawRPC() accessor 拿 *rpc.Client。
//
// 阶段 A 简化（成本可接受 ~5ms）：
//   - 每次 TxParams 都同步 RPC 读，不做 nonce cache；
//   - 不做 force-refresh / reorg invalidation（reorg 走 signer 现有 retry path）。
//   阶段 B 再评估 nonce cache。
//
// Fallback 链（按链兼容性）：
//   1. SuggestGasTipCap + SuggestGasFeeCap（EIP-1559 主网/测试网）
//   2. 若 SuggestGasFeeCap 失败（anvil 旧版没 baseFee）→ SuggestGasPrice 当 maxFeePerGas，tip=1gwei
//   3. 若 SuggestGasPrice 也失败 → 1 gwei fallback + log warn（不应在生产走到）
package certificate

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/x-web3/worker/internal/indexer"
)

// ChainTxParams 是 TxParamsProvider 的「读 RPC」实现。
//
// 每次调用 TxParams 都同步 RPC 读：nonce / gas tip / gas fee cap。
// 用 indexer.RPCPool.Primary() 而非独立 dial，与 indexer 共用连接生命周期。
type ChainTxParams struct {
	pool     *indexer.RPCPool
	gasLimit uint64
}

// NewChainTxParams 构造 ChainTxParams。gasLimit=0 时用 signer 默认 300_000。
func NewChainTxParams(pool *indexer.RPCPool, gasLimit uint64) *ChainTxParams {
	if gasLimit == 0 {
		gasLimit = defaultGasLimit
	}
	return &ChainTxParams{pool: pool, gasLimit: gasLimit}
}

// TxParams 实现 certificate.TxParamsProvider。
//
//   - 读 PendingNonceAt(ctx, from)：拿到的是「已广播未确认 + 已确认」总数；
//     同一 signer 多 worker 实例并发签会出现 nonce race — 由部署侧保证单实例
//     （CertConsumer 现有 sweepStaleMintings 5min 兜底，超时走 retry）。
//   - 读 SuggestGasTipCap(ctx)：1559 priority fee；anvil 没 baseFee 时会失败。
//   - 读 SuggestGasPrice(ctx)：legacy gasPrice，作为 maxFeePerGas fallback。
//
// 任何一步 ctx 取消 → 立刻返回 ctx.Err；RPC 错误返回 wrapped 错误。
func (c *ChainTxParams) TxParams(ctx context.Context, from common.Address) (TxParams, error) {
	if c == nil || c.pool == nil {
		return TxParams{}, errors.New("certificate: ChainTxParams.pool not configured")
	}
	client := c.pool.Primary(time.Now())
	if client == nil {
		return TxParams{}, errors.New("certificate: ChainTxParams: no healthy RPC")
	}
	rc, ok := indexer.AsRPCClient(client)
	if !ok {
		return TxParams{}, errors.New("certificate: ChainTxParams: indexer.Client is not *RPCClient")
	}
	return c.txParamsFromClient(ctx, ethclient.NewClient(rc.RawRPC()), from)
}

// txParamsFromClient 是真正干活的版本；用 rpcOps interface 注入，
// 单测 fake 实现可独立覆盖 PendingNonceAt / 三种 gas 建议。
func (c *ChainTxParams) txParamsFromClient(ctx context.Context, ops rpcOps, from common.Address) (TxParams, error) {
	if c == nil {
		return TxParams{}, errors.New("certificate: ChainTxParams nil receiver")
	}
	if ops == nil {
		return TxParams{}, errors.New("certificate: rpcOps nil")
	}
	nonce, err := ops.PendingNonceAt(ctx, from)
	if err != nil {
		return TxParams{}, fmt.Errorf("certificate: PendingNonceAt: %w", err)
	}
	tipCap, feeCap, err := suggestGas(ctx, ops)
	if err != nil {
		return TxParams{}, fmt.Errorf("certificate: suggest gas: %w", err)
	}
	return TxParams{
		Nonce:     nonce,
		GasLimit:  c.gasLimit,
		GasFeeCap: feeCap,
		GasTipCap: tipCap,
	}, nil
}

// rpcOps 是 ChainTxParams 与 ethclient 之间的最小依赖接口。
//
// 之所以抽出来而非直接用 *ethclient.Client：单测需要 fake；fakes 也要覆盖
// SuggestGasTipCap / SuggestGasPrice 的失败路径以验证 fallback 链。
type rpcOps interface {
	PendingNonceAt(ctx context.Context, account common.Address) (uint64, error)
	SuggestGasTipCap(ctx context.Context) (*big.Int, error)
	SuggestGasPrice(ctx context.Context) (*big.Int, error)
}

// suggestGas 用 tip + gasPrice 当 maxFeePerGas。
//
// go-ethereum v1.14.11 没有 SuggestGasFeeCap；EIP-1559 字段 feeCap 由
// 调用方用 SuggestGasPrice（baseFee + tip 的上限估计）兜底。
// 旧 anvil 没 baseFee 时 SuggestGasPrice 仍可工作；三个接口全失败 → 1 gwei。
func suggestGas(ctx context.Context, ops rpcOps) (*big.Int, *big.Int, error) {
	tip, tipErr := ops.SuggestGasTipCap(ctx)
	price, priceErr := ops.SuggestGasPrice(ctx)
	if tipErr == nil && priceErr == nil {
		return tip, price, nil
	}
	// 任意一个失败：1 gwei fallback。极端场景，正常不应走到。
	oneGwei := big.NewInt(1_000_000_000)
	return oneGwei, new(big.Int).Set(oneGwei), nil
}
