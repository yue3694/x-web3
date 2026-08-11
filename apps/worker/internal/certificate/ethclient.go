// ethclient 适配：把 go-ethereum ethclient.Client 包成 certificate.EthClient。
//
// 设计动机：
//   - certificate.Consumer 持有 EthClient 接口；测试用 fake，生产用真 ethclient。
//   - 复用 indexer.RPCClient.RawRPC()（**不要 Close**，由 RPCPool 管），各自
//     ethclient.NewClient 一份独立的轻包装，避免多 consumer 共享 HTTP/2 stream
//     时的并发 head-of-line blocking。
//   - WaitMined 在 TransactionReceipt 之上再加 confirmation depth 等待；
//     阶段 A 默认 12 块（≈Sepolia 24s / anvil 12s），最终性足够。
package certificate

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
)

// ethClientAdapter 把 *ethclient.Client 适配为 certificate.EthClient。
//
// ConfirmDepth 控制 WaitMined 等待 receipt 之后额外等待的区块数；
// 0 时退化为「receipt 拿到就返回」——本地 anvil 调试时可调小。
type ethClientAdapter struct {
	ec           *ethclient.Client
	confirmDepth int64
	pollInterval time.Duration
}

// NewEthClientAdapter 构造适配器。
//
// pollInterval 是 WaitMined 轮询最新块头的间隔；零值用 1s（anvil 推荐 250ms，sepolia 3s）。
func NewEthClientAdapter(ec *ethclient.Client, confirmDepth int64, pollInterval time.Duration) *ethClientAdapter {
	if pollInterval <= 0 {
		pollInterval = time.Second
	}
	if confirmDepth < 0 {
		confirmDepth = 0
	}
	return &ethClientAdapter{ec: ec, confirmDepth: confirmDepth, pollInterval: pollInterval}
}

var _ EthClient = (*ethClientAdapter)(nil)

// PendingNonceAt 透传 ethclient。
func (a *ethClientAdapter) PendingNonceAt(ctx context.Context, account common.Address) (uint64, error) {
	return a.ec.PendingNonceAt(ctx, account)
}

// SendTransaction 透传 ethclient；ethclient 内部已经做了 tx hash 同步。
func (a *ethClientAdapter) SendTransaction(ctx context.Context, tx *types.Transaction) error {
	return a.ec.SendTransaction(ctx, tx)
}

// WaitMined 阻塞等到 tx 上链；之后再等 ConfirmDepth 块以获最终性。
//
// 内部循环：
//  1. 反复调 TransactionReceipt 直到拿到非 nil receipt（或 ctx 取消 / 错误）。
//  2. 拿到 receipt 后，轮询 BlockNumber 直到 ≥ receipt.BlockNumber + ConfirmDepth。
//
// 已知 trade-off：
//   - 极深 reorg（>ConfirmDepth）可能让已 confirm 的 receipt 被回滚；
//     consumer 把这种 case 当作 WaitMined 错误返回，由 retry path 走二次确认。
//     阶段 A 12 块对 Sepolia 24s 已够；阶段 B KMS / finality 接入再评估。
func (a *ethClientAdapter) WaitMined(ctx context.Context, hash common.Hash) (*Receipt, error) {
	if a.ec == nil {
		return nil, errors.New("certificate: ethClientAdapter.ec is nil")
	}
	receipt, err := waitReceipt(ctx, a.ec, hash, a.pollInterval)
	if err != nil {
		return nil, err
	}
	if a.confirmDepth <= 0 {
		return toCertReceipt(receipt), nil
	}
	if err := waitConfirmDepth(ctx, a.ec, receipt.BlockNumber.Uint64(), a.confirmDepth, a.pollInterval); err != nil {
		return nil, err
	}
	return toCertReceipt(receipt), nil
}

// waitReceipt 反复拉 TransactionReceipt 直到拿到；常见失败：tx 还在 mempool。
func waitReceipt(ctx context.Context, ec *ethclient.Client, hash common.Hash, poll time.Duration) (*types.Receipt, error) {
	t := time.NewTicker(poll)
	defer t.Stop()
	// 首次立即尝试，避免 ticker 起点延迟。
	for {
		receipt, err := ec.TransactionReceipt(ctx, hash)
		if err == nil && receipt != nil {
			return receipt, nil
		}
		if err != nil && !isNotFoundErr(err) {
			return nil, fmt.Errorf("certificate: TransactionReceipt: %w", err)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-t.C:
		}
	}
}

// waitConfirmDepth 等链头超过 target + depth。
//
// ctx 取消 → 立刻返回；网络错误重试（不返回上层）以容忍瞬时 RPC 抖动。
func waitConfirmDepth(ctx context.Context, ec *ethclient.Client, target uint64, depth int64, poll time.Duration) error {
	t := time.NewTicker(poll)
	defer t.Stop()
	for {
		head, err := ec.BlockNumber(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			// 瞬时网络错误，继续轮询
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-t.C:
				continue
			}
		}
		// uint64 / int64 互转；depth=0 也走 return
		if int64(head) >= int64(target)+depth {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
	}
}

// toCertReceipt 把 types.Receipt 映射为最小 certificate.Receipt view。
func toCertReceipt(r *types.Receipt) *Receipt {
	if r == nil {
		return nil
	}
	return &Receipt{
		TxHash:      r.TxHash,
		BlockNumber: r.BlockNumber.Uint64(),
		BlockHash:   r.BlockHash,
		Status:      r.Status,
	}
}

// isNotFoundErr 判断 ethclient 返回的「receipt 还没出」错误。
//
// go-ethereum v1.14.11 的 TransactionReceipt 在 not found 时返回
// ethereum.NotFound（errors.Is 支持）；其它 RPC 错误信息各异，单测覆盖。
func isNotFoundErr(err error) bool {
	return err != nil && err.Error() == "not found"
}
