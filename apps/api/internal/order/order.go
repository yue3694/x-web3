// Package order 实现购买意图创建 / tx 提交 / 订单查询。
//
// 状态机：
//
//	purchase_intents: created → submitted → confirming → confirmed | failed | expired | reorged
//	orders:           submitted → confirming → confirmed | failed | expired | reorged
//
// 关键不变量：
//   - 创建 intent 时冻结 price_version / amount / market / token / chain_id；
//   - 同一 (user_id, idempotency_key) 必须幂等（同一用户重发同一 key → 返回原 intent）；
//   - 提交 tx hash 时校验 (chain_id, tx_hash) 唯一；
//   - 订单状态转换由 worker 推动；API 仅在 created → submitted / 提交 tx 时介入。
package order

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrCourseNotFound   = errors.New("order: course not found or not published")
	ErrPriceNotFound    = errors.New("order: no current price for chain")
	ErrNoWallet         = errors.New("order: user has no wallet for chain")
	ErrWalletNotOwned   = errors.New("order: wallet does not belong to user")
	ErrAlreadyPurchased = errors.New("order: user already purchased this course")
	ErrIntentExpired    = errors.New("order: purchase intent expired")
	ErrIntentNotFound   = errors.New("order: purchase intent not found")
	ErrIntentNotOwned   = errors.New("order: not the intent owner")
	ErrIntentBadState   = errors.New("order: intent not in created state")
	ErrOrderNotFound    = errors.New("order: order not found")
	ErrOrderNotOwned    = errors.New("order: not the order owner")
	ErrTxAlreadyUsed    = errors.New("order: tx hash already used by another order")
	ErrTxChainMismatch  = errors.New("order: tx hash chain does not match intent")
	ErrTxBadHash        = errors.New("order: bad tx hash")
)

