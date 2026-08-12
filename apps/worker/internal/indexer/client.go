// Package indexer 链事件监听、checkpoint 推进与 reorg 恢复。
//
// 模块拆分：
//   - client.go：定义与 ethclient 交互的接口 + WS 订阅抽象 + 工厂方法。
//   - runner.go：主循环（WS 订阅 + HTTP polling 兜底 + 多 RPC fallback）。
//   - checkpoint.go：chain_checkpoints 表的 read/write 助手。
//   - reorg.go：reorg 检测与回滚。
//
// 所有外部依赖通过接口注入；测试用 stub 替身，避开真实 RPC。
package indexer

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/rpc"
)

// Header 是 indexer 关心的最小区块头。
//
// 复刻 go-ethereum core/types.Header 的字段以避免把 core 暴露到外部：
// 任何实现 ethclient 语义的代码都能映射到这里。
type Header struct {
	Number    *big.Int
	Hash      common.Hash
	Parent    common.Hash
	Timestamp uint64
}

// LogRecord 描述一条原始链日志（来自 eth_getLogs 或 订阅）。
//
// 用途：indexer 把 LogRecord 交给 confirmer.Apply 时无需关心来源
// （WS / HTTP polling / 手动 rewind）。
type LogRecord struct {
	ChainID     *big.Int
	BlockNumber uint64
	BlockHash   common.Hash
	TxHash      common.Hash
	LogIndex    uint
	Address     common.Address
	Topics      []common.Hash
	Data        []byte
	Removed     bool
}

// Topics / Data / Removed 是 chain.LogRecordLike 接口的方法。
// 任何想用 chain.Decode 处理 raw log 的代码都可以直接传 *LogRecord。
//
// Go 不允许 struct field 与 method 同名，因此这里改名为
// TopicsList / DataBytes / IsRemoved；调用方显式实现接口时也用这套名字。
func (r *LogRecord) TopicsList() []common.Hash { return r.Topics }
func (r *LogRecord) DataBytes() []byte         { return r.Data }
func (r *LogRecord) IsRemoved() bool           { return r.Removed }

// Client 抽象了 indexer 与 RPC 节点交互所需的最小能力集。
//
// 设计动机：
//   - 真机环境：NewRPCClient(*rpc.Client) 包装 ethclient.Client；
//   - 测试环境：fakeClient 注入可预测的 header / log 序列。
//
// 任何方法必须能 ctx 取消；返回的错误应当保留（用 %w 包装）以便上层判别
// 网络/限流/链错位。
type Client interface {
	// ChainID 返回当前连接的网络 ID。
	ChainID(ctx context.Context) (*big.Int, error)
	// HeaderByNumber 取指定区块的 header；number=nil 时取 latest。
	HeaderByNumber(ctx context.Context, number *big.Int) (*Header, error)
	// BlockNumber 取 latest 区块号。
	BlockNumber(ctx context.Context) (uint64, error)
	// FilterLogs 按 query 拉 logs。
	FilterLogs(ctx context.Context, q ethereum.FilterQuery) ([]types.Log, error)
	// SubscribeNewHead 订阅新块头（WS 通道；不可用时返回 error）。
	SubscribeNewHead(ctx context.Context) (HeadSub, error)
	// Close 关闭底层连接。
	Close()
}

// HeadSub 是 WS 订阅句柄；返回的 channel 必须是带缓冲或可异步消费的，
// 否则 go-ethereum 会阻塞 head 推送。
type HeadSub interface {
	// Chan 返回头序列；订阅出错或被取消时由实现方负责关闭。
	Chan() <-chan *Header
	// Err 返回订阅期间出现的不可恢复错误。
	Err() <-chan error
	// Unsubscribe 主动退订。
	Unsubscribe()
}

// RPCClient 是 Client 的真实实现，包装 go-ethereum 的 *ethclient.Client +
// 独立 *rpc.Client（用于 WS）。
type RPCClient struct {
	ec     *ethclient.Client
	rc     *rpc.Client
	chain  atomic.Pointer[big.Int]
	closed atomic.Bool
}

// NewRPCClient 基于 *rpc.Client 构造 Client；传入的 rc 同时支持 HTTP 与 WS。
func NewRPCClient(ctx context.Context, rc *rpc.Client) (*RPCClient, error) {
	if rc == nil {
		return nil, errors.New("indexer: rpc client is nil")
	}
	c := &RPCClient{ec: ethclient.NewClient(rc), rc: rc}
	id, err := c.ec.ChainID(ctx)
	if err != nil {
		return nil, fmt.Errorf("indexer: chainid probe: %w", err)
	}
	c.chain.Store(id)
	return c, nil
}

// ChainID ...
func (c *RPCClient) ChainID(ctx context.Context) (*big.Int, error) {
	if id := c.chain.Load(); id != nil {
		return id, nil
	}
	id, err := c.ec.ChainID(ctx)
	if err != nil {
		return nil, err
	}
	c.chain.Store(id)
	return id, nil
}

// HeaderByNumber ...
func (c *RPCClient) HeaderByNumber(ctx context.Context, number *big.Int) (*Header, error) {
	h, err := c.ec.HeaderByNumber(ctx, number)
	if err != nil {
		return nil, err
	}
	if h == nil {
		return nil, ethereum.NotFound
	}
	return &Header{
		Number:    h.Number,
		Hash:      h.Hash(),
		Parent:    h.ParentHash,
		Timestamp: h.Time,
	}, nil
}

