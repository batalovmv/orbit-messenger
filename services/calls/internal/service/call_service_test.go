// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package service

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestMintTURNCredentials_RFC7635HMAC(t *testing.T) {
	userID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	now := time.Unix(1_700_000_000, 0).UTC()
	secret := "shared-turn-secret"

	username, password := mintTURNCredentials(secret, userID, now)

	parts := strings.Split(username, ":")
	if len(parts) != 2 {
		t.Fatalf("expected username format <ts>:<userID>, got %q", username)
	}
	if parts[1] != userID.String() {
		t.Fatalf("expected userID suffix %q, got %q", userID.String(), parts[1])
	}

	wantTS := strconv.FormatInt(now.Add(2*time.Hour).Unix(), 10)
	if parts[0] != wantTS {
		t.Fatalf("expected expiry timestamp %q, got %q", wantTS, parts[0])
	}

	mac := hmac.New(sha1.New, []byte(secret))
	_, _ = mac.Write([]byte(username))
	wantPassword := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	if password != wantPassword {
		t.Fatalf("unexpected HMAC password: got %q want %q", password, wantPassword)
	}
}
