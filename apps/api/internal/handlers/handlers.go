// Package handlers 包含 cmd/api 引用的 HTTP handler 构造。
//
// 拆分到独立包是为了避免 main.go 膨胀；handler 全部以函数式构造，
// 不依赖外部全局状态。
package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/x-web3/api/internal/audit"
	"github.com/x-web3/api/internal/auth"
	"github.com/x-web3/api/internal/config"
	"github.com/x-web3/api/internal/errcode"
	"github.com/x-web3/api/internal/httpkit"
	"github.com/x-web3/api/internal/user"
	"github.com/x-web3/api/internal/wallet"
)

// AuthHandler 登录、登出。
type AuthHandler struct {
	cfg      *config.Config
	pool     *pgxpool.Pool
	verifier auth.Verifier
	session  *auth.SessionStore
	auditor  *audit.Writer
	logger   *zap.Logger
}

// NewAuthHandler ...
func NewAuthHandler(cfg *config.Config, pool *pgxpool.Pool, verifier auth.Verifier, session *auth.SessionStore, auditor *audit.Writer, logger *zap.Logger) *AuthHandler {
	return &AuthHandler{cfg: cfg, pool: pool, verifier: verifier, session: session, auditor: auditor, logger: logger}
}

type privySessionReq struct {
	PrivyAccessToken string `json:"privyAccessToken" binding:"required"`
}

// PostPrivySession 验证 Privy token → upsert user → 发 sid cookie。
func (h *AuthHandler) PostPrivySession(c *httpkit.Context) {
	var req privySessionReq
	if !c.MustJSON(&req) {
		return
	}
	claims, err := h.verifier.Verify(c.Request.Context(), req.PrivyAccessToken)
	if err != nil {
		h.logger.Warn("privy_token_rejected", zap.Error(err))
		httpkit.Error(c, http.StatusUnauthorized, errcode.InvalidPrivyToken, "invalid privy token", nil)
		return
	}

	tx, err := h.pool.Begin(c.Request.Context())
	if err != nil {
		httpkit.Internal(c, err)
		return
	}
	defer tx.Rollback(c.Request.Context()) //nolint:errcheck

	repo := user.NewRepo(h.pool)
	u, err := repo.UpsertByPrivySubject(c.Request.Context(), tx, claims.Subject, "")
	if err != nil {
		httpkit.Internal(c, err)
		return
	}
	if err := repo.GrantDefaultRole(c.Request.Context(), tx, u.ID); err != nil {
		httpkit.Internal(c, err)
		return
	}
	if err := tx.Commit(c.Request.Context()); err != nil {
		httpkit.Internal(c, err)
		return
	}

	sid, _, err := h.session.Issue(c.Request.Context(), claims.Subject, fpFromCtx(c))
	if err != nil {
		httpkit.Internal(c, err)
		return
	}
	auth.SetSessionCookie(c, sid, int(h.cfg.SessionTTL.Seconds()), h.cfg.CookieSecure)

	profile := h.profileOrInternal(c, u.ID)
	if profile == nil {
		return
	}

	_ = h.auditor.Log(c.Request.Context(), audit.Entry{
		ActorUserID: &u.ID,
		Action:      audit.ActionUserLoggedIn,
		TargetType:  "user",
		TargetID:    u.ID.String(),
		IP:          c.ClientIP(),
		UserAgent:   c.Request.UserAgent(),
	})
	c.JSON(http.StatusOK, profile)
}

// DeleteSession 登出。
func (h *AuthHandler) DeleteSession(c *httpkit.Context) {
	sid, _ := c.Cookie(auth.CookieName)
	if sid != "" {
		_ = h.session.Destroy(c.Request.Context(), sid)
	}
	auth.ClearSessionCookie(c, h.cfg.CookieSecure)
	c.Status(http.StatusNoContent)
}

// RefreshSession 复用当前 cookie 的 sid 重新颁发一个 session，原子轮换 sid。
// 未登录或 sid 失效 → 401；不存在"匿名刷新"路径。
func (h *AuthHandler) RefreshSession(c *httpkit.Context) {
	sid, _ := c.Cookie(auth.CookieName)
	if sid == "" {
		httpkit.Error(c, http.StatusUnauthorized, errcode.SessionExpired, "missing session", nil)
		return
	}
	newSID, data, err := h.session.Refresh(c.Request.Context(), sid, fpFromCtx(c))
	if err != nil || data == nil {
		httpkit.Error(c, http.StatusUnauthorized, errcode.SessionExpired, "session refresh failed", nil)
		return
	}
	auth.SetSessionCookie(c, newSID, int(h.cfg.SessionTTL.Seconds()), h.cfg.CookieSecure)
	c.JSON(http.StatusOK, gin.H{
		"subject":   data.Subject,
		"expiresAt": data.ExpiresAt.UTC().Format(time.RFC3339),
	})
}

