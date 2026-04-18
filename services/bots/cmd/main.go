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
	orchidCrypto "github.com/mst-corp/orbit/pkg/crypto"
	"github.com/mst-corp/orbit/pkg/metrics"
	"github.com/mst-corp/orbit/pkg/response"
	"github.com/mst-corp/orbit/services/bots/internal/botapi"
	"github.com/mst-corp/orbit/services/bots/internal/botfather"
	"github.com/mst-corp/orbit/services/bots/internal/client"
	"github.com/mst-corp/orbit/services/bots/internal/handler"
	"github.com/mst-corp/orbit/services/bots/internal/service"
	"github.com/mst-corp/orbit/services/bots/internal/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	port := config.EnvOr("PORT", "8086")
	dbDSN := config.MustEnv("DATABASE_URL")
	redisURL := config.MustEnv("REDIS_URL")
	natsURL := config.NatsURL()
	internalSecret := config.MustEnv("INTERNAL_SECRET")
	messagingServiceURL := config.EnvOr("MESSAGING_URL", config.EnvOr("MESSAGING_SERVICE_URL", "http://localhost:8082"))
	mediaServiceURL := config.EnvOr("MEDIA_URL", config.EnvOr("MEDIA_SERVICE_URL", "http://localhost:8083"))
	botTokenSecret := config.MustEnv("BOT_TOKEN_SECRET")

	integrationsServiceURL := config.EnvOr("INTEGRATIONS_URL", config.EnvOr("INTEGRATIONS_SERVICE_URL", "http://localhost:8087"))

	logger.Info("starting bots service",
		"port", port,
		"messaging_service_url", messagingServiceURL,
		"media_service_url", mediaServiceURL,
		"integrations_service_url", integrationsServiceURL,
	)

	ctx, cancelCtx := context.WithCancel(context.Background())
	defer cancelCtx()

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

	nc, err := nats.Connect(natsURL, nats.ReconnectWait(2*time.Second), nats.MaxReconnects(-1))
	if err != nil {
		logger.Error("failed to connect to nats", "error", err)
		os.Exit(1)
	}
	defer nc.Close()

	botStore := store.NewBotStore(pool)
	tokenStore := store.NewTokenStore(pool)
	commandStore := store.NewCommandStore(pool)
	installationStore := store.NewInstallationStore(pool)
	updateQueue := service.NewUpdateQueue(rdb)
	encryptionKey := orchidCrypto.DeriveKey(botTokenSecret)
	webhookWorker := service.NewWebhookWorker(rdb, encryptionKey, logger)

	botService := service.NewBotService(botStore, tokenStore, commandStore, installationStore, botTokenSecret)
	botHandler := handler.NewBotHandler(botService, logger).
		WithCallbackSupport(rdb, webhookWorker, updateQueue, installationStore, encryptionKey)
	msgClient := client.NewMessagingClient(messagingServiceURL, internalSecret)
	mediaClient := client.NewMediaClient(mediaServiceURL, internalSecret)
	botAPIHandler := botapi.NewBotAPIHandler(botService, msgClient, mediaClient, encryptionKey, logger).WithRedis(rdb).WithUpdateQueue(updateQueue)
	natsSubscriber := service.NewBotNATSSubscriber(nc, installationStore, webhookWorker, updateQueue, logger)

	// Provision BotFather system bot
	intClient := client.NewIntegrationsClient(integrationsServiceURL, internalSecret)
	stateStore := botfather.NewRedisStateStore(rdb)
	bf, err := botfather.Provision(ctx, botService, botStore, tokenStore, commandStore, msgClient, intClient, stateStore, encryptionKey, logger)
	if err != nil {
		logger.Error("failed to provision botfather", "error", err)
		os.Exit(1)
	}
	natsSubscriber.SetBotFather(bf)
	botHandler.SetBotFather(bf)

	if err := natsSubscriber.Start(); err != nil {
		logger.Error("failed to start bot nats subscriber", "error", err)
		os.Exit(1)
	}
	go webhookWorker.Start(ctx)

	app := fiber.New(fiber.Config{
		ErrorHandler: response.FiberErrorHandler,
	})

	metricsReg := metrics.New("bots")
	app.Use(metricsReg.HTTPMiddleware())

	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok", "service": "orbit-bots"})
	})

	app.Get("/metrics", handler.RequireInternalToken(internalSecret), metricsReg.Handler())

	api := app.Group("/api/v1", handler.RequireInternalToken(internalSecret))
	botHandler.Register(api)

	botAPIGroup := app.Group("/bot/:token", botapi.TokenAuthMiddleware(botService))
	botAPIHandler.Register(botAPIGroup)

	go func() {
		if err := app.Listen(":" + port); err != nil {
			logger.Error("bots service listen failed", "error", err)
		}
	}()

	logger.Info("bots service started", "port", port)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down bots service")
	cancelCtx() // Stop webhook worker and other goroutines
	if err := app.ShutdownWithTimeout(10 * time.Second); err != nil {
		logger.Error("bots service shutdown failed", "error", err)
	}
}
