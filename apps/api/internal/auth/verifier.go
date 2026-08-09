// Package auth 实现 Privy access token 校验、session 存储。
package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/zap"
)

// Config 注入 Privy 配置。DevStub 模式下跳过 JWKS / JWT 验证，
// 仅用于本地联调；任何 staging/prod 启动都 fail-fast 关闭。
type Config struct {
	AppID      string
	JWKSURL    string
	Audience   string
	DevStub    bool
	DevSubject string
}

// Verifier 抽象出 token 校验接口，便于测试 stub。
type Verifier interface {
	Verify(ctx context.Context, token string) (*Claims, error)
}

// Claims 是 Privy 解出的最小声明集。
type Claims struct {
	Subject  string // Privy user id (DID 形式: did:privy:xxx)
	Issuer   string
	Audience []string
	Expires  time.Time
	IssuedAt time.Time
}

// jwksVerifier 通过 JWKS 验证真签名。
type jwksVerifier struct {
	appID    string
	audience string
	jwks     keyfunc.Keyfunc
	logger   *zap.Logger
}

func (v *jwksVerifier) Verify(_ context.Context, raw string) (*Claims, error) {
	raw = strings.TrimPrefix(raw, "Bearer ")
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{"ES256"}),
		jwt.WithExpirationRequired(),
		jwt.WithIssuer("privy.io"),
		jwt.WithAudience(v.audience),
	)
	tok, err := parser.Parse(raw, v.jwks.Keyfunc)
	if err != nil {
		return nil, fmt.Errorf("jwt parse: %w", err)
	}
	mc, ok := tok.Claims.(jwt.MapClaims)
	if !ok {
		return nil, errors.New("invalid claims type")
	}
	c := &Claims{}
	if sub, _ := mc.GetSubject(); sub != "" {
		c.Subject = sub
	}
	if iss, _ := mc.GetIssuer(); iss != "" {
		c.Issuer = iss
	}
	if aud, _ := mc.GetAudience(); len(aud) > 0 {
		c.Audience = aud
	}
	if exp, _ := mc.GetExpirationTime(); exp != nil {
		c.Expires = exp.Time
	}
	if iat, _ := mc.GetIssuedAt(); iat != nil {
		c.IssuedAt = iat.Time
	}
	if c.Subject == "" {
		return nil, errors.New("missing subject")
	}
	if !contains(c.Audience, v.audience) {
		return nil, errors.New("audience mismatch")
	}
	if c.Issuer != "privy.io" {
		return nil, errors.New("issuer mismatch")
	}
	if time.Now().After(c.Expires) {
		return nil, errors.New("token expired")
	}
	return c, nil
}

// devStubVerifier 接受任何 token；固定 subject。仅 dev。
type devStubVerifier struct {
	subject string
}

func (v *devStubVerifier) Verify(_ context.Context, _ string) (*Claims, error) {
	return &Claims{
		Subject:  v.subject,
		Issuer:   "dev-stub",
		Audience: []string{"dev"},
		Expires:  time.Now().Add(time.Hour),
		IssuedAt: time.Now(),
	}, nil
}

// NewVerifier 构造 Verifier。DevStub=true 时返回 stub 实现。
func NewVerifier(ctx context.Context, cfg Config, logger *zap.Logger) (Verifier, error) {
	if cfg.DevStub {
		if cfg.DevSubject == "" {
			return nil, errors.New("auth: dev stub requires subject")
		}
		logger.Warn("privy_dev_stub_enabled", zap.String("subject", cfg.DevSubject))
		return &devStubVerifier{subject: cfg.DevSubject}, nil
	}
	if cfg.AppID == "" || cfg.JWKSURL == "" {
		return nil, errors.New("auth: missing Privy config (or set PRIVY_DEV_STUB=1)")
	}
	jwks, err := keyfunc.NewDefaultCtx(ctx, []string{cfg.JWKSURL})
	if err != nil {
		return nil, fmt.Errorf("auth: jwks load: %w", err)
	}
	return &jwksVerifier{
		appID:    cfg.AppID,
		audience: cfg.AppID,
		jwks:     jwks,
		logger:   logger,
	}, nil
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// NewSessionID 生成不可猜测的 sid（hex）。
func NewSessionID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
