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
//  9. Catalog service（带缓存订阅）
//
// 10. HTTP router + handlers
// 11. graceful shutdown
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
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/x-web3/api/internal/admin/handlers" // package is `admin`
	"github.com/x-web3/api/internal/audit"
	"github.com/x-web3/api/internal/auth"
	"github.com/x-web3/api/internal/catalog"
	"github.com/x-web3/api/internal/certificate"
	"github.com/x-web3/api/internal/comment"
	"github.com/x-web3/api/internal/config"
	"github.com/x-web3/api/internal/course"
	"github.com/x-web3/api/internal/handlers"
	"github.com/x-web3/api/internal/httpkit"
	"github.com/x-web3/api/internal/learning"
	"github.com/x-web3/api/internal/media"
	"github.com/x-web3/api/internal/objectstore"
	"github.com/x-web3/api/internal/order"
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

	// 业务子系统
	courseRepo := course.NewRepo(pool)
	catalogSvc := catalog.NewService(courseRepo, rdb)
	mediaRepo := media.NewRepo(pool)
	commentRepo := comment.NewRepo(pool)

	// 生产使用 S3；开发和测试继续使用内存 fake，避免本地依赖 AWS。
	var objStore objectstore.Store
	if cfg.IsProd() {
		objStore, err = objectstore.NewS3Store(ctx, cfg.AWSRegion, cfg.ObjectStoreBucket)
		if err != nil {
			logger.Fatal("object_store_init", zap.Error(err))
		}
	} else {
		objStore = objectstore.NewFakeStore()
	}

	learningSvc := learning.NewService(pool, objStore)
	orderSvc := order.NewService(pool, cfg.PurchaseIntentTTL)
	metaGen, err := certificate.NewGenerator(objStore, "")
	if err != nil {
		logger.Fatal("cert_metadata_generator", zap.Error(err))
	}
	certificateSvc, err := certificate.NewService(certificate.ServiceConfig{
		Pool:     pool,
		Metadata: metaGen,
		ChainID:  cfg.ChainID,
		Logger:   logger,
	})
	if err != nil {
		logger.Fatal("cert_service_init", zap.Error(err))
	}

	router := httpkit.NewRouter(logger, cfg.WebOrigin)
	v1 := router.Engine.Group("/api/v1")

	// /metrics：Prometheus 抓取端点。
	//
	// 注意：按 Prometheus 惯例 /metrics 是公开抓取端点（无 auth）。
	// 本服务默认 0.0.0.0 绑定，生产部署请用防火墙 / sidecar 把 APIPort 限制到
	// 内部抓取网段（不要直接对外网开放，避免暴露自定义业务指标与命名空间）。
	//
	// HandlerFor 用 DefaultRegistry 而不是 promhttp.Handler() 默认注册表，
	// 这样可以确保 service 内部指标 + 默认 go/process collector 都被抓取。
	router.Engine.GET("/metrics", gin.WrapH(promhttp.HandlerFor(
		httpkit.DefaultRegistry,
		promhttp.HandlerOpts{Registry: httpkit.DefaultRegistry},
	)))

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
	walletAuthH := handlers.NewWalletAuthHandler(cfg, walletSvc, sessionStore, authH)
	walletH := handlers.NewWalletHandler(cfg, pool, walletSvc, auditWriter, logger)
	meH := handlers.NewMeHandler(pool, auditWriter, logger, authH)
	courseH := handlers.NewCourseHandler(courseRepo, catalogSvc, auditWriter, &course.SettlementPrice{
		ChainID: cfg.ChainID, TokenAddress: cfg.YDTokenAddress, MarketAddress: cfg.CourseMarketAddress, Decimals: 18,
	})
	mediaH := handlers.NewMediaHandler(mediaRepo, objStore, auditWriter, logger)
	learningH := handlers.NewLearningHandler(learningSvc, auditWriter, logger)
	commentH := handlers.NewCommentHandler(commentRepo, auditWriter, logger)
	orderH := handlers.NewOrderHandler(orderSvc, auditWriter)
	certificateH := handlers.NewCertificateHandler(certificateSvc, learningSvc, auditWriter, logger)

	authGroup := v1.Group("/auth")
	{
		authGroup.POST("/privy/session", httpkit.RateLimit(rdb, "login", cfg.LoginRateLimit, httpkit.ClientIPKey), httpkit.Wrap(authH.PostPrivySession))
		authGroup.POST("/wallet/nonce", httpkit.RateLimit(rdb, "wallet-login", cfg.LoginRateLimit, httpkit.ClientIPKey), httpkit.Wrap(walletAuthH.IssueNonce))
		authGroup.POST("/wallet/session", httpkit.RateLimit(rdb, "wallet-login", cfg.LoginRateLimit, httpkit.ClientIPKey), httpkit.Wrap(walletAuthH.CreateSession))
		authGroup.POST("/session/refresh", auth.Middleware(verifier, sessionStore, pool), httpkit.Wrap(authH.RefreshSession))
		authGroup.DELETE("/session", httpkit.Wrap(authH.DeleteSession))
	}
	meGroup := v1.Group("/me")
	meGroup.Use(auth.Middleware(verifier, sessionStore, pool))
	{
		meGroup.GET("", httpkit.Wrap(meH.GetMe))
		meGroup.PATCH("", httpkit.Wrap(meH.UpdateMe))
		walletLimit := httpkit.RateLimit(rdb, "wallet", cfg.WalletRateLimit, httpkit.UserIDKeyFunc)
		meGroup.POST("/wallets/nonce", walletLimit, httpkit.Wrap(walletH.IssueNonce))
		meGroup.POST("/wallets/link", walletLimit, httpkit.Wrap(walletH.Link))
		meGroup.DELETE("/wallets/:walletId", walletLimit, httpkit.Wrap(walletH.Unbind))
		meGroup.GET("/enrollments", httpkit.Wrap(certificateH.ListMineEnrollments))
		meGroup.GET("/certificates", httpkit.Wrap(certificateH.ListMineCertificates))
		meGroup.GET("/comments", httpkit.Wrap(commentH.GetMyComments))
	}
	catalogGroup := v1.Group("/courses")
	{
		catalogGroup.GET("", httpkit.Wrap(courseH.List))
		catalogGroup.GET("/:id", auth.OptionalMiddleware(verifier, sessionStore, pool), httpkit.Wrap(courseH.Get))
		catalogGroup.GET("/:id/comments", httpkit.Wrap(commentH.GetCourseComments))
	}
	coursesAuthGroup := v1.Group("/courses")
	coursesAuthGroup.Use(auth.Middleware(verifier, sessionStore, pool))
	{
		coursesAuthGroup.POST("/:id/comments", httpkit.Wrap(commentH.PostCreate))
		coursesAuthGroup.DELETE("/comments/:id", httpkit.Wrap(commentH.DeleteMine))
		coursesAuthGroup.POST("/:id/complete", httpkit.Wrap(certificateH.CompleteCourse))
	}
	orderGroup := v1.Group("/orders")
	orderGroup.Use(auth.Middleware(verifier, sessionStore, pool))
	{
		orderGroup.POST("/purchase-intents", httpkit.Wrap(orderH.PostPurchaseIntent))
		orderGroup.POST("/:intentId/transactions", httpkit.Wrap(orderH.PostTransaction))
		orderGroup.GET("/:id", httpkit.Wrap(orderH.GetOrder))
	}
	meOrdersGroup := v1.Group("/me")
	meOrdersGroup.Use(auth.Middleware(verifier, sessionStore, pool))
	{
		meOrdersGroup.GET("/orders", httpkit.Wrap(orderH.GetMyOrders))
	}
	teacherGroup := v1.Group("/teacher")
	teacherGroup.Use(auth.Middleware(verifier, sessionStore, pool))
	{
		teacherGroup.GET("/courses", rbacEngine.Middleware(user.PermCourseCreate), httpkit.Wrap(courseH.ListMine))
		teacherGroup.POST("/courses", rbacEngine.Middleware(user.PermCourseCreate), httpkit.Wrap(courseH.Create))
		teacherGroup.PUT("/courses/:id", rbacEngine.Middleware(user.PermCourseEdit), httpkit.Wrap(courseH.Update))
		teacherGroup.PUT("/courses/:id/curriculum", rbacEngine.Middleware(user.PermCourseEdit), httpkit.Wrap(courseH.ReplaceCurriculum))
		teacherGroup.POST("/courses/:id/submit", rbacEngine.Middleware(user.PermCourseEdit), httpkit.Wrap(courseH.Submit))
		teacherGroup.POST("/media/upload-intent", httpkit.Wrap(mediaH.PostUploadIntent))
		teacherGroup.POST("/media/:id/finalize", httpkit.Wrap(mediaH.PostFinalize))
		teacherGroup.GET("/media", httpkit.Wrap(mediaH.GetMine))
		teacherGroup.GET("/lessons/:id/preview", httpkit.Wrap(learningH.GetPreview))
	}
	courseAdminGroup := v1.Group("/admin/courses")
	courseAdminGroup.Use(auth.Middleware(verifier, sessionStore, pool), rbacEngine.Middleware(user.PermCourseApprove))
	{
		courseAdminGroup.GET("", httpkit.Wrap(courseH.ListReviewQueue))
		courseAdminGroup.POST("/:id/review", httpkit.Wrap(courseH.Review))
		courseAdminGroup.POST("/:id/archive", httpkit.Wrap(courseH.Archive))
	}
	commentsAdminGroup := v1.Group("/admin/comments")
	commentsAdminGroup.Use(auth.Middleware(verifier, sessionStore, pool), rbacEngine.Middleware(user.PermCommentModerate))
	{
		commentsAdminGroup.PATCH("/:id", httpkit.Wrap(commentH.PatchModerate))
	}
	learningGroup := v1.Group("/lessons")
	learningGroup.Use(auth.Middleware(verifier, sessionStore, pool))
	{
		learningGroup.GET("/:id/playback", httpkit.Wrap(learningH.GetPlayback))
		learningGroup.POST("/:id/progress", httpkit.Wrap(learningH.PostProgress))
	}

	adminGroup := v1.Group("/admin")
	adminGroup.Use(auth.Middleware(verifier, sessionStore, pool), rbacEngine.Middleware(user.PermSystemAdmin))
	adminGroup.GET("/ping", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })

	// F03-T12 / F03-T13 切片：chain rewind + DLQ admin
	chainRewindH := admin.NewChainRewindHandler(pool, auditWriter, rbacEngine, logger)
	dlqStore := admin.NewPGDLQStore(pool)
	dlqH := admin.NewDLQHandler(dlqStore, auditWriter, rbacEngine, logger)
	// T05（part）/ T06：用户管理 / 链同步状态 / 证书 mint 重试
	usersH := admin.NewUsersHandler(pool, auditWriter, rbacEngine, logger)
	chainStatusH := admin.NewChainStatusHandler(pool, cfg, rbacEngine, logger)
	certRetryH := admin.NewCertRetryHandler(pool, auditWriter, rbacEngine, logger)
	admin.RegisterRoutes(adminGroup, chainRewindH, dlqH, usersH, chainStatusH, certRetryH)

	// 缓存失效订阅
	go func() {
		if err := catalogSvc.SubscribeInvalidate(ctx); err != nil && !errors.Is(err, context.Canceled) {
			logger.Warn("catalog_invalidate_subscriber_exit", zap.Error(err))
		}
	}()

	logger.Info("api_starting",
		zap.String("env", cfg.Env),
		zap.Int("port", cfg.APIPort),
		zap.Bool("privy_dev_stub", cfg.PrivyDevStub),
		zap.String("dotenv_path", dotenv.Path),
		zap.Bool("dotenv_loaded", dotenv.Loaded),
		zap.String("cwd", dotenv.CWD),
		zap.String("database", maskURL(cfg.DatabaseURL)),
		zap.String("redis", maskURL(cfg.RedisURL)),
		zap.String("object_store_region", objStore.Region()),
		zap.String("object_store_bucket", objStore.Bucket()),
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
