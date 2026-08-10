// Package admin 实现 /admin/* 路由的 handler 集合。
//
// 当前切片：F03-T12 / F03-T13 所需的两个 endpoint：
//   - POST /admin/chain/rewind        手动 rewind（SYSTEM_ADMIN + audit）
//   - GET  /admin/dlq                  列出 unresolved DLQ
//   - POST /admin/dlq/{id}/retry       标记 re-enqueue（SYSTEM_ADMIN + audit）
//
// 复用规范：
//   - 走现有 audit.Writer 留痕；
//   - 走 rbac.Middleware(PermSystemAdmin / PermChainSyncReplay)；
//   - 走 errcode + httpkit.Error 统一错误格式；
//   - main.go 装载：v1.Group("/admin").Use(auth.Middleware(...), rbac.Middleware(PermSystemAdmin))。
//
// 锁契约（与 worker/indexer.HandleReorg / ManualRewind 一致）：
//   - mirrorManualRewind 在事务内对 chain_checkpoints(chain_id, 'indexer') 拿
//     SELECT ... FOR UPDATE；这样 admin 手动 rewind 与 worker 自动 reorg 检测
//     互斥。详见 apps/worker/internal/indexer/reorg.go。
//   - 锁在事务 commit/rollback 时释放；handler 不要持锁发 RPC。
package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/x-web3/api/internal/audit"
	"github.com/x-web3/api/internal/errcode"
	"github.com/x-web3/api/internal/httpkit"
	"github.com/x-web3/api/internal/rbac"
	"github.com/x-web3/api/internal/user"
)

// ChainRewindHandler 暴露 chain rewind 入口。
type ChainRewindHandler struct {
	pool     *pgxpool.Pool
	auditor  *audit.Writer
	rbac     *rbac.Engine
	logger   *zap.Logger
}

// NewChainRewindHandler 构造 handler。
func NewChainRewindHandler(pool *pgxpool.Pool, auditor *audit.Writer, rbac *rbac.Engine, logger *zap.Logger) *ChainRewindHandler {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &ChainRewindHandler{pool: pool, auditor: auditor, rbac: rbac, logger: logger}
}

// rewindRequest 是入参。
type rewindRequest struct {
	ChainID   int64 `json:"chainId"   binding:"required"`
	FromBlock int64 `json:"fromBlock" binding:"required"`
	Reason    string `json:"reason"`
}

// rewindConsumer 锁定 chain_checkpoints 时使用的 consumer 标识。
//
// 必须与 worker/indexer.HandleReorg / ManualRewind / runner.Config.Consumer
// 默认值一致（"indexer"），才能拿到同一行的 SELECT ... FOR UPDATE。
const rewindConsumer = "indexer"

