// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"errors"
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
	"github.com/mst-corp/orbit/pkg/metrics"
	"github.com/mst-corp/orbit/pkg/response"
	"github.com/mst-corp/orbit/services/gateway/internal/handler"
	"github.com/mst-corp/orbit/services/gateway/internal/middleware"
	"github.com/mst-corp/orbit/services/gateway/internal/push"
	"github.com/mst-corp/orbit/services/gateway/internal/ws"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	// Config (force redeploy)
	port := config.EnvOr("PORT", "8080")
	redisURL := config.MustEnv("REDIS_URL")
	natsURL := config.NatsURL()
	slog.Info("resolved NATS URL", "url", config.RedactURL(natsURL))
	authServiceURL := config.EnvOr("AUTH_URL", config.EnvOr("AUTH_SERVICE_URL", "http://localhost:8081"))
	messagingServiceURL := config.EnvOr("MESSAGING_URL", config.EnvOr("MESSAGING_SERVICE_URL", "http://localhost:8082"))
	mediaServiceURL := config.EnvOr("MEDIA_URL", config.EnvOr("MEDIA_SERVICE_URL", "http://localhost:8083"))
	callsServiceURL := config.EnvOr("CALLS_URL", config.EnvOr("CALLS_SERVICE_URL", "http://localhost:8084"))
	botsURL := config.EnvOr("BOTS_URL", config.EnvOr("BOTS_SERVICE_URL", "http://localhost:8086"))
	integrationsURL := config.EnvOr("INTEGRATIONS_URL", config.EnvOr("INTEGRATIONS_SERVICE_URL", "http://localhost:8087"))
	aiURL := config.EnvOr("AI_URL", config.EnvOr("AI_SERVICE_URL", "http://localhost:8085"))
	frontendURL := config.EnvOr("FRONTEND_URL", config.EnvOr("WEB_URL", "http://localhost:3000"))
	vapidPublicKey := config.EnvOr("VAPID_PUBLIC_KEY", "")
	vapidPrivateKey := config.EnvOr("VAPID_PRIVATE_KEY", "")
	vapidSubscriber := config.EnvOr("VAPID_SUBSCRIBER_EMAIL", "mailto:push@orbit.local")

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
	slog.Info("NATS connected", "url", config.RedactURL(natsURL))

	// JetStream: ensure the ORBIT stream exists covering all orbit.* subjects.
	// The stream provides at-least-once delivery so gateway restarts do not
	// silently drop in-flight events. Optional — falls back to core NATS when
	// JetStream is unavailable (e.g. Saturn NATS without JS config access).
	js, err := nc.JetStream()
	if err != nil {
		slog.Warn("JetStream unavailable, falling back to core NATS", "error", err)
	} else {
		_, err = js.AddStream(&nats.StreamConfig{
			Name:      "ORBIT",
			Subjects:  []string{"orbit.>"},
			Retention: nats.LimitsPolicy,
			MaxAge:    24 * time.Hour,
		})
		if err != nil {
			var apiErr *nats.APIError
			if !errors.As(err, &apiErr) || apiErr.ErrorCode != nats.JSErrCodeStreamNameInUse {
				slog.Warn("JetStream ORBIT stream not created, falling back to core NATS", "error", err)
			} else {
				slog.Info("JetStream ORBIT stream already exists")
			}
		} else {
			slog.Info("JetStream ORBIT stream created", "max_age", "24h")
		}
	}

	// WebSocket Hub
	internalSecret := config.MustEnv("INTERNAL_SECRET")
	// Startup guard: callsServiceURL is required for WebRTC signaling IDOR protection.
	// Without it, checkCallMembership fails closed (denies all signaling), so calls won't work.
	if callsServiceURL == "" {
		slog.Error("CALLS_URL not set — WebRTC signaling membership check will deny all relay requests")
		os.Exit(1)
	}
	hub := ws.NewHub()
	wsHandler := ws.NewHandler(hub, nc, callsServiceURL, internalSecret)
	defer wsHandler.Close()

	// NATS Subscriber
	pushDispatcher := push.NewDispatcher(push.Config{
		PublicKey:           vapidPublicKey,
		PrivateKey:          vapidPrivateKey,
		Subscriber:          vapidSubscriber,
		MessagingServiceURL: messagingServiceURL,
		InternalSecret:      internalSecret,
		Logger:              logger,
	})
	if !pushDispatcher.Enabled() {
		slog.Warn("web push dispatcher disabled: missing VAPID configuration")
	}

	subscriber := ws.NewSubscriber(hub, nc, messagingServiceURL, internalSecret, rdb, pushDispatcher)
	classifier := ws.NewNotificationClassifier(aiURL, internalSecret)
	subscriber.SetNotificationClassifier(classifier)
	if err := subscriber.Start(); err != nil {
		slog.Error("failed to start NATS subscriber", "error", err)
		os.Exit(1)
	}
	defer subscriber.Stop()

	// Fiber app
	fiberCfg := fiber.Config{
		BodyLimit:    55 * 1024 * 1024, // 55MB — media uploads up to 50MB, chunked chunks up to 10MB
		ErrorHandler: response.FiberErrorHandler,
		// Trust X-Forwarded-For from Saturn.ac ingress — required for correct per-IP rate limiting.
		// Without this, c.IP() returns the ingress IP and all clients share one rate-limit bucket.
		ProxyHeader: "X-Forwarded-For",
	}
	// Only trust X-Forwarded-For from known proxies — prevents IP spoofing for rate limiting.
	// SECURITY: EnableTrustedProxyCheck is ALWAYS on. Without TRUSTED_PROXIES, c.IP() falls
	// back to the raw connection IP (safe). Without this, any client can spoof X-Forwarded-For
	// to bypass per-IP rate limiting on auth endpoints.
	fiberCfg.EnableTrustedProxyCheck = true
	if proxies := config.EnvOr("TRUSTED_PROXIES", ""); proxies != "" {
		fiberCfg.TrustedProxies = strings.Split(proxies, ",")
	} else {
		slog.Warn("TRUSTED_PROXIES not set — X-Forwarded-For will be ignored, c.IP() returns raw connection IP")
	}
	app := fiber.New(fiberCfg)

	// Metrics registry + middleware — one per service, mounts /metrics
	// behind the internal token so only the platform scraper can read it.
	metricsReg := metrics.New("gateway")
	wsConnectionsGauge := metricsReg.Gauge(
		"orbit_ws_active_connections",
		"Active WebSocket connections currently held by this gateway instance.",
	)
	hub.SetMetricsCallback(func(delta int) {
		wsConnectionsGauge.WithLabelValues().Add(float64(delta))
	})

	// Global middleware
	app.Use(middleware.SecurityHeadersMiddleware())
	app.Use(middleware.LoggingMiddleware())
	app.Use(metricsReg.HTTPMiddleware())
	app.Use(middleware.CORSMiddleware(frontendURL))

	// Health
	app.Get("/health", handler.HealthHandler)

	// /metrics is guarded by the internal token so only the platform
	// scraper (or an operator with INTERNAL_SECRET) can read it. No JWT
	// required — Prometheus doesn't speak JWT.
	app.Get("/metrics", middleware.RequireInternalToken(internalSecret), metricsReg.Handler())

	// Auth proxy (no JWT validation needed)
	authGroup := app.Group("/api/v1/auth")
	authSensitiveRateLimit := middleware.RateLimitMiddleware(middleware.RateLimitConfig{
		Redis: rdb, MaxPerMin: 5, KeyPrefix: "auth_sensitive",
	})
	authInviteValidationRateLimit := middleware.RateLimitMiddleware(middleware.RateLimitConfig{
		Redis: rdb, MaxPerMin: 20, KeyPrefix: "auth_invite_validation",
	})
	authSessionRateLimit := middleware.RateLimitMiddleware(middleware.RateLimitConfig{
		Redis:      rdb,
		MaxPerMin:  60,
		KeyPrefix:  "auth_session",
		Identifier: middleware.AuthRateLimitIdentifierByIP,
	})
	handler.RegisterAuthProxyRoutes(authGroup, handler.ProxyConfig{
		AuthServiceURL:      authServiceURL,
		MessagingServiceURL: messagingServiceURL,
		MediaServiceURL:     mediaServiceURL,
		FrontendURL:         frontendURL,
		InternalSecret:      internalSecret,
	}, handler.AuthProxyMiddlewares{
		Sensitive:        authSensitiveRateLimit,
		InviteValidation: authInviteValidationRateLimit,
		Session:          authSessionRateLimit,
	})

	// API routes with JWT validation
	jwtMW := middleware.JWTMiddleware(middleware.JWTConfig{
		AuthServiceURL: authServiceURL,
		Redis:          rdb,
		CacheTTL:       30 * time.Second,
	})
	apiRateLimit := middleware.RateLimitMiddleware(middleware.RateLimitConfig{
		Redis: rdb, MaxPerMin: 600, KeyPrefix: "api",
	})
	// AI endpoints are expensive (Claude/Whisper API spend) and already
	// enforce 20/min/user inside the ai service. Mirror that limit at the
	// edge so abusive callers get rejected before we pay for a Redis
	// round-trip and a downstream proxy hop.
	aiRateLimit := middleware.RateLimitMiddleware(middleware.RateLimitConfig{
		Redis: rdb, MaxPerMin: 20, KeyPrefix: "ai",
	})

	// WebSocket endpoint — auth happens via first "auth" frame after connection,
	// NOT via query param (tokens must not appear in URLs per TZ §8.1).
	// The limit keys on IP here because we run before JWT auth; when
	// TRUSTED_PROXIES is unset (as it often is on Saturn edge) every user
	// appears from the same ingress IP, so a strict 10/min would throttle
	// every legitimate user at once during a reconnect storm. 60/min/IP is
	// enough to stop casual scanning while still absorbing a full reconnect
	// wave. Per-user throttling happens post-auth inside the WS handler.
	wsRateLimit := middleware.RateLimitMiddleware(middleware.RateLimitConfig{
		Redis: rdb, MaxPerMin: 60, KeyPrefix: "ws",
	})
	app.Use("/api/v1/ws", wsRateLimit, func(c *fiber.Ctx) error {
		if websocket.IsWebSocketUpgrade(c) {
			return c.Next()
		}
		return fiber.ErrUpgradeRequired
	})
	app.Get("/api/v1/ws", wsHandler.Upgrade(authServiceURL, rdb))

	// SFU signaling WebSocket — group voice/video calls (Phase 6 Stage 3).
	// Registered BEFORE the generic /api/v1/calls/* HTTP proxy so that ws://
	// upgrades go through the bidirectional proxy instead of doProxy()'s
	// fasthttp HTTP path. Auth is the standard JWT middleware: by the time
	// the upgrade handler runs, X-User-ID is set from the validated token.
	sfuWsRateLimit := middleware.RateLimitMiddleware(middleware.RateLimitConfig{
		Redis: rdb, MaxPerMin: 10, KeyPrefix: "sfu-ws",
	})
	app.Get(
		"/api/v1/calls/:id/sfu-ws",
		sfuWsRateLimit,
		handler.SFUProxyUpgradeGuard(),
		handler.SFUProxyHandler(handler.SFUProxyConfig{
			CallsServiceURL: callsServiceURL,
			AuthServiceURL:  authServiceURL,
			InternalSecret:  internalSecret,
			Redis:           rdb,
		}),
	)

	// Public endpoints (no JWT) — must be registered before apiGroup
	inviteRateLimit := middleware.RateLimitMiddleware(middleware.RateLimitConfig{
		Redis: rdb, MaxPerMin: 20, KeyPrefix: "invite",
	})
	app.Get("/api/v1/chats/invite/:hash", inviteRateLimit, handler.PublicInviteProxy(messagingServiceURL, frontendURL))
	app.All("/api/v1/bot/:token", handler.PublicBotAPIProxy(botsURL, frontendURL))
	app.All("/api/v1/bot/:token/*", handler.PublicBotAPIProxy(botsURL, frontendURL))
	// Accept both POST (default Orbit-native, InsightFlow/ASA presets) and
	// GET (Keitaro postbacks, HMAC in query params). Backend integrations
	// service enforces method-vs-preset matching and rejects the wrong method.
	app.All("/api/v1/webhooks/in/:connectorId", handler.PublicIntegrationWebhookProxy(integrationsURL, frontendURL))

	// API group with JWT + rate limiting
	// Note: media GET routes are handled by apiGroup.All("/media/*") in SetupProxy,
	// which applies JWT middleware and forwards X-Internal-Token to the media service.
	apiGroup := app.Group("/api/v1", jwtMW, apiRateLimit)

	// Stricter per-user limit for AI endpoints — must be registered before
	// SetupProxy so the middleware applies to apiGroup.All("/ai/*").
	apiGroup.Use("/ai/*", aiRateLimit)

	// Setup proxy routes
	handler.SetupProxy(app, apiGroup, handler.ProxyConfig{
		AuthServiceURL:      authServiceURL,
		MessagingServiceURL: messagingServiceURL,
		MediaServiceURL:     mediaServiceURL,
		CallsServiceURL:     callsServiceURL,
		BotsServiceURL:      botsURL,
		IntegrationsServiceURL: integrationsURL,
		AiServiceURL:        aiURL,
		FrontendURL:         frontendURL,
		InternalSecret:      internalSecret,
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
