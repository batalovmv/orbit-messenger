package main

import (
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"

	"github.com/mst-corp/orbit/pkg/config"
	"github.com/mst-corp/orbit/pkg/response"
	"github.com/mst-corp/orbit/services/gateway/internal/handler"
	"github.com/mst-corp/orbit/services/gateway/internal/middleware"
	"github.com/mst-corp/orbit/services/gateway/internal/ws"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	// Config
	port := config.EnvOr("PORT", "8080")
	redisURL := config.MustEnv("REDIS_URL")
	natsURL := config.MustEnv("NATS_URL")
	authServiceURL := config.EnvOr("AUTH_SERVICE_URL", "http://localhost:8081")
	messagingServiceURL := config.EnvOr("MESSAGING_SERVICE_URL", "http://localhost:8082")
	frontendURL := config.EnvOr("FRONTEND_URL", "http://localhost:3000")

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

	// WebSocket Hub
	hub := ws.NewHub()
	wsHandler := ws.NewHandler(hub, nc)

	// NATS Subscriber
	subscriber := ws.NewSubscriber(hub, nc, messagingServiceURL)
	if err := subscriber.Start(); err != nil {
		slog.Error("failed to start NATS subscriber", "error", err)
		os.Exit(1)
	}
	defer subscriber.Stop()

	// Fiber app
	app := fiber.New(fiber.Config{
		ErrorHandler: response.FiberErrorHandler,
	})

	// Global middleware
	app.Use(middleware.LoggingMiddleware())
	app.Use(middleware.CORSMiddleware(frontendURL))

	// Health
	app.Get("/health", handler.HealthHandler)

	// Auth proxy (no JWT validation needed)
	authGroup := app.Group("/api/v1/auth")
	authRateLimit := middleware.RateLimitMiddleware(middleware.RateLimitConfig{
		Redis: rdb, MaxPerMin: 20, KeyPrefix: "auth",
	})
	authGroup.Use(authRateLimit)

	// API routes with JWT validation
	jwtMW := middleware.JWTMiddleware(middleware.JWTConfig{
		AuthServiceURL: authServiceURL,
		Redis:          rdb,
		CacheTTL:       30 * time.Second,
	})
	apiRateLimit := middleware.RateLimitMiddleware(middleware.RateLimitConfig{
		Redis: rdb, MaxPerMin: 100, KeyPrefix: "api",
	})

	// WebSocket endpoint — auth happens via first "auth" frame after connection,
	// NOT via query param (tokens must not appear in URLs per TZ §8.1)
	wsRateLimit := middleware.RateLimitMiddleware(middleware.RateLimitConfig{
		Redis: rdb, MaxPerMin: 10, KeyPrefix: "ws",
	})
	app.Use("/api/v1/ws", wsRateLimit, func(c *fiber.Ctx) error {
		if websocket.IsWebSocketUpgrade(c) {
			return c.Next()
		}
		return fiber.ErrUpgradeRequired
	})
	app.Get("/api/v1/ws", wsHandler.Upgrade(authServiceURL, rdb))

	// API group with JWT + rate limiting
	apiGroup := app.Group("/api/v1", jwtMW, apiRateLimit)

	// Setup proxy routes
	handler.SetupProxy(app, authGroup, apiGroup, handler.ProxyConfig{
		AuthServiceURL:      authServiceURL,
		MessagingServiceURL: messagingServiceURL,
	})

	// Graceful shutdown
	go func() {
		if err := app.Listen(":" + port); err != nil {
			slog.Error("server error", "error", err)
		}
	}()

	slog.Info("gateway started", "port", port)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down gateway")
	if err := app.ShutdownWithTimeout(10 * time.Second); err != nil {
		slog.Error("shutdown error", "error", err)
	}
}
