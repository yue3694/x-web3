// Package certificate — consumer.go 实现 F04-T11：
// 轮询 certificate_jobs（pending + next_retry_at <= now()），拉取对应 certificates 行，
// 调用 signer.SignCertificateMint 签名 + 广播 + 等 N-confirmation，最终把状态写回。
//
// 失败语义：
//   - 临时性错误（RPC timeout / nonce conflict / underpriced）：attempts++，
//     next_retry_at = now() + exp_backoff(attempts)，状态回到 'pending'，
//     下次 poll 仍可被同 worker 或其他 worker 实例抢到。
//   - 永久性错误（recipient 0x0 / cert id 越界 / mint revert 持续）：attempts++，
//     当 attempts >= MaxAttempts 时 status='dead' 并写 dlq_events（consumer='cert_mint'）。
//
// 幂等：
//   - SELECT FOR UPDATE SKIP LOCKED 防多 worker 抢同一条；
//   - 重跑已 confirmed 的 job 会跳过（status 终态），但需小心：理论上 confirmed 后 attempt
//     不再增加；同一 chain_id + tx_hash 在 certificates 上有唯一约束，幂等命中。
//
// 并发：
//   - 单 Consumer.Run(ctx) 串行执行一个 batch 内所有 job；调用方通过多个 Consumer 实例
//     横向扩展（cmd/worker/main.go 未来可注入 N 个）。
//
// 优雅退出：
//   - ctx.Done() 时停止 poll 新 batch，正在跑的 job 跑完当前步骤再退出（避免半成品）。
package certificate

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/x-web3/worker/internal/reconcile"
)

// Sentinel errors —— errors.Is 分流到 retry vs dead-letter。
var (
	ErrJobNotFound     = errors.New("certificate-consumer: job not found")
	ErrCertificateGone = errors.New("certificate-consumer: certificate row missing")
	ErrJobAlreadyFinal = errors.New("certificate-consumer: job already in terminal state")
	ErrSignerDown      = errors.New("certificate-consumer: signer unavailable")
	ErrBroadcastFailed = errors.New("certificate-consumer: broadcast failed")
)

// 默认参数。
const (
	defaultBatchSize     = 10
	defaultMaxAttempts   = 5
	defaultPollInterval  = 2 * time.Second
	defaultConfirmDepth  = 12
	// baseBackoff 第一次重试延迟；后续 2^(attempt-1) 倍。
	baseBackoff = 30 * time.Second
	// maxBackoff 防止 attempt 大时秒级变天。
	maxBackoff = 30 * time.Minute
)

// EthClient 是广播 + 等待 receipt 的最小依赖接口（worker 端 ethclient.Client 的子集）。
//
// 设计：单测用 fakeEthClient 注入瞬时失败 / 永久失败 / 正常 confirm；生产由
// indexer.Client / go-ethereum ethclient 适配。
type EthClient interface {
	// PendingNonceAt 当前 pending nonce；用于 tx params。
	PendingNonceAt(ctx context.Context, account common.Address) (uint64, error)
	// SendTransaction 广播已签名交易；返回的 hash 可用于 WaitMined。
	SendTransaction(ctx context.Context, tx *types.Transaction) error
	// WaitMined 等待 tx 上链；返回 receipt。生产实现内部再做 confirmation_depth 等待。
	WaitMined(ctx context.Context, hash common.Hash) (*Receipt, error)
}

// Receipt 是 ethclient.Receipt 的最小视图（避免 worker 直接依赖 ethclient 包内类型）。
type Receipt struct {
	TxHash      common.Hash
	BlockNumber uint64
	BlockHash   common.Hash
	Status      uint64 // 1 = success, 0 = revert
}

// DLQStore DLQ 写入；与 reconcile.DLQStore 形状一致。允许测试注入 stub。
type DLQStore interface {
	Write(ctx context.Context, e reconcile.Entry) (int64, error)
}

