// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

// Package httputil holds tiny shared helpers for HTTP edge work.
package httputil

import (
	"strings"

	"github.com/gofiber/fiber/v2"
)

// ClientIP returns the leftmost untrusted IP from the X-Forwarded-For
// chain that fiber's c.IP() exposes. With EnableTrustedProxyCheck and
// a ProxyHeader configured, fiber returns the raw header VALUE (the
// whole chain like "1.2.3.4, 5.6.7.8") rather than a parsed single
// IP. Downstream consumers — notably the auth service which inserts
// the IP into a postgres `inet` column — reject the comma-form with
// SQLSTATE 22P02. Strip everything after the first comma so the
// leftmost (real client) wins.
//
// Empty input returns empty. Single-IP input is unchanged. Whitespace
// around the leftmost IP is trimmed.
func ClientIP(c *fiber.Ctx) string {
	return Leftmost(c.IP())
}

// Leftmost is the string-only variant for callers that already have
// the IP as a value (e.g. read from a header in non-fiber code).
func Leftmost(ip string) string {
	if i := strings.IndexByte(ip, ','); i >= 0 {
		ip = ip[:i]
	}
	return strings.TrimSpace(ip)
}
