package indexer

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

// TestHandleReorg_RejectsInvalidArgs 校验入参合法性。
func TestHandleReorg_RejectsInvalidArgs(t *testing.T) {
	_, _, err := HandleReorg(context.Background(), nil, ReorgInfo{}, nil)
	if err == nil {
		t.Fatal("expected error for empty chain")
	}
	if !errors.Is(err, err) { // sanity
	}
	_, _, err = HandleReorg(context.Background(), nil, ReorgInfo{ChainID: 1, CommonBlock: -1}, nil)
	if err == nil {
		t.Fatal("expected error for negative block")
	}
}

// TestManualRewind_RejectsInvalidArgs 校验入参。
func TestManualRewind_RejectsInvalidArgs(t *testing.T) {
	_, _, err := ManualRewind(context.Background(), nil, 0, 0, nil, nil)
	if err == nil {
		t.Fatal("expected error for chain=0")
	}
	_, _, err = ManualRewind(context.Background(), nil, 1, -5, nil, nil)
	if err == nil {
		t.Fatal("expected error for fromBlock<0")
	}
}

// TestPGStore_LoadReturnsNotFound 校验未初始化时返回 ErrCheckpointNotFound。
func TestPGStore_LoadReturnsNotFound(t *testing.T) {
	// 跳过：无 pool（用 integration 环境跑）。保留函数以便 build tag 切到
	// integration build 时跑全链路。
	if !hasIntegrationDB(t) {
		t.Skip("integration db not available; set DATABASE_URL_TEST or INTEGRATION_USE_TC=1")
	}
	pool := integrationPool(t)
	defer pool.Close()
	store := NewPGCheckpointStore(pool)
	_, err := store.Load(context.Background(), 999999, "no-such-consumer")
	if !errors.Is(err, ErrCheckpointNotFound) {
		t.Fatalf("expected ErrCheckpointNotFound, got %v", err)
	}
}

// TestPGStore_RoundTrip 写入 → 读取。
func TestPGStore_RoundTrip(t *testing.T) {
	if !hasIntegrationDB(t) {
		t.Skip("integration db not available")
	}
	pool := integrationPool(t)
	defer pool.Close()
	store := NewPGCheckpointStore(pool)
	chainID := int64(11155111)
	consumer := "test-" + uuid.NewString()
	if err := store.Save(context.Background(), &Checkpoint{
		ChainID:      chainID,
		Consumer:     consumer,
		NextBlock:    12345,
		LastBlockHash: []byte{0x01, 0x02, 0x03, 0x04},
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := store.Load(context.Background(), chainID, consumer)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.NextBlock != 12345 {
		t.Errorf("next_block = %d, want 12345", got.NextBlock)
	}
	if len(got.LastBlockHash) != 4 {
		t.Errorf("last_block_hash len = %d, want 4", len(got.LastBlockHash))
	}
	_ = store.Reset(context.Background(), chainID, consumer, 100)
	got, _ = store.Load(context.Background(), chainID, consumer)
	if got.NextBlock != 100 {
		t.Errorf("after reset next_block = %d, want 100", got.NextBlock)
	}
	if got.LastBlockHash != nil {
		t.Errorf("after reset last_block_hash should be nil, got %x", got.LastBlockHash)
	}
	// cleanup
	_, _ = pool.Exec(context.Background(), `DELETE FROM chain_checkpoints WHERE consumer=$1`, consumer)
}

// hasIntegrationDB 检查 DATABASE_URL_TEST 或 INTEGRATION_USE_TC 是否提供。
func hasIntegrationDB(t *testing.T) bool {
	t.Helper()
	return integrationDBEnabled()
}
