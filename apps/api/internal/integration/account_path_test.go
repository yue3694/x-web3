package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/x-web3/api/internal/audit"
	"github.com/x-web3/api/internal/auth"
	"github.com/x-web3/api/internal/config"
	"github.com/x-web3/api/internal/handlers"
	"github.com/x-web3/api/internal/httpkit"
	"github.com/x-web3/api/internal/wallet"
)

// httpFixture 是一组针对全 HTTP 路径（router）的集成测试 fixture。
//
// 与 wallet_bind_test.go 不同：本文件**不**直接调用 wallet.Service，
// 而是构造完整 router + handler，跑 httptest.Server，让网络栈处理 cookie /
// JSON 解码 / status code / Set-Cookie 等中间环节。
type httpFixture struct {
	pool     *pgxpool.Pool
	rdb      *redis.Client
	mr       *miniredis.Miniredis
	store    *auth.SessionStore
	verifier auth.Verifier
	server   *httptest.Server
	domain   string
	secret   []byte
	cfg      *config.Config
	subject  string // 已签发 session 对应的 privy subject
}

// newHTTPFixture 构造真实 router + httptest.Server；
//
// 关键点：
//   - verifier = auth.NewVerifier(DevStub=true) 跳过 Privy JWKS，
//     用 DevSubject 模拟登录；
//   - redismini：每个用例独立 miniredis 实例，避免 nonce / session 串扰；
//   - rate limit（5/min wallet）：每个用例独立计数器所以不会撞限。
func newHTTPFixture(t *testing.T) *httpFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)

	pool := testPool(t) // BootPG / 测试容器已就位

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	logger := zap.NewNop()

	// 真正的 dev-stub verifier：subject 由测试方传入以保持用例之间隔离
	// （避免前一个用例把 status 改成 suspended 之后影响下一个用例）。
	subject := "did:privy:integration-http-" + uuid.NewString()
	verifier := &fixedSubjectVerifier{subject: subject}

	secret := []byte("0123456789abcdef0123456789abcdef")
	store := auth.NewSessionStore(rdb, secret, time.Hour)

	cfg := &config.Config{
		Env:             "dev",
		APIPort:         0,
		BaseURL:         "http://localhost",
		DatabaseURL:     "", // unused in router
		RedisURL:        "", // unused in router
		WebOrigin:       "", // allow any Origin for cross-origin ease
		SessionSecret:   secret,
		SessionTTL:      time.Hour,
		CookieSecure:    false,
		APIDomain:       "localhost",
		WalletNonceTTL:  5 * time.Minute,
		LoginRateLimit:  100, // 测试宽松限速
		WalletRateLimit: 100,
		LogLevel:        "info",
	}

	nonceStore := wallet.NewNonceStore(rdb, cfg.WalletNonceTTL)
	auditWriter := audit.NewWriter(pool, logger)
	walletSvc := wallet.NewService(pool, nonceStore, cfg.APIDomain, auditWriter)

	// 用真实路由：与 main.go 一致但只保留身份相关分组即可保持 DoD 紧凑。
	router := httpkit.NewRouter(logger, cfg.WebOrigin)
	v1 := router.Engine.Group("/api/v1")

	authH := handlers.NewAuthHandler(cfg, pool, verifier, store, auditWriter, logger)
	walletH := handlers.NewWalletHandler(cfg, pool, walletSvc, auditWriter, logger)
	meH := handlers.NewMeHandler(pool, auditWriter, logger, authH)

	authGroup := v1.Group("/auth")
	{
		authGroup.POST("/privy/session", httpkit.RateLimit(rdb, "login", cfg.LoginRateLimit, httpkit.ClientIPKey), httpkit.Wrap(authH.PostPrivySession))
		authGroup.POST("/session/refresh", auth.Middleware(verifier, store, pool), httpkit.Wrap(authH.RefreshSession))
		authGroup.DELETE("/session", httpkit.Wrap(authH.DeleteSession))
	}
	meGroup := v1.Group("/me")
	meGroup.Use(auth.Middleware(verifier, store, pool))
	{
		meGroup.GET("", httpkit.Wrap(meH.GetMe))
		walletLimit := httpkit.RateLimit(rdb, "wallet", cfg.WalletRateLimit, httpkit.UserIDKeyFunc)
		meGroup.POST("/wallets/nonce", walletLimit, httpkit.Wrap(walletH.IssueNonce))
		meGroup.POST("/wallets/link", walletLimit, httpkit.Wrap(walletH.Link))
		meGroup.DELETE("/wallets/:walletId", walletLimit, httpkit.Wrap(walletH.Unbind))
	}

	server := httptest.NewServer(router.Engine)
	t.Cleanup(server.Close)

	// 入参 isolates 跨用例 residue：每个用例 truncate wallets + 清 redis 前缀。
	if _, err := pool.Exec(context.Background(), `TRUNCATE wallets`); err != nil {
		t.Fatalf("truncate wallets: %v", err)
	}
	mr.FlushAll()

	return &httpFixture{
		pool: pool, rdb: rdb, mr: mr,
		store: store, verifier: verifier, server: server,
		domain: cfg.APIDomain, secret: secret, cfg: cfg,
		subject: subject,
	}
}

