// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package storage

import (
	"testing"
)

// TestIsLocalEndpoint locks the heuristic that gates R2 public-policy apply.
// This is the actual security primitive behind CRITICAL #3 (audit 2026-04-26):
// EnsureBucket refuses to apply a bucket-wide public-read policy unless the
// endpoint is clearly a developer/local one. Misclassifying a real R2 / S3
// endpoint as local would re-open the audit finding (private attachments
// going public). Misclassifying MinIO as prod would break local dev only.
func TestIsLocalEndpoint(t *testing.T) {
	tests := []struct {
		endpoint string
		want     bool
	}{
		// Local / dev — must pass.
		{"http://localhost:9000", true},
		{"http://127.0.0.1:9000", true},
		{"http://[::1]:9000", true},
		{"http://minio:9000", true},
		{"http://host.docker.internal:9000", true},
		{"localhost:9000", true},
		{"minio:9000", true},

		// Production-shaped — must fail closed.
		{"https://abc123.r2.cloudflarestorage.com", false},
		{"https://s3.amazonaws.com", false},
		{"https://s3.eu-central-1.amazonaws.com", false},
		{"https://orbit-media.example.com", false},
		{"https://localhost.evil.com", false}, // suffix attack — must fail
		{"", false},                           // empty endpoint must not pass
	}

	for _, tt := range tests {
		t.Run(tt.endpoint, func(t *testing.T) {
			got := isLocalEndpoint(tt.endpoint)
			if got != tt.want {
				t.Fatalf("isLocalEndpoint(%q) = %v, want %v", tt.endpoint, got, tt.want)
			}
		})
	}
}
