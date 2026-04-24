// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package store

import (
	"testing"

	"github.com/mst-corp/orbit/pkg/crypto"
)

// decryptContent wraps encrypted plaintext and must round-trip it. Legacy
// plaintext rows (stored before at-rest encryption was enabled) fail
// base64-decode and are returned as-is; we guard that fallback explicitly.

func TestDecryptContent_RoundTrip(t *testing.T) {
	key := crypto.DeriveKey("test-key")
	s := &messageStore{atRest: key}

	plain := "hello мир 🚀"
	ct, err := crypto.Encrypt(plain, key)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	field := ct
	if err := s.decryptContent(&field); err != nil {
		t.Fatalf("decryptContent: %v", err)
	}
	if field != plain {
		t.Fatalf("round-trip mismatch: got %q, want %q", field, plain)
	}
}

func TestDecryptContent_NilAndEmpty(t *testing.T) {
	s := &messageStore{atRest: crypto.DeriveKey("k")}

	if err := s.decryptContent(nil); err != nil {
		t.Fatalf("nil input: %v", err)
	}
	empty := ""
	if err := s.decryptContent(&empty); err != nil {
		t.Fatalf("empty string: %v", err)
	}
	if empty != "" {
		t.Fatalf("expected empty string untouched, got %q", empty)
	}
}

func TestDecryptContent_LegacyPlaintextFallback(t *testing.T) {
	s := &messageStore{atRest: crypto.DeriveKey("k")}

	// Plaintext from before encryption was introduced — not valid base64 due
	// to spaces. decryptContent must NOT return an error and must preserve
	// the value (legacy rows stay readable).
	legacy := "hi there, this is plain text"
	field := legacy
	if err := s.decryptContent(&field); err != nil {
		t.Fatalf("legacy plaintext should not error: %v", err)
	}
	if field != legacy {
		t.Fatalf("legacy field mutated: got %q, want %q", field, legacy)
	}
}

func TestDecryptContent_WrongKeyFallback(t *testing.T) {
	k1 := crypto.DeriveKey("key-a")
	k2 := crypto.DeriveKey("key-b")

	ct, err := crypto.Encrypt("secret", k1)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	// A reader running with the wrong key hits the AEAD-auth-fail branch.
	// The fallback keeps the ciphertext as-is so it doesn't leak as plaintext
	// to the WS publish path in any downstream consumer — which is exactly
	// the failure mode we debugged earlier in this session.
	s := &messageStore{atRest: k2}
	field := ct
	if err := s.decryptContent(&field); err != nil {
		t.Fatalf("wrong key must not error (fallback): %v", err)
	}
	if field != ct {
		t.Fatalf("wrong-key fallback mutated field: %q != %q", field, ct)
	}
}

// TestEncryptDecryptContent_PreservesNil ensures encryptContent does not
// mutate its input pointer — callers rely on msg.Content staying plaintext
// in memory while a separate ciphertext pointer is passed to the INSERT.
func TestEncryptContent_DoesNotMutateInput(t *testing.T) {
	s := &messageStore{atRest: crypto.DeriveKey("k")}

	plain := "hello"
	input := plain
	ct, err := s.encryptContent(&input)
	if err != nil {
		t.Fatalf("encryptContent: %v", err)
	}
	if input != plain {
		t.Fatalf("encryptContent mutated input: %q != %q", input, plain)
	}
	if ct == nil || *ct == plain {
		t.Fatalf("encryptContent returned plaintext, not ciphertext: %v", ct)
	}
}

func TestEncryptContent_NilStaysNil(t *testing.T) {
	s := &messageStore{atRest: crypto.DeriveKey("k")}
	out, err := s.encryptContent(nil)
	if err != nil {
		t.Fatalf("encryptContent(nil): %v", err)
	}
	if out != nil {
		t.Fatalf("expected nil pointer for nil input, got %v", out)
	}
}
