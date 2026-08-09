// Package config 集中加载与校验环境变量。所有 handler 通过依赖注入拿 cfg，
// 禁止直接读 os.Getenv。
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config 包含 API 启动所需的所有配置。
// 字段以 K8s/Secrets 可注入为目标：env var 名稳定。
type Config struct {
	Env     string // dev / staging / prod
	APIPort int
	BaseURL string // e.g. https://api.example.com

	DatabaseURL string
	RedisURL    string

	WebOrigin string // CORS 白名单

	// Privy
	PrivyAppID      string
	PrivyJWKSURL    string
	PrivyAudience   string
	PrivyDevStub    bool   // 仅 dev：跳过 JWT 校验
	PrivyDevSubject string // 仅 dev：固定 subject

	// Session
	SessionSecret []byte // 32 字节
	SessionTTL    time.Duration
	CookieSecure  bool

	// Wallet
	APIDomain      string // EIP-191 签名 domain
	WalletNonceTTL time.Duration

	// Logging
	LogLevel string
}

func MustLoad() *Config {
	cfg, err := Load()
	if err != nil {
		panic(fmt.Errorf("config: %w", err))
	}
	return cfg
}

func Load() (*Config, error) {
	c := &Config{
		Env:             getEnv("API_ENV", "dev"),
		APIPort:         getEnvInt("API_PORT", 8080),
		BaseURL:         getEnv("API_BASE_URL", "http://localhost:8080"),
		DatabaseURL:     os.Getenv("DATABASE_URL"),
		RedisURL:        os.Getenv("REDIS_URL"),
		WebOrigin:       getEnv("WEB_ORIGIN", "http://localhost:5173"),
		PrivyAppID:      os.Getenv("PRIVY_APP_ID"),
		PrivyJWKSURL:    os.Getenv("PRIVY_JWKS_URL"),
		PrivyAudience:   os.Getenv("PRIVY_AUDIENCE"),
		PrivyDevStub:    os.Getenv("PRIVY_DEV_STUB") == "1",
		PrivyDevSubject: os.Getenv("PRIVY_DEV_STUB_SUBJECT"),
		SessionSecret:   []byte(os.Getenv("SESSION_SECRET")),
		SessionTTL:      time.Duration(getEnvInt("SESSION_TTL_HOURS", 168)) * time.Hour,
		CookieSecure:    getEnvBool("SESSION_COOKIE_SECURE", false),
		APIDomain:       getEnv("API_DOMAIN", "localhost:8080"),
		WalletNonceTTL:  time.Duration(getEnvInt("WALLET_NONCE_TTL_SECONDS", 300)) * time.Second,
		LogLevel:        getEnv("LOG_LEVEL", "info"),
	}

	var errs []string
	if c.DatabaseURL == "" {
		errs = append(errs, "DATABASE_URL is required")
	}
	if c.RedisURL == "" {
		errs = append(errs, "REDIS_URL is required")
	}
	if len(c.SessionSecret) < 32 {
		errs = append(errs, "SESSION_SECRET must be at least 32 bytes")
	}
	if (c.Env == "staging" || c.IsProd()) && c.PrivyDevStub {
		errs = append(errs, "PRIVY_DEV_STUB must be disabled outside development")
	}
	if c.IsProd() && !c.CookieSecure {
		errs = append(errs, "SESSION_COOKIE_SECURE must be enabled in production")
	}
	if c.PrivyDevStub {
		if c.PrivyDevSubject == "" {
			errs = append(errs, "PRIVY_DEV_STUB=1 requires PRIVY_DEV_STUB_SUBJECT")
		}
	} else {
		if c.PrivyAppID == "" || c.PrivyJWKSURL == "" {
			errs = append(errs, "PRIVY_APP_ID and PRIVY_JWKS_URL are required (or set PRIVY_DEV_STUB=1)")
		}
	}
	if len(errs) > 0 {
		return nil, errors.New("config: " + strings.Join(errs, "; "))
	}
	return c, nil
}

func getEnv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func getEnvInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func getEnvBool(k string, def bool) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(k)))
	if v == "" {
		return def
	}
	return v == "1" || v == "true" || v == "yes"
}

// IsProd reports whether the environment should enforce stricter checks.
func (c *Config) IsProd() bool { return c.Env == "prod" || c.Env == "production" }