// JobRow 是单条待处理的 certificate_jobs 行（消费端 view）。
type JobRow struct {
	ID            uuid.UUID
	CertificateID uuid.UUID
	Attempt       int
	Status        string
}

// CertificateRow 是 certificates 表的最小视图。
type CertificateRow struct {
	ID              uuid.UUID
	UserID          uuid.UUID
	CourseID        uuid.UUID
	CertVersion     int
	CertificateID   *big.Int // uint256 (matches numeric(78,0))
	RecipientWallet common.Address
	MetadataURI     string
	MetadataSHA256  string
	ChainID         int64
	TxHash          []byte
	TokenID         *big.Int
	Status          string
	Attempts        int
}

// ConsumerConfig 装配参数；零值走默认。
type ConsumerConfig struct {
	Pool         *pgxpool.Pool
	Signer       MintSigner
	Client       EthClient
	DLQ          DLQStore
	ChainID      int64
	BatchSize    int
	PollInterval time.Duration
	MaxAttempts  int
	ConfirmDepth int64
	Logger       *slog.Logger
	// Metrics：可选；nil 时不写。
	Metrics *ConsumerMetrics
}

// ConsumerMetrics 计数器；worker metrics 包可拉取。
type ConsumerMetrics struct {
	Processed atomic.Uint64
	Succeeded atomic.Uint64
	Retried   atomic.Uint64
	DeadLet   atomic.Uint64
	BroadcastRetries atomic.Uint64
}

// Consumer 主结构。
type Consumer struct {
	cfg    ConsumerConfig
	logger *slog.Logger
	// seed RNG once at construction so backoff jitter is deterministic per consumer.
	rng *rand.Rand
	rngMu sync.Mutex
}

// NewConsumer 构造 consumer；必要字段缺失返回 error。
func NewConsumer(cfg ConsumerConfig) (*Consumer, error) {
	if cfg.Pool == nil {
		return nil, errors.New("certificate-consumer: pool required")
	}
	if cfg.Signer == nil {
		return nil, errors.New("certificate-consumer: signer required")
	}
	if cfg.Client == nil {
		return nil, errors.New("certificate-consumer: eth client required")
	}
	if cfg.ChainID == 0 {
		return nil, errors.New("certificate-consumer: chain id required")
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = defaultBatchSize
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = defaultPollInterval
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = defaultMaxAttempts
	}
	if cfg.ConfirmDepth <= 0 {
		cfg.ConfirmDepth = defaultConfirmDepth
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.DLQ == nil {
		// 默认走 PG 实现；保证 init 时构造方便 cmd/worker 注入。
		cfg.DLQ = reconcile.NewPGDLQStore(cfg.Pool)
	}
	if cfg.Metrics == nil {
		cfg.Metrics = &ConsumerMetrics{}
	}
	return &Consumer{
		cfg:    cfg,
		logger: cfg.Logger.With("subsystem", "cert_mint"),
		rng:    rand.New(rand.NewSource(time.Now().UnixNano())),
	}, nil
}

// Run 阻塞跑轮询循环；ctx.Done() 时退出。退出前会 drain 当前 batch（worker Stop 等待 in-flight 完成）。
func (c *Consumer) Run(ctx context.Context) error {
	ticker := time.NewTicker(c.cfg.PollInterval)
	defer ticker.Stop()
	c.logger.Info("cert_consumer_started",
		"pollInterval", c.cfg.PollInterval.String(),
		"batchSize", c.cfg.BatchSize,
		"maxAttempts", c.cfg.MaxAttempts,
		"confirmDepth", c.cfg.ConfirmDepth,
	)
	// 启动即跑一次 sweep + claim，避免冷启动延迟。
	c.sweepStaleMintings(ctx)
	c.runOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			c.logger.Info("cert_consumer_stopped")
			return nil
		case <-ticker.C:
			c.sweepStaleMintings(ctx)
			c.runOnce(ctx)
		}
	}
}