// PurchaseIntent 视图层结构。
type PurchaseIntent struct {
	ID             uuid.UUID `json:"id"`
	UserID         uuid.UUID `json:"userId"`
	WalletID       uuid.UUID `json:"walletId"`
	CourseID       uuid.UUID `json:"courseId"`
	PriceID        uuid.UUID `json:"priceId"`
	CourseKeyHex   string    `json:"courseKey"`
	PriceVersion   int       `json:"priceVersion"`
	ChainID        int64     `json:"chainId"`
	TokenAddress   string    `json:"tokenAddress"`
	Amount         string    `json:"amount"`         // decimal string
	MarketAddress  string    `json:"marketAddress"`
	IdempotencyKey string    `json:"idempotencyKey"`
	Status         string    `json:"status"`
	ExpiresAt      time.Time `json:"expiresAt"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

// Order 视图层结构。
//
// 字段命名对齐 `packages/shared/openapi/order.yaml#/OrderResponse`：
//   - onchainTxHash 32 字节 hex（带 0x 前缀；空字符串表示未提交 tx）
//   - enrollmentId 由 worker 事件确认后回填（confirmed 时非空）
type Order struct {
	ID             uuid.UUID  `json:"id"`
	IntentID       uuid.UUID  `json:"intentId"`
	UserID         uuid.UUID  `json:"userId"`
	CourseID       uuid.UUID  `json:"courseId"`
	Status         string     `json:"status"`
	ChainID        int64      `json:"chainId"`
	OnchainTxHash  string     `json:"onchainTxHash,omitempty"`
	BlockNumber    *int64     `json:"blockNumber,omitempty"`
	LogIndex       *int       `json:"logIndex,omitempty"`
	ConfirmedAt    *time.Time `json:"confirmedAt,omitempty"`
	FailureCode    *string    `json:"failureCode,omitempty"`
	EnrollmentID   *uuid.UUID `json:"enrollmentId,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

// Service order 子系统的入口。
type Service struct {
	pool        *pgxpool.Pool
	intentTTL   time.Duration
}

// NewService ...
func NewService(pool *pgxpool.Pool, intentTTL time.Duration) *Service {
	if intentTTL <= 0 {
		intentTTL = 15 * time.Minute
	}
	return &Service{pool: pool, intentTTL: intentTTL}
}

// CreateIntentInput 入参。
type CreateIntentInput struct {
	UserID         uuid.UUID
	CourseID       uuid.UUID
	ChainID        int64
	WalletID       uuid.UUID
	IdempotencyKey string
}

// CreateIntent 创建购买意图：
//   1. 校验课程已发布；
//   2. 选当前 (chain_id) 价格版本；
//   3. 校验 wallet 归属当前 user 且 chain_id 匹配；
//   4. 检查 enrollment 唯一约束 → 已购直接返回 ALREADY_PURCHASED；
//   5. INSERT intent；唯一冲突 (user_id, idempotency_key) → 返回原记录。
//
// courseKey = keccak256(course_id) — 离线计算，无需外部依赖。
func (s *Service) CreateIntent(ctx context.Context, in CreateIntentInput) (*PurchaseIntent, error) {
	in.IdempotencyKey = strings.TrimSpace(in.IdempotencyKey)
	if in.IdempotencyKey == "" {
		return nil, errors.New("order: idempotency_key required")
	}
	// 课程是否已发布 + 取当前价格版本
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var priceID uuid.UUID
	var priceVersion int
	var tokenAddress, marketAddress string
	var amountStr string
	var decimals int
	err = tx.QueryRow(ctx, `
SELECT cp.id, cp.version, cp.token_address, cp.market_address, cp.amount::text, cp.decimals
FROM courses c
JOIN course_prices cp ON cp.course_id = c.id AND cp.valid_to IS NULL
WHERE c.id = $1 AND c.status = 'published' AND c.deleted_at IS NULL
  AND cp.chain_id = $2`,
		in.CourseID, in.ChainID).Scan(&priceID, &priceVersion, &tokenAddress, &marketAddress, &amountStr, &decimals)
	if errors.Is(err, pgx.ErrNoRows) {
		// 区分课程不存在 vs 没当前价格
		var exists bool
		_ = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM courses WHERE id=$1 AND status='published' AND deleted_at IS NULL)`, in.CourseID).Scan(&exists)
		if !exists {
			return nil, ErrCourseNotFound
		}
		return nil, ErrPriceNotFound
	}
	if err != nil {
		return nil, err
	}
	// wallet 归属
	var walletUser uuid.UUID
	var walletChain int64
	err = tx.QueryRow(ctx, `SELECT user_id, chain_id FROM wallets WHERE id=$1`, in.WalletID).Scan(&walletUser, &walletChain)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNoWallet
	}
	if err != nil {
		return nil, err
	}
	if walletUser != in.UserID {
		return nil, ErrWalletNotOwned
	}
	if walletChain != in.ChainID {
		return nil, fmt.Errorf("order: wallet chain %d != intent chain %d", walletChain, in.ChainID)
	}
	// 已购买？
	var alreadyEnrolled bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM enrollments WHERE user_id=$1 AND course_id=$2)`, in.UserID, in.CourseID).Scan(&alreadyEnrolled); err == nil && alreadyEnrolled {
		return nil, ErrAlreadyPurchased
	}
	// 落 intent
	courseKey := CourseKey(in.CourseID)
	expiresAt := time.Now().UTC().Add(s.intentTTL)
	var pi PurchaseIntent
	var amount numericString
	err = tx.QueryRow(ctx, `INSERT INTO purchase_intents(user_id,wallet_id,course_id,price_id,course_key,price_version,chain_id,token_address,amount,market_address,idempotency_key,expires_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9::numeric,$10,$11,$12)
RETURNING id,user_id,wallet_id,course_id,price_id,encode(course_key,'hex'),price_version,chain_id,token_address,amount::text,market_address,idempotency_key,status,expires_at,created_at,updated_at`,
		in.UserID, in.WalletID, in.CourseID, priceID, courseKey, priceVersion, in.ChainID, tokenAddress, amountStr, marketAddress, in.IdempotencyKey, expiresAt).Scan(
		&pi.ID, &pi.UserID, &pi.WalletID, &pi.CourseID, &pi.PriceID, &pi.CourseKeyHex, &pi.PriceVersion, &pi.ChainID, &pi.TokenAddress, &amount, &pi.MarketAddress, &pi.IdempotencyKey, &pi.Status, &pi.ExpiresAt, &pi.CreatedAt, &pi.UpdatedAt)
	if err != nil {
		// 幂等命中：返回原记录
		if isUniqueViolation(err) {
			_ = tx.Rollback(ctx)
			existing, getErr := s.GetIntentByIdempotency(ctx, in.UserID, in.IdempotencyKey)
			if getErr != nil {
				return nil, err
			}
			return existing, nil
		}
		return nil, err
	}
	pi.Amount = string(amount)
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &pi, nil
}

// GetIntentByIdempotency 通过幂等键返回 intent。
func (s *Service) GetIntentByIdempotency(ctx context.Context, userID uuid.UUID, key string) (*PurchaseIntent, error) {
	var pi PurchaseIntent
	err := s.pool.QueryRow(ctx, `SELECT id,user_id,wallet_id,course_id,price_id,encode(course_key,'hex'),price_version,chain_id,token_address,amount::text,market_address,idempotency_key,status,expires_at,created_at,updated_at
