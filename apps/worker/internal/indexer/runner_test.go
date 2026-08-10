package indexer

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"math/big"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/jackc/pgx/v5/pgxpool"

	workerorder "github.com/x-web3/worker/internal/order"
)

// fakeClient 是 indexer.Client 的测试替身：可注入 head / logs 序列 + 错误。
type fakeClient struct {
	mu sync.Mutex
	chainID *big.Int
	headers map[int64]*Header
	logs    map[uint64][]types.Log
	headerErr error
	filterErr error
	closeCount atomic.Int32
}

func newFakeClient(chainID int64) *fakeClient {
	return &fakeClient{
		chainID: big.NewInt(chainID),
		headers: map[int64]*Header{},
		logs:    map[uint64][]types.Log{},
	}
}

func (f *fakeClient) setHeader(n int64, h *Header) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.headers[n] = h
}

func (f *fakeClient) setLogs(block uint64, logs []types.Log) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.logs[block] = logs
}

func (f *fakeClient) ChainID(_ context.Context) (*big.Int, error) {
	return f.chainID, nil
}

func (f *fakeClient) HeaderByNumber(_ context.Context, number *big.Int) (*Header, error) {
	if f.headerErr != nil {
		return nil, f.headerErr
	}
	if number == nil {
		// latest：返回 headers 里的最大号
		f.mu.Lock()
		defer f.mu.Unlock()
		var max int64 = -1
		for n := range f.headers {
			if n > max {
				max = n
			}
		}
		if max < 0 {
			return nil, errors.New("no headers")
		}
		return f.headers[max], nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	h, ok := f.headers[number.Int64()]
	if !ok {
		return nil, ethereum.NotFound
	}
	return h, nil
}

func (f *fakeClient) BlockNumber(_ context.Context) (uint64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var max int64 = -1
	for n := range f.headers {
		if n > max {
			max = n
		}
	}
	if max < 0 {
		return 0, errors.New("no headers")
	}
	return uint64(max), nil
}

func (f *fakeClient) FilterLogs(_ context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
	if f.filterErr != nil {
		return nil, f.filterErr
	}
	if q.FromBlock == nil || q.ToBlock == nil {
		return nil, errors.New("from/to required")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []types.Log{}
	for b := q.FromBlock.Uint64(); b <= q.ToBlock.Uint64(); b++ {
		out = append(out, f.logs[b]...)
	}
	return out, nil
}

func (f *fakeClient) SubscribeNewHead(_ context.Context) (HeadSub, error) {
	return nil, errors.New("ws not supported in fake")
}

func (f *fakeClient) Close() {
	f.closeCount.Add(1)
}

func newDiscardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// memCheckpointStore 内存版 CheckpointStore，用于单测。
type memCheckpointStore struct {
	mu sync.Mutex
	m  map[string]*Checkpoint
}

func newMemCheckpointStore() *memCheckpointStore {
	return &memCheckpointStore{m: map[string]*Checkpoint{}}
}

func (s *memCheckpointStore) key(chainID int64, consumer string) string {
	return mkKey(chainID, consumer)
}

func mkKey(chainID int64, consumer string) string {
	return itoa(chainID) + "|" + consumer
}

func (s *memCheckpointStore) Load(_ context.Context, chainID int64, consumer string) (*Checkpoint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp, ok := s.m[s.key(chainID, consumer)]
	if !ok {
		return nil, ErrCheckpointNotFound
	}
	return cp, nil
}

func (s *memCheckpointStore) Save(_ context.Context, cp *Checkpoint) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp2 := *cp
	if cp2.LastBlockHash != nil {
		cp2.LastBlockHash = append([]byte(nil), cp2.LastBlockHash...)
	}
	s.m[s.key(cp.ChainID, cp.Consumer)] = &cp2
	return nil
}

func (s *memCheckpointStore) Reset(_ context.Context, chainID int64, consumer string, fromBlock int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[s.key(chainID, consumer)] = &Checkpoint{
		ChainID:   chainID,
		Consumer:  consumer,
		NextBlock: fromBlock,
	}
	return nil
}

func itoa(n int64) string {
	// 避免 strconv 依赖；只用于测试 key
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// stubConfirmer 记录 Apply 调用。
type stubConfirmer struct {
	mu     sync.Mutex
	calls  []workerorder.ApplyInput
	states []string
	err    error
}

func (s *stubConfirmer) Apply(_ context.Context, in workerorder.ApplyInput) (any, string, error) {
	s.mu.Lock()
	s.calls = append(s.calls, in)
	s.mu.Unlock()
	if s.err != nil {
		return nil, "failed", s.err
	}
	return nil, "confirmed", nil
}

func (s *stubConfirmer) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

func mkHeader(num int64, hashByte byte) *Header {
	var h common.Hash
	h[0] = hashByte
	return &Header{
		Number: big.NewInt(num),
		Hash:   h,
	}
}

// TestRunner_PollingCycleAdvancesCheckpoint 验证：每跑一次 cycle，checkpoint 从 next 推到 next+1。
func TestRunner_PollingCycleAdvancesCheckpoint(t *testing.T) {
	cl := newFakeClient(11155111)
	cl.setHeader(100, mkHeader(100, 0xAA))
	cl.setHeader(101, mkHeader(101, 0xBB))
	store := newMemCheckpointStore()
	confirmer := &stubConfirmer{}
	pool := indexerRPCPool(t, cl)

	cfg := Config{
		ChainID:         11155111,
		Consumer:        "test",
		ConfirmDepth:    5,
		PollInterval:    50 * time.Millisecond,
		HealthWindow:    100 * time.Millisecond,
		BatchSize:       1000,
		SubscribeTimeout: time.Second,
		Logger:          newDiscardLogger(),
		Decoder:         noopDecoder{},
		Confirmer:       confirmer,
		CheckpointStore: store,
		RPCPool:         pool,
		Metrics:         &Metrics{},
	}
	r, err := NewRunner(cfg)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	// 手动跑一次 cycle 避免等 ticker。
	if err := r.runCycle(context.Background()); err != nil {
		t.Fatalf("runCycle: %v", err)
	}
	cp, err := store.Load(context.Background(), 11155111, "test")
	if err != nil {
		t.Fatalf("Load cp: %v", err)
	}
	// 0..96 (safeHead=101-5) → checkpoint 推到 97
	wantNext := int64(97)
	if cp.NextBlock != wantNext {
		t.Errorf("next_block = %d, want %d", cp.NextBlock, wantNext)
	}
}

// TestRunner_RPCPoolFallbackOnError 验证：主 RPC 失败时，pool 切到备用。
func TestRunner_RPCPoolFallbackOnError(t *testing.T) {
	primary := newFakeClient(1)
	primary.headerErr = errors.New("boom")
	secondary := newFakeClient(1)
	secondary.setHeader(50, mkHeader(50, 0x11))
	store := newMemCheckpointStore()
	confirmer := &stubConfirmer{}
	pool := NewRPCPool([]Client{primary, secondary}, newDiscardLogger(), &Metrics{})

	cfg := Config{
		ChainID:         1,
		Consumer:        "t",
		ConfirmDepth:    1, // 显式 > 0，避免 default 12 覆盖
		PollInterval:    50 * time.Millisecond,
		HealthWindow:    200 * time.Millisecond,
		Logger:          newDiscardLogger(),
		Decoder:         noopDecoder{},
		Confirmer:       confirmer,
		CheckpointStore: store,
		RPCPool:         pool,
		Metrics:         &Metrics{},
	}
	r, err := NewRunner(cfg)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	// 第一次 cycle 失败（primary unhealthy），第二次切到 secondary。
	if err := r.runCycle(context.Background()); err == nil {
		t.Fatal("expected error from primary")
	}
	if err := r.runCycle(context.Background()); err != nil {
		t.Fatalf("second runCycle: %v", err)
	}
	// secondary 应当被选中 → 拉到了 head=50 → ConfirmDepth=1 → to=49 → checkpoint=50
	cp, err := store.Load(context.Background(), 1, "t")
	if err != nil {
		t.Fatalf("Load cp: %v", err)
	}
	if cp.NextBlock != 50 {
		t.Errorf("next_block = %d, want 50", cp.NextBlock)
	}
	// 切回时 metrics +1
	if got := pool.metrics.RPCSwapEvents.Load(); got < 1 {
		t.Errorf("RPCSwapEvents = %d, want >= 1", got)
	}
}

// TestRunner_DecodeTriggersApply 验证：raw log 经过 Decoder → Apply 链路。
func TestRunner_DecodeTriggersApply(t *testing.T) {
	cl := newFakeClient(1)
	// head 必须 > ConfirmDepth + logBlock；让 log 进入 [0..head-ConfirmDepth] 范围。
	cl.setHeader(15, mkHeader(15, 0xCC))
	// 注入一条 dummy log 在 block 10
	cl.setLogs(10, []types.Log{{
		BlockNumber: 10,
		BlockHash:   common.HexToHash("0xcc"),
		TxHash:      common.HexToHash("0x01"),
		Index:       0,
	}})
	store := newMemCheckpointStore()
	confirmer := &stubConfirmer{}
	pool := indexerRPCPool(t, cl)

	dec := &countingDecoder{shouldApply: true}
	cfg := Config{
		ChainID:         1,
		Consumer:        "t",
		ConfirmDepth:    1, // 显式 > 0
		PollInterval:    10 * time.Millisecond,
		HealthWindow:    50 * time.Millisecond,
		Logger:          newDiscardLogger(),
		Decoder:         dec,
		Confirmer:       confirmer,
		CheckpointStore: store,
		RPCPool:         pool,
		Metrics:         &Metrics{},
	}
	r, err := NewRunner(cfg)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	if err := r.runCycle(context.Background()); err != nil {
		t.Fatalf("runCycle: %v", err)
	}
	// 应用是异步的；等一下让 goroutine 跑完。
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if confirmer.callCount() > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := confirmer.callCount(); got != 1 {
		t.Errorf("confirmer calls = %d, want 1", got)
	}
	if dec.decodeCalls.Load() != 1 {
		t.Errorf("decoder calls = %d, want 1", dec.decodeCalls.Load())
	}
}

// TestRunner_GracefulShutdownDrainsInFlight 验证：ctx cancel 时 in-flight 应用被等完。
func TestRunner_GracefulShutdownDrainsInFlight(t *testing.T) {
	cl := newFakeClient(1)
	cl.setHeader(15, mkHeader(15, 0xDD))
	cl.setLogs(10, []types.Log{{BlockNumber: 10, TxHash: common.HexToHash("0x1"), Index: 0}})
	store := newMemCheckpointStore()
	started := make(chan struct{}, 4)
	release := make(chan struct{})
	confirmer := &slowConfirmer{started: started, release: release}
	pool := indexerRPCPool(t, cl)

	cfg := Config{
		ChainID:         1,
		Consumer:        "t",
		ConfirmDepth:    1, // 显式 > 0
		PollInterval:    10 * time.Millisecond,
		HealthWindow:    50 * time.Millisecond,
		Logger:          newDiscardLogger(),
		Decoder:         &countingDecoder{shouldApply: true},
		Confirmer:       confirmer,
		CheckpointStore: store,
		RPCPool:         pool,
		Metrics:         &Metrics{},
	}
	r, err := NewRunner(cfg)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// 第一次 runCycle 触发 Apply（异步）。
	if err := r.runCycle(ctx); err != nil {
		t.Fatalf("runCycle: %v", err)
	}
	// 等 in-flight 进入 Apply
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("in-flight apply did not start")
	}
	// 取消 ctx → 等待退出（Wait 应该等 in-flight 完成）
	cancel()
	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()
	close(release) // 释放 slowConfirmer
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("in-flight did not drain within 2s")
	}
}