// profileOrInternal 返回用户 profile，失败时已写 500。
func (h *AuthHandler) profileOrInternal(c *httpkit.Context, id uuid.UUID) any {
	repo := user.NewRepo(h.pool)
	u, err := repo.GetByID(c.Request.Context(), id)
	if err != nil || u == nil {
		httpkit.Internal(c, errors.New("profile lookup failed"))
		return nil
	}
	wallets, err := repo.ListWallets(c.Request.Context(), u.ID)
	if err != nil {
		httpkit.Internal(c, err)
		return nil
	}
	roles, err := repo.ListRoleCodes(c.Request.Context(), u.ID)
	if err != nil {
		httpkit.Internal(c, err)
		return nil
	}
	perms, err := repo.ListPermissions(c.Request.Context(), u.ID)
	if err != nil {
		httpkit.Internal(c, err)
		return nil
	}
	primary := (*map[string]any)(nil)
	if len(wallets) > 0 {
		w := wallets[0]
		primary = &map[string]any{
			"id":        w.ID.String(),
			"chainId":   w.ChainID,
			"address":   w.Address,
			"isPrimary": w.IsPrimary,
			"boundAt":   w.BoundAt.UTC().Format(time.RFC3339),
		}
	}
	return gin.H{
		"id":            u.ID.String(),
		"displayName":   u.DisplayName,
		"primaryWallet": primary,
		"wallets":       toWallets(wallets),
		"roles":         roles,
		"permissions":   perms,
	}
}

func fpFromCtx(c *httpkit.Context) string {
	return c.Request.UserAgent()
}

// MeHandler 当前用户 profile。
type MeHandler struct {
	pool    *pgxpool.Pool
	auditor *audit.Writer
	logger  *zap.Logger
	auth    *AuthHandler // 复用 profile 构造
}

func NewMeHandler(pool *pgxpool.Pool, auditor *audit.Writer, logger *zap.Logger, auth *AuthHandler) *MeHandler {
	return &MeHandler{pool: pool, auditor: auditor, logger: logger, auth: auth}
}

func (h *MeHandler) GetMe(c *httpkit.Context) {
	uid, err := userIDFromCtx(c)
	if err != nil {
		httpkit.Error(c, http.StatusUnauthorized, errcode.SessionExpired, "no session", nil)
		return
	}
	profile := h.auth.profileOrInternal(c, uid)
	if profile == nil {
		return
	}
	c.JSON(http.StatusOK, profile)
}

// WalletHandler 钱包绑定 / 解绑。
type WalletHandler struct {
	cfg       *config.Config
	pool      *pgxpool.Pool
	walletSvc *wallet.Service
	auditor   *audit.Writer
	logger    *zap.Logger
}

func NewWalletHandler(cfg *config.Config, pool *pgxpool.Pool, svc *wallet.Service, auditor *audit.Writer, logger *zap.Logger) *WalletHandler {
	return &WalletHandler{cfg: cfg, pool: pool, walletSvc: svc, auditor: auditor, logger: logger}
}

type linkReq struct {
	ChainID   int64  `json:"chainId"   binding:"required"`
	Address   string `json:"address"   binding:"required"`
	Nonce     string `json:"nonce"     binding:"required"`
	Expiry    string `json:"expiry"    binding:"required"`
	Signature string `json:"signature" binding:"required"`
	Domain    string `json:"domain"`
}

// IssueNonce 颁发与当前用户绑定的一次性钱包签名 challenge。
func (h *WalletHandler) IssueNonce(c *httpkit.Context) {
	uid, err := userIDFromCtx(c)
	if err != nil {
		httpkit.Error(c, http.StatusUnauthorized, errcode.SessionExpired, "no session", nil)
		return
	}
	nonce, expiresAt, err := h.walletSvc.IssueNonce(c.Request.Context(), uid)
	if err != nil {
		httpkit.Internal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"nonce":     nonce,
		"domain":    h.cfg.APIDomain,
		"expiresAt": expiresAt.UTC().Format(time.RFC3339),
	})
}