// fixedSubjectVerifier 是一个 dev-stub 替代：固定 subject，方便断言；
// 不依赖 NewVerifier 的 JWKS 加载，避免外部网络。
type fixedSubjectVerifier struct {
	subject string
}

func (v *fixedSubjectVerifier) Verify(_ context.Context, _ string) (*auth.Claims, error) {
	return &auth.Claims{
		Subject:  v.subject,
		Issuer:   "integration-stub",
		Audience: []string{"dev"},
		Expires:  time.Now().Add(time.Hour),
		IssuedAt: time.Now(),
	}, nil
}

// doJSON 简化 JSON 调用。
func (f *httpFixture) doJSON(t *testing.T, method, path string, body any, cookies ...*http.Cookie) (*http.Response, []byte) {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, f.server.URL+path, rdr)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	resp, err := f.server.Client().Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp, out
}

// extractSID 解析 set-cookie 头里的 sid 值。
func extractSID(t *testing.T, resp *http.Response) string {
	t.Helper()
	for _, sc := range resp.Header.Values("Set-Cookie") {
		if i := strings.Index(sc, "="); i >= 0 {
			name := sc[:i]
			if name == auth.CookieName {
				rest := sc[i+1:]
				if semi := strings.Index(rest, ";"); semi >= 0 {
					return rest[:semi]
				}
				return rest
			}
		}
	}
	t.Fatalf("no sid cookie set")
	return ""
}

