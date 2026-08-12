//go:build integration

// F03-T13：reconcile.Scanner 漏块扫描集成测试。
//
// 覆盖 DoD #5：worker 因 RPC 抖动 / WS 断 / 进程 crash 错过了
// [lastEventBlock+1, checkpoint.next_block-1] 区间的事件；下次启动
// 后 reconcile.ScanningLoop（Scanner.ScanOnce）应当识别为深 gap 并
// 写一条 DLQ（severity=error, kind=gap）。
//
// 入参：DATABASE_URL_TEST（已应用 0007_reorg_reconcile migration）。
//
// 跑法：go test -tags integration -run TestScanner_CatchesMissedBlock ./internal/reconcile/
package reconcile

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	scannerChainID = int64(11155111)
	scannerTestID  = "scanner-dod-test"
)

func scannerItPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL_TEST")
	if dsn == "" {
		t.Skip("DATABASE_URL_TEST not set; scanner integration test skipped")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		t.Skipf("pg ping failed: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// freshScanner 隔离测试用 consumer + cleanup。
func freshScanner(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	consumer := scannerTestID + "-" + uuid.NewString()
	ctx := context.Background()
	if _, err := pool.Exec(ctx,
		`DELETE FROM chain_checkpoints WHERE consumer=$1`, consumer); err != nil {
		t.Fatalf("reset cp: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`DELETE FROM dlq_events WHERE consumer=$1`, consumer); err != nil {
		t.Fatalf("reset dlq: %v", err)
	}
	// 同时清理其他测试可能插入的 (chain_id=scannerChainID) 干扰数据。
	if _, err := pool.Exec(ctx,
		`DELETE FROM chain_events WHERE chain_id=$1 AND tx_hash LIKE decode('f0f0f0','hex') || '%'`,
		scannerChainID); err != nil {
		t.Fatalf("reset events: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM chain_checkpoints WHERE consumer=$1`, consumer)
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM dlq_events WHERE consumer=$1`, consumer)
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM chain_events WHERE chain_id=$1 AND tx_hash LIKE decode('f0f0f0','hex') || '%'`,
			scannerChainID)
	})
	return consumer
}

// seedEvent 插入 canonical=true 的 chain_event（用 f0f0f0 前缀便于 cleanup）。
func seedScannerEvent(t *testing.T, pool *pgxpool.Pool, block int64, logIdx int) {
	t.Helper()
	txHash := make([]byte, 32)
	for i := range txHash {
		txHash[i] = 0xF0
	}
	txHash[3] = byte(block)
	txHash[4] = byte(logIdx)
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO chain_events(chain_id, tx_hash, log_index, block_number, block_hash, event_signature, payload, canonical)
VALUES($1, $2, $3, $4, decode('01','hex'), decode('00','hex'), '{}'::jsonb, true)
ON CONFLICT (chain_id, tx_hash, log_index) DO UPDATE SET canonical=true, block_number=EXCLUDED.block_number`,
		scannerChainID, txHash, logIdx, block); err != nil {
		t.Fatalf("seed event: %v", err)
	}
}

// seedCheckpoint 写入 chain_checkpoints。
func seedScannerCheckpoint(t *testing.T, pool *pgxpool.Pool, consumer string, nextBlock int64) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
INSERT INTO chain_checkpoints(chain_id, consumer, next_block, last_block_hash)
VALUES($1, $2, $3, NULL)
ON CONFLICT (chain_id, consumer) DO UPDATE SET next_block=EXCLUDED.next_block, last_block_hash=NULL`,
		scannerChainID, consumer, nextBlock); err != nil {
		t.Fatalf("seed checkpoint: %v", err)
	}
}

// countUnresolvedGapDLQ 统计测试 consumer 写出的 kind='gap' DLQ 行数。
func countUnresolvedGapDLQ(t *testing.T, pool *pgxpool.Pool, consumer string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), `
SELECT count(*) FROM dlq_events
WHERE consumer=$1 AND kind='gap' AND resolved=false`,
		consumer).Scan(&n); err != nil {
		t.Fatalf("count dlq: %v", err)
	}
	return n
}

