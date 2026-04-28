// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package store

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// TestCanAccessMediaQuery_RequiresChatMembership pins the SQL shape so the
// IDOR regression that motivated this fix can't return without breaking a
// test. The audit (audits/MERGED-AUDIT-FINAL.md, CRITICAL #2) found a query
// that granted access whenever the media was attached to ANY message,
// regardless of whether the caller belonged to that chat. The fix joins
// through chat_members; this test makes the join non-optional.
func TestCanAccessMediaQuery_RequiresChatMembership(t *testing.T) {
	q := strings.ToLower(canAccessMediaQuery)

	for _, want := range []string{
		"chat_members",
		"messages",
		"message_media",
		"cm.user_id = $2",
	} {
		if !strings.Contains(q, strings.ToLower(want)) {
			t.Fatalf("CanAccess query lost %q — IDOR regression?\nquery:\n%s", want, canAccessMediaQuery)
		}
	}

	// The pre-fix query had a bare `EXISTS(SELECT 1 FROM message_media WHERE
	// media_id = $1)` with no user predicate. Forbid that exact shape.
	bad := "from message_media where media_id = $1)"
	if strings.Contains(strings.Join(strings.Fields(q), " "), bad) {
		t.Fatalf("CanAccess query reintroduced unbounded message_media existence check:\n%s", canAccessMediaQuery)
	}
}

// TestCanAccess_RejectsNilUser guards the defence-in-depth check: an
// unauthenticated caller (uuid.Nil) must never get a positive answer, even
// before the SQL runs. The pool is intentionally nil — if the guard fails,
// the test will panic rather than silently pass.
func TestCanAccess_RejectsNilUser(t *testing.T) {
	s := &MediaStore{pool: nil}
	ok, err := s.CanAccess(context.Background(), uuid.New(), uuid.Nil)
	if err != nil {
		t.Fatalf("nil user must not error, got %v", err)
	}
	if ok {
		t.Fatal("nil user must not be granted access")
	}
}
