// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package service

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/mst-corp/orbit/pkg/metrics"
)

// ClassifierMetrics groups Prometheus instruments for the notification
// classifier so we can observe quality (rule vs AI mix, feedback overrides)
// and cost (cumulative cents, AI call latency) on the same registry the
// rest of the AI service exposes via /metrics.
//
// Nil-safe: every method tolerates a nil receiver so the classifier path
// keeps working in tests and in environments where metrics aren't wired
// (e.g. miniredis-based unit tests).
type ClassifierMetrics struct {
	classifyTotal    *prometheus.CounterVec   // labels: source, priority
	classifyDuration *prometheus.HistogramVec // labels: source
	costCents        prometheus.Counter
	feedback         *prometheus.CounterVec // labels: result
}

// NewClassifierMetrics registers the classifier instruments on the given
// registry. Returns nil if reg is nil so callers don't have to special-case
// "metrics disabled" environments.
func NewClassifierMetrics(reg *metrics.Registry) *ClassifierMetrics {
	if reg == nil {
		return nil
	}
	// Classifier latency lives in the sub-second range — rules return in
	// ~µs, Claude Haiku in 100-500ms. Tighten the bucket grid versus the
	// default HTTP histogram so we can distinguish "cache hit" from
	// "rule" from "ai" on a Grafana panel.
	classifyBuckets := []float64{
		0.0005, 0.001, 0.005, 0.01, 0.05, 0.1, 0.25, 0.5, 1, 2,
	}
	return &ClassifierMetrics{
		classifyTotal: reg.Counter(
			"orbit_notification_classify_total",
			"Notification classifications produced, labelled by source (rule/ai/cache) and resulting priority.",
			"source", "priority",
		),
		classifyDuration: reg.HistogramWithBuckets(
			"orbit_notification_classify_duration_seconds",
			"Classifier end-to-end latency by source, seconds.",
			classifyBuckets,
			"source",
		),
		costCents: reg.Counter(
			"orbit_notification_classify_cost_cents_total",
			"Cumulative classifier cost in 1/100ths of a US cent (cost_cents column unit).",
		).WithLabelValues(),
		feedback: reg.Counter(
			"orbit_notification_classify_feedback_total",
			"User feedback on classifier output, labelled by whether the user override matched the AI classification.",
			"result",
		),
	}
}

// normalizeSource clamps the source label to the known set so a misconfigured
// caller (or a model that ever leaks a non-validated string into a priority
// field) cannot inflate label cardinality.
func normalizeSource(s string) string {
	switch s {
	case "cache", "rule", "ai":
		return s
	default:
		return "unknown"
	}
}

func normalizePriority(p string) string {
	switch p {
	case "urgent", "important", "normal", "low":
		return p
	default:
		return "unknown"
	}
}

func (m *ClassifierMetrics) recordClassify(source, priority string) {
	if m == nil {
		return
	}
	m.classifyTotal.WithLabelValues(normalizeSource(source), normalizePriority(priority)).Inc()
}

func (m *ClassifierMetrics) recordDuration(source string, seconds float64) {
	if m == nil {
		return
	}
	m.classifyDuration.WithLabelValues(normalizeSource(source)).Observe(seconds)
}

func (m *ClassifierMetrics) recordCost(costCents int) {
	if m == nil || costCents <= 0 {
		return
	}
	m.costCents.Add(float64(costCents))
}

// RecordFeedback maps a feedback event to the matched/overridden counter.
// Exported so the handler layer can call it without reaching into AIService.
// Defensive: even if a caller bypasses the handler-level priority validation
// the result label remains binary {matched, overridden}.
func (m *ClassifierMetrics) RecordFeedback(classified, override string) {
	if m == nil {
		return
	}
	result := "matched"
	if normalizePriority(classified) != normalizePriority(override) {
		result = "overridden"
	}
	m.feedback.WithLabelValues(result).Inc()
}
