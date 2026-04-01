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
	"github.com/mst-corp/orbit/pkg/response"
	"github.com/mst-corp/orbit/services/messaging/internal/handler"
	"github.com/mst-corp/orbit/services/messaging/internal/search"
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
	slog.Info("NATS connected", "url", natsURL)

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

	// Stores
	chatStore := store.NewChatStore(pool)
	messageStore := store.NewMessageStore(pool)
	userStore := store.NewUserStore(pool)
	inviteStore := store.NewInviteStore(pool)
	privacyStore := store.NewPrivacySettingsStore(pool)
	blockedStore := store.NewBlockedUsersStore(pool)
	userSettingsStore := store.NewUserSettingsStore(pool)
	notifStore := store.NewNotificationSettingsStore(pool)
	pushStore := store.NewPushSubscriptionStore(pool)

	// Services
	chatSvc := service.NewChatService(chatStore, messageStore, natsPublisher)
	msgSvc := service.NewMessageService(messageStore, chatStore, blockedStore, natsPublisher, rdb)
	userSvc := service.NewUserService(userStore, chatStore)
	linkPreviewSvc := service.NewLinkPreviewService(rdb, logger)
	inviteSvc := service.NewInviteService(inviteStore, chatStore, natsPublisher)
	settingsSvc := service.NewSettingsService(privacyStore, blockedStore, userSettingsStore, notifStore)
	searchSvc := service.NewSearchService(searchClient, chatStore)

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
	}

	// Handlers
	internalSecret := config.MustEnv("INTERNAL_SECRET")
	chatHandler := handler.NewChatHandler(chatSvc, logger, internalSecret)
	msgHandler := handler.NewMessageHandler(msgSvc, linkPreviewSvc, logger)
	userHandler := handler.NewUserHandler(userSvc, logger)
	inviteHandler := handler.NewInviteHandler(inviteSvc, logger)
	settingsHandler := handler.NewSettingsHandler(settingsSvc, pushStore, logger)
	searchHandler := handler.NewSearchHandler(searchSvc, logger)

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
	inviteHandler.Register(app)
	inviteHandler.RegisterPublic(app)
	settingsHandler.Register(app)
	searchHandler.Register(app)

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