// TestAccountLifecycle_HappyPath 跑设计文档 §2/§3 的"完整开户/绑定/解绑路径"，
// 走真实 router + 真实 wallet.Service + 真实 audit_logs 表。
func TestAccountLifecycle_HappyPath(t *testing.T) {
	f := newHTTPFixture(t)
	ctx := context.Background()

	// ─── 1. 开户：POST /api/v1/auth/privy/session ─────────────────
	loginResp, loginBody := f.doJSON(t, http.MethodPost, "/api/v1/auth/privy/session",
		map[string]any{"privyAccessToken": "opaque-stub-token"})
	if loginResp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d body=%s", loginResp.StatusCode, loginBody)
	}
	var profile struct {
		ID          string   `json:"id"`
		DisplayName string   `json:"displayName"`
		Roles       []string `json:"roles"`
		Permissions []string `json:"permissions"`
		Wallets     []any    `json:"wallets"`
	}
	if err := json.Unmarshal(loginBody, &profile); err != nil {
		t.Fatalf("decode profile: %v body=%s", err, loginBody)
	}
	if profile.ID == "" {
		t.Fatalf("missing id in profile: %s", loginBody)
	}
	uid, err := uuid.Parse(profile.ID)
	if err != nil {
		t.Fatalf("parse id: %v", err)
	}
	if len(profile.Roles) == 0 || profile.Roles[0] != "student" {
		t.Errorf("roles = %v, want student first", profile.Roles)
	}
	// student 默认应当至少包含 CERTIFICATE_READ / ORDER_CREATE
	wantPerms := map[string]bool{"CERTIFICATE_READ": false, "ORDER_CREATE": false}
	for _, p := range profile.Permissions {
		if _, ok := wantPerms[p]; ok {
			wantPerms[p] = true
		}
	}
	for p, seen := range wantPerms {
		if !seen {
			t.Errorf("missing student permission %s (got %v)", p, profile.Permissions)
		}
	}

	sid := extractSID(t, loginResp)
	cookie := &http.Cookie{Name: auth.CookieName, Value: sid}

	// ─── 2. 二次登录幂等：同人 subject 第二次请求仍只产生一个 user ──
	_, _ = f.doJSON(t, http.MethodPost, "/api/v1/auth/privy/session",
		map[string]any{"privyAccessToken": "opaque-stub-token"})
	var userCount int
	if err := f.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM users WHERE id = $1`, uid).Scan(&userCount); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if userCount != 1 {
		t.Fatalf("after 2x login user_count = %d, want 1 (AC-001)", userCount)
	}

	// ─── 3. GET /me ────────────────────────────────────────────────
	meResp, meBody := f.doJSON(t, http.MethodGet, "/api/v1/me", nil, cookie)
	if meResp.StatusCode != http.StatusOK {
		t.Fatalf("/me status = %d body=%s", meResp.StatusCode, meBody)
	}

	// ─── 4. 钱包绑定：nonce → sign → bind ─────────────────────────
	priv, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	addr := crypto.PubkeyToAddress(priv.PublicKey).Hex()

	nonceResp, nonceBody := f.doJSON(t, http.MethodPost, "/api/v1/me/wallets/nonce", nil, cookie)
	if nonceResp.StatusCode != http.StatusOK {
		t.Fatalf("nonce status = %d body=%s", nonceResp.StatusCode, nonceBody)
	}
	var noncePayload struct {
		Nonce     string    `json:"nonce"`
		Domain    string    `json:"domain"`
		ExpiresAt time.Time `json:"expiresAt"`
	}
	if err := json.Unmarshal(nonceBody, &noncePayload); err != nil {
		t.Fatalf("decode nonce: %v body=%s", err, nonceBody)
	}
	if noncePayload.Nonce == "" || noncePayload.Domain != f.domain {
		t.Fatalf("bad nonce payload: %+v", noncePayload)
	}

	// 用真实签名（与服务端 CanonicalMessage 完全一致）
	msg := wallet.CanonicalMessage(
		noncePayload.Nonce, 11155111, strings.ToLower(addr),
		f.domain, noncePayload.ExpiresAt.Format(time.RFC3339))
	raw, err := crypto.Sign(wallet.PrefixedHashForTest(msg), priv)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	raw[64] += 27
	sig := common.Bytes2Hex(raw)

	linkResp, linkBody := f.doJSON(t, http.MethodPost, "/api/v1/me/wallets/link", map[string]any{
		"chainId":   11155111,
		"address":   addr,
		"nonce":     noncePayload.Nonce,
		"expiry":    noncePayload.ExpiresAt.Format(time.RFC3339),
		"signature": sig,
		"domain":    f.domain,
	}, cookie)
	if linkResp.StatusCode != http.StatusOK {
		t.Fatalf("link status = %d body=%s", linkResp.StatusCode, linkBody)
	}
	var bindProfile struct {
		Wallets []map[string]any `json:"wallets"`
	}
	if err := json.Unmarshal(linkBody, &bindProfile); err != nil {
		t.Fatalf("decode bind: %v body=%s", err, linkBody)
	}
	if len(bindProfile.Wallets) != 1 {
		t.Fatalf("wallets len = %d, want 1", len(bindProfile.Wallets))
	}
	walletIDStr, _ := bindProfile.Wallets[0]["id"].(string)
	walletID, err := uuid.Parse(walletIDStr)
	if err != nil {
		t.Fatalf("parse wallet id: %v", err)
	}
	if !strings.EqualFold(bindProfile.Wallets[0]["address"].(string), addr) {
		t.Errorf("address mismatch: %v vs %s", bindProfile.Wallets[0]["address"], addr)
	}

	// 二次绑定同地址：应幂等 OK（设计允许）
	if link2Resp, link2Body := f.doJSON(t, http.MethodPost, "/api/v1/me/wallets/link", map[string]any{
		"chainId":   11155111,
		"address":   addr,
		"nonce":     noncePayload.Nonce, // 已用 → 409
		"expiry":    noncePayload.ExpiresAt.Format(time.RFC3339),
		"signature": sig,
		"domain":    f.domain,
	}, cookie); link2Resp.StatusCode == http.StatusOK {
		t.Errorf("nonce reuse should fail: status=%d body=%s", link2Resp.StatusCode, link2Body)
	}

	// ─── 5. 解绑最后钱包：应 409 ─────────────────────────────────
	lastUnbindResp, lastUnbindBody := f.doJSON(t, http.MethodDelete,
		fmt.Sprintf("/api/v1/me/wallets/%s", walletID), nil, cookie)
	if lastUnbindResp.StatusCode != http.StatusConflict {
		t.Fatalf("unbind last wallet status = %d body=%s, want 409",
			lastUnbindResp.StatusCode, lastUnbindBody)
	}
	var lastErrEnv struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(lastUnbindBody, &lastErrEnv)
	if lastErrEnv.Error.Code != "CANNOT_UNBIND_LAST_WALLET" {
		t.Errorf("error code = %q, want CANNOT_UNBIND_LAST_WALLET", lastErrEnv.Error.Code)
	}

	// ─── 6. 绑第二个地址后解绑第一个：应 204 ─────────────────────
	priv2, _ := crypto.GenerateKey()
	addr2 := crypto.PubkeyToAddress(priv2.PublicKey).Hex()

	nonceResp2, nonceBody2 := f.doJSON(t, http.MethodPost, "/api/v1/me/wallets/nonce", nil, cookie)
	if nonceResp2.StatusCode != http.StatusOK {
		t.Fatalf("nonce2 status = %d body=%s", nonceResp2.StatusCode, nonceBody2)
	}
	var nonce2 struct {
		Nonce     string    `json:"nonce"`
		Domain    string    `json:"domain"`
		ExpiresAt time.Time `json:"expiresAt"`
	}
	_ = json.Unmarshal(nonceBody2, &nonce2)
	msg2 := wallet.CanonicalMessage(nonce2.Nonce, 11155111, strings.ToLower(addr2),
		f.domain, nonce2.ExpiresAt.Format(time.RFC3339))
	raw2, _ := crypto.Sign(wallet.PrefixedHashForTest(msg2), priv2)
	raw2[64] += 27
	link2Resp, link2Body := f.doJSON(t, http.MethodPost, "/api/v1/me/wallets/link", map[string]any{
		"chainId":   11155111,
		"address":   addr2,
		"nonce":     nonce2.Nonce,
		"expiry":    nonce2.ExpiresAt.Format(time.RFC3339),
		"signature": common.Bytes2Hex(raw2),
		"domain":    f.domain,
	}, cookie)
	if link2Resp.StatusCode != http.StatusOK {
		t.Fatalf("link2 status = %d body=%s", link2Resp.StatusCode, link2Body)
	}
	// 取 walletID2（最新一条）
	_ = []struct {
		ID      string `json:"id"`
		Address string `json:"address"`
	}{} // 占位：当前 draft 未使用；保留结构以便后续按需 fill
	listResp, listBody := f.doJSON(t, http.MethodGet, "/api/v1/me", nil, cookie)
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("/me status = %d body=%s", listResp.StatusCode, listBody)
	}
	var meList struct {
		Wallets []map[string]any `json:"wallets"`
	}
	_ = json.Unmarshal(listBody, &meList)
	if len(meList.Wallets) != 2 {
		t.Fatalf("wallets = %d, want 2", len(meList.Wallets))
	}
	for _, w := range meList.Wallets {
		if strings.EqualFold(w["address"].(string), addr) {
			walletIDStr = w["id"].(string)
		}
	}
	walletID, _ = uuid.Parse(walletIDStr)

	// 解绑第一个
	unbindResp, unbindBody := f.doJSON(t, http.MethodDelete,
		fmt.Sprintf("/api/v1/me/wallets/%s", walletID), nil, cookie)
	if unbindResp.StatusCode != http.StatusNoContent {
		t.Fatalf("unbind status = %d body=%s, want 204", unbindResp.StatusCode, unbindBody)
	}

	// ─── 7. 审计落库：登录 + bind + unbind 三条 ─────────────────
	var auditCount int
	if err := f.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM audit_logs WHERE actor_user_id = $1 AND action IN ('user.logged_in','wallet.linked','wallet.unbound')`,
		uid).Scan(&auditCount); err != nil {
		t.Fatalf("count audit: %v", err)
	}
	// 登录可能 1~2 次（两次 login 走 dev-stub 同样 subject → 同样 user）
	// bind 至少 1 次（第二次会 409 不写），unbind 1 次。
	if auditCount < 3 {
		t.Errorf("audit_logs count = %d, want >= 3", auditCount)
	}

	// ─── 8. 退出：DELETE /api/v1/auth/session ─────────────────────
	logoutResp, _ := f.doJSON(t, http.MethodDelete, "/api/v1/auth/session", nil, cookie)
	if logoutResp.StatusCode != http.StatusNoContent {
		t.Fatalf("logout status = %d", logoutResp.StatusCode)
	}
	// 再次 GET /me 应 401
	post, _ := f.doJSON(t, http.MethodGet, "/api/v1/me", nil, cookie)
	if post.StatusCode != http.StatusUnauthorized {
		t.Errorf("after logout /me = %d, want 401", post.StatusCode)
	}
}