FROM purchase_intents WHERE user_id=$1 AND idempotency_key=$2`, userID, key).Scan(
		&pi.ID, &pi.UserID, &pi.WalletID, &pi.CourseID, &pi.PriceID, &pi.CourseKeyHex, &pi.PriceVersion, &pi.ChainID, &pi.TokenAddress, &pi.Amount, &pi.MarketAddress, &pi.IdempotencyKey, &pi.Status, &pi.ExpiresAt, &pi.CreatedAt, &pi.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrIntentNotFound
	}
	return &pi, err
}

// GetIntent 返回 intent。
func (s *Service) GetIntent(ctx context.Context, id uuid.UUID) (*PurchaseIntent, error) {
	var pi PurchaseIntent
	err := s.pool.QueryRow(ctx, `SELECT id,user_id,wallet_id,course_id,price_id,encode(course_key,'hex'),price_version,chain_id,token_address,amount::text,market_address,idempotency_key,status,expires_at,created_at,updated_at
FROM purchase_intents WHERE id=$1`, id).Scan(
		&pi.ID, &pi.UserID, &pi.WalletID, &pi.CourseID, &pi.PriceID, &pi.CourseKeyHex, &pi.PriceVersion, &pi.ChainID, &pi.TokenAddress, &pi.Amount, &pi.MarketAddress, &pi.IdempotencyKey, &pi.Status, &pi.ExpiresAt, &pi.CreatedAt, &pi.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrIntentNotFound
	}
	return &pi, err
}

// SubmitTransaction 把 intent 推到 submitted，并建/更新 order。
//
// 校验：
//   - intent 存在 / 未过期 / 属于 user / 状态为 created
//   - chain 匹配
//   - tx hash 长度合法 (32 bytes)
//
// order 状态：submitted。worker 事件确认后推到 confirming → confirmed。
func (s *Service) SubmitTransaction(ctx context.Context, intentID, userID uuid.UUID, chainID int64, txHash []byte) (*Order, error) {
	if len(txHash) != 32 {
		return nil, ErrTxBadHash
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	intent, err := s.GetIntent(ctx, intentID)
	if err != nil {
		return nil, err
	}
	if intent.UserID != userID {
		return nil, ErrIntentNotOwned
	}
	if intent.ChainID != chainID {
		return nil, ErrTxChainMismatch
	}
	if intent.Status != "created" {
		return nil, ErrIntentBadState
	}
	if time.Now().UTC().After(intent.ExpiresAt) {
		// 过期：标 expired，返回 ErrIntentExpired
		_, _ = s.pool.Exec(ctx, `UPDATE purchase_intents SET status='expired' WHERE id=$1 AND status='created'`, intentID)
		return nil, ErrIntentExpired
	}
	// 把 intent 推到 submitted
	if _, err := tx.Exec(ctx, `UPDATE purchase_intents SET status='submitted' WHERE id=$1 AND status='created'`, intentID); err != nil {
		return nil, err
	}
	// upsert order：(chain_id, tx_hash) 唯一
	var ord Order
	err = tx.QueryRow(ctx, `INSERT INTO orders(intent_id,user_id,course_id,status,chain_id,tx_hash)
