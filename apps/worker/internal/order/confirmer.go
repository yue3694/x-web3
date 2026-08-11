// Package workerorder 实现事件确认 + enrollment 派生。
//
// 核心事务：
//
//	BEGIN
//	  INSERT INTO chain_events(chain_id, tx_hash, log_index, block_number, block_hash, event_signature, payload)
//	    -- unique (chain_id, tx_hash, log_index) 保证幂等
//	  UPDATE orders SET status='confirming'|'confirmed'|'failed'|'reorged',
//	                    tx_hash, block_number, log_index, block_hash, confirmed_at,
//	                    failure_code
//	    WHERE id = $orderId AND status IN ('submitted','confirming')
//	  INSERT INTO enrollments(user_id, course_id)
//	    -- unique (user_id, course_id) 保证幂等
//	  INSERT INTO outbox_events(aggregate, type, payload)
//	COMMIT
//
// 任一步失败整体回滚；调用方重试。
//
// 幂等 & reorg 保护：
//   - confirmed / failed / reorged 状态的 order **不会**被 Apply 推进回中间态：
//     UPDATE WHERE 子句强制 status IN ('submitted','confirming')，0 行匹配即代表
//     已经处理过（含 admin rewind 之后 re-deliver 同一事件）；调用方拿到当前
//     state 后由其决定（绝大多数情况直接丢弃）。
//   - chain_events 的 unique (chain_id, tx_hash, log_index) 保证事件落库幂等；
//     同 tx_hash 不同 log_index（multi-log tx）允许重复插入。
//   - enrollments 的 unique (user_id, course_id) 保证不出现双重选课。
package workerorder

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/x-web3/worker/internal/chain"
)

var (
	ErrOrderNotFound = errors.New("workerorder: order not found")
	ErrIntentMissing = errors.New("workerorder: intent missing")
	ErrMismatch      = errors.New("workerorder: receipt mismatch")
)

// Confirmer 把已校验的事件入库并推进 order / enrollment。
type Confirmer struct {
	pool *pgxpool.Pool
}

// NewConfirmer ...
func NewConfirmer(pool *pgxpool.Pool) *Confirmer { return &Confirmer{pool: pool} }

