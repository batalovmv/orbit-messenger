// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package service

import (
	"context"
	"errors"
	"math/rand"
	"time"

	"github.com/mst-corp/orbit/pkg/metrics"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	auditWriteTimeout = 1 * time.Second
)

type auditRetryer interface {
	run(fn func(ctx context.Context) error) (attempts int, err error)
}

type sleepFunc func(time.Duration)
type jitterFunc func(maxDelta time.Duration) time.Duration

type boundedAuditRetryer struct {
	timeout  time.Duration
	backoffs []time.Duration
	sleep    sleepFunc
	jitter   jitterFunc
	metrics  *auditMetrics
}

func newAuditRetryer() *boundedAuditRetryer {
	src := rand.New(rand.NewSource(time.Now().UnixNano()))
	return &boundedAuditRetryer{
		timeout:  auditWriteTimeout,
		backoffs: []time.Duration{50 * time.Millisecond, 200 * time.Millisecond},
		sleep:    time.Sleep,
		jitter: func(maxDelta time.Duration) time.Duration {
			if maxDelta <= 0 {
				return 0
			}
			return time.Duration(src.Int63n(int64(maxDelta)*2+1)) - maxDelta
		},
	}
}

func (r *boundedAuditRetryer) run(fn func(ctx context.Context) error) (int, error) {
	attempts := len(r.backoffs) + 1
	for attempt := 1; attempt <= attempts; attempt++ {
		attemptCtx, cancel := context.WithTimeout(context.Background(), r.timeout)
		start := time.Now()
		err := fn(attemptCtx)
		duration := time.Since(start)
		cancel()

		if err == nil {
			r.observe("success", duration)
			return attempt, nil
		}

		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			r.observe("timeout", duration)
		} else {
			r.observe("transient_error", duration)
		}

		if attempt == attempts {
			return attempt, err
		}

		backoff := r.backoffs[attempt-1] + r.jitter(20*time.Millisecond)
		if backoff < 0 {
			backoff = 0
		}
		r.sleep(backoff)
	}
	return attempts, nil
}

func (r *boundedAuditRetryer) observe(result string, duration time.Duration) {
	if r == nil || r.metrics == nil {
		return
	}
	r.metrics.attempts.With(prometheus.Labels{"result": result}).Inc()
	r.metrics.duration.Observe(duration.Seconds())
}

type auditMetrics struct {
	attempts *prometheus.CounterVec
	duration prometheus.Observer
}

func newAuditMetrics(reg *metrics.Registry) *auditMetrics {
	if reg == nil {
		return nil
	}
	return &auditMetrics{
		attempts: reg.Counter(
			"media_audit_write_attempts_total",
			"Total media malware audit append attempts by result.",
			"result",
		),
		duration: reg.Histogram(
			"media_audit_write_duration_seconds",
			"Duration of media malware audit append attempts in seconds.",
		).WithLabelValues(),
	}
}