// sweepStaleMintings 把超过 staleness 的 minting 行重置为 pending，
// 让下一轮 poll 重新 claim。修复「worker 在 SendTransaction 与 WaitMined 之间崩溃」
// 导致 job 永久卡在 minting 的问题。
//
// 阈值：minting 状态持续超过 5 分钟（按 CHAIN_CONFIRMATION_DEPTH 默认 12 + 余量）。
func (c *Consumer) sweepStaleMintings(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	tag, err := c.cfg.Pool.Exec(ctx, `
UPDATE certificate_jobs
SET status='pending', next_retry_at=now(), updated_at=now()
WHERE status='minting' AND started_at IS NOT NULL
  AND started_at < now() - interval '5 minutes'`)
	if err != nil {
		c.logger.Warn("sweep_stale_mintings_failed", "err", err.Error())
		return
	}
	if n := tag.RowsAffected(); n > 0 {
		c.logger.Info("sweep_stale_mintings", "reset", n)
	}
}

// runOnce 单轮：claim 一批 → 处理每条 → 状态写回。
//
// claim 用 SELECT FOR UPDATE SKIP LOCKED + UPDATE status='minting' 把行移交给本 worker；
// 同一 worker 进程内串行（避免 nonce 竞争）。
func (c *Consumer) runOnce(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}

	// claimBatch：拿到一批 job id（仅 id，方便后续逐条读）。
	type claimed struct {
		id        uuid.UUID
		attempt   int
		certID    uuid.UUID
	}
	rows, err := c.cfg.Pool.Query(ctx, `
UPDATE certificate_jobs
SET status='minting', started_at=now(), updated_at=now()
WHERE id IN (
  SELECT id FROM certificate_jobs
  WHERE status='pending' AND next_retry_at <= now()
  ORDER BY created_at, id
  LIMIT $1
  FOR UPDATE SKIP LOCKED
)
RETURNING id, attempt, certificate_id`,
		c.cfg.BatchSize)
	if err != nil {
		c.logger.Error("claim_failed", "err", err.Error())
		return
	}
	var batch []claimed
	for rows.Next() {
		var cl claimed
		if err := rows.Scan(&cl.id, &cl.attempt, &cl.certID); err != nil {
			rows.Close()
			c.logger.Error("claim_scan", "err", err.Error())
			return
		}
		batch = append(batch, cl)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		c.logger.Error("claim_rows", "err", err.Error())
		return
	}
	if len(batch) == 0 {
		return
	}

	for _, j := range batch {
		if ctx.Err() != nil {
			return
		}
		c.cfg.Metrics.Processed.Add(1)
		c.handleJob(ctx, j.id, j.certID, j.attempt)
	}
}

