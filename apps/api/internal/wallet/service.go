package wallet

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/x-web3/api/internal/audit"
	"github.com/x-web3/api/internal/user"
)

// BindRequest 包含绑定所需的全部字段。
type BindRequest struct {
	UserID    uuid.UUID
	ChainID   int64
	Address   string
	Nonce     string
	Expiry    time.Time
	Signature string
	Domain    string
	IP        string
	UA        string
}

type LoginRequest struct {
	ChainID     int64
	Address     string
	Nonce       string
	Expiry      time.Time
	Signature   string
	Domain      string
	DisplayName string
}

// Service 是绑定业务入口。
type Service struct {
	pool    *pgxpool.Pool
	nonces  *NonceStore
	domain  string
	auditor *audit.Writer
}

func NewService(pool *pgxpool.Pool, nonces *NonceStore, domain string, auditor *audit.Writer) *Service {
	return &Service{pool: pool, nonces: nonces, domain: domain, auditor: auditor}
}

func (s *Service) IssueNonce(ctx context.Context, userID uuid.UUID) (string, time.Time, error) {
	return s.nonces.Issue(ctx, userID.String())
}

func loginNonceOwner(chainID int64, address string) string {
	return fmt.Sprintf("login:%d:%s", chainID, strings.ToLower(common.HexToAddress(address).Hex()))
}

func (s *Service) IssueLoginNonce(ctx context.Context, chainID int64, address string) (string, time.Time, bool, string, error) {
	if chainID <= 0 || !common.IsHexAddress(address) {
		return "", time.Time{}, false, "", errors.New("wallet: bad login identity")
	}
	addr := strings.ToLower(common.HexToAddress(address).Hex())
	var displayName string
	err := s.pool.QueryRow(ctx, `SELECT u.display_name FROM wallets w JOIN users u ON u.id=w.user_id
WHERE w.chain_id=$1 AND lower(w.address)=$2`, chainID, addr).Scan(&displayName)
	registered := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", time.Time{}, false, "", err
	}
	nonce, expiry, err := s.nonces.Issue(ctx, loginNonceOwner(chainID, addr))
	return nonce, expiry, registered, displayName, err
}

