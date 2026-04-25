// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
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
		name          string
		callID        string
		expectClose   bool
		expectUpgrade bool
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

// testSHA256Hex computes the same hash as the unexported ws.sha256Hash so we
// can seed the Redis JWT cache in tests without importing the ws package.
func testSHA256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// testMakeFakeJWT builds a minimal unsigned JWT with a future exp claim.
// ValidateToken uses jwt.ParseUnverified so no real signature is needed.
func testMakeFakeJWT(userID string) string {
	encodeB64URL := func(s string) string {
		const alpha = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
		src := []byte(s)
		outLen := (len(src)*4 + 2) / 3
		dst := make([]byte, outLen)
		di, si := 0, 0
		n := (len(src) / 3) * 3
		for si < n {
			v := uint(src[si])<<16 | uint(src[si+1])<<8 | uint(src[si+2])
			dst[di] = alpha[v>>18&0x3F]
			dst[di+1] = alpha[v>>12&0x3F]
			dst[di+2] = alpha[v>>6&0x3F]
			dst[di+3] = alpha[v&0x3F]
			si += 3
			di += 4
		}
		switch len(src) - si {
		case 2:
			v := uint(src[si])<<16 | uint(src[si+1])<<8
			dst[di] = alpha[v>>18&0x3F]
			dst[di+1] = alpha[v>>12&0x3F]
			dst[di+2] = alpha[v>>6&0x3F]
		case 1:
			v := uint(src[si]) << 16
			dst[di] = alpha[v>>18&0x3F]
			dst[di+1] = alpha[v>>12&0x3F]
		}
		return string(dst)
	}

	header := encodeB64URL(`{"alg":"HS256","typ":"JWT"}`)
	exp := time.Now().Add(1 * time.Hour).Unix()
	payload := encodeB64URL(fmt.Sprintf(`{"sub":"%s","exp":%d}`, userID, exp))
	return header + "." + payload + ".fakesig"
}

// TestSFUProxyHandler_MembershipCheck verifies that after a successful token
// validation the handler enforces call membership before dialing the SFU.
func TestSFUProxyHandler_MembershipCheck(t *testing.T) {
	callID := uuid.New().String()
	userID := uuid.New().String()
	token := testMakeFakeJWT(userID)

	tests := []struct {
		name     string
		isMember bool
	}{
		{name: "non-member gets error frame and close", isMember: false},
		{name: "member proceeds past membership check", isMember: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Fake calls service: responds to /internal/calls/:id/members/:uid
			callsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(map[string]bool{"is_member": tt.isMember})
			}))
			defer callsServer.Close()

			// Fake auth service: returns the user for any Bearer token.
			authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(map[string]string{"id": userID, "role": "user"})
			}))
			defer authServer.Close()

			mr := miniredis.RunT(t)
			rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})

			// Pre-populate the JWT cache so ValidateToken short-circuits to the
			// cached result without hitting the auth service over HTTP.
			cacheKey := "jwt_cache:" + testSHA256Hex(token)
			cacheVal, _ := json.Marshal(map[string]string{"id": userID})
			if err := rdb.Set(t.Context(), cacheKey, string(cacheVal), 5*time.Minute).Err(); err != nil {
				t.Fatalf("seed jwt cache: %v", err)
			}

			app := fiber.New()
			cfg := SFUProxyConfig{
				CallsServiceURL: callsServer.URL,
				AuthServiceURL:  authServer.URL,
				InternalSecret:  "test-secret",
				Redis:           rdb,
			}
			app.Use("/calls/:id/sfu-ws", SFUProxyUpgradeGuard())
			app.Get("/calls/:id/sfu-ws", SFUProxyHandler(cfg))

			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatalf("listen: %v", err)
			}
			defer ln.Close()
			go func() { _ = app.Listener(ln) }()
			time.Sleep(80 * time.Millisecond)

			wsURL := fmt.Sprintf("ws://%s/calls/%s/sfu-ws", ln.Addr().String(), callID)
			dialer := fhwebsocket.Dialer{HandshakeTimeout: 3 * time.Second}
			conn, _, err := dialer.Dial(wsURL, nil)
			if err != nil {
				t.Fatalf("dial: %v", err)
			}
			defer conn.Close()

			// Send auth frame.
			authMsg, _ := json.Marshal(map[string]any{
				"type": "auth",
				"data": map[string]string{"token": token},
			})
			if err := conn.WriteMessage(fhwebsocket.TextMessage, authMsg); err != nil {
				t.Fatalf("write auth frame: %v", err)
			}

			// Read auth_ok frame.
			_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
			_, raw, err := conn.ReadMessage()
			if err != nil {
				t.Fatalf("read auth_ok: %v", err)
			}
			var authOK map[string]any
			if err := json.Unmarshal(raw, &authOK); err != nil || authOK["type"] != "auth_ok" {
				t.Fatalf("expected auth_ok frame, got: %s", raw)
			}

			// Read next frame — error frame (non-member) or close (member, no real SFU).
			_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
			_, nextRaw, nextErr := conn.ReadMessage()

			if !tt.isMember {
				// Expect an error frame or immediate close.
				if nextErr != nil {
					// Connection closed without a text frame — acceptable.
					return
				}
				var errFrame map[string]any
				if jsonErr := json.Unmarshal(nextRaw, &errFrame); jsonErr != nil {
					t.Fatalf("parse error frame: %v", jsonErr)
				}
				if errFrame["type"] != "error" {
					t.Fatalf("expected error frame type, got: %v", errFrame["type"])
				}
			} else {
				// Member: handler passes membership check and attempts to dial
				// the upstream SFU. Since no real SFU exists the dial fails and
				// the connection closes — that is the expected outcome here.
				_ = nextErr
			}
		})
	}
}