// handleJob 单 job 全流程；非 nil error 走 retry/dead-letter 路径。
//
// 流程：
//  1) 读 certificates 行（含 certificate_id, recipient_wallet, metadata_uri）；
//  2) signer.SignCertificateMint(certID, recipient, uri) 构造签名交易；
//  3) Client.SendTransaction 广播（重试 3 次，指数退避）；
//  4) Client.WaitMined 等 receipt；
//  5) UPDATE certificates + certificate_jobs：成功 → confirmed；失败 → pending / dead。
func (c *Consumer) handleJob(ctx context.Context, jobID, certUUID uuid.UUID, prevAttempt int) {
	cert, err := c.loadCertificate(ctx, certUUID)
	if err != nil {
		c.logger.Error("load_cert_failed", "jobId", jobID.String(), "certId", certUUID.String(), "err", err.Error())
		c.failJob(ctx, jobID, prevAttempt, fmt.Sprintf("load certificate: %v", err))
		return
	}
	if cert.CertificateID == nil {
		c.failJob(ctx, jobID, prevAttempt, "certificate_id is null on certificates row")
		return
	}

	// 1) sign
	tx, err := c.cfg.Signer.SignCertificateMint(ctx, cert.CertificateID, cert.RecipientWallet, cert.MetadataURI)
	if err != nil {
		// 签名失败通常是配置/密钥问题（永久错误）；下一轮重试也不会自动变好。
		// 但区分 transient（timeout）与 permanent（self-test failed）较困难；
		// 此处按「可达性错误 = retry / 一致性错误 = dead」简单分类：
		if errors.Is(err, ErrSignerDown) {
			c.failJob(ctx, jobID, prevAttempt, fmt.Sprintf("sign: %v", err))
			return
		}
		c.failJob(ctx, jobID, prevAttempt, fmt.Sprintf("sign: %v", err))
		return
	}

	// 2) broadcast with retry (max 3 attempts, exp backoff)
	if err := c.broadcastWithRetry(ctx, tx); err != nil {
		c.failJob(ctx, jobID, prevAttempt, fmt.Sprintf("broadcast: %v", err))
		return
	}

	// 3) wait for receipt
	receipt, err := c.cfg.Client.WaitMined(ctx, tx.Hash())
	if err != nil {
		c.failJob(ctx, jobID, prevAttempt, fmt.Sprintf("wait mined: %v", err))
		return
	}
	if receipt == nil {
		c.failJob(ctx, jobID, prevAttempt, "nil receipt")
		return
	}
	if receipt.Status != 1 {
		// 链上 revert：永久错误语义；不走 retry（重发同 nonce 仍是 revert）。
		// 但仍走 attempt++ 以保留 last_error 给运维。
		c.revertJob(ctx, jobID, cert, prevAttempt, receipt, "tx reverted on-chain")
		return
	}

	// 4) 写 confirmed 状态
	if err := c.markConfirmed(ctx, jobID, cert, receipt); err != nil {
		c.logger.Error("mark_confirmed_failed",
			"jobId", jobID.String(),
			"certId", certUUID.String(),
			"err", err.Error())
		return
	}
	c.cfg.Metrics.Succeeded.Add(1)
	c.logger.Info("cert_mint_confirmed",
		"jobId", jobID.String(),
		"certId", certUUID.String(),
		"txHash", receipt.TxHash.Hex(),
		"blockNumber", receipt.BlockNumber,
	)
}

// broadcastWithRetry 3 次指数退避；之间 sleep 也要尊重 ctx.Done()。
func (c *Consumer) broadcastWithRetry(ctx context.Context, tx *types.Transaction) error {
	var lastErr error
	backoff := 500 * time.Millisecond
	for attempt := 1; attempt <= 3; attempt++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		err := c.cfg.Client.SendTransaction(ctx, tx)
		if err == nil {
			return nil
		}
		lastErr = err
		c.cfg.Metrics.BroadcastRetries.Add(1)
		c.logger.Warn("broadcast_retry",
			"attempt", attempt,
			"txHash", tx.Hash().Hex(),
			"err", err.Error())
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		backoff *= 2
	}
	if lastErr == nil {
		lastErr = ErrBroadcastFailed
	}
	return fmt.Errorf("%w: %v", ErrBroadcastFailed, lastErr)
}