// TestRunner_StopsWhenNoRPC 验证：pool 空时 NewRunner 报错。
func TestRunner_StopsWhenNoRPC(t *testing.T) {
	_, err := NewRunner(Config{
		ChainID:         1,
		Consumer:        "t",
		CheckpointStore: newMemCheckpointStore(),
		RPCPool:         NewRPCPool(nil, nil, nil),
	})
	if err == nil {
		t.Error("expected error for empty RPCPool")
	}
}

// TestRedactURL_StripsPathAndQuery 验证：URL 脱敏只保留 scheme://host。
func TestRedactURL_StripsPathAndQuery(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"https://example.com", "https://example.com"},
		{"https://example.com:443", "https://example.com:443"},
		{"wss://alchemy.example/ws/v2/secret?token=abc", "wss://alchemy.example"},
		{"http://localhost:8545", "http://localhost:8545"},
		{"http://[::1]:8545", "http://[::1]:8545"},
	}
	for _, c := range cases {
		if got := RedactURL(c.in); got != c.want {
			t.Errorf("RedactURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestRPCPool_PrimarySwap 验证：主 client 不健康 → 切到下一个。
func TestRPCPool_PrimarySwap(t *testing.T) {
	a := newFakeClient(1)
	b := newFakeClient(1)
	pool := NewRPCPool([]Client{a, b}, newDiscardLogger(), &Metrics{})
	if pool.Primary(time.Now()) != a {
		t.Fatal("first primary should be a")
	}
	pool.MarkUnhealthy(a, 10*time.Second)
	got := pool.Primary(time.Now())
	if got != b {
		t.Fatalf("after mark unhealthy, primary should be b, got %v", got)
	}
}

// ---------- helpers used in tests ----------

func indexerRPCPool(t *testing.T, c Client) *RPCPool {
	t.Helper()
	return NewRPCPool([]Client{c}, newDiscardLogger(), &Metrics{})
}

// countingDecoder 计数 + 可选 apply。
type countingDecoder struct {
	shouldApply bool
	decodeCalls atomic.Int32
}

func (d *countingDecoder) Decode(_ context.Context, _ int64, _ *Header, _ LogRecord) ([]workerorder.ApplyInput, bool, error) {
	d.decodeCalls.Add(1)
	if !d.shouldApply {
		return nil, true, nil
	}
	// 生成一条伪 ApplyInput（orderID 留空 → Confirmer 仍会接收，但 Apply 不会成功。
	// 这里只关心"被调用"次数，不关心业务结果。
	return []workerorder.ApplyInput{{
		ChainID:     1,
		BlockNumber: 10,
		LogIndex:    0,
	}}, false, nil
}

// slowConfirmer 阻塞直到 release 信号。
type slowConfirmer struct {
	started chan struct{}
	release chan struct{}
}

func (s *slowConfirmer) Apply(ctx context.Context, _ workerorder.ApplyInput) (any, string, error) {
	select {
	case s.started <- struct{}{}:
	default:
	}
	select {
	case <-s.release:
	case <-ctx.Done():
		return nil, "", ctx.Err()
	}
	return nil, "confirmed", nil
}

// avoid unused import of pgxpool in this file.
var _ = (*pgxpool.Pool)(nil)