// PostRewind 实现 POST /admin/chain/rewind。
//
// 流程：
//  1) 解析 JSON + 校验 fromBlock >= 0；
//  2) 二次 rbac 校验；
//  3) 先写一条 audit row（status=attempted），保证被拒 / 失败请求也可追溯；
//  4) mirrorManualRewind（事务内锁 chain_checkpoints + reorg + 复位）；
//  5) audit row 追加（after=stats, status=succeeded/failed）。
//
// 当前实现是同步阻塞；如未来扫描成本高，可改成 outbox+worker async。
func (h *ChainRewindHandler) PostRewind(c *httpkit.Context) {
	uid, err := userIDFromCtx(c)
	if err != nil {
		httpkit.Error(c, http.StatusUnauthorized, errcode.SessionExpired, "no session", nil)
		return
	}
	var req rewindRequest
	if !c.MustJSON(&req) {
		return
	}
	if req.FromBlock < 0 {
		httpkit.Error(c, http.StatusBadRequest, errcode.BadRequest, "fromBlock must be non-negative", nil)
		return
	}
	// 二次 rbac 校验（中间件已挂，但保留 defense in depth）。
	if err := h.rbac.Require(user.PermChainSyncReplay)(c.Request.Context(), uid); err != nil {
		// 即便被拒也留 audit 痕迹 — denied attempts visible。
		_ = h.auditor.Log(c.Request.Context(), audit.Entry{
			ActorUserID:   &uid,
			Action:        audit.ActionChainReplayed,
			TargetType:    "chain",
			TargetID:      strconv.FormatInt(req.ChainID, 10),
			Before:        map[string]any{"fromBlock": req.FromBlock, "reason": req.Reason},
			After:         map[string]any{"status": "denied"},
			IP:            c.ClientIP(),
			UserAgent:     c.Request.UserAgent(),
			CorrelationID: c.RequestID(),
		})
		httpkit.Error(c, http.StatusForbidden, errcode.Forbidden, "permission denied", nil)
		return
	}
	if req.Reason == "" {
		req.Reason = "admin manual rewind"
	}
	actor := uid
	// 在调用 rewind 之前先写一条 audit row（status=attempted），
	// 这样 rewind 失败时仍能看到"谁尝试过"的痕迹 — denied attempts visible。
	_ = h.auditor.Log(c.Request.Context(), audit.Entry{
		ActorUserID:   &actor,
		Action:        audit.ActionChainReplayed,
		TargetType:    "chain",
		TargetID:      strconv.FormatInt(req.ChainID, 10),
		Before:        map[string]any{"fromBlock": req.FromBlock, "reason": req.Reason},
		After:         map[string]any{"status": "attempted"},
		IP:            c.ClientIP(),
		UserAgent:     c.Request.UserAgent(),
		CorrelationID: c.RequestID(),
	})

	// 复用 worker 侧的锁契约 + SQL 路径。
	// apps/api 引入 apps/worker 会有 module 依赖问题；为避免 cycle，
	// 这里走 admin 自己的 SQL 路径，但锁契约与 worker 端一致。
	orphaned, affected, err := mirrorManualRewind(c.Request.Context(), h.pool, req.ChainID, req.FromBlock, req.Reason, actor)
	if err != nil {
		h.logger.Error("chain_rewind_failed",
			zap.Int64("chainId", req.ChainID),
			zap.Int64("fromBlock", req.FromBlock),
			zap.Error(err))
		// 失败也写一条 audit row，便于事后追溯（After=status=failed）。
		_ = h.auditor.Log(c.Request.Context(), audit.Entry{
			ActorUserID:   &actor,
			Action:        audit.ActionChainReplayed,
			TargetType:    "chain",
			TargetID:      strconv.FormatInt(req.ChainID, 10),
			Before:        map[string]any{"fromBlock": req.FromBlock, "reason": req.Reason},
			After:         map[string]any{"status": "failed", "error": err.Error()},
			IP:            c.ClientIP(),
			UserAgent:     c.Request.UserAgent(),
			CorrelationID: c.RequestID(),
		})
		httpkit.Error(c, http.StatusInternalServerError, errcode.Internal, "rewind failed", nil)
		return
	}
	// audit 留痕（最终结果）。
	_ = h.auditor.Log(c.Request.Context(), audit.Entry{
		ActorUserID:   &actor,
		Action:        audit.ActionChainReplayed,
		TargetType:    "chain",
		TargetID:      strconv.FormatInt(req.ChainID, 10),
		Before:        map[string]any{"fromBlock": req.FromBlock, "reason": req.Reason},
		After:         map[string]any{"orphanedEvents": orphaned, "affectedOrders": affected, "status": "succeeded"},
		IP:            c.ClientIP(),
		UserAgent:     c.Request.UserAgent(),
		CorrelationID: c.RequestID(),
	})
	c.JSON(http.StatusOK, gin.H{
		"chainId":        req.ChainID,
		"fromBlock":      req.FromBlock,
		"orphanedEvents": orphaned,
		"affectedOrders": affected,
		"rewoundAt":      time.Now().UTC().Format(time.RFC3339),
	})
}

// DLQHandler DLQ 列表 + retry 入口。
type DLQHandler struct {
	store   dlqStore
	auditor *audit.Writer
	rbac    *rbac.Engine
	logger  *zap.Logger
}

// NewDLQHandler 构造 DLQ handler。
func NewDLQHandler(store dlqStore, auditor *audit.Writer, rbac *rbac.Engine, logger *zap.Logger) *DLQHandler {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &DLQHandler{store: store, auditor: auditor, rbac: rbac, logger: logger}
}

// List GET /admin/dlq。
func (h *DLQHandler) List(c *httpkit.Context) {
	limit := 100
	if raw := c.Query("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	items, err := h.store.ListUnresolved(c.Request.Context(), limit)
	if err != nil {
		httpkit.Internal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "count": len(items)})
}

type retryReq struct {
	Resolution string `json:"resolution" binding:"required"`
}

