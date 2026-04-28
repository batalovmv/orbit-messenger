// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package store

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// TestCanAccess_RejectsNilUser locks the defence-in-depth uuid.Nil short-circuit:
// callers that reach the store without an authenticated user must always get
// false, never run the SQL. Pool is intentionally nil — if the guard regresses
// the test panics on the QueryRow rather than silently passing.
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
