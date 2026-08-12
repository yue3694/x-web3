package auth_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MicahParks/jwkset"
	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/zap"

	"github.com/x-web3/api/internal/auth"
)

// signPrivyToken 用指定私钥签发一个符合 Privy 字段约定的 ES256 JWT。
func signPrivyToken(t *testing.T, priv *ecdsa.PrivateKey, kid string, claims jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	tok.Header["kid"] = kid
	s, err := tok.SignedString(priv)
	if err != nil {
		t.Fatalf("SignedString: %v", err)
	}
	return s
}

// jwksFromPubToJSON 把公钥打包成 JWKS JSON（JWK Set），用于 HTTP mock。
func jwksFromPubToJSON(t *testing.T, pub *ecdsa.PublicKey, kid string) jwkset.JWKSMarshal {
	t.Helper()
	jwk, err := jwkset.NewJWKFromKey(pub, jwkset.JWKOptions{
		Metadata: jwkset.JWKMetadataOptions{KID: kid},
	})
	if err != nil {
		t.Fatalf("NewJWKFromKey: %v", err)
	}
	return jwkset.JWKSMarshal{Keys: []jwkset.JWKMarshal{jwk.Marshal()}}
}

// newJWKSServer 启动一个 httptest server 提供 JWKS 内容。
func newJWKSServer(t *testing.T, kid string, pub *ecdsa.PublicKey) *httptest.Server {
	t.Helper()
	set := jwksFromPubToJSON(t, pub, kid)
	raw, err := json.Marshal(set)
	if err != nil {
		t.Fatalf("marshal jwks: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestDevStubVerifier_Verify 验证 dev stub 在固定 subject 下放行任意 token。
func TestDevStubVerifier_Verify(t *testing.T) {
	logger := zap.NewNop()
	v, err := auth.NewVerifier(context.Background(), auth.Config{
		DevStub:    true,
		DevSubject: "did:privy:stub-123",
	}, logger)
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	c, err := v.Verify(context.Background(), "anything")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if c.Subject != "did:privy:stub-123" {
		t.Errorf("subject = %q, want did:privy:stub-123", c.Subject)
	}
	if time.Until(c.Expires) <= 0 {
		t.Errorf("expected expires in the future, got %v", c.Expires)
	}
}

// TestDevStub_MissingSubject 拒绝空 subject。
func TestDevStub_MissingSubject(t *testing.T) {
	logger := zap.NewNop()
	_, err := auth.NewVerifier(context.Background(), auth.Config{
		DevStub: true,
	}, logger)
	if err == nil {
		t.Fatal("expected error when DevSubject is empty")
	}
}

// TestJWKSVerifier_MissingConfig 拒绝缺少 Privy 配置（除非 dev stub）。
func TestJWKSVerifier_MissingConfig(t *testing.T) {
	logger := zap.NewNop()
	_, err := auth.NewVerifier(context.Background(), auth.Config{
		// 故意留空
	}, logger)
	if err == nil {
		t.Fatal("expected error when Privy config missing")
	}
}

// TestJWKSVerifier_AcceptsValidES256Token 验证合法 ES256 token 能完整解析所有 claim。
func TestJWKSVerifier_AcceptsValidES256Token(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	kid := "test-kid-1"
	server := newJWKSServer(t, kid, &priv.PublicKey)
	defer server.Close()

	logger := zap.NewNop()
	v, err := auth.NewVerifier(context.Background(), auth.Config{
		AppID:    "app-1",
		JWKSURL:  server.URL,
		Audience: "app-1",
	}, logger)
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	claims := jwt.MapClaims{
		"sub": "did:privy:user-1",
		"iss": "privy.io",
		"aud": "app-1",
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Unix(),
	}
	token := signPrivyToken(t, priv, kid, claims)

	got, err := v.Verify(context.Background(), "Bearer "+token)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got.Subject != "did:privy:user-1" {
		t.Errorf("subject = %q", got.Subject)
	}
	if got.Issuer != "privy.io" {
		t.Errorf("issuer = %q", got.Issuer)
	}
	if len(got.Audience) == 0 || got.Audience[0] != "app-1" {
		t.Errorf("audience = %v", got.Audience)
	}
	if time.Until(got.Expires) <= 0 {
		t.Errorf("expires in past: %v", got.Expires)
	}
}

// TestJWKSVerifier_RejectsExpiredToken 过期 token 必须被拒绝。
func TestJWKSVerifier_RejectsExpiredToken(t *testing.T) {
	priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	kid := "kid-exp"
	server := newJWKSServer(t, kid, &priv.PublicKey)
	defer server.Close()

	v, err := auth.NewVerifier(context.Background(), auth.Config{
		AppID: "app-x", JWKSURL: server.URL, Audience: "app-x",
	}, zap.NewNop())
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	claims := jwt.MapClaims{
		"sub": "did:privy:user-exp",
		"iss": "privy.io",
		"aud": "app-x",
		"exp": time.Now().Add(-time.Minute).Unix(), // 已过期
		"iat": time.Now().Add(-time.Hour).Unix(),
	}
	token := signPrivyToken(t, priv, kid, claims)
	if _, err := v.Verify(context.Background(), token); err == nil {
		t.Fatal("expected error for expired token")
	}
}

// TestJWKSVerifier_RejectsWrongAudience audience 不匹配必须被拒绝。
func TestJWKSVerifier_RejectsWrongAudience(t *testing.T) {
	priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	kid := "kid-aud"
	server := newJWKSServer(t, kid, &priv.PublicKey)
	defer server.Close()

	v, err := auth.NewVerifier(context.Background(), auth.Config{
		AppID: "app-correct", JWKSURL: server.URL, Audience: "app-correct",
	}, zap.NewNop())
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	claims := jwt.MapClaims{
		"sub": "did:privy:u",
		"iss": "privy.io",
		"aud": "app-other",
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Unix(),
	}
	token := signPrivyToken(t, priv, kid, claims)
	if _, err := v.Verify(context.Background(), token); err == nil {
		t.Fatal("expected error for wrong audience")
	}
}

// TestJWKSVerifier_RejectsWrongIssuer 非 privy.io issuer 必须被拒绝。
func TestJWKSVerifier_RejectsWrongIssuer(t *testing.T) {
	priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	kid := "kid-iss"
	server := newJWKSServer(t, kid, &priv.PublicKey)
	defer server.Close()

	v, err := auth.NewVerifier(context.Background(), auth.Config{
		AppID: "app-y", JWKSURL: server.URL, Audience: "app-y",
	}, zap.NewNop())
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	claims := jwt.MapClaims{
		"sub": "did:privy:u",
		"iss": "evil-issuer.example",
		"aud": "app-y",
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Unix(),
	}
	token := signPrivyToken(t, priv, kid, claims)
	if _, err := v.Verify(context.Background(), token); err == nil {
		t.Fatal("expected error for wrong issuer")
	}
}

// TestJWKSVerifier_RejectsMissingSubject 缺少 sub 必须被拒绝。
func TestJWKSVerifier_RejectsMissingSubject(t *testing.T) {
	priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	kid := "kid-sub"
	server := newJWKSServer(t, kid, &priv.PublicKey)
	defer server.Close()

	v, err := auth.NewVerifier(context.Background(), auth.Config{
		AppID: "app-z", JWKSURL: server.URL, Audience: "app-z",
	}, zap.NewNop())
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	claims := jwt.MapClaims{
		// sub 故意缺失
		"iss": "privy.io",
		"aud": "app-z",
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Unix(),
	}
	token := signPrivyToken(t, priv, kid, claims)
	if _, err := v.Verify(context.Background(), token); err == nil {
		t.Fatal("expected error for missing subject")
	}
}

// TestJWKSVerifier_RejectsBadSignature 签发者密钥与 JWKS 不匹配必须被拒绝。
func TestJWKSVerifier_RejectsBadSignature(t *testing.T) {
	signer, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	jwksHolder, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	kid := "kid-mismatch"
	// server 只暴露 jwksHolder 公钥，但 token 用 signer 签
	server := newJWKSServer(t, kid, &jwksHolder.PublicKey)
	defer server.Close()

	v, err := auth.NewVerifier(context.Background(), auth.Config{
		AppID: "app-s", JWKSURL: server.URL, Audience: "app-s",
	}, zap.NewNop())
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	claims := jwt.MapClaims{
		"sub": "did:privy:u",
		"iss": "privy.io",
		"aud": "app-s",
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Unix(),
	}
	token := signPrivyToken(t, signer, kid, claims)
	if _, err := v.Verify(context.Background(), token); err == nil {
		t.Fatal("expected error for signature mismatch")
	}
}

// TestJWKSVerifier_RejectsNonES256Algorithm 非 ES256 必须被拒绝。
func TestJWKSVerifier_RejectsNonES256Algorithm(t *testing.T) {
	priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	kid := "kid-alg"
	server := newJWKSServer(t, kid, &priv.PublicKey)
	defer server.Close()

	v, err := auth.NewVerifier(context.Background(), auth.Config{
		AppID: "app-a", JWKSURL: server.URL, Audience: "app-a",
	}, zap.NewNop())
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	// 用 HS256（symmetric）签发 — 应被 WithValidMethods 拒绝
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "did:privy:u",
		"iss": "privy.io",
		"aud": "app-a",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	tok.Header["kid"] = kid
	s, err := tok.SignedString([]byte("not-a-secret-but-key-func-cares-about-alg"))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if _, err := v.Verify(context.Background(), s); err == nil {
		t.Fatal("expected error for non-ES256 algorithm")
	}
}

// TestJWKSVerifier_AcceptsTokenWithoutBearerPrefix 兼容 Bearer 前缀缺失的请求。
func TestJWKSVerifier_AcceptsTokenWithoutBearerPrefix(t *testing.T) {
	priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	kid := "kid-prefix"
	server := newJWKSServer(t, kid, &priv.PublicKey)
	defer server.Close()

	v, err := auth.NewVerifier(context.Background(), auth.Config{
		AppID: "app-p", JWKSURL: server.URL, Audience: "app-p",
	}, zap.NewNop())
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	claims := jwt.MapClaims{
		"sub": "did:privy:u",
		"iss": "privy.io",
		"aud": "app-p",
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Unix(),
	}
	token := signPrivyToken(t, priv, kid, claims)
	// 直接传不带 Bearer 前缀的 token
	got, err := v.Verify(context.Background(), token)
	if err != nil {
		t.Fatalf("Verify without Bearer: %v", err)
	}
	if got.Subject != "did:privy:u" {
		t.Errorf("subject = %q", got.Subject)
	}
}

// TestJWKSVerifier_RejectsMalformedToken 乱码必须被拒绝。
func TestJWKSVerifier_RejectsMalformedToken(t *testing.T) {
	priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	kid := "kid-mal"
	server := newJWKSServer(t, kid, &priv.PublicKey)
	defer server.Close()

	v, err := auth.NewVerifier(context.Background(), auth.Config{
		AppID: "app-m", JWKSURL: server.URL, Audience: "app-m",
	}, zap.NewNop())
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	if _, err := v.Verify(context.Background(), "not.a.real.jwt"); err == nil {
		t.Fatal("expected error for malformed token")
	}
}

// TestJWKSVerifier_AudienceFallbackToAppID 未配置 Audience 时默认走 AppID。
func TestJWKSVerifier_AudienceFallbackToAppID(t *testing.T) {
	priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	kid := "kid-fb"
	server := newJWKSServer(t, kid, &priv.PublicKey)
	defer server.Close()

	v, err := auth.NewVerifier(context.Background(), auth.Config{
		AppID: "app-fallback", JWKSURL: server.URL,
		// Audience 留空 → 应回退到 AppID
	}, zap.NewNop())
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	claims := jwt.MapClaims{
		"sub": "did:privy:u",
		"iss": "privy.io",
		"aud": "app-fallback",
		"exp": time.Now().Add(time.Hour).Unix(),
	}
	token := signPrivyToken(t, priv, kid, claims)
	got, err := v.Verify(context.Background(), token)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got.Subject != "did:privy:u" {
		t.Errorf("subject = %q", got.Subject)
	}
}

// TestNewSessionID_UniqueAndHex 验证 session id 是 hex 且唯一。
func TestNewSessionID_UniqueAndHex(t *testing.T) {
	a, err := auth.NewSessionID()
	if err != nil {
		t.Fatalf("NewSessionID: %v", err)
	}
	b, err := auth.NewSessionID()
	if err != nil {
		t.Fatalf("NewSessionID: %v", err)
	}
	if a == b {
		t.Fatal("session ids must be unique")
	}
	if len(a) != 64 { // 32 bytes hex = 64 chars
		t.Errorf("expected 64-char hex, got %d (%q)", len(a), a)
	}
	if strings.ContainsAny(a, "ghijklmnopqrstuvwxyzGHIJKLMNOPQRSTUVWXYZ!@#$") {
		t.Errorf("session id must be lowercase hex only, got %q", a)
	}
}
