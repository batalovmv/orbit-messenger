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

	// Metrics registry — created before push dispatcher so we can pass the
	// per-outcome counter into push.Config and keep all registrations in one
	// place. The HTTP middleware and other consumers below read from the same
	// registry.
	metricsReg := metrics.New("gateway")
	pushAttemptsCounter := metricsReg.Counter(
		"orbit_push_attempts_total",
		"Web push delivery attempts grouped by outcome (ok / fail / stale).",
		"result",
	)
	// Day 5.1 VAPID hardening: surface dispatcher state as a gauge so prod
	// dashboards/alerts can flag "push has been silently broken for hours".
	// Stays at 0 forever if VAPID env vars are missing — pre-Day 5.1 we only
	// had a startup WARN that nobody noticed. Alert rule lives in
	// monitoring/prometheus/rules/orbit.yml.
	pushDispatcherEnabledGauge := metricsReg.Gauge(
		"orbit_push_dispatcher_enabled",
		"1 if the gateway push dispatcher has VAPID + messaging URL configured at startup, 0 otherwise.",
	)

	// NATS Subscriber
	pushDispatcher := push.NewDispatcher(push.Config{
		PublicKey:           vapidPublicKey,
		PrivateKey:          vapidPrivateKey,
		Subscriber:          vapidSubscriber,
		MessagingServiceURL: messagingServiceURL,
		InternalSecret:      internalSecret,
		Logger:              logger,
		AttemptsCounter:     pushAttemptsCounter,
	})
	if pushDispatcher.Enabled() {
		pushDispatcherEnabledGauge.WithLabelValues().Set(1)
	} else {
		pushDispatcherEnabledGauge.WithLabelValues().Set(0)
		// ERROR (not WARN): pre-Day 5.1 incident on 2026-04-28 showed that a
		// WARN-level startup line is invisible in prod log noise — VAPID env
		// vars were empty for an unknown duration and push (incl. Day 4b
		// cross-device read-sync) silently no-op'd. Loud line + the gauge
		// above ensure this is surfaced quickly next time.
		slog.Error("web push dispatcher disabled: missing VAPID configuration — all web push will silently no-op",
			"vapid_public_set", vapidPublicKey != "",
			"vapid_private_set", vapidPrivateKey != "",
			"messaging_url_set", messagingServiceURL != "",
		)
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

	// /metrics is mounted behind the internal token so only the platform
	// scraper can read it. metricsReg is the same registry created above.
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

	// X-App-Version handshake. Mounted BEFORE rate limiting so a forced
	// upgrade response doesn't burn the client's per-IP quota. Both env
	// vars are optional and default to no-op:
	//   APP_LATEST_VERSION — when set, every response carries
	//     `X-App-Latest-Version: <ver>` and the web client triggers an
	//     update banner if it differs from its baked-in APP_VERSION. Set
	//     by CI/deploy from web/package.json's "version".
	//   APP_MIN_VERSION — when set, requests carrying `X-App-Version`
	//     below this are rejected with 426 + `X-Required-Version`.
	//     Opt-in: leave empty unless you have a hard-incompatibility
	//     between client and server (schema break, security fix that
	//     requires the new client). /auth/* and /public/* are always
	//     allowed through so users can still log in / poll version.txt.
	app.Use(middleware.AppVersionMiddleware(middleware.AppVersionConfig{
		LatestVersion: config.EnvOr("APP_LATEST_VERSION", ""),
		MinVersion:    config.EnvOr("APP_MIN_VERSION", ""),
	}))

	// Health
	app.Get("/health", handler.HealthHandler)

	// /metrics is guarded by the internal token so only the platform
	// scraper (or an operator with INTERNAL_SECRET) can read it. No JWT
	// required — Prometheus doesn't speak JWT.
	app.Get("/metrics", middleware.RequireInternalToken(internalSecret), metricsReg.Handler())

	// Internal admin-tool endpoints. Gated by X-Internal-Token: the messaging
	// service (which owns the user-facing /api/v1/admin/* surface and the
	// SysManageSettings perm check) calls these for primitives that only
	// gateway can perform — push dispatch with per-device report (Day 5.1
	// Push Inspector), session revocation broadcast (Day 5.2), etc.
	internalGroup := app.Group("/internal", middleware.RequireInternalToken(internalSecret))
	handler.RegisterAdminPushInternalRoute(internalGroup, handler.AdminPushInternalConfig{
		Dispatcher: pushDispatcher,
		Logger:     logger,
	})

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
	// RUM beacons (Web Vitals): one envelope per tab visit, but mobile users
	// can fire visibilitychange repeatedly (background → foreground loops).
	// 60/min/user is plenty without amplifying noisy clients.
	rumRateLimit := middleware.RateLimitMiddleware(middleware.RateLimitConfig{
		Redis: rdb, MaxPerMin: 60, KeyPrefix: "rum",
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
	// Unauthenticated system config — maintenance banner + unauth-safe flags.
	// Cheap, idempotent, no PII; the messaging service caches behind it. Rate
	// limit reuses the API limiter via per-IP keying (low-traffic by design).
	publicConfigRateLimit := middleware.RateLimitMiddleware(middleware.RateLimitConfig{
		Redis: rdb, MaxPerMin: 60, KeyPrefix: "system-config",
	})
	app.Get("/api/v1/public/system/config", publicConfigRateLimit, handler.PublicSystemConfigProxy(messagingServiceURL, frontendURL))
	app.All("/api/v1/bot/:token", handler.PublicBotAPIProxy(botsURL, frontendURL))
	app.All("/api/v1/bot/:token/*", handler.PublicBotAPIProxy(botsURL, frontendURL))
	// Accept both POST (default Orbit-native, InsightFlow/ASA presets) and
	// GET (Keitaro postbacks, HMAC in query params). Backend integrations
	// service enforces method-vs-preset matching and rejects the wrong method.
	app.All("/api/v1/webhooks/in/:connectorId", handler.PublicIntegrationWebhookProxy(integrationsURL, frontendURL))

	// API group with JWT + rate limiting
	// Note: media GET routes are handled by apiGroup.All("/media/*") in SetupProxy,
	// which applies JWT middleware and forwards X-Internal-Token to the media service.
	maintenanceMW := middleware.MaintenanceMiddleware(middleware.MaintenanceConfig{
		MessagingURL: messagingServiceURL,
		PollInterval: 10 * time.Second,
	})
	apiGroup := app.Group("/api/v1", jwtMW, maintenanceMW, apiRateLimit)

	// Stricter per-user limit for AI endpoints — must be registered before
	// SetupProxy so the middleware applies to apiGroup.All("/ai/*").
	apiGroup.Use("/ai/*", aiRateLimit)

	// RUM ingestion: gateway accepts Web Vitals beacons from authenticated
	// tabs and exports them as Prometheus histograms via /metrics. Behind
	// jwtMW (apiGroup) — only logged-in users contribute, which keeps the
	// signal clean and rules out anonymous flood.
	apiGroup.Post("/rum", rumRateLimit, handler.RUMHandler(handler.RUMConfig{
		Logger:   logger,
		Registry: metricsReg,
	}))

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
