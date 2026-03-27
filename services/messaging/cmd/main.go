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
	"github.com/mst-corp/orbit/services/messaging/internal/handler"
	"github.com/mst-corp/orbit/services/messaging/internal/service"
	"github.com/mst-corp/orbit/services/messaging/internal/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	// Config
	port := config.EnvOr("PORT", "8082")
	dbDSN, dbPassword, dbRawPassword := config.DatabaseDSN()
	slog.Info("database config", "dsn", dbDSN, "password_len", len(dbPassword))
	redisURL := config.MustEnv("REDIS_URL")
	natsURL := config.NatsURL()

	// PostgreSQL — try password as-is first, then without backslashes
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