// Retry POST /admin/dlq/{id}/retry。
//
// 当前实现：
//   - 校验 resolution 字段非空；
//   - 原子地执行「MarkResolved + IncrementRetry」：只有当 MarkResolved 真把状态从
//     false→true 翻转后，才递增 retry_count；避免在已 resolved / DB 失败的情形下
//     出现「retry 计数 +1 但 resolved 仍是原状」的不一致；
//   - audit 留痕：按 resolution 分派到三个独立 Action（replayed → ActionDLQRetriedReplay,
//     ignored → ActionDLQRetriedIgnored, manual → ActionDLQRetriedManual），
//     便于直接按 action 字段筛选；resolution 一并写入 After。
//
// "re-enqueue" 的真实实现应再向 outbox_events / indexer 队列写一条；
// 当前 scope 集中在 admin API + 持久化状态变更。
func (h *DLQHandler) Retry(c *httpkit.Context) {
	uid, err := userIDFromCtx(c)
	if err != nil {
		httpkit.Error(c, http.StatusUnauthorized, errcode.SessionExpired, "no session", nil)
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		httpkit.Error(c, http.StatusBadRequest, errcode.BadRequest, "id must be a positive integer", nil)
		return
	}
	var req retryReq
	if !c.MustJSON(&req) {
		return
	}
	// resolution 与 audit.Action 严格对应：避免所有 retry 都打到同一个 Action 上
	// 导致按 action 维度无法区分 replayed/ignored/manual。
	var action audit.Action
	switch req.Resolution {
	case "replayed":
		action = audit.ActionDLQRetriedReplay
	case "ignored":
		action = audit.ActionDLQRetriedIgnored
	case "manual":
		action = audit.ActionDLQRetriedManual
	default:
		httpkit.Error(c, http.StatusBadRequest, errcode.BadRequest,
			"resolution must be one of: replayed, ignored, manual", nil)
		return
	}
	// 关键顺序：先 mark resolved（保证 retry 计数器只在真正 resolve 成功时 +1），
	// 再 inc retry。这样已 resolved / 行不存在的请求会立刻短路返回，
	// retry_count 不会有副作用。底层 PGDLQStore.ResolveAndIncrementRetry
	// 把它做成单条 UPDATE，原子且幂等。
	resolved, err := h.store.ResolveAndIncrementRetry(c.Request.Context(), id, uid, req.Resolution)
	if err != nil {
		if errors.Is(err, errAlreadyResolved) {
			httpkit.Error(c, http.StatusConflict, errcode.Conflict, "dlq already resolved", nil)
			return
		}
		if errors.Is(err, pgx.ErrNoRows) {
			httpkit.Error(c, http.StatusNotFound, errcode.NotFound, "dlq not found", nil)
			return
		}
		httpkit.Internal(c, err)
		return
	}
	if !resolved {
		// 行不存在：返回 404 而不是 500（运维排查更友好）。
		httpkit.Error(c, http.StatusNotFound, errcode.NotFound, "dlq not found", nil)
		return
	}
	_ = h.auditor.Log(c.Request.Context(), audit.Entry{
		ActorUserID:   &uid,
		Action:        action,
		TargetType:    "dlq",
		TargetID:      strconv.FormatInt(id, 10),
		After:         map[string]any{"resolution": req.Resolution},
		IP:            c.ClientIP(),
		UserAgent:     c.Request.UserAgent(),
		CorrelationID: c.RequestID(),
	})
	c.JSON(http.StatusOK, gin.H{
		"id":         id,
		"resolution": req.Resolution,
		"resolvedAt": time.Now().UTC().Format(time.RFC3339),
	})
}

// dlqStore 抽象出 DLQ 操作；admin 包内通过此接口注入，
// 避免与 apps/worker/reconcile 之间的 import 循环。
//
// ResolveAndIncrementRetry 是 MarkResolved + IncrementRetry 的原子组合：
// 只有当 resolved 从 false 翻到 true 时，retry_count 才会 +1；调用方据此
// 决定 retry 是否真的生效。已 resolved / 行不存在 / DB 失败都不会留下
// 「retry +1 但 resolved 未改」的脏状态。
type dlqStore interface {
	ListUnresolved(ctx context.Context, limit int) ([]dlqRow, error)
	IncrementRetry(ctx context.Context, id int64) (int, error)
	MarkResolved(ctx context.Context, id int64, resolver uuid.UUID, resolution string) error
	ResolveAndIncrementRetry(ctx context.Context, id int64, resolver uuid.UUID, resolution string) (resolved bool, err error)
}

// dlqRow 是 admin 端视图层（解耦 worker 包内的 struct）。
type dlqRow struct {
	ID         int64                  `json:"id"`
	Consumer   string                 `json:"consumer"`
	ChainID    *int64                 `json:"chainId,omitempty"`
	Kind       string                 `json:"kind"`
	Severity   string                 `json:"severity"`
	Summary    string                 `json:"summary"`
	Payload    map[string]any         `json:"payload"`
	RetryCount int                    `json:"retryCount"`
	Resolved   bool                   `json:"resolved"`
	CreatedAt  time.Time              `json:"createdAt"`
	Resolution *string                `json:"resolution,omitempty"`
}

// errAlreadyResolved 哨兵错误。
var errAlreadyResolved = errors.New("dlq: already resolved")

