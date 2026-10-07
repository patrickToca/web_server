package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"mywebapp/internal/auth"
	"mywebapp/internal/config"
	"mywebapp/internal/credentials"
	"mywebapp/internal/handler"
	"mywebapp/internal/logging"
	"mywebapp/internal/middleware"
	"mywebapp/internal/service"
	"mywebapp/internal/web"
	"mywebapp/pkg/db"
	"mywebapp/pkg/r2"
)

func main() {
	logger, cleanup, err := logging.Setup()
	if err != nil {
		slog.Error("failed to set up logging", "error", err)
		os.Exit(1)
	}
	defer cleanup()

	// Load secrets before anything else. The credentials loader is
	// fail-closed in production: a missing SOPS file, a missing age
	// key, a missing secret, or a short signing key aborts the boot.
	//
	// In development, a missing secret produces a warning and the
	// consumer's own fallback applies (random per-process key). That
	// is why this call must run before any service is constructed:
	// the services read os.Getenv, and they must see the values this
	// call sets.
	if err := credentials.Load(); err != nil {
		logger.Error("failed to load credentials", "error", err)
		os.Exit(1)
	}

	logger.Info("starting mywebapp",
		"version", "0.9.0",
		"pid", os.Getpid(),
		"env", string(config.Current()),
	)

	dbConfig := db.LoadConfig()

	bootCtx, bootCancel := context.WithTimeout(context.Background(), 15*time.Second)
	pool, err := db.NewPool(bootCtx, dbConfig)
	bootCancel()
	if err != nil {
		logger.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	appCtx, appCancel := context.WithCancel(context.Background())
	defer appCancel()

	go logPoolStats(pool, 30*time.Second, appCtx.Done())

	userService := service.NewUserServiceWithPool(pool)
	sessionService := service.NewSessionService(pool)

	jwtService, err := auth.NewJWTService()
	if err != nil {
		logger.Error("jwt service initialisation failed", "error", err)
		os.Exit(1)
	}

	logger.Info("jwt configuration",
		"access_ttl", jwtService.AccessTTL().String(),
		"refresh_ttl", jwtService.RefreshTTL().String(),
	)

	var r2Client *r2.Client

	r2Cfg := r2.Config{
		AccountID:       os.Getenv("R2_ACCOUNT_ID"),
		AccessKeyID:     os.Getenv("R2_ACCESS_KEY_ID"),
		SecretAccessKey: os.Getenv("R2_SECRET_ACCESS_KEY"),
		BucketName:      os.Getenv("R2_BUCKET_NAME"),
	}

	if r2Cfg.AccountID != "" && r2Cfg.AccessKeyID != "" &&
		r2Cfg.SecretAccessKey != "" && r2Cfg.BucketName != "" {

		client, err := r2.NewClient(context.Background(), r2Cfg)
		if err != nil {
			logger.Warn("r2: client initialisation failed; welcome image disabled",
				"error", err)
		} else {
			r2Client = client
			imageKey := os.Getenv("R2_IMAGE_KEY")
			if imageKey == "" {
				imageKey = "welcome.jpg"
			}
			logger.Info("r2 configured",
				"bucket", r2Cfg.BucketName,
				"image_key", imageKey)
		}
	} else {
		logger.Info("r2 not configured; welcome image disabled")
	}

	userHandler := handler.NewUserHandler(userService)
	webHandler := web.NewWebHandler(userService)
	meHandler := web.NewMeHandler(userService, r2Client)
	homeHandler := web.NewHomeHandler(userService, r2Client)
	healthHandler := web.NewHealthHandler(pool)

	if os.Getenv("GIN_MODE") == "" {
		gin.SetMode(gin.ReleaseMode)
	}
	router := gin.New()

	if err := router.SetTrustedProxies([]string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
	}); err != nil {
		logger.Error("SetTrustedProxies failed", "error", err)
		os.Exit(1)
	}
	router.RemoteIPHeaders = []string{"X-Forwarded-For"}

	router.SetFuncMap(web.GetTemplateFuncs())
	router.LoadHTMLGlob("internal/templates/**/*.html")
	router.Static("/static", "./static")

	csrfCfg, err := middleware.LoadCSRFConfig()
	if err != nil {
		logger.Error("csrf configuration failed", "error", err)
		os.Exit(1)
	}
	cookieCfg := web.LoadCookieConfig()

	authHandler := web.NewAuthHandler(userService, sessionService, jwtService, csrfCfg)

	router.Use(middleware.RequestID(logger))
	router.Use(middleware.BodyLimit())
	router.Use(middleware.SecurityHeaders(middleware.LoadSecurityHeadersConfig()))
	router.Use(middleware.NoCache())
	router.Use(gin.Recovery())
	router.Use(middleware.AccessLog(logger))
	router.Use(middleware.Gzip())

	globalLimiter := middleware.NewRateLimiter(20, 40, 10*time.Minute, 100_000)
	router.Use(globalLimiter.Middleware())
	router.Use(web.HTMXMiddleware())

	router.GET("/health", healthHandler.Health)
	router.GET("/health/ready", healthHandler.Ready)
	router.GET("/version", healthHandler.Version)

	router.GET("/favicon.ico", func(c *gin.Context) {
		c.Header("Content-Type", "image/svg+xml")
		c.String(http.StatusOK,
			`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100">`+
				`<text y=".9em" font-size="90">🚀</text></svg>`)
	})

	router.GET("/",
		web.OptionalAuthMiddleware(jwtService),
		middleware.CSRFMiddleware(csrfCfg),
		homeHandler.HomePage)

	router.GET("/web/home",
		web.OptionalAuthMiddleware(jwtService),
		middleware.CSRFMiddleware(csrfCfg),
		homeHandler.HomePage)

	authLimiter := middleware.NewRateLimiter(5.0/60.0, 5, 15*time.Minute, 10_000)

	router.GET("/web/login",
		middleware.CSRFMiddleware(csrfCfg),
		authHandler.LoginPage)
	router.POST("/web/login",
		authLimiter.Middleware(),
		middleware.CSRFMiddleware(csrfCfg),
		authHandler.Login)
	router.GET("/web/register",
		middleware.CSRFMiddleware(csrfCfg),
		authHandler.RegisterPage)
	router.POST("/web/register",
		authLimiter.Middleware(),
		middleware.CSRFMiddleware(csrfCfg),
		authHandler.Register)

	refreshLimiter := middleware.NewRateLimiter(1.0, 30, 15*time.Minute, 10_000)
	router.POST("/web/refresh",
		refreshLimiter.Middleware(),
		authHandler.Refresh)

	router.POST("/web/logout",
		middleware.CSRFMiddleware(csrfCfg),
		authHandler.Logout)

	router.POST("/api/v1/auth/token",
		authLimiter.Middleware(),
		authHandler.IssueAPIToken)

	authMW := web.AuthMiddleware(jwtService, userService, sessionService, cookieCfg)

	membersGroup := router.Group("/web",
		authMW,
		middleware.RequireRole("moderator", "admin"),
		middleware.CSRFMiddleware(csrfCfg),
	)
	{
		membersGroup.GET("/members", homeHandler.MembersPage)
		membersGroup.GET("/members/partial", homeHandler.MembersPartial)
		membersGroup.GET("/users/stats", webHandler.GetUserStats)
	}

	meGroup := router.Group("/web/me",
		authMW,
		middleware.CSRFMiddleware(csrfCfg),
	)
	{
		meGroup.GET("", meHandler.ProfilePage)
	}

	api := router.Group("/api/v1",
		authMW,
		middleware.RequireRole("admin"),
	)
	{
		users := api.Group("/users")
		{
			users.POST("", userHandler.CreateUser)
			users.GET("", userHandler.ListUsers)
			users.GET("/:id", userHandler.GetUser)
			users.PUT("/:id", userHandler.UpdateUser)
			users.PUT("/:id/role", userHandler.UpdateUserRole)
			users.DELETE("/:id", userHandler.DeleteUser)
		}
	}

	webGroup := router.Group("/web",
		authMW,
		middleware.RequireRole("admin"),
		middleware.CSRFMiddleware(csrfCfg),
	)
	{
		webGroup.GET("/users", webHandler.ListUsersPage)
		webGroup.GET("/users/by-id", webHandler.ListUsersByIDPage)
		webGroup.GET("/users/new", webHandler.NewUserForm)
		webGroup.POST("/users", webHandler.CreateUserWeb)
		webGroup.GET("/users/:id/edit", webHandler.EditUserForm)
		webGroup.PUT("/users/:id", webHandler.UpdateUserWeb)
		webGroup.GET("/users/:id/role", webHandler.EditUserRoleForm)
		webGroup.PUT("/users/:id/role", webHandler.UpdateUserRoleWeb)
		webGroup.DELETE("/users/:id", webHandler.DeleteUserWeb)
	}

	adminGroup := router.Group("/admin",
		authMW,
		middleware.RequireRole("admin"),
	)
	{
		adminGroup.GET("/health/detail", healthHandler.Detail)
	}

	go sessionCleanupLoop(appCtx, sessionService, logger, time.Hour)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		logger.Info("http server listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server failed", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	logger.Info("shutdown signal received", "signal", sig.String())

	appCancel()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
	}

	globalLimiter.Stop()
	authLimiter.Stop()
	refreshLimiter.Stop()

	pool.Close()
	logger.Info("server stopped")
}

func logPoolStats(pool interface {
	Stat() *pgxpool.Stat
}, interval time.Duration, done <-chan struct{}) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			s := pool.Stat()
			slog.Info("pgx pool stats",
				"total", s.TotalConns(),
				"idle", s.IdleConns(),
				"acquired", s.AcquiredConns(),
				"constructing", s.ConstructingConns(),
				"max", s.MaxConns(),
				"empty_acquire", s.EmptyAcquireCount(),
			)
		}
	}
}

func sessionCleanupLoop(
	ctx context.Context,
	svc *service.SessionService,
	logger *slog.Logger,
	interval time.Duration,
) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			logger.Info("session cleanup stopped")
			return
		case <-ticker.C:
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			if err := svc.CleanupExpired(cleanupCtx); err != nil {
				logger.Warn("session cleanup failed", "error", err)
			}
			cancel()
		}
	}
}