func (s *Service) Login(ctx context.Context, req LoginRequest) (*user.User, bool, error) {
	if err := VerifyDomain(req.Domain, s.domain); err != nil {
		return nil, false, err
	}
	if req.Expiry.Before(time.Now().UTC()) || !common.IsHexAddress(req.Address) {
		return nil, false, errors.New("wallet: login challenge expired or invalid")
	}
	addr := strings.ToLower(common.HexToAddress(req.Address).Hex())
	msg := CanonicalLoginMessage(req.Nonce, req.ChainID, addr, req.Domain, req.Expiry.UTC().Format(time.RFC3339))
	if err := VerifyEIP191(msg, req.Signature, addr); err != nil {
		return nil, false, err
	}
	if err := s.nonces.Consume(ctx, req.Nonce, loginNonceOwner(req.ChainID, addr)); err != nil {
		return nil, false, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	repo := user.NewRepo(s.pool)
	var userID uuid.UUID
	err = tx.QueryRow(ctx, `SELECT user_id FROM wallets WHERE chain_id=$1 AND lower(address)=$2 FOR UPDATE`, req.ChainID, addr).Scan(&userID)
	created := errors.Is(err, pgx.ErrNoRows)
	if err != nil && !created {
		return nil, false, err
	}
	var u *user.User
	if created {
		name := strings.TrimSpace(req.DisplayName)
		if len([]rune(name)) < 2 || len([]rune(name)) > 40 {
			return nil, false, errors.New("wallet: display name must be 2-40 characters")
		}
		subject := fmt.Sprintf("wallet:eip155:%d:%s", req.ChainID, addr)
		u, err = repo.UpsertByPrivySubject(ctx, tx, subject, name)
		if err != nil {
			return nil, false, err
		}
		if err = repo.GrantDefaultRole(ctx, tx, u.ID); err != nil {
			return nil, false, err
		}
		if err = repo.BindWallet(ctx, tx, &user.Wallet{UserID: u.ID, ChainID: req.ChainID, Address: addr, IsPrimary: true}); err != nil {
			return nil, false, err
		}
	} else {
		u, err = repo.GetByID(ctx, userID)
		if err != nil || u == nil {
			return nil, false, fmt.Errorf("wallet: user lookup: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, false, err
	}
	return u, created, nil
}

// Bind 完整流程：
//  1. 校验 nonce 未用且未过期
//  2. 校验 eip-191 签名
//  3. tx 内：lock (chain_id, address) 行；如已存在 → 409
//  4. insert wallets(user_id, ...)
//  5. 写 audit（tx 外）
func (s *Service) Bind(ctx context.Context, req BindRequest) error {
	// 1. domain check
	if err := VerifyDomain(req.Domain, s.domain); err != nil {
		return err
	}
	if req.Expiry.Before(time.Now().UTC()) {
		return errors.New("wallet: signature expired")
	}
	if !common.IsHexAddress(req.Address) {
		return errors.New("wallet: bad address")
	}
	addr := strings.ToLower(common.HexToAddress(req.Address).Hex())
	// 2. signature
	msg := CanonicalMessage(req.Nonce, req.ChainID, addr, req.Domain, req.Expiry.UTC().Format(time.RFC3339))
	if err := VerifyEIP191(msg, req.Signature, addr); err != nil {
		return err
	}

	// 3. nonce consume (idempotent; if reused → reject)
	if err := s.nonces.Consume(ctx, req.Nonce, req.UserID.String()); err != nil {
		return err
	}

	// 4. tx：lock + insert
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Lock existing row if any (race-safe)
	const lockQ = `SELECT id FROM wallets WHERE chain_id = $1 AND address = $2 FOR UPDATE`
	var existingID uuid.UUID
	err = tx.QueryRow(ctx, lockQ, req.ChainID, addr).Scan(&existingID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("wallet: lock: %w", err)
	}
	if existingID != uuid.Nil {
		return errors.New("wallet: already bound to another user")
	}

	// Insert
	_, err = tx.Exec(ctx, `
INSERT INTO wallets (user_id, chain_id, address, is_primary)
VALUES ($1, $2, $3, false)
`, req.UserID, req.ChainID, addr)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return errors.New("wallet: already bound (race)")
		}
		return fmt.Errorf("wallet: insert: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}

	// 5. audit (best-effort, non-blocking)
	_ = s.auditor.Log(ctx, audit.Entry{
		ActorUserID: &req.UserID,
		Action:      audit.ActionWalletLinked,
		TargetType:  "wallet",
		TargetID:    addr,
		After:       map[string]any{"chain_id": req.ChainID, "address": addr},
		IP:          req.IP,
		UserAgent:   req.UA,
	})
	return nil
}

// Unbind 解绑钱包。
//
// 拒绝条件：
//   - 钱包不存在；
//   - 钱包不属于该用户；
//   - 这是用户最后一个钱包（解绑后无登录入口）。
//   - 用户仍有 pending 订单（F03 引入后启用）。
func (s *Service) Unbind(ctx context.Context, userID, walletID uuid.UUID, ip, ua string) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// 验证归属 + lock
	const q1 = `SELECT user_id FROM wallets WHERE id = $1 FOR UPDATE`
	var ownerID uuid.UUID
	err = tx.QueryRow(ctx, q1, walletID).Scan(&ownerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return errors.New("wallet: not found")
	}
	if err != nil {
		return err
	}
	if ownerID != userID {
		return errors.New("wallet: forbidden")
	}

	// 计数
	var count int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM wallets WHERE user_id = $1`, userID).Scan(&count); err != nil {
		return err
	}
	if count <= 1 {
		return errors.New("wallet: cannot unbind last wallet")
	}

	// 检查 pending order（占位；F03 真正接入后启用）
	// var hasPending bool
	// _ = tx.QueryRow(ctx, `SELECT EXISTS (...)`).Scan(&hasPending)
	// if hasPending { return errors.New("wallet: pending orders") }

	if _, err := tx.Exec(ctx, `DELETE FROM wallets WHERE id = $1`, walletID); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	_ = s.auditor.Log(ctx, audit.Entry{
		ActorUserID: &userID,
		Action:      audit.ActionWalletUnbound,
		TargetType:  "wallet",
		TargetID:    walletID.String(),
		IP:          ip,
		UserAgent:   ua,
	})
	return nil
}
