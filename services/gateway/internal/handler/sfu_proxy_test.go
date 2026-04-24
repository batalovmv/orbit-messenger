// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package handler

import (
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	fhwebsocket "github.com/fasthttp/websocket"
)

func TestSFUProxyHandler_CallIDValidation(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	app := fiber.New()
	cfg := SFUProxyConfig{
		CallsServiceURL: "http://localhost:9999",
		AuthServiceURL:  "http://localhost:9998",
		InternalSecret:  "test-secret",
		Redis:           rdb,
	}
	app.Use("/calls/:id/sfu-ws", SFUProxyUpgradeGuard())
	app.Get("/calls/:id/sfu-ws", SFUProxyHandler(cfg))

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	defer listener.Close()

	go func() {
		_ = app.Listener(listener)
	}()

	time.Sleep(100 * time.Millisecond)

	serverAddr := listener.Addr().String()

	tests := []struct {
		name           string
		callID         string
		expectClose    bool
		expectUpgrade  bool
	}{
		{
			name:          "empty callID closes connection",
			callID:        "",
			expectClose:   true,
			expectUpgrade: false,
		},
		{
			name:          "non-UUID callID closes connection",
			callID:        "notauuid",
			expectClose:   true,
			expectUpgrade: false,
		},
		{
			name:          "path traversal callID closes connection",
			callID:        "../../etc/passwd",
			expectClose:   true,
			expectUpgrade: false,
		},
		{
			name:          "valid UUID callID proceeds past validation",
			callID:        uuid.New().String(),
			expectClose:   false,
			expectUpgrade: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wsURL := fmt.Sprintf("ws://%s/calls/%s/sfu-ws", serverAddr, tt.callID)
			dialer := fhwebsocket.Dialer{
				HandshakeTimeout: 2 * time.Second,
			}

			conn, resp, err := dialer.Dial(wsURL, nil)

			if tt.expectUpgrade {
				if err != nil {
					t.Fatalf("expected successful upgrade, got error: %v", err)
				}
				if resp.StatusCode != http.StatusSwitchingProtocols {
					t.Fatalf("expected status 101, got %d", resp.StatusCode)
				}

				// Connection should stay open waiting for auth frame.
				// We don't send auth frame in this test, so we expect a timeout
				// when trying to read (handler sets 10s deadline for auth frame).
				_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
				_, _, readErr := conn.ReadMessage()
				if readErr == nil {
					t.Fatal("expected read timeout or close, got successful read")
				}

				_ = conn.Close()
				return
			}

			// For invalid callID, we expect the connection to close immediately
			// after upgrade attempt. The handler closes the connection before
			// any frame exchange.
			if err == nil {
				// Connection was established but should close immediately
				_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
				_, _, readErr := conn.ReadMessage()
				if readErr == nil {
					t.Fatal("expected connection close, but read succeeded")
				}
				_ = conn.Close()
			} else {
				// Connection failed during handshake or closed immediately
				// Both are acceptable for invalid callID
				if resp != nil && resp.StatusCode == http.StatusSwitchingProtocols {
					t.Fatal("expected connection to close, but got successful upgrade")
				}
			}
		})
	}
}

func TestSFUProxyUpgradeGuard_RejectsNonWebSocketRequests(t *testing.T) {
	app := fiber.New()
	app.Use("/calls/:id/sfu-ws", SFUProxyUpgradeGuard())
	app.Get("/calls/:id/sfu-ws", func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	// Plain HTTP GET (no Upgrade header) must be rejected with 426.
	req, err := http.NewRequest(http.MethodGet, "/calls/"+uuid.New().String()+"/sfu-ws", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}

	resp, testErr := app.Test(req, -1)
	if testErr != nil {
		t.Fatalf("app.Test: %v", testErr)
	}
	if resp.StatusCode != http.StatusUpgradeRequired {
		t.Fatalf("expected status %d, got %d", http.StatusUpgradeRequired, resp.StatusCode)
	}
}
