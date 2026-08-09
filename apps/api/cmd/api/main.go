// x-web3 api entrypoint.
//
// 启动顺序：
//  1. 加载 config（fail-fast）
//  2. zap logger
//  3. PostgreSQL pool（pgx）
//  4. Redis client
//  5. Privy verifier（dev stub 可选）
//  6. Session store
//  7. RBAC engine
//  8. Audit writer
//  9. HTTP router + handlers
//
// 10. graceful shutdown
package main

import (
	"context"
	"errors"
	"net/http"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/x-web3/api/internal/audit"
	"github.com/x-web3/api/internal/auth"
	"github.com/x-web3/api/internal/config"
	"github.com/x-web3/api/internal/handlers"
	"github.com/x-web3/api/internal/httpkit"
	"github.com/x-web3/api/internal/rbac"
	"github.com/x-web3/api/internal/user"
	"github.com/x-web3/api/internal/wallet"
)

func main() {
	// 自动从 CWD 或祖先目录加载 .env；找不到也不报错（prod 走真 env）
	dotenv := config.LoadDotenv()
	cfg := config.MustLoad()

	logger, err := newLogger(cfg)
	if err != nil {
		panic(err)
	}
	defer logger.Sync() //nolint:errcheck

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Fatal("pgx_connect_failed", zap.Error(err))
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		logger.Fatal("pg_ping_failed", zap.Error(err))
	}

	redisOpts, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		logger.Fatal("redis_parse_url", zap.Error(err))
	}
	rdb := redis.NewClient(redisOpts)
	defer rdb.Close()
	if err := rdb.Ping(ctx).Err(); err != nil {
		logger.Fatal("redis_ping_failed", zap.Error(err))
	}

	verifier, err := auth.NewVerifier(ctx, auth.Config{
		AppID:      cfg.PrivyAppID,
		JWKSURL:    cfg.PrivyJWKSURL,
		Audience:   cfg.PrivyAudience,
		DevStub:    cfg.PrivyDevStub,
		DevSubject: cfg.PrivyDevSubject,
	}, logger)
	if err != nil {
		logger.Fatal("privy_verifier_init", zap.Error(err))
	}

	sessionStore := auth.NewSessionStore(rdb, cfg.SessionSecret, cfg.SessionTTL)
	nonceStore := wallet.NewNonceStore(rdb, cfg.WalletNonceTTL)
	auditWriter := audit.NewWriter(pool, logger)
	rbacEngine := rbac.NewEngine(pool, logger)
	walletSvc := wallet.NewService(pool, nonceStore, cfg.APIDomain, auditWriter)

	router := httpkit.NewRouter(logger, cfg.WebOrigin)
	v1 := router.Engine.Group("/api/v1")

	// Health
	router.Engine.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	router.Engine.GET("/readyz", func(c *gin.Context) {
		if err := pool.Ping(c.Request.Context()); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "db_unavailable"})
			return
		}
		if err := rdb.Ping(c.Request.Context()).Err(); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "redis_unavailable"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	authH := handlers.NewAuthHandler(cfg, pool, verifier, sessionStore, auditWriter, logger)
	walletH := handlers.NewWalletHandler(cfg, pool, walletSvc, auditWriter, logger)
	meH := handlers.NewMeHandler(pool, auditWriter, logger, authH)

	authGroup := v1.Group("/auth")
	{
		authGroup.POST("/privy/session", httpkit.RateLimit(rdb, "login", cfg.LoginRateLimit, httpkit.ClientIPKey), httpkit.Wrap(authH.PostPrivySession))
		authGroup.DELETE("/session", httpkit.Wrap(authH.DeleteSession))
	}
	meGroup := v1.Group("/me")
	meGroup.Use(auth.Middleware(verifier, sessionStore, pool))
	{
		meGroup.GET("", httpkit.Wrap(meH.GetMe))
		walletLimit := httpkit.RateLimit(rdb, "wallet", cfg.WalletRateLimit, httpkit.UserIDKeyFunc)
		meGroup.POST("/wallets/nonce", walletLimit, httpkit.Wrap(walletH.IssueNonce))
		meGroup.POST("/wallets/link", walletLimit, httpkit.Wrap(walletH.Link))
		meGroup.DELETE("/wallets/:walletId", walletLimit, httpkit.Wrap(walletH.Unbind))
	}

	adminGroup := v1.Group("/admin")
	adminGroup.Use(auth.Middleware(verifier, sessionStore, pool), rbacEngine.Middleware(user.PermSystemAdmin))
	adminGroup.GET("/ping", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })

	logger.Info("api_starting",
		zap.String("env", cfg.Env),
		zap.Int("port", cfg.APIPort),
		zap.Bool("privy_dev_stub", cfg.PrivyDevStub),
		zap.String("dotenv_path", dotenv.Path),
		zap.Bool("dotenv_loaded", dotenv.Loaded),
		zap.String("cwd", dotenv.CWD),
		zap.String("database", maskURL(cfg.DatabaseURL)),
		zap.String("redis", maskURL(cfg.RedisURL)),
	)
	if !dotenv.Loaded {
		logger.Warn("dotenv_not_loaded",
			zap.String("cwd", dotenv.CWD),
			zap.Strings("candidates", dotenv.Candidates),
			zap.Error(dotenv.Err),
		)
	}

	srv := &http.Server{
		Addr:              ":" + strconv.Itoa(cfg.APIPort),
		Handler:           router.Engine,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Fatal("listen_failed", zap.Error(err))
		}
	}()

	<-ctx.Done()
	logger.Info("api_shutdown_start")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown_failed", zap.Error(err))
	}
	logger.Info("api_shutdown_done")
}

func newLogger(cfg *config.Config) (*zap.Logger, error) {
	if cfg.IsProd() {
		return zap.NewProduction()
	}
	return zap.NewDevelopment()
}

// maskURL 把 postgres://user:pass@host 里的密码换成 ***。
func maskURL(u string) string {
	if u == "" {
		return ""
	}
	if scheme, rest, ok := strings.Cut(u, "://"); ok {
		creds, host, ok := strings.Cut(rest, "@")
		if ok {
			if user, _, hasColon := strings.Cut(creds, ":"); hasColon {
				return scheme + "://" + user + ":***@" + host
			}
		}
	}
	return u
}
