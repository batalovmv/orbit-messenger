// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package botapi

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestFileIDCodec_RoundTrip(t *testing.T) {
	codec := NewFileIDCodec([]byte("test-secret-32-bytes-aaaaaaaaaaaaaaaa"))
	media := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	chat := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	bot := uuid.MustParse("33333333-3333-3333-3333-333333333333")

	id := codec.Encode(media, chat, bot)
	if id == "" {
		t.Fatal("empty file_id")
	}
	gotMedia, gotChat, err := codec.Decode(id, bot)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if gotMedia != media || gotChat != chat {
		t.Fatalf("round trip lost data: media=%v chat=%v", gotMedia, gotChat)
	}
}

func TestFileIDCodec_Deterministic(t *testing.T) {
	codec := NewFileIDCodec([]byte("test-secret"))
	media := uuid.New()
	chat := uuid.New()
	bot := uuid.New()
	a := codec.Encode(media, chat, bot)
	b := codec.Encode(media, chat, bot)
	if a != b {
		t.Fatalf("expected deterministic file_id, got %q vs %q", a, b)
	}
}

func TestFileIDCodec_DifferentBotFailsVerification(t *testing.T) {
	codec := NewFileIDCodec([]byte("test-secret"))
	media := uuid.New()
	chat := uuid.New()
	botA := uuid.New()
	botB := uuid.New()
	id := codec.Encode(media, chat, botA)
	if _, _, err := codec.Decode(id, botB); err == nil {
		t.Fatal("expected error decoding bot A's file_id with bot B")
	}
}

func TestFileIDCodec_TamperedSignatureRejected(t *testing.T) {
	codec := NewFileIDCodec([]byte("test-secret"))
	media := uuid.New()
	chat := uuid.New()
	bot := uuid.New()
	id := codec.Encode(media, chat, bot)
	// flip a char in the signature half
	if len(id) < 4 {
		t.Fatal("file_id too short for tampering test")
	}
	tampered := id[:len(id)-1] + flipChar(id[len(id)-1])
	if tampered == id {
		t.Skip("could not tamper")
	}
	if _, _, err := codec.Decode(tampered, bot); err == nil {
		t.Fatal("expected error for tampered file_id")
	}
}

func TestFileIDCodec_Malformed(t *testing.T) {
	codec := NewFileIDCodec([]byte("test-secret"))
	bot := uuid.New()
	cases := []string{"", "...", "not-base64!", strings.Repeat("a", 100)}
	for _, in := range cases {
		if _, _, err := codec.Decode(in, bot); err == nil {
			t.Fatalf("expected error for %q", in)
		}
	}
}

func TestFileIDCodec_UniqueIDIsBotIndependent(t *testing.T) {
	codec := NewFileIDCodec([]byte("test-secret"))
	media := uuid.New()
	a := codec.EncodeUnique(media)
	b := codec.EncodeUnique(media)
	if a != b || a == "" {
		t.Fatalf("file_unique_id not stable: %q vs %q", a, b)
	}
}

func flipChar(c byte) string {
	if c == 'a' {
		return "b"
	}
	return "a"
}
