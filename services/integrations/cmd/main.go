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
	"github.com/redis/go-redis/v9"

	"github.com/mst-corp/orbit/pkg/config"
	orchidCrypto "github.com/mst-corp/orbit/pkg/crypto"
	"github.com/mst-corp/orbit/pkg/response"
	"github.com/mst-corp/orbit/services/integrations/internal/client"
	"github.com/mst-corp/orbit/services/integrations/internal/handler"
	"github.com/mst-corp/orbit/services/integrations/internal/service"
	"github.com/mst-corp/orbit/services/integrations/internal/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	port := config.EnvOr("PORT", "8087")
	dbDSN := config.MustEnv("DATABASE_URL")
	redisURL := config.MustEnv("REDIS_URL")
	internalSecret := config.MustEnv("INTERNAL_SECRET")
	messagingServiceURL := config.EnvOr("MESSAGING_URL", config.EnvOr("MESSAGING_SERVICE_URL", "http://localhost:8082"))

	logger.Info("starting integrations service",
		"port", port,
		"messaging_service_url", messagingServiceURL,
	)

	ctx := context.Background()

	pool, err := pgxpool.New(ctx, dbDSN)
	if err != nil {
		logger.Error("failed to connect to postgres", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		logger.Error("failed to ping postgres", "error", err)
		os.Exit(1)
	}

	redisOpts, err := redis.ParseURL(redisURL)
	if err != nil {
		logger.Error("failed to parse redis url", "error", err)
		os.Exit(1)
	}
	rdb := redis.NewClient(redisOpts)
	defer rdb.Close()

	if err := rdb.Ping(ctx).Err(); err != nil {
		logger.Error("failed to ping redis", "error", err)
		os.Exit(1)
	}

	connectorStore := store.NewConnectorStore(pool)
	routeStore := store.NewRouteStore(pool)
	deliveryStore := store.NewDeliveryStore(pool)

	msgClient := client.NewMessagingClient(messagingServiceURL, internalSecret)

	encryptionKey := orchidCrypto.DeriveKey(internalSecret)
	integrationService := service.NewIntegrationService(connectorStore, routeStore, deliveryStore, msgClient, encryptionKey, logger)
	deliveryWorker := service.NewDeliveryWorker(deliveryStore, msgClient, logger)
	connectorHandler := handler.NewConnectorHandler(integrationService, logger).WithRedis(rdb)

	app := fiber.New(fiber.Config{
		ErrorHandler: response.FiberErrorHandler,
	})

	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok", "service": "orbit-integrations"})
	})

	// Public inbound webhook endpoint is registered at /webhooks/in/:connectorId
	// (WITHOUT the /api/v1 prefix) to ensure Fiber's group-level Use() middleware
	// from the authenticated api group does NOT apply to it.
	// The gateway's PublicIntegrationWebhookProxy forwards to this path directly.
	connectorHandler.RegisterPublic(app)

	api := app.Group("/api/v1", handler.RequireInternalToken(internalSecret))
	connectorHandler.Register(api)

	workerCtx, cancelWorker := context.WithCancel(context.Background())
	defer cancelWorker()
	go deliveryWorker.Start(workerCtx)

	go func() {
		if err := app.Listen(":" + port); err != nil {
			logger.Error("integrations service listen failed", "error", err)
		}
	}()

	logger.Info("integrations service started", "port", port)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down integrations service")
	cancelWorker()
	if err := app.ShutdownWithTimeout(10 * time.Second); err != nil {
		logger.Error("integrations service shutdown failed", "error", err)
	}
}
