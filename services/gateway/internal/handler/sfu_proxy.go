package handler

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	fhwebsocket "github.com/fasthttp/websocket"
	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"

	gatewayws "github.com/mst-corp/orbit/services/gateway/internal/ws"
)

const (
	sfuAuthFrameTimeout = 10 * time.Second
	sfuTokenValidateTTL = 10 * time.Second
)

// SFUProxyConfig is the minimum the WS proxy needs from the gateway config.
type SFUProxyConfig struct {
	// CallsServiceURL is the http(s):// base of the calls service. We rewrite
	// it to ws:// or wss:// for the upstream WebSocket dial.
	CallsServiceURL string
	// AuthServiceURL is used to validate the access token from the first WS
	// frame (browsers can't set Authorization headers on WebSocket).
	AuthServiceURL string
	InternalSecret string
	Redis          *redis.Client
}

// authFrame is the JSON envelope the client sends as its first message after
// the WebSocket upgrade. The browser cannot set Authorization headers, so we
// reuse the same auth-over-frame pattern as the main /api/v1/ws endpoint.
type authFrame struct {
	Type string `json:"type"`
	Data struct {
		Token string `json:"token"`
	} `json:"data"`
}

type sfuAuthSession struct {
	client       *websocket.Conn
	done         chan struct{}
	tokenExpiry  time.Time
	tokenHash    *string
	revalidateFn func(context.Context) error
	closeOnce    sync.Once
}

func (s *sfuAuthSession) TokenExpiry() time.Time {
	return s.tokenExpiry
}

func (s *sfuAuthSession) Revalidate(ctx context.Context) error {
	if s.revalidateFn == nil {
		return nil
	}
	return s.revalidateFn(ctx)
}

func (s *sfuAuthSession) Done() <-chan struct{} {
	return s.done
}

func (s *sfuAuthSession) Close(code int, text string) error {
	var closeErr error
	s.closeOnce.Do(func() {
		_ = s.client.SetWriteDeadline(time.Now().Add(5 * time.Second))
		_ = s.client.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(code, text))
		closeErr = s.client.Close()
	})
	return closeErr
}

