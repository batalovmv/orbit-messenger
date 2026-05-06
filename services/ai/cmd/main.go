// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"context"
	"crypto/subtle"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/mst-corp/orbit/pkg/config"
	"github.com/mst-corp/orbit/pkg/metrics"
	"github.com/mst-corp/orbit/pkg/response"
	"github.com/mst-corp/orbit/services/ai/internal/client"
	"github.com/mst-corp/orbit/services/ai/internal/handler"
	"github.com/mst-corp/orbit/services/ai/internal/service"
	"github.com/mst-corp/orbit/services/ai/internal/store"
)

// Phase 8A AI service — Claude + Whisper integration for summarise, translate,
// reply-suggest, transcribe, usage stats. Port 8085.
//
// The service intentionally starts successfully even when ANTHROPIC_API_KEY
// and OPENAI_API_KEY are empty or "placeholder" — in that mode every /ai/*
// endpoint returns 503 service_unavailable. This lets ops deploy the image
// before real credentials are provisioned and swap them in later on
// Saturn.ac without rebuilding.
func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	port := config.EnvOr("PORT", "8085")
	dbDSN := config.MustEnv("DATABASE_URL")
	redisURL := config.MustEnv("REDIS_URL")
	internalSecret := config.MustEnv("INTERNAL_SECRET")
	messagingURL := config.EnvOr("MESSAGING_URL", config.EnvOr("MESSAGING_SERVICE_URL", "http://localhost:8082"))
	mediaURL := config.EnvOr("MEDIA_URL", config.EnvOr("MEDIA_SERVICE_URL", "http://localhost:8083"))

	anthropicKey := os.Getenv("ANTHROPIC_API_KEY")
	anthropicModel := config.EnvOr("ANTHROPIC_MODEL", "claude-sonnet-4-6")
	classifyModel := config.EnvOr("ANTHROPIC_CLASSIFY_MODEL", "claude-3-haiku-20240307")
	// Optional override — when set, routes API calls through a proxy
	// (e.g. http://tokenator/anthropic/v1 for local testing). Empty in
	// production: must NEVER be set on Saturn — the value is the proxy
	// vendor's, not ours, and would route private chat content through them.
	anthropicBaseURL := os.Getenv("ANTHROPIC_BASE_URL")
	whisperKey := os.Getenv("OPENAI_API_KEY")
	whisperModel := config.EnvOr("WHISPER_MODEL", "whisper-1")
	// OPENAI_BASE_URL routes Whisper through any OpenAI-compatible provider.
	// We use Groq's `/openai/v1` endpoint to avoid OpenAI's per-request
	// pricing — Groq's free tier covers our pilot. Empty falls back to
	// api.openai.com.
	whisperBaseURL := strings.TrimSpace(os.Getenv("OPENAI_BASE_URL"))

	logger.Info("starting ai service",
		"port", port,
		"messaging_url", messagingURL,
		"media_url", mediaURL,
		"anthropic_configured", anthropicKey != "" && anthropicKey != "placeholder",
		"whisper_configured", whisperKey != "" && whisperKey != "placeholder",
		"anthropic_model", anthropicModel,
		"whisper_model", whisperModel,
		"whisper_base_override", whisperBaseURL != "",
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

	// External clients — all tolerant to missing credentials (see client.Configured()).
	anthropicClient := client.NewAnthropicClient(anthropicKey, anthropicModel, logger)
	classifyClient := client.NewAnthropicClient(anthropicKey, classifyModel, logger)
	if anthropicBaseURL != "" {
		logger.Warn("ANTHROPIC_BASE_URL override is active — DO NOT use this in production",
			"base_url", anthropicBaseURL)
		anthropicClient.SetBaseURL(anthropicBaseURL)
		classifyClient.SetBaseURL(anthropicBaseURL)
	}
	whisperClient := client.NewWhisperClient(whisperKey, whisperModel, logger)
	if whisperBaseURL != "" {
		whisperClient.SetBaseURL(whisperBaseURL)
	}
	messagingClient := client.NewMessagingClient(messagingURL, internalSecret)

	// Metrics registry has to be built before the service so the classifier
	// can record on it from the very first request.
	metricsReg := metrics.New("ai")
	classifierMetrics := service.NewClassifierMetrics(metricsReg)

	// Store + service wiring.
	usageStore := store.NewUsageStore(pool)
	notificationStore := store.NewNotificationStore(pool)
	aiService := service.NewAIService(service.AIServiceConfig{
		Anthropic:         anthropicClient,
		ClassifyClient:    classifyClient,
		Whisper:           whisperClient,
		Messaging:         messagingClient,
		Usage:             usageStore,
		Notification:      notificationStore,
		Redis:             rdb,
		MediaServiceURL:   mediaURL,
		InternalToken:     internalSecret,
		Logger:            logger,
		RateLimitPerMin:   20, // ТЗ §11.8
		ClassifierMetrics: classifierMetrics,
	})

	aiHandler := handler.NewAIHandler(aiService, logger)
	notifHandler := handler.NewNotificationHandler(aiService, notificationStore, logger)

	app := fiber.New(fiber.Config{
		ErrorHandler: response.FiberErrorHandler,
		// SSE streaming requires disabling keepalive-free and body-limit caps.
		StreamRequestBody: true,
		ReadBufferSize:    8192,
		WriteBufferSize:   8192,
	})

	app.Use(metricsReg.HTTPMiddleware())

	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"status":               "ok",
			"service":              "orbit-ai",
			"anthropic_configured": anthropicClient.Configured(),
			"whisper_configured":   whisperClient.Configured(),
		})
	})

	app.Get("/metrics", func(c *fiber.Ctx) error {
		token := c.Get("X-Internal-Token")
		if internalSecret == "" || subtle.ConstantTimeCompare([]byte(token), []byte(internalSecret)) != 1 {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		return metricsReg.Handler()(c)
	})

	api := app.Group("/api/v1", handler.RequireInternalToken(internalSecret))
	aiHandler.Register(api)
	notifHandler.Register(api)

	go func() {
		if err := app.Listen(":" + port); err != nil {
			logger.Error("ai service listen failed", "error", err)
		}
	}()

	logger.Info("ai service started", "port", port)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down ai service")
	cancelCtx()
	if err := app.ShutdownWithTimeout(10 * time.Second); err != nil {
		logger.Error("ai service shutdown failed", "error", err)
	}
}