VALUES($1,$2,$3,'submitted',$4,$5)
RETURNING id,intent_id,user_id,course_id,status,chain_id,'0x' || encode(tx_hash,'hex'),block_number,log_index,confirmed_at,failure_code,enrollment_id,created_at,updated_at`,
		intentID, intent.UserID, intent.CourseID, chainID, txHash).Scan(
		&ord.ID, &ord.IntentID, &ord.UserID, &ord.CourseID, &ord.Status, &ord.ChainID, &ord.OnchainTxHash, &ord.BlockNumber, &ord.LogIndex, &ord.ConfirmedAt, &ord.FailureCode, &ord.EnrollmentID, &ord.CreatedAt, &ord.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrTxAlreadyUsed
		}
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &ord, nil
}

// GetOrder 返回 order；admin 可看任意；普通用户只看自己的。
func (s *Service) GetOrder(ctx context.Context, id, userID uuid.UUID, isAdmin bool) (*Order, error) {
	var ord Order
	var owner uuid.UUID
	err := s.pool.QueryRow(ctx, `SELECT id,intent_id,user_id,course_id,status,chain_id,'0x' || encode(tx_hash,'hex'),block_number,log_index,confirmed_at,failure_code,enrollment_id,created_at,updated_at
FROM orders WHERE id=$1`, id).Scan(
		&ord.ID, &ord.IntentID, &ord.UserID, &ord.CourseID, &ord.Status, &ord.ChainID, &ord.OnchainTxHash, &ord.BlockNumber, &ord.LogIndex, &ord.ConfirmedAt, &ord.FailureCode, &ord.EnrollmentID, &ord.CreatedAt, &ord.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrOrderNotFound
	}
	if err != nil {
		return nil, err
	}
	owner = ord.UserID
	if !isAdmin && owner != userID {
		return nil, ErrOrderNotOwned
	}
	return &ord, nil
}

// ListMyOrders 当前用户的订单列表（按 created_at desc）。
func (s *Service) ListMyOrders(ctx context.Context, userID uuid.UUID, limit int) ([]Order, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `SELECT id,intent_id,user_id,course_id,status,chain_id,'0x' || encode(tx_hash,'hex'),block_number,log_index,confirmed_at,failure_code,enrollment_id,created_at,updated_at
FROM orders WHERE user_id=$1 ORDER BY created_at DESC LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Order, 0, limit)
	for rows.Next() {
		var ord Order
		if err := rows.Scan(&ord.ID, &ord.IntentID, &ord.UserID, &ord.CourseID, &ord.Status, &ord.ChainID, &ord.OnchainTxHash, &ord.BlockNumber, &ord.LogIndex, &ord.ConfirmedAt, &ord.FailureCode, &ord.EnrollmentID, &ord.CreatedAt, &ord.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, ord)
	}
	return out, rows.Err()
}

// CourseKey sha256(uuid_bytes) — SSOT 三端必须用同一算法（api / web / worker test fixture）。
//
// 算法历史：
//   - 早期 doc 误写为 keccak256；实际实现是 SHA-256，本注释对齐实现。
//   - 合约侧（packages/contracts/src/CourseMarket.sol）把 courseKey 当
//     mapping key，不验证内容字节含义；改 keccak256 需同步：
//     1) 这里 2) apps/web/src/features/checkout/derive.ts::courseKeyFromUuid
//     3) apps/web/src/contracts/market.abi.ts 注释
//     4) apps/worker/internal/order/confirmer_test.go::courseKeyForTest
//     5) 任何 frontend-intent 序列化/反序列化测试。
//   - 阶段 A 评估后决定：保留 SHA-256 不动；合约不验内容，跨端漂移风险大于收益。
func CourseKey(courseID uuid.UUID) []byte {
	h := sha256.Sum256(courseID[:])
	return h[:]
}

// AmountToBigInt 把 numeric string 转 *big.Int（用于 ABI 编码）。
func AmountToBigInt(amount string) (*big.Int, error) {
	v, ok := new(big.Int).SetString(amount, 10)
	if !ok {
		return nil, fmt.Errorf("order: bad amount %q", amount)
	}
	return v, nil
}

// isUniqueViolation 简单判断 PG unique_violation (sqlstate 23505)。
//
// pgx 在 v5 把 sqlstate 暴露为 *pgconn.PgError.Code，但要求 pgconn import。
// 这里用错误信息兜底匹配；测试 / 调试可改成结构化。
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "23505") || strings.Contains(msg, "duplicate key") || strings.Contains(msg, "unique constraint")
}

// numericString 是 pgx numeric(78,0) 文本表示；保留为 string 以避免精度丢失。
type numericString string

// Scan 实现 pgx.Scanner。
func (n *numericString) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*n = ""
	case string:
		*n = numericString(v)
	case []byte:
		*n = numericString(string(v))
	default:
		return fmt.Errorf("order: cannot scan %T into numericString", src)
	}
	return nil
}