// SFUProxyHandler returns a Fiber WebSocket handler that bidirectionally
// proxies a client WebSocket to the calls service SFU endpoint.
//
// Auth flow:
//  1. Client opens WS without any token (browser limitation).
//  2. Client immediately sends {type:"auth", data:{token:"..."}}.
//  3. We validate the token via the auth service (using the same cache +
//     blacklist path as the main WS in internal/ws/handler.go).
//  4. We dial the calls service with X-User-ID + X-Internal-Token headers
//     and start bidirectional frame copying.
//
// Why a bespoke proxy: fasthttp's HTTP reverse proxy does not handle WebSocket
// upgrades. Doing the dial ourselves keeps full control over headers and lets
// us terminate the connection cleanly on either side.
func SFUProxyHandler(cfg SFUProxyConfig) fiber.Handler {
	dialer := &fhwebsocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
		// We dial internal infrastructure on the docker network — TLS is
		// not used in production for service-to-service traffic.
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
	}
	authClient := &http.Client{Timeout: sfuAuthFrameTimeout}
	upstreamBase := toWSScheme(cfg.CallsServiceURL)

	return websocket.New(func(client *websocket.Conn) {
		callID := client.Params("id")
		if callID == "" {
			_ = client.Close()
			return
		}

		// Step 1: read the auth frame within sfuAuthFrameTimeout. If the
		// client never sends one we drop the connection so a misbehaving
		// peer cannot pin a goroutine forever.
		_ = client.SetReadDeadline(time.Now().Add(sfuAuthFrameTimeout))
		_, raw, err := client.ReadMessage()
		if err != nil {
			slog.Warn("sfu proxy: no auth frame", "error", err)
			_ = client.Close()
			return
		}
		var af authFrame
		if err := json.Unmarshal(raw, &af); err != nil || af.Type != "auth" || af.Data.Token == "" {
			_ = client.WriteJSON(map[string]any{"type": "error", "data": map[string]string{"message": "expected auth frame"}})
			_ = client.Close()
			return
		}

		// Step 2: validate the token. ValidateToken handles both the
		// jwt_blacklist check and the jwt_cache short-circuit identical to
		// the main /api/v1/ws endpoint.
		ctx, cancel := context.WithTimeout(context.Background(), sfuTokenValidateTTL)
		tokenInfo, err := gatewayws.ValidateToken(ctx, authClient, cfg.Redis, cfg.AuthServiceURL, af.Data.Token)
		cancel()
		if err != nil || tokenInfo == nil || tokenInfo.UserID == "" {
			slog.Warn("sfu proxy: token rejected", "error", err)
			_ = client.WriteJSON(map[string]any{"type": "error", "data": map[string]string{"message": "invalid token"}})
			_ = client.Close()
			return
		}
		// Auth ok — clear the read deadline so subsequent ReadMessage calls
		// (the proxy loop) can block on real client traffic.
		_ = client.SetReadDeadline(time.Time{})
		_ = client.WriteJSON(map[string]any{"type": "auth_ok", "data": map[string]string{}})

		session := &sfuAuthSession{
			client:      client,
			done:        make(chan struct{}),
			tokenExpiry: tokenInfo.ExpiresAt,
			tokenHash:   &tokenInfo.TokenHash,
			revalidateFn: func(ctx context.Context) error {
				return gatewayws.RevalidateToken(ctx, authClient, cfg.Redis, cfg.AuthServiceURL, af.Data.Token, tokenInfo.TokenHash)
			},
		}
		defer close(session.done)
		gatewayws.StartTokenRevalidation(session)

		// Step 3: dial the calls service with the resolved user id.
		upstreamURL := upstreamBase + "/calls/" + callID + "/sfu-ws"
		hdr := http.Header{}
		hdr.Set("X-User-ID", tokenInfo.UserID)
		hdr.Set("X-Internal-Token", cfg.InternalSecret)

		upstream, _, dialErr := dialer.Dial(upstreamURL, hdr)
		if dialErr != nil {
			slog.Error("sfu proxy: upstream dial failed", "error", dialErr, "url", upstreamURL)
			_ = client.WriteMessage(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseInternalServerErr, "upstream unavailable"))
			_ = client.Close()
			return
		}

		var once sync.Once
		closeBoth := func() {
			once.Do(func() {
				_ = client.Close()
				_ = upstream.Close()
			})
		}
		defer closeBoth()

		// Step 4: bidirectional pump.
		// upstream → client
		go func() {
			defer closeBoth()
			for {
				mt, payload, err := upstream.ReadMessage()
				if err != nil {
					if !isExpectedClose(err) {
						slog.Info("sfu proxy: upstream read end", "error", err)
					}
					return
				}
				if err := client.WriteMessage(mt, payload); err != nil {
					return
				}
			}
		}()

		// client → upstream (this goroutine)
		for {
			mt, payload, err := client.ReadMessage()
			if err != nil {
				if !isExpectedClose(err) {
					slog.Info("sfu proxy: client read end", "error", err)
				}
				return
			}
			if err := upstream.WriteMessage(mt, payload); err != nil {
				return
			}
		}
	})
}

// SFUProxyUpgradeGuard rejects non-WebSocket requests early so the proxy
// handler is only invoked when an actual upgrade is in flight. The route is
// otherwise unauthenticated — auth happens via the first WS frame inside
// SFUProxyHandler. Rate limiting is applied at the route registration site.
func SFUProxyUpgradeGuard() fiber.Handler {
	return func(c *fiber.Ctx) error {
		if !websocket.IsWebSocketUpgrade(c) {
			return fiber.ErrUpgradeRequired
		}
		return c.Next()
	}
}

func toWSScheme(httpURL string) string {
	switch {
	case strings.HasPrefix(httpURL, "https://"):
		return "wss://" + strings.TrimPrefix(httpURL, "https://")
	case strings.HasPrefix(httpURL, "http://"):
		return "ws://" + strings.TrimPrefix(httpURL, "http://")
	default:
		return httpURL
	}
}

func isExpectedClose(err error) bool {
	if err == nil {
		return true
	}
	var ce *fhwebsocket.CloseError
	if errors.As(err, &ce) {
		switch ce.Code {
		case fhwebsocket.CloseNormalClosure,
			fhwebsocket.CloseGoingAway,
			fhwebsocket.CloseNoStatusReceived,
			fhwebsocket.CloseAbnormalClosure:
			return true
		}
	}
	if strings.Contains(err.Error(), "websocket: close") {
		return true
	}
	return false
}
