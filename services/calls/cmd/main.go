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
	"github.com/mst-corp/orbit/pkg/metrics"
	"github.com/mst-corp/orbit/pkg/response"
	"github.com/mst-corp/orbit/services/calls/internal/handler"
	"github.com/mst-corp/orbit/services/calls/internal/service"
	"github.com/mst-corp/orbit/services/calls/internal/store"
	sfu "github.com/mst-corp/orbit/services/calls/internal/webrtc"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	// Config
	port := config.EnvOr("PORT", "8084")
	dbDSN, dbPassword, dbRawPassword := config.DatabaseDSN()
	slog.Info("database config", "dsn", config.RedactURL(dbDSN), "password_len", len(dbPassword))
	natsURL := config.NatsURL()
	internalSecret := config.MustEnv("INTERNAL_SECRET")
	// TURN_PUBLIC_URL is what clients use to reach coturn (must be reachable from user browsers).
	// Falls back to TURN_URL for backwards compatibility with older deployments.
	turnURL := config.EnvOr("TURN_PUBLIC_URL", config.EnvOr("TURN_URL", ""))
	// When TURN_SHARED_SECRET is set, coturn must be configured with
	// use-auth-secret and matching static-auth-secret in turnserver.conf.
	turnSharedSecret := config.EnvOr("TURN_SHARED_SECRET", "")
	turnUser := config.EnvOr("TURN_USER", "")
	turnPassword := config.EnvOr("TURN_PASSWORD", "")
	if turnURL != "" && turnSharedSecret == "" && (turnUser == "" || turnPassword == "") {
		slog.Warn("TURN_PUBLIC_URL set but TURN_USER/TURN_PASSWORD missing — TURN server will be dropped from ICE config",
			"turn_url", turnURL, "has_user", turnUser != "", "has_password", turnPassword != "")
	}
	if turnURL != "" && turnSharedSecret == "" && turnUser != "" && turnPassword != "" {
		slog.Warn("TURN_SHARED_SECRET unset — falling back to static TURN credentials", "turn_url", turnURL)
	}
	if turnURL != "" && turnSharedSecret != "" {
		slog.Info("TURN shared secret configured for short-lived credentials", "turn_url", turnURL)
	}
	if turnURL == "" {
		slog.Warn("TURN_PUBLIC_URL empty — calls will fall back to STUN only, NAT traversal may fail for corporate networks")
	}

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
	callSvc := service.NewCallService(callStore, participantStore, natsPublisher, logger).WithTURNSharedSecret(turnSharedSecret)

	// SFU (Pion) — group call media plane. Lives inside the calls service.
	sfuInstance, err := sfu.NewSFU(logger)
	if err != nil {
		slog.Error("failed to init SFU", "error", err)
		os.Exit(1)
	}

	// Handlers
	callHandler := handler.NewCallHandler(callSvc, logger, turnURL, turnUser, turnPassword)
	sfuHandler := handler.NewSFUHandler(callSvc, sfuInstance, logger)

	// Fiber
	app := fiber.New(fiber.Config{
		ErrorHandler: response.FiberErrorHandler,
	})

	metricsReg := metrics.New("calls")
	app.Use(metricsReg.HTTPMiddleware())

	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok", "service": "orbit-calls"})
	})

	app.Get("/metrics", handler.RequireInternalToken(internalSecret), metricsReg.Handler())

	// Register routes behind internal token middleware
	api := app.Group("", handler.RequireInternalToken(internalSecret))
	callHandler.Register(api)
	sfuHandler.Register(api)

	// SFU cleanup: drop rooms whose peers have all disconnected without
	// the explicit leave path firing (e.g. browser crash before WS close).
	go sfuInstance.StartCleanupLoop(ctx, 30*time.Second)

	// Background worker: expire ringing calls older than 60 seconds.
	// Without this, a ringing call sits forever if the callee's client dies
	// before declining — caller stuck in "ringing" and new calls blocked.
	expireCtx, expireCancel := context.WithCancel(ctx)
	defer expireCancel()
	go func() {
		const ringingTimeout = 60 * time.Second
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-expireCtx.Done():
				return
			case <-ticker.C:
				if err := callSvc.ExpireRingingCalls(expireCtx, ringingTimeout); err != nil {
					slog.Error("expire ringing calls", "error", err)
				}
			}
		}
	}()

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
