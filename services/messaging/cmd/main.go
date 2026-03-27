package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"

	"github.com/mst-corp/orbit/pkg/config"
	"github.com/mst-corp/orbit/pkg/response"
	"github.com/mst-corp/orbit/services/messaging/internal/handler"
	"github.com/mst-corp/orbit/services/messaging/internal/service"
	"github.com/mst-corp/orbit/services/messaging/internal/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	// Config
	port := config.EnvOr("PORT", "8082")
	dbURL := config.DatabaseURL()
	// Debug: log masked DATABASE_URL to diagnose auth failures
	if len(dbURL) > 30 {
		slog.Info("database URL resolved", "prefix", dbURL[:30]+"...", "len", len(dbURL), "source_env", os.Getenv("DATABASE_URL") != "")
	} else {
		slog.Info("database URL resolved", "url", dbURL, "source_env", os.Getenv("DATABASE_URL") != "")
	}
	redisURL := config.MustEnv("REDIS_URL")
	natsURL := config.NatsURL()

	// PostgreSQL
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
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
	// Redis used for link preview caching and future needs

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

	// Stores
	chatStore := store.NewChatStore(pool)
	messageStore := store.NewMessageStore(pool)
	userStore := store.NewUserStore(pool)

	// Services
	natsPublisher := service.NewNATSPublisher(nc)
	chatSvc := service.NewChatService(chatStore)
	msgSvc := service.NewMessageService(messageStore, chatStore, natsPublisher)
	userSvc := service.NewUserService(userStore, chatStore)
	linkPreviewSvc := service.NewLinkPreviewService(rdb, logger)

	// Handlers
	chatHandler := handler.NewChatHandler(chatSvc, logger)
	msgHandler := handler.NewMessageHandler(msgSvc, linkPreviewSvc, logger)
	userHandler := handler.NewUserHandler(userSvc, logger)

	// Fiber
	app := fiber.New(fiber.Config{
		ErrorHandler: response.FiberErrorHandler,
	})

	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok", "service": "orbit-messaging"})
	})

	// Register routes (gateway strips /api/v1, so messaging receives paths directly)
	chatHandler.Register(app)
	msgHandler.Register(app)
	userHandler.Register(app)

	// Graceful shutdown
	go func() {
		if err := app.Listen(":" + port); err != nil {
			slog.Error("server error", "error", err)
		}
	}()

	slog.Info("messaging service started", "port", port)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down messaging service")
	if err := app.ShutdownWithTimeout(10 * time.Second); err != nil {
		slog.Error("shutdown error", "error", err)
	}
}
