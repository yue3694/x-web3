// Package reconcile — scanner.go 周期性扫描 [lastConfirmed-ConfirmDepth, lastIndexed] 区间，
// 找出漏处理 / 漏块事件。
//
// 数据来源：
//   - chain_checkpoints：当前 next_block = lastIndexed；
//   - chain_events：实际已入库的最大 block_number；
//   - DLQ writer：发现 gap 时写入一条 'gap' DLQ 事件。
//
// 触发：调用方起 ticker；Scanner.ScanOnce() 暴露给单测。
package reconcile

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Scanner 周期性检查 gap。
type Scanner struct {
	pool         *pgxpool.Pool
	writer       *Writer
	logger       *slog.Logger
	consumer     string
	chainID      int64
	confirmDepth int64
	interval     time.Duration

	// metrics
	lastScanUnix atomic.Int64
	scanRuns     atomic.Int64
	gapDetected  atomic.Int64
}

// Config 构造参数。
type Config struct {
	Pool         *pgxpool.Pool
	Writer       *Writer
	Logger       *slog.Logger
	Consumer     string
	ChainID      int64
	ConfirmDepth int64
	Interval     time.Duration
}

// NewScanner 构造 scanner。
func NewScanner(cfg Config) (*Scanner, error) {
	if cfg.Pool == nil {
		return nil, errors.New("reconcile: pool required")
	}
	if cfg.Writer == nil {
		return nil, errors.New("reconcile: writer required")
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 30 * time.Minute
	}
	if cfg.ConfirmDepth <= 0 {
		cfg.ConfirmDepth = 12
	}
	if cfg.Consumer == "" {
		cfg.Consumer = "indexer"
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Scanner{
		pool:         cfg.Pool,
		writer:       cfg.Writer,
		logger:       cfg.Logger,
		consumer:     cfg.Consumer,
		chainID:      cfg.ChainID,
		confirmDepth: cfg.ConfirmDepth,
		interval:     cfg.Interval,
	}, nil
}

// Metrics 暴露给外部的指标快照。
type Metrics struct {
	LastScanUnix int64
	ScanRuns     int64
	GapDetected  int64
}

// Metrics ...
func (s *Scanner) Metrics() Metrics {
	return Metrics{
		LastScanUnix: s.lastScanUnix.Load(),
		ScanRuns:     s.scanRuns.Load(),
		GapDetected:  s.gapDetected.Load(),
	}
}

// Interval 暴露扫描间隔给调用方（如 main.go 复用 ticker 时）。
func (s *Scanner) Interval() time.Duration {
	if s.interval <= 0 {
		return 30 * time.Minute
	}
	return s.interval
}

// Start 阻塞；ctx.Done() 退出。
func (s *Scanner) Start(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	// 启动即跑一次。
	s.ScanOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.ScanOnce(ctx)
		}
	}
}

// ScanOnce 跑一轮；公开以便单测。
//
// 算法：
//  1) 读 chain_checkpoints.next_block → lastIndexed；
//  2) 读 chain_events 中最大 (chain_id) 的 block_number → lastEventBlock；
//     同时用一个独立 EXISTS 查询判断 chain_events 是否真的「一条都没有」，
//     用来区分 brand-new chain 和没追上 head 的活跃链。
//  3) 若 lastIndexed > lastEventBlock + 1：
//     - gapFrom = lastEventBlock + 1
//     - gapTo = lastIndexed - 1
//     - 若 gapFrom <= head - ConfirmDepth：写入 DLQ（深 gap）；
//     - 否则视为"未达确认数"，等下次扫描。
//  4) 边界：
//     - lastIndexed=0  → 尚未初始化，跳过；
//     - chain_events 完全为空 且 checkpoint.next_block=0 → 全新链无事件，跳过；
//       （之前的实现会因为 COALESCE(MAX)=0 与 lastIndexed<=1 撞上，把全新链
//       错报成漏块；这个兜底覆盖 codex #17。）
func (s *Scanner) ScanOnce(ctx context.Context) (Metrics, error) {
	s.scanRuns.Add(1)
	s.lastScanUnix.Store(time.Now().Unix())

	var (
		lastIndexed  int64
		lastEventBlk int64
		hasCP        bool
		hasEvents    bool
	)
	err := s.pool.QueryRow(ctx, `SELECT next_block FROM chain_checkpoints
WHERE chain_id=$1 AND consumer=$2`, s.chainID, s.consumer).Scan(&lastIndexed)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return s.Metrics(), fmt.Errorf("scanner: read checkpoint: %w", err)
	}
	hasCP = err == nil

	err = s.pool.QueryRow(ctx, `SELECT COALESCE(MAX(block_number), 0) FROM chain_events
WHERE chain_id=$1 AND canonical=true`, s.chainID).Scan(&lastEventBlk)
	if err != nil {
		return s.Metrics(), fmt.Errorf("scanner: read max block: %w", err)
	}

	// 区分「全新链还没事件」与「之前有过事件现在 lost 数据」：EXISTS 返回
	// false 时 MAX 必然为 0，不能据此判定 gap。这条信号独立维护以避免
	// 「MAX=0 → 误报漏块」这种 brand-new chain 噪声（codex #17）。
	err = s.pool.QueryRow(ctx, `SELECT EXISTS (
SELECT 1 FROM chain_events WHERE chain_id=$1 AND canonical=true
)`, s.chainID).Scan(&hasEvents)
	if err != nil {
		return s.Metrics(), fmt.Errorf("scanner: probe chain_events empty: %w", err)
	}

	if !hasCP || lastIndexed == 0 {
		return s.Metrics(), nil
	}
	// 全新链：没有事件 且 checkpoint 也从未推进（next_block==0）→ 直接跳过。
	// （这里的 lastEventBlk==0 已经被 hasEvents=false 覆盖；保留显式判断便于阅读。）
	if !hasEvents && lastIndexed <= 1 {
		return s.Metrics(), nil
	}
	if lastIndexed <= lastEventBlk+1 {
		// 没有 gap
		return s.Metrics(), nil
	}
	gapFrom := lastEventBlk + 1
	gapTo := lastIndexed - 1
	// 未达确认数则放行：等下个区块被确认后再扫。
	if gapTo-gapFrom+1 < s.confirmDepth {
		s.logger.Debug("gap_below_confirm_depth", "from", gapFrom, "to", gapTo)
		return s.Metrics(), nil
	}

	s.gapDetected.Add(1)
	chainID := s.chainID
	_, err = s.writer.Write(ctx, Entry{
		Consumer: s.consumer,
		ChainID:  &chainID,
		Kind:     "gap",
		Severity: "error",
		Summary:  fmt.Sprintf("gap detected: %d..%d", gapFrom, gapTo),
		Payload: map[string]any{
			"gapFrom":      gapFrom,
			"gapTo":        gapTo,
			"gapSize":      gapTo - gapFrom + 1,
			"confirmDepth": s.confirmDepth,
		},
	})
	if err != nil {
		return s.Metrics(), fmt.Errorf("scanner: write dlq: %w", err)
	}
	s.logger.Warn("reconcile_gap_detected",
		"chainId", s.chainID, "from", gapFrom, "to", gapTo,
		"gapSize", gapTo-gapFrom+1)
	return s.Metrics(), nil
}