// TestScanner_CatchesMissedBlock 覆盖 DoD #5：
//   - checkpoint 推到 110，chain_events 仅有 block 100（被确认）→
//     101..109 都已错过（gap=9, 超过 ConfirmDepth=3）。
//   - Scanner.ScanOnce 应识别为深 gap 并写 DLQ。
//   - 控制用例：未达 ConfirmDepth 的浅 gap 不写 DLQ。
func TestScanner_CatchesMissedBlock(t *testing.T) {
	pool := scannerItPool(t)
	consumer := freshScanner(t, pool)
	ctx := context.Background()

	// 深 gap case：next_block=110，最后一个已 confirm 的 event=block 100。
	// ConfirmDepth=3，gap=9 > 3 → 应写 DLQ。
	seedScannerEvent(t, pool, 100, 0)
	seedScannerCheckpoint(t, pool, consumer, 110)
	// 注意：Scanner 把 consumer 写进 dlq_events.consumer；Writer 会校验
	// Consumer 非空。链 ID 通过 Entry.ChainID 写入 payload。
	chainID := scannerChainID
	w := NewWriter(&pgDLQWriterPool{pool: pool}, newDiscardLogger())
	s := &Scanner{
		pool:         pool,
		writer:       w,
		logger:       newDiscardLogger(),
		consumer:     consumer,
		chainID:      scannerChainID,
		confirmDepth: 3,
		interval:     0,
	}
	if _, err := s.ScanOnce(ctx); err != nil {
		t.Fatalf("ScanOnce: %v", err)
	}
	if got := countUnresolvedGapDLQ(t, pool, consumer); got != 1 {
		t.Errorf("dlq rows after deep gap = %d, want 1", got)
	}
	if got := s.Metrics().GapDetected; got != 1 {
		t.Errorf("GapDetected = %d, want 1", got)
	}
	if got := s.Metrics().ScanRuns; got < 1 {
		t.Errorf("ScanRuns = %d, want >= 1", got)
	}
	// cleanup dlq 后跑浅 gap case：next_block=102, 最后一个 event=100。
	// gap = 1, < ConfirmDepth=3 → 不应写 DLQ。
	if _, err := pool.Exec(ctx, `DELETE FROM dlq_events WHERE consumer=$1`, consumer); err != nil {
		t.Fatalf("clear dlq: %v", err)
	}
	seedScannerCheckpoint(t, pool, consumer, 102)
	// 重置 scanner 内部指标以便独立断言。
	s2 := &Scanner{
		pool:         pool,
		writer:       w,
		logger:       newDiscardLogger(),
		consumer:     consumer,
		chainID:      scannerChainID,
		confirmDepth: 3,
	}
	before := s2.Metrics().GapDetected
	if _, err := s2.ScanOnce(ctx); err != nil {
		t.Fatalf("ScanOnce shallow: %v", err)
	}
	if got := countUnresolvedGapDLQ(t, pool, consumer); got != 0 {
		t.Errorf("dlq rows after shallow gap = %d, want 0 (below ConfirmDepth)", got)
	}
	if got := s2.Metrics().GapDetected; got != before {
		t.Errorf("GapDetected = %d, want unchanged (no new gap below confirm depth)", got)
	}

	// 边界：fresh chain，无 event 且 next_block=0 → no-op。
	emptyConsumer := scannerTestID + "-empty-" + uuid.NewString()
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM chain_checkpoints WHERE consumer=$1`, emptyConsumer)
	})
	if _, err := pool.Exec(ctx,
		`DELETE FROM chain_checkpoints WHERE consumer=$1`, emptyConsumer); err != nil {
		t.Fatalf("reset empty cp: %v", err)
	}
	// 这里 Scanner 用 fresh consumer 测，next_block 仍 0 → 早返回。
	// 但前一个 case 已写 next=102；让本 case 用全新 consumer。
	s3 := &Scanner{
		pool:         pool,
		writer:       w,
		logger:       newDiscardLogger(),
		consumer:     emptyConsumer,
		chainID:      scannerChainID,
		confirmDepth: 3,
	}
	if _, err := s3.ScanOnce(ctx); err != nil {
		t.Fatalf("ScanOnce empty: %v", err)
	}
	if got := s3.Metrics().GapDetected; got != 0 {
		t.Errorf("empty chain GapDetected = %d, want 0 (skipped)", got)
	}
	// 防 unused
	_ = chainID
}

// pgDLQWriterPool 把 *pgxpool.Pool 包成 reconcile.DLQStore，让 Scanner 走真实 PG。
type pgDLQWriterPool struct{ pool *pgxpool.Pool }

func (p *pgDLQWriterPool) Write(_ context.Context, e Entry) (int64, error) {
	return NewPGDLQStore(p.pool).Write(context.Background(), e)
}
func (p *pgDLQWriterPool) ListUnresolved(_ context.Context, limit int) ([]DLQRow, error) {
	return NewPGDLQStore(p.pool).ListUnresolved(context.Background(), limit)
}
func (p *pgDLQWriterPool) Get(_ context.Context, id int64) (*DLQRow, error) {
	return NewPGDLQStore(p.pool).Get(context.Background(), id)
}
func (p *pgDLQWriterPool) MarkResolved(_ context.Context, id int64, resolver uuid.UUID, resolution string) error {
	return NewPGDLQStore(p.pool).MarkResolved(context.Background(), id, resolver, resolution)
}
func (p *pgDLQWriterPool) IncrementRetry(_ context.Context, id int64) (int, error) {
	return NewPGDLQStore(p.pool).IncrementRetry(context.Background(), id)
}