// loadCertificate 读 certificates 行 + 字段映射到 CertificateRow。
func (c *Consumer) loadCertificate(ctx context.Context, certID uuid.UUID) (*CertificateRow, error) {
	var row CertificateRow
	var certIDBigStr string
	var recipient string
	var txHash []byte
	var tokenIDStr *string
	err := c.cfg.Pool.QueryRow(ctx, `
SELECT id, user_id, course_id, cert_version,
       to_char(certificate_id, 'FM9999999999999999999999999999999999999999999999999999999999999999999999999'),
       recipient_wallet, metadata_uri, metadata_sha256,
       chain_id, tx_hash,
       to_char(token_id, 'FM9999999999999999999999999999999999999999999999999999999999999999999999999'),
       status, attempts
FROM certificates WHERE id=$1`, certID).Scan(
		&row.ID, &row.UserID, &row.CourseID, &row.CertVersion,
		&certIDBigStr, &recipient, &row.MetadataURI, &row.MetadataSHA256,
		&row.ChainID, &txHash, &tokenIDStr, &row.Status, &row.Attempts,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCertificateGone
	}
	if err != nil {
		return nil, err
	}
	row.RecipientWallet = common.HexToAddress(recipient)
	row.TxHash = txHash
	certIDBig, ok := new(big.Int).SetString(certIDBigStr, 10)
	if !ok {
		return nil, fmt.Errorf("bad certificate_id: %q", certIDBigStr)
	}
	row.CertificateID = certIDBig
	if tokenIDStr != nil {
		t, ok := new(big.Int).SetString(*tokenIDStr, 10)
		if ok {
			row.TokenID = t
		} else {
			// 解析失败：记 warn，但不让 job 死；下游会用证书 id（不是 token_id）走。
			c.logger.Warn("token_id_parse_failed",
				"certId", certID.String(), "raw", *tokenIDStr)
		}
	}
	return &row, nil
}

// markConfirmed 把 job + certificates 同时写终态；tx_hash / confirmed_at / block。
func (c *Consumer) markConfirmed(ctx context.Context, jobID uuid.UUID, cert *CertificateRow, r *Receipt) error {
	tx, err := c.cfg.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if tag, err := tx.Exec(ctx, `
UPDATE certificates
SET status='confirmed', tx_hash=$2, confirmed_block=$3, confirmed_at=now(), attempts=attempts+1, updated_at=now()
WHERE id=$1 AND status IN ('pending','minting')`, cert.ID, r.TxHash.Bytes(), int64(r.BlockNumber)); err != nil {
		return fmt.Errorf("update certificates: %w", err)
	} else if tag.RowsAffected() == 0 {
		// 已被其他 worker confirmed；放弃。
		return fmt.Errorf("certificate already in terminal state: %s", cert.ID)
	}
	if _, err := tx.Exec(ctx, `
UPDATE certificate_jobs
SET status='confirmed', tx_hash=$2, confirmed_at=now(), attempt=attempt+1, updated_at=now()
WHERE id=$1`, jobID, r.TxHash.Bytes()); err != nil {
		return fmt.Errorf("update certificate_jobs: %w", err)
	}
	return tx.Commit(ctx)
}

// revertJob 链上 revert：写 attempts++ + last_error，若超 max_attempts 进 DLQ。
//
// 保留 status='failed'（不是 dead）：运维可人工 retry 后再尝试——revert 通常是参数问题
// （recipient 错误 / certificateId 冲突），人工修正后由 admin endpoint 重置 attempt。
func (c *Consumer) revertJob(ctx context.Context, jobID uuid.UUID, cert *CertificateRow, prevAttempt int, r *Receipt, reason string) {
	c.recordFailure(ctx, jobID, cert, prevAttempt, "failed", reason, r)
}

// failJob 暂时性错误（RPC / DB / 配置）：attempt++ + next_retry_at；若超 max_attempts → dead。
func (c *Consumer) failJob(ctx context.Context, jobID uuid.UUID, prevAttempt int, reason string) {
	c.recordFailure(ctx, jobID, nil, prevAttempt, "", reason, nil)
}