// Apply 一次性把事件应用到 DB（包含 enrollment + outbox）。
//
// 输入：orderID + 解码后的事件 + 已校验的 intent（用于对账冗余）。
//
// 返回值：
//   - enrollmentID：新插入的 enrollment id；幂等命中时为 uuid.Nil（已存在）。
//   - state：orders.status 实际值。可能是 "confirmed" / "failed" / "reorged" /
//     "submitted" / "confirming"。当 order 已经处于终态（confirmed / failed /
//     reorged）时 Apply 不会回退；调用方应检查 state 再决定是否上报。
func (c *Confirmer) Apply(ctx context.Context, in ApplyInput) (uuid.UUID, string, error) {
	tx, err := c.pool.Begin(ctx)
	if err != nil {
		return uuid.Nil, "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// 1) chain_events：unique (chain_id, tx_hash, log_index)
	payload, _ := json.Marshal(map[string]any{
		"courseKey":    fmt.Sprintf("%x", in.Event.CourseKey),
		"buyer":        in.Event.Buyer.Hex(),
		"token":        in.Event.Token.Hex(),
		"amount":       fmt.Sprintf("%x", in.Event.Amount),
		"intentId":     fmt.Sprintf("%x", in.Event.IntentID),
		"priceVersion": fmt.Sprintf("%x", in.Event.PriceVersion),
	})
	eventSig := in.EventSig
	if eventSig == ([32]byte{}) {
		eventSig = chain.CoursePurchasedTopic
	}
	if _, err := tx.Exec(ctx, `INSERT INTO chain_events(chain_id,tx_hash,log_index,block_number,block_hash,event_signature,payload)
VALUES($1,$2,$3,$4,$5,$6,$7)
ON CONFLICT(chain_id,tx_hash,log_index) DO NOTHING`,
		in.ChainID, in.TxHash, in.LogIndex, in.BlockNumber, in.BlockHash, eventSig[:], payload); err != nil {
		return uuid.Nil, "", fmt.Errorf("insert chain_event: %w", err)
	}

	// 2) 取 order + intent（用于冗余校验 + 决定 state）
	var orderID uuid.UUID
	var userID, courseID, intentID uuid.UUID
	var status string
	err = tx.QueryRow(ctx, `SELECT o.id,o.user_id,o.course_id,o.intent_id,o.status
FROM orders o WHERE o.id=$1`, in.OrderID).Scan(&orderID, &userID, &courseID, &intentID, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, "", ErrOrderNotFound
	}
	if err != nil {
		return uuid.Nil, "", err
	}

	// 3) 校验 intent 字段与事件匹配
	var (
		dbCourseKey     []byte
		dbTokenAddress  string
		dbAmount        string
		dbPriceVersion  int
		dbChainID       int64
		dbMarketAddress string
		dbWalletAddress string
	)
	err = tx.QueryRow(ctx, `SELECT encode(pi.course_key,'hex'),pi.token_address,pi.amount::text,pi.price_version,pi.chain_id,pi.market_address,w.address
FROM purchase_intents pi JOIN wallets w ON w.id=pi.wallet_id
WHERE pi.id=$1`, intentID).Scan(&dbCourseKey, &dbTokenAddress, &dbAmount, &dbPriceVersion, &dbChainID, &dbMarketAddress, &dbWalletAddress)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, "", ErrIntentMissing
	}
	if err != nil {
		return uuid.Nil, "", err
	}
	if dbChainID != in.ChainID {
		return uuid.Nil, "", fmt.Errorf("%w: chainId", ErrMismatch)
	}
	// want 严格派生自 DB 的 intent + wallet；**绝不**从 in.Event 拷贝。
	//
	// 历史教训（PR-A2 修复）：早期版本 CourseKey / Token / Amount / IntentID /
	// PriceVersion 全部从 in.Event 取，导致 chain.ValidateReceipt 形同自比 —
	// 恶意事件 / bug 事件能直接推进 order 到 confirmed。
	want, err := buildIntentFromDB(dbCourseKey, dbTokenAddress, dbAmount, dbPriceVersion, intentID, dbWalletAddress)
	if err != nil {
		return uuid.Nil, "", fmt.Errorf("%w: %v", ErrMismatch, err)
	}
	if err := chain.ValidateReceipt(in.Event, &want); err != nil {
		// 标记 failed：仅当 order 还在中间态时才翻；reorged / confirmed 不回退。
		tag, err := tx.Exec(ctx, `UPDATE orders SET status='failed', failure_code='RECEIPT_MISMATCH', updated_at=now()
WHERE id=$1 AND status IN ('submitted','confirming')`, orderID)
		if err != nil {
			return uuid.Nil, "", err
		}
		_ = tag // 即使 0 行匹配也走提交，下面读最新 status 返回
		// 读最新 status 用于返回
		var current string
		if err := tx.QueryRow(ctx, `SELECT status FROM orders WHERE id=$1`, orderID).Scan(&current); err != nil {
			return uuid.Nil, "", err
		}
		if err := tx.Commit(ctx); err != nil {
			return uuid.Nil, current, err
		}
		return uuid.Nil, current, nil
	}

	// 4) confirmed → 写 enrollment + outbox
	//    关键幂等：WHERE status IN ('submitted','confirming')，
	//    命中 0 行 → order 已终态（confirmed / failed / reorged），
	//    不再做 enrollment + outbox 写入（这些副作用不应该重放）。
	confirmedAt := in.BlockTime
	if confirmedAt.IsZero() {
		confirmedAt = time.Now().UTC()
	}
	tag, err := tx.Exec(ctx, `UPDATE orders SET status='confirmed', tx_hash=$2, block_number=$3, log_index=$4, block_hash=$5, confirmed_at=$6, updated_at=now()
WHERE id=$1 AND status IN ('submitted','confirming')`,
		orderID, in.TxHash, in.BlockNumber, in.LogIndex, in.BlockHash, confirmedAt)
	if err != nil {
		return uuid.Nil, "", err
	}
	if tag.RowsAffected() == 0 {
		// 终态回放：返回 order 当前 status，不再写 enrollment / outbox。
		var current string
		if err := tx.QueryRow(ctx, `SELECT status FROM orders WHERE id=$1`, orderID).Scan(&current); err != nil {
			return uuid.Nil, "", err
		}
		if err := tx.Commit(ctx); err != nil {
			return uuid.Nil, current, err
		}
		return uuid.Nil, current, nil
	}
	var enrollmentID uuid.UUID
	err = tx.QueryRow(ctx, `INSERT INTO enrollments(user_id,course_id,source) VALUES($1,$2,'order')
ON CONFLICT(user_id,course_id) DO UPDATE SET source=enrollments.source
RETURNING id`, userID, courseID).Scan(&enrollmentID)
	if err != nil {
		return uuid.Nil, "", err
	}
	// 回填 enrollment_id 到 orders：让 API OrderResponse enrollmentId 字段
	// 在 confirmed 状态下立即可用；前端 MyOrders / GetOrder 无需再二次查询。
	if _, err := tx.Exec(ctx, `UPDATE orders SET enrollment_id=$1 WHERE id=$2`, enrollmentID, orderID); err != nil {
		return uuid.Nil, "", err
	}
	outboxPayload, _ := json.Marshal(map[string]any{
		"orderId":      orderID.String(),
		"userId":       userID.String(),
		"courseId":     courseID.String(),
		"enrollmentId": enrollmentID.String(),
		"amount":       dbAmount,
		"token":        dbTokenAddress,
		"chainId":      dbChainID,
		"market":       dbMarketAddress,
	})
	if _, err := tx.Exec(ctx, `INSERT INTO outbox_events(aggregate,type,payload) VALUES('order','order.confirmed',$1)`, outboxPayload); err != nil {
		return uuid.Nil, "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil, "", err
	}
	return enrollmentID, "confirmed", nil
}