func (h *WalletHandler) Link(c *httpkit.Context) {
	var req linkReq
	if !c.MustJSON(&req) {
		return
	}
	uid, err := userIDFromCtx(c)
	if err != nil {
		httpkit.Error(c, http.StatusUnauthorized, errcode.SessionExpired, "no session", nil)
		return
	}
	expiry, err := time.Parse(time.RFC3339, req.Expiry)
	if err != nil {
		httpkit.Error(c, http.StatusBadRequest, errcode.BadRequest, "expiry must be RFC3339", nil)
		return
	}
	if err := h.walletSvc.Bind(c.Request.Context(), wallet.BindRequest{
		UserID:    uid,
		ChainID:   req.ChainID,
		Address:   req.Address,
		Nonce:     req.Nonce,
		Expiry:    expiry,
		Signature: req.Signature,
		Domain:    ifEmpty(req.Domain, h.cfg.APIDomain),
		IP:        c.ClientIP(),
		UA:        c.Request.UserAgent(),
	}); err != nil {
		mapWalletErr(c, err)
		return
	}
	// 200: 重新查 wallet 列表返回
	repo := user.NewRepo(h.pool)
	ws, err := repo.ListWallets(c.Request.Context(), uid)
	if err != nil {
		httpkit.Internal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"wallets": toWallets(ws)})
}

func (h *WalletHandler) Unbind(c *httpkit.Context) {
	wid, err := uuid.Parse(c.Param("walletId"))
	if err != nil {
		httpkit.Error(c, http.StatusBadRequest, errcode.BadRequest, "walletId must be uuid", nil)
		return
	}
	uid, err := userIDFromCtx(c)
	if err != nil {
		httpkit.Error(c, http.StatusUnauthorized, errcode.SessionExpired, "no session", nil)
		return
	}
	if err := h.walletSvc.Unbind(c.Request.Context(), uid, wid, c.ClientIP(), c.Request.UserAgent()); err != nil {
		mapWalletErr(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// userIDFromCtx 解析当前登录 user。
// auth.Middleware 已经把 DB user.id 注入到 ctx 的 "user_id" 键；
// 这里只是把 string 解析回 uuid。
func userIDFromCtx(c *httpkit.Context) (uuid.UUID, error) {
	raw := c.UserID()
	if raw == "" {
		return uuid.Nil, errors.New("no user_id in context")
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, fmt.Errorf("parse user_id: %w", err)
	}
	return id, nil
}

// toWallets 把 repo wallet 转 JSON 友好结构。
func toWallets(ws []user.Wallet) []map[string]any {
	out := make([]map[string]any, 0, len(ws))
	for _, w := range ws {
		out = append(out, map[string]any{
			"id":        w.ID.String(),
			"chainId":   w.ChainID,
			"address":   w.Address,
			"isPrimary": w.IsPrimary,
			"boundAt":   w.BoundAt.UTC().Format(time.RFC3339),
		})
	}
	return out
}

func ifEmpty(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func mapWalletErr(c *httpkit.Context, err error) {
	msg := err.Error()
	switch {
	case contains(msg, "already bound"):
		httpkit.Error(c, http.StatusConflict, errcode.WalletAlreadyBound, msg, nil)
	case contains(msg, "nonce reused"):
		httpkit.Error(c, http.StatusConflict, errcode.WalletNonceReused, msg, nil)
	case contains(msg, "signature"):
		httpkit.Error(c, http.StatusUnprocessableEntity, errcode.WalletSignatureInvalid, msg, nil)
	case contains(msg, "last wallet"):
		httpkit.Error(c, http.StatusConflict, errcode.CannotUnbindLastWallet, msg, nil)
	case contains(msg, "domain"):
		httpkit.Error(c, http.StatusUnprocessableEntity, errcode.WalletSignatureInvalid, msg, nil)
	case contains(msg, "expired"):
		httpkit.Error(c, http.StatusUnprocessableEntity, errcode.WalletSignatureInvalid, msg, nil)
	default:
		httpkit.Internal(c, err)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	// avoid importing strings for one call
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// subject-to-user-id helper reserved for future use; auth.Middleware
// already performs the lookup and injects user_id into ctx.
func resolveUserID(ctx context.Context, pool *pgxpool.Pool, subject string) (uuid.UUID, error) {
	const q = `SELECT id FROM users WHERE privy_user_id = $1`
	var id uuid.UUID
	err := pool.QueryRow(ctx, q, subject).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, errors.New("user not found for subject")
	}
	return id, err
}
