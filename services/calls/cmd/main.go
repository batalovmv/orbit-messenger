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

	"github.com/mst-corp/orbit/pkg/config"
	"github.com/mst-corp/orbit/pkg/response"
	"github.com/mst-corp/orbit/services/calls/internal/handler"
	"github.com/mst-corp/orbit/services/calls/internal/service"
	"github.com/mst-corp/orbit/services/calls/internal/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	// Config
	port := config.EnvOr("PORT", "8084")
	dbDSN, dbPassword, dbRawPassword := config.DatabaseDSN()
	slog.Info("database config", "dsn", dbDSN, "password_len", len(dbPassword))
	natsURL := config.NatsURL()
	internalSecret := config.MustEnv("INTERNAL_SECRET")
	turnURL := config.EnvOr("TURN_URL", "")
	turnUser := config.EnvOr("TURN_USER", "")
	turnPassword := config.EnvOr("TURN_PASSWORD", "")

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
			slog.Info("connected with fallback password", "password_len", len(pass))
		}
		slog.Info("database connected successfully")
		break
	}
	if err != nil {
		slog.Error("all database connection attempts failed")
		os.Exit(1)
	}
	defer pool.Close()

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

	// Stores
	callStore := store.NewCallStore(pool)
	participantStore := store.NewParticipantStore(pool)

	// Services
	callSvc := service.NewCallService(callStore, participantStore, natsPublisher, logger)

	// Handlers
	callHandler := handler.NewCallHandler(callSvc, logger, turnURL, turnUser, turnPassword)

	// Fiber
	app := fiber.New(fiber.Config{
		ErrorHandler: response.FiberErrorHandler,
	})

	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok", "service": "orbit-calls"})
	})

	// Register routes behind internal token middleware
	api := app.Group("", handler.RequireInternalToken(internalSecret))
	callHandler.Register(api)

	// Graceful shutdown
	go func() {
		if err := app.Listen(":" + port); err != nil {
			slog.Error("server error", "error", err)
		}
	}()

	slog.Info("calls service started", "port", port)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down calls service")
	if err := app.ShutdownWithTimeout(10 * time.Second); err != nil {
		slog.Error("shutdown error", "error", err)
	}
}
