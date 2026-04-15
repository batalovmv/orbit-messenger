package handler

import (
	"bufio"
	"encoding/json"
	"fmt"

	"github.com/gofiber/fiber/v2"
	"github.com/valyala/fasthttp"

	"github.com/mst-corp/orbit/services/ai/internal/client"
)

// sseHeaders configures the response for Server-Sent Events. Set before any
// streaming writes, and includes `X-Accel-Buffering: no` so nginx (and any
// other reverse proxy in the path) does NOT buffer the stream — otherwise
// users would wait for the whole response before seeing any text.
//
// The gateway's Fiber proxy must also forward the chunked transfer encoding
// without buffering. See services/gateway for the proxy wiring.
func sseHeaders(c *fiber.Ctx) {
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")
	c.Set("X-Accel-Buffering", "no")
}

// streamClaudeEvents pipes events from the AI service's StreamEvent channel
// into an SSE response. Frontend parses the format:
//
//	data: {"type":"delta","text":"Hello"}\n\n
//	data: {"type":"done","input_tokens":42,"output_tokens":87}\n\n
//	data: {"type":"error","message":"..."}\n\n
//	data: [DONE]\n\n
//
// The `[DONE]` sentinel is a Saturn-wide convention so the frontend
// AsyncGenerator knows when to stop reading.
func streamClaudeEvents(c *fiber.Ctx, events <-chan client.StreamEvent) error {
	sseHeaders(c)

	c.Context().SetBodyStreamWriter(fasthttp.StreamWriter(func(w *bufio.Writer) {
		for event := range events {
			payload, err := encodeStreamEvent(event)
			if err != nil {
				// Fall back to a synthetic error frame so the client
				// always sees something rather than an abrupt EOF.
				_, _ = fmt.Fprintf(w, "data: {\"type\":\"error\",\"message\":\"encode failed\"}\n\n")
				_ = w.Flush()
				break
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
				return
			}
			if err := w.Flush(); err != nil {
				return
			}
			if event.Done != nil || event.Err != nil {
				break
			}
		}
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
		_ = w.Flush()
	}))
	return nil
}

func encodeStreamEvent(event client.StreamEvent) ([]byte, error) {
	switch {
	case event.Err != nil:
		return json.Marshal(map[string]any{
			"type":    "error",
			"message": event.Err.Error(),
		})
	case event.Done != nil:
		return json.Marshal(map[string]any{
			"type":          "done",
			"input_tokens":  event.Done.InputTokens,
			"output_tokens": event.Done.OutputTokens,
		})
	default:
		return json.Marshal(map[string]any{
			"type": "delta",
			"text": event.Delta,
		})
	}
}