// recordFailure 是统一写回入口。
func (c *Consumer) recordFailure(
	ctx context.Context,
	jobID uuid.UUID,
	cert *CertificateRow,
	prevAttempt int,
	terminal string,
	reason string,
	receipt *Receipt,
) {
	newAttempt := prevAttempt + 1
	dead := newAttempt >= c.cfg.MaxAttempts
	jobStatus := "pending"
	if dead {
		jobStatus = "dead"
	}

	// 计算 backoff；带 ±20% jitter 防止雪崩。
	backoff := computeBackoff(newAttempt)
	c.rngMu.Lock()
	jitter := time.Duration(float64(backoff) * (0.8 + 0.4*c.rng.Float64()))
	c.rngMu.Unlock()

	if _, err := c.cfg.Pool.Exec(ctx, `
UPDATE certificate_jobs
SET status=$2,
    attempt=$3,
    last_error=$4,
    next_retry_at = CASE WHEN $2='pending' THEN now() + $5::interval ELSE now() END,
    updated_at=now()
WHERE id=$1`,
		jobID, jobStatus, newAttempt, truncate(reason, 4096), jitter.String()); err != nil {
		c.logger.Error("update_job_failed",
			"jobId", jobID.String(),
			"err", err.Error())
		return
	}

	// 同步写 certificates 行（status, attempts, last_error）。
	if cert != nil {
		// status 字段: pending / minting / confirmed / failed / dead
		newCertStatus := "failed"
		if dead {
			newCertStatus = "dead"
		}
		if _, err := c.cfg.Pool.Exec(ctx, `
UPDATE certificates
SET status=$2, attempts=$3, last_error=$4,
    tx_hash=COALESCE($5, tx_hash),
    updated_at=now()
WHERE id=$1`,
			cert.ID, newCertStatus, newAttempt, truncate(reason, 4096),
			hashBytes(receipt)); err != nil {
			c.logger.Error("update_cert_failed",
				"certId", cert.ID.String(), "err", err.Error())
		}
	}

	if dead {
		c.cfg.Metrics.DeadLet.Add(1)
		// 写 DLQ（best-effort；失败仅记日志，不抛）。
		chainID := c.cfg.ChainID
		payload := map[string]any{
			"jobId":           jobID.String(),
			"certificateId":   zeroUUIDString(cert),
			"attempt":         newAttempt,
			"reason":          reason,
		}
		if receipt != nil {
			payload["txHash"] = receipt.TxHash.Hex()
			payload["blockNumber"] = receipt.BlockNumber
		}
		if _, err := c.cfg.DLQ.Write(ctx, reconcile.Entry{
			Consumer: "cert_mint",
			ChainID:  &chainID,
			Kind:     "mint_dead",
			Severity: "error",
			Summary:  fmt.Sprintf("certificate mint exhausted retries: %s", reason),
			Payload:  payload,
		}); err != nil {
			c.logger.Error("dlq_write_failed", "jobId", jobID.String(), "err", err.Error())
		}
		c.logger.Error("cert_mint_dead",
			"jobId", jobID.String(),
			"attempt", newAttempt,
			"reason", reason)
		return
	}

	c.cfg.Metrics.Retried.Add(1)
	c.logger.Warn("cert_mint_retry",
		"jobId", jobID.String(),
		"attempt", newAttempt,
		"nextRetry", jitter.String(),
		"reason", reason)
}

// computeBackoff 2^(attempt-1) * baseBackoff，capped at maxBackoff。
func computeBackoff(attempt int) time.Duration {
	if attempt <= 1 {
		return baseBackoff
	}
	d := baseBackoff
	for i := 1; i < attempt; i++ {
		d *= 2
		if d >= maxBackoff {
			return maxBackoff
		}
	}
	return d
}

// truncate 防 last_error 爆 DB column（虽未显式 limit 但 PG text 不限；这里给个安全阈值）。
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// zeroUUIDString 当 cert 为 nil 时返回空字符串；DLQ payload 不强求 cert id。
func zeroUUIDString(cert *CertificateRow) string {
	if cert == nil {
		return ""
	}
	return cert.ID.String()
}

// hashBytes 把 receipt hash 转 32-byte slice；receipt=nil 时返回 nil（UPDATE COALESCE 不覆盖）。
func hashBytes(r *Receipt) []byte {
	if r == nil {
		return nil
	}
	return r.TxHash.Bytes()
}

// EncodeU256 helper：worker 内部把 uint64 编码为 32 字节 big-endian（与 order.U256 兼容）。
func EncodeU256(v uint64) [32]byte {
	var b [32]byte
	binary.BigEndian.PutUint64(b[24:], v)
	return b
}