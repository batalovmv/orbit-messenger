// Package metrics wraps Prometheus primitives with the conventions we use
// across Orbit services so every service exposes the same shape of data
// (same label names, same histogram buckets) without copy-pasting the
// registration dance. Each service instantiates its own `*Registry` at
// startup, registers its counters/histograms/gauges through that, and
// mounts the `Handler()` on an internal-token-gated /metrics route.
package metrics

import (
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/valyala/fasthttp/fasthttpadaptor"
)

// Registry is an isolated Prometheus registry with service-wide default
// labels applied. One registry per service; callers should pass it to
// every component that exports metrics (middleware, background workers,
// stores, etc).
type Registry struct {
	reg     *prometheus.Registry
	service string
}

// New creates a Registry scoped to `service`. The standard Go runtime
// collector and the process collector are registered automatically so
// every service exposes goroutine count, heap usage, CPU, etc.
func New(service string) *Registry {
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector())
	reg.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	return &Registry{reg: reg, service: service}
}

// Prometheus exposes the underlying registry for callers that need to
// register custom collectors directly.
func (r *Registry) Prometheus() *prometheus.Registry { return r.reg }

// Service returns the service name this registry was created with.
func (r *Registry) Service() string { return r.service }

// Counter registers a labeled counter on this registry.
func (r *Registry) Counter(name, help string, labels ...string) *prometheus.CounterVec {
	c := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:        name,
		Help:        help,
		ConstLabels: prometheus.Labels{"service": r.service},
	}, labels)
	r.reg.MustRegister(c)
	return c
}

// Gauge registers a labeled gauge on this registry.
func (r *Registry) Gauge(name, help string, labels ...string) *prometheus.GaugeVec {
	g := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name:        name,
		Help:        help,
		ConstLabels: prometheus.Labels{"service": r.service},
	}, labels)
	r.reg.MustRegister(g)
	return g
}

// Histogram registers a labeled histogram with latency-friendly buckets
// tuned for HTTP request durations (1ms..10s). Callers that need custom
// buckets should use HistogramWithBuckets.
func (r *Registry) Histogram(name, help string, labels ...string) *prometheus.HistogramVec {
	return r.HistogramWithBuckets(name, help, latencyBuckets, labels...)
}

// HistogramWithBuckets is Histogram with explicit bucket boundaries.
func (r *Registry) HistogramWithBuckets(name, help string, buckets []float64, labels ...string) *prometheus.HistogramVec {
	h := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:        name,
		Help:        help,
		Buckets:     buckets,
		ConstLabels: prometheus.Labels{"service": r.service},
	}, labels)
	r.reg.MustRegister(h)
	return h
}

// latencyBuckets covers the useful range for HTTP inside a cluster:
// 1ms..10s, denser at the low end where SLO math happens.
var latencyBuckets = []float64{
	0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10,
}

// Handler returns a Fiber handler that serves the Prometheus exposition
// format for this registry. Mount under a route guarded by the internal
// token so scrape requests from Prometheus are not exposed publicly.
func (r *Registry) Handler() fiber.Handler {
	h := promhttp.HandlerFor(r.reg, promhttp.HandlerOpts{Registry: r.reg})
	adapted := fasthttpadaptor.NewFastHTTPHandler(h)
	return func(c *fiber.Ctx) error {
		adapted(c.Context())
		return nil
	}
}

// HTTPMiddleware records request count and latency for every handled
// request. The route label uses the matched Fiber route pattern (e.g.
// /api/v1/chats/:id) rather than the raw path so cardinality stays
// bounded regardless of how many chat IDs get hit.
func (r *Registry) HTTPMiddleware() fiber.Handler {
	requests := r.Counter(
		"orbit_http_requests_total",
		"Total HTTP requests handled by the service.",
		"method", "route", "status",
	)
	duration := r.Histogram(
		"orbit_http_request_duration_seconds",
		"End-to-end HTTP request duration, seconds.",
		"method", "route", "status",
	)

	return func(c *fiber.Ctx) error {
		start := time.Now()
		err := c.Next()
		status := strconv.Itoa(c.Response().StatusCode())
		// Route() is the matched pattern when a handler ran; for 404s
		// Fiber leaves it blank and we fall back to a literal "unmatched"
		// so noise from bots/scanners doesn't blow up cardinality.
		route := c.Route().Path
		if route == "" {
			route = "unmatched"
		}
		labels := prometheus.Labels{
			"method": strings.Clone(c.Method()),
			"route":  strings.Clone(route),
			"status": status,
		}
		requests.With(labels).Inc()
		duration.With(labels).Observe(time.Since(start).Seconds())
		return err
	}
}