// TestAccountLifecycle_LoginIsIdempotent 上游 AC-001：同人 subject 重复登录
// 仍只产生一个 user。完整 HTTP 路径版。
func TestAccountLifecycle_LoginIsIdempotent(t *testing.T) {
	f := newHTTPFixture(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		r, body := f.doJSON(t, http.MethodPost, "/api/v1/auth/privy/session",
			map[string]any{"privyAccessToken": fmt.Sprintf("tok-%d", i)})
		if r.StatusCode != http.StatusOK {
			t.Fatalf("login #%d status=%d body=%s", i, r.StatusCode, body)
		}
	}
	var n int
	if err := f.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM users WHERE privy_user_id = $1`, f.subject).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("user count = %d, want 1 (AC-001)", n)
	}
}

// TestAccountLifecycle_SuspendedUserBlocked 上游 AC-003：被 suspend 的用户
// 在 session 存活期内下一次访问会被强制拒绝并清 sid。
func TestAccountLifecycle_SuspendedUserBlocked(t *testing.T) {
	f := newHTTPFixture(t)
	ctx := context.Background()

	r, body := f.doJSON(t, http.MethodPost, "/api/v1/auth/privy/session",
		map[string]any{"privyAccessToken": "x"})
	if r.StatusCode != http.StatusOK {
		t.Fatalf("login status=%d body=%s", r.StatusCode, body)
	}
	sid := extractSID(t, r)
	cookie := &http.Cookie{Name: auth.CookieName, Value: sid}

	// 此时 /me 应 200
	if r1, _ := f.doJSON(t, http.MethodGet, "/api/v1/me", nil, cookie); r1.StatusCode != http.StatusOK {
		t.Fatalf("/me pre-suspend status=%d", r1.StatusCode)
	}
	// 把 user 状态改为 suspended
	if _, err := f.pool.Exec(ctx,
		`UPDATE users SET status='suspended' WHERE privy_user_id=$1`, f.subject); err != nil {
		t.Fatalf("suspend: %v", err)
	}
	// 再访问 /me：403 + session 被销毁
	r2, body2 := f.doJSON(t, http.MethodGet, "/api/v1/me", nil, cookie)
	if r2.StatusCode != http.StatusForbidden {
		t.Fatalf("/me post-suspend status=%d body=%s want 403", r2.StatusCode, body2)
	}
	data, err := f.store.Read(ctx, sid)
	if err != nil {
		t.Fatalf("read session: %v", err)
	}
	if data != nil {
		t.Errorf("expected session destroyed, got %+v", data)
	}
}
