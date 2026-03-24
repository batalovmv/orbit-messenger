package main

import (
	"log/slog"
	"os"
	"os/signal"
	"strings"
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
	subscriber := ws.NewSubscriber(hub, nc)
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
	authGroup := app.Group("/auth")
	authRateLimit := middleware.RateLimitMiddleware(middleware.RateLimitConfig{
		Redis: rdb, MaxPerMin: 5, KeyPrefix: "auth",
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

	// WebSocket endpoint (before API group to avoid conflict)
	app.Use("/api/v1/ws", func(c *fiber.Ctx) error {
		if websocket.IsWebSocketUpgrade(c) {
			// Validate JWT from query param or header
			token := c.Query("token")
			if token == "" {
				auth := c.Get("Authorization")
				if strings.HasPrefix(auth, "Bearer ") {
					token = auth[7:]
				}
			}
			if token == "" {
				return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
					"error": "unauthorized", "message": "Missing token", "status": 401,
				})
			}
			// Validate via middleware helper — inline for WS
			c.Request().Header.Set("Authorization", "Bearer "+token)
			jwtMW(c)
			userID := string(c.Request().Header.Peek("X-User-ID"))
			if userID == "" {
				return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
					"error": "unauthorized", "message": "Invalid token", "status": 401,
				})
			}
			c.Locals("user_id", userID)
			return c.Next()
		}
		return fiber.ErrUpgradeRequired
	})
	app.Get("/api/v1/ws", wsHandler.Upgrade())

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
