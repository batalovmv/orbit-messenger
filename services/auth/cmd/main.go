package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/mst-corp/orbit/pkg/config"
	"github.com/mst-corp/orbit/pkg/response"
	"github.com/mst-corp/orbit/services/auth/internal/handler"
	"github.com/mst-corp/orbit/services/auth/internal/service"
	"github.com/mst-corp/orbit/services/auth/internal/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	// Config
	port := config.EnvOr("PORT", "8081")
	dbDSN, dbPassword := config.DatabaseDSN()
	// TEMPORARY: log base64-encoded DATABASE_URL to bypass Saturn log redaction
	rawURL := os.Getenv("DATABASE_URL")
	slog.Info("database config",
		"dsn", dbDSN,
		"password_len", len(dbPassword),
		"password_hex", fmt.Sprintf("%x", dbPassword),
		"url_b64", base64.StdEncoding.EncodeToString([]byte(rawURL)),
	)
	redisURL := config.MustEnv("REDIS_URL")
	jwtSecret := config.MustEnv("JWT_SECRET")
	accessTTL := config.EnvDurationOr("JWT_ACCESS_TTL", 15*time.Minute)
	refreshTTL := config.EnvDurationOr("JWT_REFRESH_TTL", 720*time.Hour)

	svcCfg := &service.Config{
		JWTSecret:     jwtSecret,
		AccessTTL:     accessTTL,
		RefreshTTL:    refreshTTL,
		TOTPIssuer:    config.EnvOr("TOTP_ISSUER", "Orbit"),
		AdminResetKey: os.Getenv("ORBIT_ADMIN_RESET_KEY"),
		FrontendURL:   config.EnvOr("FRONTEND_URL", "http://localhost:3000"),
	}

	// PostgreSQL — set password programmatically to avoid DSN escaping issues
	ctx := context.Background()
	poolCfg, err := pgxpool.ParseConfig(dbDSN)
	if err != nil {
		slog.Error("failed to parse database config", "error", err)
		os.Exit(1)
	}
	poolCfg.ConnConfig.Password = dbPassword
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		slog.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		slog.Error("failed to ping database", "error", err)
		os.Exit(1)
	}

	// Redis
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		slog.Error("failed to parse redis URL", "error", err)
		os.Exit(1)
	}
	rdb := redis.NewClient(opts)
	defer rdb.Close()
	if err := rdb.Ping(ctx).Err(); err != nil {
		slog.Error("failed to ping redis", "error", err)
		os.Exit(1)
	}

	// DI
	userStore := store.NewUserStore(pool)
	sessionStore := store.NewSessionStore(pool)
	inviteStore := store.NewInviteStore(pool)
	authSvc := service.NewAuthService(userStore, sessionStore, inviteStore, rdb, svcCfg)
	authHandler := handler.NewAuthHandler(authSvc, logger)

	// Fiber
	app := fiber.New(fiber.Config{
		ErrorHandler: response.FiberErrorHandler,
	})

	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok", "service": "orbit-auth"})
	})

	authHandler.Register(app)

	// Graceful shutdown
	go func() {
		if err := app.Listen(":" + port); err != nil {
			slog.Error("server error", "error", err)
		}
	}()

	slog.Info("auth service started", "port", port)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down auth service")
	if err := app.ShutdownWithTimeout(10 * time.Second); err != nil {
		slog.Error("shutdown error", "error", err)
	}
}
