// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"context"
	"crypto/subtle"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/mst-corp/orbit/pkg/config"
	"github.com/mst-corp/orbit/pkg/metrics"
	"github.com/mst-corp/orbit/pkg/migrator"
	"github.com/mst-corp/orbit/pkg/response"
	"github.com/mst-corp/orbit/services/auth/internal/handler"
	"github.com/mst-corp/orbit/services/auth/internal/middleware"
	"github.com/mst-corp/orbit/services/auth/internal/service"
	"github.com/mst-corp/orbit/services/auth/internal/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	// Config
	port := config.EnvOr("PORT", "8081")
	dbDSN, dbPassword, dbRawPassword := config.DatabaseDSN()
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
		// Welcome flow (mig 069). Both must be set for auth.Register to call
		// the messaging /internal/.../join-default-chats endpoint. When either
		// is empty the call is skipped — typical in unit tests, and a no-op
		// in production deployments before this rollout finishes.
		MessagingURL:   config.EnvOr("MESSAGING_URL", config.EnvOr("MESSAGING_SERVICE_URL", "")),
		InternalSecret: config.EnvOr("INTERNAL_SECRET", ""),
	}

	// PostgreSQL — try password as-is first, then without backslashes
	// Saturn may strip backslashes from POSTGRES_PASSWORD env var during DB init,
	// but preserves %5C in DATABASE_URL (URL-encoded), causing a mismatch.
	ctx := context.Background()
	poolCfg, err := pgxpool.ParseConfig(dbDSN)
	if err != nil {
		slog.Error("failed to parse database config", "error", err)
		os.Exit(1)
	}

	// Try multiple password variants — Saturn may transform the password
	// during PostgreSQL initialization differently than in DATABASE_URL
	passwords := []string{dbPassword}
	if noBS := strings.ReplaceAll(dbPassword, `\`, ""); noBS != dbPassword {
		passwords = append(passwords, noBS)
	}
	if doubleBS := strings.ReplaceAll(dbPassword, `\`, `\\`); doubleBS != dbPassword {
		passwords = append(passwords, doubleBS)
	}
	if dbRawPassword != "" && dbRawPassword != dbPassword {
		passwords = append(passwords, dbRawPassword)
	}

	var pool *pgxpool.Pool
	for i, pass := range passwords {
		poolCfg.ConnConfig.Password = pass
		pool, err = pgxpool.NewWithConfig(ctx, poolCfg)
		if err != nil {
			slog.Warn("failed to create pool", "attempt", i, "error", err)
			continue
		}
		if err = pool.Ping(ctx); err != nil {
			pool.Close()
			slog.Warn("failed to ping database", "attempt", i, "password_len", len(pass), "error", err)
			continue
		}
		if i > 0 {
			slog.Info("connected with fallback password (backslash stripped)", "password_len", len(pass))
		}
		slog.Info("database connected successfully")
		break
	}
	if err != nil {
		slog.Error("all database connection attempts failed")
		os.Exit(1)
	}
	defer pool.Close()

	// Run pending SQL migrations from MIGRATIONS_DIR (default: /migrations).
	// Tracked in schema_migrations; idempotent on restart.
	migrationsDir := config.EnvOr("MIGRATIONS_DIR", "/migrations")
	if _, statErr := os.Stat(migrationsDir); statErr == nil {
		if mErr := migrator.Run(ctx, pool, migrationsDir); mErr != nil {
			slog.Error("migrator failed", "error", mErr)
			os.Exit(1)
		}
	} else {
		slog.Warn("migrations directory not found, skipping migrator", "dir", migrationsDir)
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

	// Rate limiting (defense-in-depth — gateway also has limits)
	loginRateLimit := middleware.RateLimitMiddleware(middleware.RateLimitConfig{
		Redis:      rdb,
		MaxPerMin:  5,
		KeyPrefix:  "auth_login",
		Identifier: middleware.AuthRateLimitIdentifierByIP,
	})
	registerRateLimit := middleware.RateLimitMiddleware(middleware.RateLimitConfig{
		Redis:      rdb,
		MaxPerMin:  10,
		KeyPrefix:  "auth_register",
		Identifier: middleware.AuthRateLimitIdentifierByIP,
	})
	resetAdminRateLimit := middleware.RateLimitMiddleware(middleware.RateLimitConfig{
		Redis:      rdb,
		MaxPerMin:  5,
		KeyPrefix:  "auth_reset_admin",
		Identifier: middleware.AuthRateLimitIdentifierByIP,
	})

	// DI
	userStore := store.NewUserStore(pool)
	sessionStore := store.NewSessionStore(pool)
	inviteStore := store.NewInviteStore(pool)
	authSvc := service.NewAuthService(userStore, sessionStore, inviteStore, rdb, svcCfg, logger)
	internalSecret := config.EnvOr("INTERNAL_SECRET", "")
	// BOOTSTRAP_SECRET gates the /auth/bootstrap endpoint. When empty, the
	// endpoint is hard-disabled. Set this only during initial provisioning,
	// then clear it from the environment and restart the service.
	bootstrapSecret := config.EnvOr("BOOTSTRAP_SECRET", "")
	if bootstrapSecret == "" {
		slog.Warn("BOOTSTRAP_SECRET not set — /auth/bootstrap endpoint is disabled")
	}
	authHandler := handler.NewAuthHandler(authSvc, logger, internalSecret, bootstrapSecret)

	// Fiber
	app := fiber.New(fiber.Config{
		ErrorHandler: response.FiberErrorHandler,
		// Trust X-Trusted-Client-IP forwarded by the gateway.
		// Without this, c.IP() returns the gateway's IP for all requests,
		// collapsing all clients into one rate-limit bucket.
		ProxyHeader:             "X-Trusted-Client-IP",
		EnableTrustedProxyCheck: true,
		TrustedProxies:          []string{"127.0.0.0/8", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"},
	})

	metricsReg := metrics.New("auth")
	app.Use(metricsReg.HTTPMiddleware())

	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok", "service": "orbit-auth"})
	})

	app.Get("/metrics", func(c *fiber.Ctx) error {
		token := c.Get("X-Internal-Token")
		if internalSecret == "" || subtle.ConstantTimeCompare([]byte(token), []byte(internalSecret)) != 1 {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		return metricsReg.Handler()(c)
	})

	authHandler.Register(app, handler.RateLimitMiddlewares{
		Login:      loginRateLimit,
		Register:   registerRateLimit,
		ResetAdmin: resetAdminRateLimit,
	})

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