// ApplyInput Confirmer.Apply 入参。
type ApplyInput struct {
	OrderID     uuid.UUID
	ChainID     int64
	TxHash      []byte // 32 bytes
	LogIndex    int
	BlockNumber int64
	BlockHash   []byte // 32 bytes
	BlockTime   time.Time
	EventSig    [32]byte
	Event       *chain.CoursePurchased
}

// U256 把 uint64 编码为 32 字节 big-endian（worker 内部 helper）。
func U256(v uint64) [32]byte {
	var b [32]byte
	binary.BigEndian.PutUint64(b[24:], v)
	return b
}

// buildIntentFromDB 把 purchase_intents + wallets 的行转成 chain.Intent。
//
// 输入：
//   - dbCourseKey:    encode(pi.course_key,'hex') 的 64-char hex
//   - dbTokenAddress: 0x 开头的 ERC20 地址
//   - dbAmount:       numeric → text（decimal 字符串）
//   - dbPriceVersion: int
//   - intentID:       UUID
//   - dbWalletAddress:0x 开头的钱包地址
//
// 任何字段转换失败 → 返回 ErrMismatch 包装错误，调用方把 order 标记 failed。
func buildIntentFromDB(
	dbCourseKey []byte,
	dbTokenAddress, dbAmount string,
	dbPriceVersion int,
	intentID uuid.UUID,
	dbWalletAddress string,
) (chain.Intent, error) {
	courseKey, err := chain.HexToBytes32(string(dbCourseKey))
	if err != nil {
		return chain.Intent{}, fmt.Errorf("courseKey: %w", err)
	}
	token := common.HexToAddress(dbTokenAddress)
	if token == (common.Address{}) {
		return chain.Intent{}, fmt.Errorf("token: zero address")
	}
	amountBig, ok := new(big.Int).SetString(dbAmount, 10)
	if !ok {
		return chain.Intent{}, fmt.Errorf("amount: not a decimal integer: %q", dbAmount)
	}
	amountBytes, err := chain.BigIntToU256(amountBig)
	if err != nil {
		return chain.Intent{}, fmt.Errorf("amount: %w", err)
	}
	wallet := common.HexToAddress(dbWalletAddress)
	if wallet == (common.Address{}) {
		return chain.Intent{}, fmt.Errorf("wallet: zero address")
	}
	return chain.Intent{
		CourseKey:    courseKey,
		Buyer:        wallet,
		Token:        token,
		Amount:       amountBytes,
		IntentID:     chain.Bytes16FromUUID(intentID),
		PriceVersion: U256(uint64(dbPriceVersion)), //nolint:gosec // DB 列已约束为非负 int
	}, nil
}
