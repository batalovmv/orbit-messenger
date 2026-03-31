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
	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"

	"github.com/mst-corp/orbit/pkg/config"
	"github.com/mst-corp/orbit/pkg/response"
	"github.com/mst-corp/orbit/services/media/internal/handler"
	"github.com/mst-corp/orbit/services/media/internal/service"
	"github.com/mst-corp/orbit/services/media/internal/storage"
	"github.com/mst-corp/orbit/services/media/internal/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	// Config
	port := config.EnvOr("PORT", "8083")
	dbDSN, dbPassword, dbRawPassword := config.DatabaseDSN()
	redisURL := config.MustEnv("REDIS_URL")
	natsURL := config.NatsURL()

	r2Endpoint := config.MustEnv("R2_ENDPOINT")
	r2AccessKey := config.MustEnv("R2_ACCESS_KEY_ID")
	r2SecretKey := config.MustEnv("R2_SECRET_ACCESS_KEY")
	r2Bucket := config.EnvOr("R2_BUCKET", "orbit-media")
	r2PublicEndpoint := config.EnvOr("R2_PUBLIC_ENDPOINT", "") // Browser-accessible S3 endpoint for presigned URLs
	internalSecret := config.MustEnv("INTERNAL_SECRET")

	// PostgreSQL
	ctx := context.Background()
	poolCfg, err := pgxpool.ParseConfig(dbDSN)
	if err != nil {
		slog.Error("failed to parse database config", "error", err)
		os.Exit(1)
	}

	passwords := []string{dbPassword}
	if noBS := strings.ReplaceAll(dbPassword, `\`, ""); noBS != dbPassword {
		passwords = append(passwords, noBS)
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
			slog.Warn("failed to ping database", "attempt", i, "error", err)
			continue
		}
		slog.Info("database connected successfully")
		break
	}
	if err != nil {
		slog.Error("all database connection attempts failed")
		os.Exit(1)
	}
	defer pool.Close()

	// Redis
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		slog.Error("failed to parse redis URL", "error", err)
		os.Exit(1)
	}
	rdb := redis.NewClient(opts)
	defer rdb.Close()

	// NATS
	nc, err := nats.Connect(natsURL,
		nats.ReconnectWait(2*time.Second),
		nats.MaxReconnects(-1),
	)
	if err != nil {
		slog.Error("failed to connect to NATS", "error", err)
		os.Exit(1)
	}
	defer nc.Close()
	slog.Info("NATS connected", "url", natsURL)

	// R2 / S3 client
	r2Client, err := storage.NewR2Client(r2Endpoint, r2AccessKey, r2SecretKey, r2Bucket, r2PublicEndpoint)
	if err != nil {
		slog.Error("failed to create R2 client", "error", err)
		os.Exit(1)
	}

	// Ensure bucket exists (for MinIO local dev)
	if err := r2Client.EnsureBucket(ctx); err != nil {
		slog.Warn("ensure bucket failed (may already exist)", "error", err)
	}

	// Store
	mediaStore := store.NewMediaStore(pool)

	// Service
	mediaSvc := service.NewMediaService(mediaStore, r2Client, rdb, nc)

	// Start orphan cleanup loop (every 6 hours)
	appCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	mediaSvc.StartCleanupLoop(appCtx, 6*time.Hour)

	// Handlers
	uploadHandler := handler.NewUploadHandler(mediaSvc, logger, internalSecret)
	mediaHandler := handler.NewMediaHandler(mediaSvc, logger, internalSecret)

	// Fiber
	app := fiber.New(fiber.Config{
		BodyLimit:    55 * 1024 * 1024, // 55MB for simple uploads
		ErrorHandler: response.FiberErrorHandler,
	})

	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok", "service": "orbit-media"})
	})

	// Register routes
	uploadHandler.Register(app)
	mediaHandler.Register(app)

	// Start server
	go func() {
		if err := app.Listen(":" + port); err != nil {
			slog.Error("server error", "error", err)
		}
	}()

	slog.Info("media service started", "port", port)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down media service")
	cancel()
	if err := app.ShutdownWithTimeout(10 * time.Second); err != nil {
		slog.Error("shutdown error", "error", err)
	}
}
