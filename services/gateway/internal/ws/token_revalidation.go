// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ws

import (
	"context"
	"time"
)

const closeCodePolicyViolation = 1008

type tokenRevalidationTarget interface {
	TokenExpiry() time.Time
	Revalidate(context.Context) error
	Close(code int, text string) error
	Done() <-chan struct{}
	Context() context.Context
}

func StartTokenRevalidation(target tokenRevalidationTarget) {
	ticker := time.NewTicker(pingInterval)
	go runTokenRevalidation(target, ticker.C, ticker.Stop, time.Now)
}

func runTokenRevalidation(
	target tokenRevalidationTarget,
	ticks <-chan time.Time,
	stop func(),
	now func() time.Time,
) {
	if stop != nil {
		defer stop()
	}

	for {
		select {
		case <-ticks:
			expiry := target.TokenExpiry()
			if !expiry.IsZero() && !now().Before(expiry) {
				_ = target.Close(closeCodePolicyViolation, "token expired")
				return
			}

			ctx, cancel := context.WithTimeout(target.Context(), authTimeout)
			err := target.Revalidate(ctx)
			cancel()
			if err != nil {
				_ = target.Close(closeCodePolicyViolation, "token revoked")
				return
			}
		case <-target.Done():
			return
		}
	}
}
