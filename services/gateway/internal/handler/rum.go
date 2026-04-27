package handler

import (
	"encoding/json"
	"log/slog"

	"github.com/gofiber/fiber/v2"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/mst-corp/orbit/pkg/metrics"
)

// RUMConfig wires the Web Vitals ingestion endpoint to the gateway's
// Prometheus registry. Histograms are sized for the canonical Web Vitals
// thresholds so the dashboard reads naturally (good / needs improvement /
// poor at standard breakpoints).
type RUMConfig struct {
	Logger   *slog.Logger
	Registry *metrics.Registry
}

// rumPayload describes the JSON body posted by web/src/util/webVitals.ts.
// All metric values are optional — the client sends whichever it has
// captured by the time the user backgrounds the tab.
type rumPayload struct {
	URL            string  `json:"url"`
	Effective      string  `json:"effectiveType,omitempty"` // navigator.connection.effectiveType
	DeviceMemoryGB float64 `json:"deviceMemory,omitempty"`
	IsPWA          bool    `json:"isPWA,omitempty"`
	Platform       string  `json:"platform,omitempty"` // ios | android | desktop | unknown
	Build          string  `json:"build,omitempty"`

	FCP        *float64 `json:"fcp,omitempty"`
	LCP        *float64 `json:"lcp,omitempty"`
	INP        *float64 `json:"inp,omitempty"`
	CLS        *float64 `json:"cls,omitempty"`
	TTFB       *float64 `json:"ttfb,omitempty"`
	LongTasks  *int     `json:"longTasks,omitempty"`
	MemoryMB   *float64 `json:"memoryMb,omitempty"`
	TapRecover *int     `json:"tapRecover,omitempty"` // synthetic clicks on Android
	TapNative  *int     `json:"tapNative,omitempty"`
}

// RUMHandler returns a Fiber handler that decodes a Web Vitals payload from
// the client and emits Prometheus observations + a slog line. This sits
// behind JWT, so we always have a known user (and the spammer surface is
// limited to authenticated users; an explicit small body limit prevents
// memory abuse).
func RUMHandler(cfg RUMConfig) fiber.Handler {
	if cfg.Registry == nil {
		// Fail-closed: refuse to mount without a registry rather than
		// silently dropping observations.
		panic("RUMHandler: nil Registry")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	// Web Vitals canonical thresholds (ms / unitless for CLS):
	//   FCP good <1.8s, poor >3s
	//   LCP good <2.5s, poor >4s
	//   INP good <200ms, poor >500ms
	//   TTFB good <0.8s, poor >1.8s
	//   CLS good <0.1, poor >0.25
	timingBuckets := []float64{50, 100, 200, 400, 800, 1500, 2500, 4000, 6000, 10000}
	clsBuckets := []float64{0.01, 0.05, 0.1, 0.15, 0.25, 0.4, 0.6, 1.0}
	memBuckets := []float64{16, 32, 64, 128, 256, 512, 1024, 2048}

	fcp := cfg.Registry.HistogramWithBuckets(
		"orbit_rum_fcp_ms", "First Contentful Paint, milliseconds, real users.",
		timingBuckets, "platform", "is_pwa")
	lcp := cfg.Registry.HistogramWithBuckets(
		"orbit_rum_lcp_ms", "Largest Contentful Paint, milliseconds, real users.",
		timingBuckets, "platform", "is_pwa")
	inp := cfg.Registry.HistogramWithBuckets(
		"orbit_rum_inp_ms", "Interaction to Next Paint, milliseconds, real users.",
		timingBuckets, "platform", "is_pwa")
	ttfb := cfg.Registry.HistogramWithBuckets(
		"orbit_rum_ttfb_ms", "Time to First Byte, milliseconds, real users.",
		timingBuckets, "platform", "is_pwa")
	cls := cfg.Registry.HistogramWithBuckets(
		"orbit_rum_cls", "Cumulative Layout Shift score, real users.",
		clsBuckets, "platform", "is_pwa")
	mem := cfg.Registry.HistogramWithBuckets(
		"orbit_rum_js_heap_mb", "Reported JS heap size, MB, when supported.",
		memBuckets, "platform", "is_pwa")
	longTasks := cfg.Registry.Counter(
		"orbit_rum_long_tasks_total", "Long tasks (>50ms) observed via PerformanceObserver.",
		"platform", "is_pwa")
	tapRecover := cfg.Registry.Counter(
		"orbit_rum_tap_recovery_total", "Synthesized clicks recovered after pointercancel on Android.",
		"platform")
	tapNative := cfg.Registry.Counter(
		"orbit_rum_tap_native_total", "Native clicks delivered without recovery.",
		"platform")
	beacons := cfg.Registry.Counter(
		"orbit_rum_beacons_total", "Total RUM beacons accepted from clients.",
		"platform", "is_pwa")

	return func(c *fiber.Ctx) error {
		body := c.Body()
		// Reject obviously oversized bodies — clients should batch one
		// envelope per tab visit, ~1KB. 32KB is a generous ceiling.
		if len(body) > 32*1024 {
			return fiber.ErrRequestEntityTooLarge
		}

		var payload rumPayload
		if err := json.Unmarshal(body, &payload); err != nil {
			return fiber.ErrBadRequest
		}

		platform := payload.Platform
		if platform == "" {
			platform = "unknown"
		}
		isPWA := "false"
		if payload.IsPWA {
			isPWA = "true"
		}
		labels := prometheus.Labels{"platform": platform, "is_pwa": isPWA}

		if payload.FCP != nil {
			fcp.With(labels).Observe(*payload.FCP)
		}
		if payload.LCP != nil {
			lcp.With(labels).Observe(*payload.LCP)
		}
		if payload.INP != nil {
			inp.With(labels).Observe(*payload.INP)
		}
		if payload.TTFB != nil {
			ttfb.With(labels).Observe(*payload.TTFB)
		}
		if payload.CLS != nil {
			cls.With(labels).Observe(*payload.CLS)
		}
		if payload.MemoryMB != nil {
			mem.With(labels).Observe(*payload.MemoryMB)
		}
		if payload.LongTasks != nil && *payload.LongTasks > 0 {
			longTasks.With(labels).Add(float64(*payload.LongTasks))
		}
		if payload.TapRecover != nil && *payload.TapRecover > 0 {
			tapRecover.With(prometheus.Labels{"platform": platform}).Add(float64(*payload.TapRecover))
		}
		if payload.TapNative != nil && *payload.TapNative > 0 {
			tapNative.With(prometheus.Labels{"platform": platform}).Add(float64(*payload.TapNative))
		}
		beacons.With(labels).Inc()

		cfg.Logger.Debug("rum beacon",
			"url", payload.URL,
			"platform", payload.Platform,
			"is_pwa", payload.IsPWA,
			"effective_type", payload.Effective,
			"build", payload.Build,
		)

		return c.SendStatus(fiber.StatusNoContent)
	}
}
