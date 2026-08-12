//go:build integration

package certificate

import (
	"context"

	"github.com/google/uuid"
)

// HandleJobForTest 直接调用 handleJob，绕过 claim 阶段。
//
// 仅 integration test 用（//go:build integration 守护）。
// 真实 Run 走 runOnce 的 SKIP LOCKED 声明路径。
func (c *Consumer) HandleJobForTest(ctx context.Context, jobID, certUUID uuid.UUID, prevAttempt int) {
	c.handleJob(ctx, jobID, certUUID, prevAttempt)
}

// RunOnceForTest 直接调用 runOnce，触发单轮 claim + process。
//
// 仅 integration test 用。
func (c *Consumer) RunOnceForTest(ctx context.Context) {
	c.runOnce(ctx)
}

// ResetStaleMintingsForTest 把超过 staleness 的 minting 行重置为 pending，
// 让下一轮 poll 重新 claim；用于「crash recovery」测试。
//
// 仅 integration test 用。
func (c *Consumer) ResetStaleMintingsForTest(ctx context.Context, olderThan string) (int64, error) {
	tag, err := c.cfg.Pool.Exec(ctx, `
UPDATE certificate_jobs
SET status='pending', next_retry_at=now(), updated_at=now()
WHERE status='minting' AND started_at IS NOT NULL
  AND started_at < now() - $1::interval`, olderThan)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}