// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package middleware

import (
	"log/slog"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
)

// AppVersionConfig wires the X-App-Version handshake. Two independent knobs:
//
//   - LatestVersion: when set, every response carries `X-App-Latest-Version`
//     so clients can detect a deploy on the very next API call instead of
//     waiting for the periodic version.txt poll. No-op when empty.
//   - MinVersion: when set, requests carrying `X-App-Version` lower than this
//     are rejected with 426 Upgrade Required + `X-Required-Version`. When
//     empty (or the client omits the header), the request passes through —
//     this is opt-in and defaults to off so a misconfigured deploy can't
//     lock everyone out.
//
// SkipPathPrefixes are paths the min-version gate must NOT block — typically
// /auth/* (so users can log in to receive a fresh token) and version-check
// endpoints. Defaults are applied when the slice is nil.
type AppVersionConfig struct {
	LatestVersion    string
	MinVersion       string
	SkipPathPrefixes []string
}

// AppVersionMiddleware enforces the X-App-Version contract above. It must be
// mounted BEFORE rate limiting so a forced-upgrade response does not consume
// the client's per-IP quota; ordering vs. JWT is irrelevant because the
// version check needs neither auth nor user identity.
func AppVersionMiddleware(cfg AppVersionConfig) fiber.Handler {
	skip := cfg.SkipPathPrefixes
	if skip == nil {
		// /auth/* — login/refresh must work even from a too-old client so
		// the user can get a fresh token after the page reloads to the new
		// build. /public/system/config — public maintenance/version polling
		// must keep working when the client is below MinVersion.
		skip = []string{"/api/v1/auth", "/api/v1/public"}
	}

	// Validate MinVersion at startup. A malformed env value (typo, leading
	// "v", prerelease tag) would otherwise silently disable enforcement
	// (well-formed clients sort above the malformed threshold) — operator
	// thinks the gate is on when it isn't. Log loudly so the warning shows
	// up in the deploy log; do not panic so a bad env doesn't take the
	// gateway down.
	if cfg.MinVersion != "" {
		parts := parseSemver(stripPrerelease(cfg.MinVersion))
		if parts[0] < 0 || parts[1] < 0 || parts[2] < 0 {
			slog.Warn("APP_MIN_VERSION is malformed — version gate effectively disabled",
				"min_version", cfg.MinVersion,
				"hint", "expected MAJOR.MINOR or MAJOR.MINOR.PATCH (e.g. 12.0.21)")
		}
	}

	return func(c *fiber.Ctx) error {
		// Skip OPTIONS preflight — defence in depth. CORS middleware (mounted
		// upstream) already short-circuits matched-origin preflights with 204
		// and never calls Next(), but if CORS is ever reordered or replaced,
		// we don't want preflight requests being 426'd here.
		if c.Method() == fiber.MethodOptions {
			return c.Next()
		}

		if cfg.LatestVersion != "" {
			c.Response().Header.Set("X-App-Latest-Version", cfg.LatestVersion)
		}

		if cfg.MinVersion == "" {
			return c.Next()
		}
		path := c.Path()
		for _, p := range skip {
			if strings.HasPrefix(path, p) {
				return c.Next()
			}
		}
		clientVersion := strings.TrimSpace(c.Get("X-App-Version"))
		if clientVersion == "" {
			// Older clients predate this header. Allowing them through is the
			// safer default: a hard 426 would brick them with no way to
			// receive the new build. The response header above still tells
			// them an update is available; periodic version.txt polling and
			// the SW stale-chunk handler will eventually trigger reload.
			return c.Next()
		}
		if compareSemver(clientVersion, cfg.MinVersion) < 0 {
			c.Response().Header.Set("X-Required-Version", cfg.MinVersion)
			// Retry-After hints clients to back off rather than hammer the
			// gateway during a forced-upgrade window. 30s is enough for the
			// page to reload and pick up the new bundle.
			c.Response().Header.Set("Retry-After", "30")
			return c.Status(fiber.StatusUpgradeRequired).JSON(fiber.Map{
				"error":            "upgrade_required",
				"message":          "Client version is too old. Please reload to upgrade.",
				"required_version": cfg.MinVersion,
			})
		}
		return c.Next()
	}
}

// compareSemver compares two version strings of the form "MAJOR.MINOR" or
// "MAJOR.MINOR.PATCH" (with an optional prerelease/build suffix that is
// stripped before comparison). Missing components are treated as 0 and any
// non-numeric component is treated as -1 so unparseable inputs sort below
// well-formed inputs.
//
// Returns:
//
//	negative if a < b
//	0        if a == b
//	positive if a > b
func compareSemver(a, b string) int {
	pa := parseSemver(stripPrerelease(a))
	pb := parseSemver(stripPrerelease(b))
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			return pa[i] - pb[i]
		}
	}
	return 0
}

// stripPrerelease removes everything from the first '-' or '+' onward so
// "12.0.21-rc1" and "12.0.21+ci.42" both compare against "12.0.21" as
// equal. Treating an RC build of the current version as equal-to-current
// is intentional: we'd rather let an internal RC bypass MinVersion than
// 426 every dev/QA build that hasn't been released yet.
func stripPrerelease(v string) string {
	for i, c := range v {
		if c == '-' || c == '+' {
			return v[:i]
		}
	}
	return v
}

// parseSemver returns a 3-element fixed array; missing components are 0,
// non-numeric components are -1.
func parseSemver(v string) [3]int {
	var out [3]int
	parts := strings.Split(v, ".")
	for i := 0; i < 3; i++ {
		if i >= len(parts) {
			out[i] = 0
			continue
		}
		n, err := strconv.Atoi(strings.TrimSpace(parts[i]))
		if err != nil {
			out[i] = -1
			continue
		}
		out[i] = n
	}
	return out
}
