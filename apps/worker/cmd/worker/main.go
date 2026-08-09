// Command worker runs asynchronous chain-indexing and certificate jobs.
//
// 当前实现的子系统：
//   - Order Confirmer：消费外部投递（HTTP / 或 in-process channel）的链事件，
//     入库 chain_events → 推进 orders → 派生 enrollments → 写 outbox_events。
//
// 后续接入（待 F07 infra）：
//   - WS 监听 + HTTP 回扫
//   - reorg 检测 + checkpoint 推进
//   - 漏块对账（reconcile）
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	workerorder "github.com/x-web3/worker/internal/order" // package is workerorder
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		logger.Error("DATABASE_URL not set; exiting")
		os.Exit(1)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		logger.Error("pgx_connect_failed", "err", err.Error())
		os.Exit(1)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		logger.Error("pg_ping_failed", "err", err.Error())
		os.Exit(1)
	}
	confirmer := workerorder.NewConfirmer(pool)

	// 简易内存 channel；生产环境会替换为 ethclient.WS 监听或 HTTP inbox。
	var (
		mu   sync.Mutex
		pend []workerorder.ApplyInput
	)
	if err := loadPendingFromOutbox(ctx, pool, &mu); err != nil {
		logger.Warn("load_pending_failed", "err", err.Error())
	}

	logger.Info("worker_started", "mode", "confirmer-only", "pending", len(pend))
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			logger.Info("worker_stopped")
			return
		case <-tick.C:
			mu.Lock()
			batch := pend
			pend = nil
			mu.Unlock()
			for _, in := range batch {
				enrollID, state, err := confirmer.Apply(ctx, in)
				if err != nil {
					logger.Error("apply_failed", "err", err.Error(), "orderId", in.OrderID.String())
					mu.Lock()
					pend = append(pend, in)
					mu.Unlock()
					continue
				}
				logger.Info("apply_ok",
					"orderId", in.OrderID.String(),
					"state", state,
					"enrollmentId", enrollID.String(),
				)
			}
		}
	}
}

// loadPendingFromOutbox 暂为空实现：MVP 阶段不持久化 pending 队列，
// 所有 apply 同步执行；后续接 WS / inbox 时再补。
//
// 函数保留用于显式"测试可注入 pending"的入口。
func loadPendingFromOutbox(_ context.Context, _ *pgxpool.Pool, _ *sync.Mutex) error {
	if v := os.Getenv("WORKER_DEBUG_PENDING"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			// 用于本地手测：往 pend 注入 n 条空记录（不会落库）。
		}
		_ = errors.New("no-op")
	}
	return nil
}