// BlockNumber ...
func (c *RPCClient) BlockNumber(ctx context.Context) (uint64, error) {
	return c.ec.BlockNumber(ctx)
}

// FilterLogs ...
func (c *RPCClient) FilterLogs(ctx context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
	return c.ec.FilterLogs(ctx, q)
}

// SubscribeNewHead ...
func (c *RPCClient) SubscribeNewHead(ctx context.Context) (HeadSub, error) {
	ch := make(chan *types.Header, 16)
	sub, err := c.ec.SubscribeNewHead(ctx, ch)
	if err != nil {
		return nil, err
	}
	return &rpcHeadSub{sub: sub, ch: ch}, nil
}

// Close 关闭底层 RPC 连接。
func (c *RPCClient) Close() {
	if c.closed.Swap(true) {
		return
	}
	if c.rc != nil {
		c.rc.Close()
	}
}

// RawRPC 返回底层 *rpc.Client，供同进程内其它组件（cert consumer /
// treasury monitor / ChainTxParams）复用已 dial 的连接。
//
// ⚠️ 调用方**不得 Close** 这个 client — 它的生命周期由 RPCPool 统一管理；
// 多次 Close 会让后续 dial 失败并把 RPCPool 标 unhealthy。
func (c *RPCClient) RawRPC() *rpc.Client { return c.rc }

// AsRPCClient 在 Client 接口实现上做 type assertion；
// 给不想直接依赖 *RPCClient 的代码用（典型：ChainTxParams）。
func AsRPCClient(c Client) (*RPCClient, bool) {
	if rc, ok := c.(*RPCClient); ok {
		return rc, true
	}
	return nil, false
}

// rpcHeadSub 适配 types.Header 通道为 indexer.Header。
//
// Chan() 返回带缓冲的内部通道：上游推送持续写入；下游消费者按需读取。
// 慢消费者场景下 producer 会阻塞 go-ethereum 的内部 goroutine —— 通过
// 内部 lastDropped 计数暴露，让消费者在下一次循环里 fast-forward。
type rpcHeadSub struct {
	sub         event.Subscription
	ch          chan *types.Header
	lastDropped atomic.Int64 // 上次 drain 以来丢弃的 raw head 数
}

func (s *rpcHeadSub) Chan() <-chan *Header {
	out := make(chan *Header, 32)
	go func() {
		defer close(out)
		for raw := range s.ch {
			if raw == nil {
				continue
			}
			hdr := &Header{
				Number:    raw.Number,
				Hash:      raw.Hash(),
				Parent:    raw.ParentHash,
				Timestamp: raw.Time,
			}
			// 带 timeout 的 blocking send：避免无脑丢 head（go-ethereum
			// 内部 goroutine 会因上游 ch 满而阻塞）。
			t := time.NewTimer(200 * time.Millisecond)
			select {
			case out <- hdr:
				t.Stop()
			case <-t.C:
				// 慢消费者：记录丢弃数，让 eventLoop 在下一次 drain 时
				// 选择最近一次成功的 head（fast-forward）。
				s.lastDropped.Add(1)
			}
		}
	}()
	return out
}

// LastDropped 返回该订阅以来累计丢弃的 head 数（用于健康检查/日志）。
func (s *rpcHeadSub) LastDropped() int64 { return s.lastDropped.Load() }

// Err 直接返回底层 Subscription 的 Err 通道；不额外起 goroutine 防泄漏。
func (s *rpcHeadSub) Err() <-chan error { return s.sub.Err() }

func (s *rpcHeadSub) Unsubscribe() { s.sub.Unsubscribe() }

// DialWS / DialHTTP 是 NewRPCClient 之前的连接工厂；放在 indexer 包是为了
// 减少 main.go 内的外部依赖。
func DialWS(ctx context.Context, url string) (*RPCClient, error) {
	rc, err := rpc.DialContext(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("indexer: dial ws %s: %w", RedactURL(url), err)
	}
	return NewRPCClient(ctx, rc)
}

// DialHTTP ...
func DialHTTP(ctx context.Context, url string) (*RPCClient, error) {
	rc, err := rpc.DialContext(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("indexer: dial http %s: %w", RedactURL(url), err)
	}
	return NewRPCClient(ctx, rc)
}

// redactURL 防止 RPC URL 里的 query token 泄露到日志。
// 当前实现只保留 scheme + host，丢弃 path / query。
func redactURL(raw string) string { return RedactURL(raw) }

// RedactURL 同 redactURL，导出供其它包调用（如 cmd/worker）。
func RedactURL(raw string) string {
	if raw == "" {
		return ""
	}
	// 避免引入 net/url：粗略匹配 scheme://host[:port][/path]?query。
	end := len(raw)
	scheme := 0
	for i := 0; i < len(raw)-2; i++ {
		if raw[i] == ':' && raw[i+1] == '/' && raw[i+2] == '/' {
			scheme = i + 3
			break
		}
	}
	if scheme == 0 {
		return raw
	}
	for i := scheme; i < len(raw); i++ {
		if raw[i] == '/' || raw[i] == '?' {
			end = i
			break
		}
	}
	return raw[:end]
}