// mirrorManualRewind 在 API 进程内执行 rewind；逻辑镜像 worker/internal/indexer.ManualRewind。
//
// 之所以不复用：apps/api 与 apps/worker 是两个 module，跨 module 引入
// internal 包违反 Go 习惯。两个 module 必须各自实现 admin → DB 路径。
//
// 锁契约（critical）：事务内第一步 SELECT ... FOR UPDATE 锁住
// (chain_id, rewindConsumer) 行；这样 admin 手动 rewind 与 worker
// HandleReorg / ManualRewind 互斥。consumer 必须与 worker 端一致（"indexer"）。
func mirrorManualRewind(ctx context.Context, pool *pgxpool.Pool, chainID, fromBlock int64, reason string, actor uuid.UUID) (orphaned, affected int64, err error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// 0) 锁 chain_checkpoints 行（与 worker 同 consumer）。
	if err := ensureCheckpointLocked(ctx, tx, chainID, rewindConsumer); err != nil {
		return 0, 0, err
	}

	tag, err := tx.Exec(ctx, `UPDATE chain_events SET canonical=false
WHERE chain_id=$1 AND block_number >= $2 AND canonical=true`, chainID, fromBlock)
	if err != nil {
		return 0, 0, err
	}
	orphaned = tag.RowsAffected()

	tag, err = tx.Exec(ctx, `UPDATE orders
SET status='reorged', failure_code='EVENT_REORGED', updated_at=now()
WHERE chain_id=$1
  AND block_number >= $2
  AND status IN ('confirming','confirmed')`, chainID, fromBlock)
	if err != nil {
		return 0, 0, err
	}
	affected = tag.RowsAffected()

	payload := map[string]any{
		"actorUserId": actor.String(),
		"userReason":  reason,
		"reason":      "manual_rewind",
		"at":          time.Now().UTC().Format(time.RFC3339Nano),
	}
	raw, _ := json.Marshal(payload)
	if _, err := tx.Exec(ctx, `INSERT INTO chain_reorgs(chain_id, common_block, reason, orphaned_events, affected_orders, payload)
VALUES($1,$2,$3,$4,$5,$6::jsonb)`,
		chainID, fromBlock, "manual_rewind", orphaned, affected, raw); err != nil {
		return 0, 0, err
	}

	// checkpoint 复位守卫：next_block > $3 或 last_block_hash 不为空才更新，
	// 避免 rewind 到等于 current next_block 的 no-op 场景下覆盖已有数据。
	if _, err := tx.Exec(ctx, `UPDATE chain_checkpoints
SET next_block=$3, last_block_hash=NULL, updated_at=now()
WHERE chain_id=$1 AND consumer=$2 AND (next_block > $3 OR last_block_hash IS NOT NULL)`,
		chainID, rewindConsumer, fromBlock); err != nil {
		return 0, 0, err
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, 0, err
	}
	return orphaned, affected, nil
}

// ensureCheckpointLocked 在事务内确保 (chain_id, consumer) 行存在并被锁定。
//
// 行为：
//   - 若行已存在：SELECT ... FOR UPDATE 返回现有记录；
//   - 若不存在：INSERT ... ON CONFLICT DO NOTHING（不覆盖已存在行），
//     再 SELECT ... FOR UPDATE 拿到锁。
//
// 用于 rewind 这类"必须持锁但行可能尚未初始化"的场景。
// 镜像 worker/internal/indexer.EnsureCheckpointLocked（跨 module 不 import）。
func ensureCheckpointLocked(ctx context.Context, tx pgx.Tx, chainID int64, consumer string) error {
	const upsert = `
INSERT INTO chain_checkpoints(chain_id, consumer, next_block, last_block_hash, updated_at)
VALUES($1, $2, 0, NULL, now())
ON CONFLICT (chain_id, consumer) DO NOTHING`
	if _, err := tx.Exec(ctx, upsert, chainID, consumer); err != nil {
		return err
	}
	const lockQ = `
SELECT 1 FROM chain_checkpoints
WHERE chain_id=$1 AND consumer=$2
FOR UPDATE`
	var one int
	if err := tx.QueryRow(ctx, lockQ, chainID, consumer).Scan(&one); err != nil {
		return err
	}
	return nil
}

// userIDFromCtx 与 handlers 包同款；为避免循环 import 这里再写一份。
func userIDFromCtx(c *httpkit.Context) (uuid.UUID, error) {
	raw := c.UserID()
	if raw == "" {
		return uuid.Nil, errors.New("no user_id in context")
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, err
	}
	return id, nil
}

// RegisterRoutes 把 admin 路由挂到给定 group。
//
// 调用方需在挂 group 时已经加 auth.Middleware + rbac.Middleware(PermSystemAdmin)；
// 此函数只追加 endpoint。
func RegisterRoutes(group *gin.RouterGroup, rewind *ChainRewindHandler, dlq *DLQHandler) {
	group.POST("/chain/rewind", httpkit.Wrap(rewind.PostRewind))
	group.GET("/dlq", httpkit.Wrap(dlq.List))
	group.POST("/dlq/:id/retry", httpkit.Wrap(dlq.Retry))
}
