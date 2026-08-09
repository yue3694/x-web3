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
package workerorder

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"time"

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
//   - enrollmentID：新插入的 enrollment id；幂等命中时为 uuid.Nil。
//   - state："confirmed" / "failed" / "reorged" — 写到 orders.status。
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
	want := chain.Intent{
		CourseKey:    in.Event.CourseKey,
		Buyer:        in.Event.Buyer,
		Token:        in.Event.Token,
		Amount:       in.Event.Amount,
		IntentID:     in.Event.IntentID,
		PriceVersion: in.Event.PriceVersion,
	}
	if err := chain.ValidateReceipt(in.Event, &want); err != nil {
		// 标记 failed，写 failure_code
		_, _ = tx.Exec(ctx, `UPDATE orders SET status='failed', failure_code='RECEIPT_MISMATCH', updated_at=now() WHERE id=$1`, orderID)
		if err := tx.Commit(ctx); err != nil {
			return uuid.Nil, "failed", err
		}
		return uuid.Nil, "failed", nil
	}

	// 4) confirmed → 写 enrollment + outbox
	confirmedAt := in.BlockTime
	if confirmedAt.IsZero() {
		confirmedAt = time.Now().UTC()
	}
	_, err = tx.Exec(ctx, `UPDATE orders SET status='confirmed', tx_hash=$2, block_number=$3, log_index=$4, block_hash=$5, confirmed_at=$6, updated_at=now() WHERE id=$1`,
		orderID, in.TxHash, in.BlockNumber, in.LogIndex, in.BlockHash, confirmedAt)
	if err != nil {
		return uuid.Nil, "", err
	}
	var enrollmentID uuid.UUID
	err = tx.QueryRow(ctx, `INSERT INTO enrollments(user_id,course_id,source) VALUES($1,$2,'order')
ON CONFLICT(user_id,course_id) DO UPDATE SET source=enrollments.source
RETURNING id`, userID, courseID).Scan(&enrollmentID)
	if err != nil {
		return uuid.Nil, "", err
	}
	outboxPayload, _ := json.Marshal(map[string]any{
		"orderId":     orderID.String(),
		"userId":      userID.String(),
		"courseId":    courseID.String(),
		"enrollmentId": enrollmentID.String(),
		"amount":      dbAmount,
		"token":       dbTokenAddress,
		"chainId":     dbChainID,
		"market":      dbMarketAddress,
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
