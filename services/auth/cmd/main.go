package main

import (
	"context"
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
	"github.com/mst-corp/orbit/pkg/migrator"
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

	// DI
	userStore := store.NewUserStore(pool)
	sessionStore := store.NewSessionStore(pool)
	inviteStore := store.NewInviteStore(pool)
	authSvc := service.NewAuthService(userStore, sessionStore, inviteStore, rdb, svcCfg)
	// E2E Key Management
	keyStore := store.NewKeyStore(pool)
	preKeyStore := store.NewPreKeyStore(pool)
	transparencyStore := store.NewTransparencyStore(pool)
	keySvc := service.NewKeyService(keyStore, preKeyStore, transparencyStore)
	internalSecret := config.EnvOr("INTERNAL_SECRET", "")
	// BOOTSTRAP_SECRET gates the /auth/bootstrap endpoint. When empty, the
	// endpoint is hard-disabled. Set this only during initial provisioning,
	// then clear it from the environment and restart the service.
	bootstrapSecret := config.EnvOr("BOOTSTRAP_SECRET", "")
	if bootstrapSecret == "" {
		slog.Warn("BOOTSTRAP_SECRET not set — /auth/bootstrap endpoint is disabled")
	}
	authHandler := handler.NewAuthHandler(authSvc, logger, internalSecret, bootstrapSecret)
	keyHandler := handler.NewKeyHandler(keySvc, logger, internalSecret)

	// Fiber
	app := fiber.New(fiber.Config{
		ErrorHandler: response.FiberErrorHandler,
	})

	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok", "service": "orbit-auth"})
	})

	authHandler.Register(app)
	keyHandler.Register(app)

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
