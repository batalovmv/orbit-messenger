package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"

	"github.com/mst-corp/orbit/pkg/config"
	"github.com/mst-corp/orbit/pkg/crypto"
	"github.com/mst-corp/orbit/pkg/metrics"
	"github.com/mst-corp/orbit/pkg/response"
	"github.com/mst-corp/orbit/services/messaging/internal/handler"
	"github.com/mst-corp/orbit/services/messaging/internal/search"
	"github.com/mst-corp/orbit/services/messaging/internal/service"
	"github.com/mst-corp/orbit/services/messaging/internal/store"
	"github.com/mst-corp/orbit/services/messaging/internal/tenor"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	// Config
	port := config.EnvOr("PORT", "8082")
	dbDSN, dbPassword, dbRawPassword := config.DatabaseDSN()
	slog.Info("database config", "dsn", config.RedactURL(dbDSN), "password_len", len(dbPassword))
	redisURL := config.MustEnv("REDIS_URL")
	natsURL := config.NatsURL()
	internalSecret := config.MustEnv("INTERNAL_SECRET")
	mediaServiceURL := config.EnvOr("MEDIA_URL", config.EnvOr("MEDIA_SERVICE_URL", "http://localhost:8083"))
	telegramBotToken := config.EnvOr("TELEGRAM_BOT_TOKEN", "")

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
	slog.Info("NATS connected")

	natsPublisher := service.NewNATSPublisher(nc)

	// Meilisearch
	meiliURL := config.EnvOr("MEILISEARCH_URL", "http://localhost:7700")
	meiliKey := config.EnvOr("MEILISEARCH_KEY", "")
	var searchClient service.SearchClient
	var meiliClient *search.MeilisearchClient
	if meiliKey != "" {
		mc, err := search.NewMeilisearchClient(meiliURL, meiliKey)
		if err != nil {
			slog.Error("meilisearch init failed, search disabled", "error", err)
			searchClient = search.NewNoopSearchClient()
		} else {
			meiliClient = mc
			searchClient = mc
			slog.Info("meilisearch connected", "url", meiliURL)
		}
	} else {
		searchClient = search.NewNoopSearchClient()
		slog.Warn("meilisearch not configured, search will return empty results")
	}

	// At-rest content encryption key. The master key comes from env as a
	// secret string; DeriveKey hashes it to 32 bytes (AES-256). A stolen
	// Postgres dump without this env var reveals only ciphertext. Admin
	// reads through the service layer stay transparent — the store layer
	// wraps/unwraps on every write/read.
	atRestKey := crypto.DeriveKey(config.MustEnv("ORBIT_MESSAGE_ENCRYPTION_KEY"))

	// Stores
	chatStore := store.NewChatStore(pool, atRestKey)
	messageStore := store.NewMessageStore(pool, atRestKey)
	userStore := store.NewUserStore(pool)
	inviteStore := store.NewInviteStore(pool)
	privacyStore := store.NewPrivacySettingsStore(pool)
	blockedStore := store.NewBlockedUsersStore(pool)
	userSettingsStore := store.NewUserSettingsStore(pool)
	notifStore := store.NewNotificationSettingsStore(pool)
	pushStore := store.NewPushSubscriptionStore(pool)
	reactionStore := store.NewReactionStore(pool)
	stickerStore := store.NewStickerStore(pool)
	gifStore := store.NewGIFStore(pool)
	pollStore := store.NewPollStore(pool)
	scheduledStore := store.NewScheduledMessageStore(pool, atRestKey)
	searchHistoryStore := store.NewSearchHistoryStore(pool)
	auditStore := store.NewAuditStore(pool)

	// Services
	searchSvc := service.NewSearchService(searchClient, chatStore, userStore)
	msgSvc := service.NewMessageService(messageStore, chatStore, blockedStore, natsPublisher, rdb, auditStore)
	// Phase 8A: @orbit-ai chat mention. Disabled unless the bot user has
	// been provisioned AND the AI service is reachable.
	msgSvc.ConfigureOrbitAIBot(
		config.EnvOr("ORBIT_AI_BOT_USER_ID", ""),
		config.EnvOr("AI_SERVICE_URL", "http://localhost:8085"),
		internalSecret,
	)
	userSvc := service.NewUserService(userStore, chatStore, privacyStore, searchSvc).WithPublisher(natsPublisher)
	linkPreviewSvc := service.NewLinkPreviewService(rdb, logger)
	inviteSvc := service.NewInviteService(inviteStore, chatStore, natsPublisher)
	settingsSvc := service.NewSettingsService(privacyStore, blockedStore, userSettingsStore, notifStore, chatStore)
	reactionSvc := service.NewReactionService(reactionStore, messageStore, chatStore, natsPublisher, logger)
	telegramStickerClient := service.NewTelegramBotStickerClient(telegramBotToken, logger)
	stickerMediaUploader := service.NewMediaServiceStickerUploader(mediaServiceURL, internalSecret, logger)
	stickerSvc := service.NewStickerService(
		stickerStore,
		logger,
		service.WithStickerImportClients(telegramStickerClient, stickerMediaUploader),
	)
	tenorClient := tenor.NewClientFromEnv(rdb, logger)
	gifSvc := service.NewGIFService(gifStore, tenorClient, logger)
	pollSvc := service.NewPollService(pollStore, messageStore, chatStore, natsPublisher, logger)
	chatSvc := service.NewChatService(chatStore, messageStore, natsPublisher, pollSvc, service.WithChatSearchIndexer(searchSvc))
	scheduledSvc := service.NewScheduledMessageService(
		scheduledStore,
		messageStore,
		pollStore,
		chatStore,
		blockedStore,
		natsPublisher,
		rdb,
		logger,
	)
	// NATS subscriber: update user status + last_seen_at in DB
	statusSub, subErr := nc.Subscribe("orbit.user.*.status", func(msg *nats.Msg) {
		var event struct {
			Event string          `json:"event"`
			Data  json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(msg.Data, &event); err != nil {
			return
		}
		var sd struct {
			UserID   string `json:"user_id"`
			Status   string `json:"status"`
			LastSeen string `json:"last_seen,omitempty"`
		}
		if err := json.Unmarshal(event.Data, &sd); err != nil {
			return
		}
		// Validate user_id is a valid UUID
		if _, err := uuid.Parse(sd.UserID); err != nil {
			slog.Warn("invalid user_id in status event", "user_id", sd.UserID)
			return
		}
		// Validate status against allowed enum values
		validStatuses := map[string]bool{"online": true, "offline": true, "away": true, "dnd": true}
		if !validStatuses[sd.Status] {
			slog.Warn("invalid status in status event", "status", sd.Status, "user_id", sd.UserID)
			return
		}
		var lastSeen *time.Time
		if sd.LastSeen != "" {
			if t, err := time.Parse(time.RFC3339, sd.LastSeen); err == nil {
				lastSeen = &t
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := userStore.UpdateStatus(ctx, sd.UserID, sd.Status, lastSeen); err != nil {
			slog.Error("failed to update user status", "user_id", sd.UserID, "error", err)
		}
	})
	if subErr != nil {
		slog.Error("failed to subscribe to user status events", "error", subErr)
		os.Exit(1)
	}
	defer statusSub.Unsubscribe()

	// Search indexer (listens to NATS message events → indexes in Meilisearch)
	if meiliClient != nil {
		indexer := search.NewIndexer(meiliClient, nc, logger)
		if err := indexer.Start(); err != nil {
			slog.Error("failed to start search indexer", "error", err)
			os.Exit(1)
		}
		defer indexer.Stop()

		// Populate users and chats indices from DB on startup (non-fatal).
		go func() {
			bootstrapCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			slog.Info("search: bootstrapping user and chat indices")
			searchSvc.BootstrapIndices(bootstrapCtx)
			slog.Info("search: bootstrap complete")
		}()
	}

	cronCtx, cancelCron := context.WithCancel(context.Background())
	defer cancelCron()
	go runScheduledDeliveryCron(cronCtx, scheduledSvc, logger)

	// Handlers
	chatHandler := handler.NewChatHandler(chatSvc, logger, internalSecret)
	msgHandler := handler.NewMessageHandler(msgSvc, pollSvc, scheduledSvc, linkPreviewSvc, logger).SetReactionService(reactionSvc)
	userHandler := handler.NewUserHandler(userSvc, logger, chatSvc)
	inviteHandler := handler.NewInviteHandler(inviteSvc, logger)
	settingsHandler := handler.NewSettingsHandler(settingsSvc, pushStore, logger, internalSecret)
	searchHandler := handler.NewSearchHandler(searchSvc, logger, searchHistoryStore)
	reactionHandler := handler.NewReactionHandler(reactionSvc, logger)
	stickerHandler := handler.NewStickerHandler(stickerSvc, logger)
	gifHandler := handler.NewGIFHandler(gifSvc, logger)
	pollHandler := handler.NewPollHandler(pollSvc, logger)
	scheduledHandler := handler.NewScheduledHandler(scheduledSvc, logger)
	adminSvc := service.NewAdminService(userStore, chatStore, auditStore, natsPublisher)
	adminHandler := handler.NewAdminHandler(adminSvc)

	// Fiber
	app := fiber.New(fiber.Config{
		ErrorHandler: response.FiberErrorHandler,
	})

	metricsReg := metrics.New("messaging")
	app.Use(metricsReg.HTTPMiddleware())

	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok", "service": "orbit-messaging"})
	})

	app.Get("/metrics", handler.RequireInternalToken(internalSecret), metricsReg.Handler())

	// Register routes behind internal token middleware.
	// X-User-ID is only trusted when X-Internal-Token is valid,
	// preventing identity spoofing if the service is reached outside the gateway.
	api := app.Group("", handler.RequireInternalToken(internalSecret))
	chatHandler.Register(api)
	msgHandler.Register(api)
	userHandler.Register(api)
	inviteHandler.Register(api)
	inviteHandler.RegisterPublic(api)
	settingsHandler.Register(api)
	searchHandler.Register(api)
	reactionHandler.Register(api)
	stickerHandler.Register(api)
	gifHandler.Register(api)
	pollHandler.Register(api)
	scheduledHandler.Register(api)
	adminHandler.Register(api)

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

func runScheduledDeliveryCron(ctx context.Context, scheduledSvc *service.ScheduledMessageService, logger *slog.Logger) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			deliverCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			count, err := scheduledSvc.DeliverPending(deliverCtx)
			cancel()

			if err != nil {
				logger.Error("deliver pending scheduled messages", "error", err)
				continue
			}
			if count > 0 {
				logger.Info("delivered scheduled messages", "count", count)
			}
		}
	}
}
