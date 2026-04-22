package service

import (
	"strings"
	"testing"
)

func TestNotifCacheKey_IncludesUserID(t *testing.T) {
	text := "hello world"
	k1 := notifCacheKey("user-1", "sender-1", text)
	k2 := notifCacheKey("user-2", "sender-1", text)

	if k1 == k2 {
		t.Fatal("cache keys must differ when userID differs")
	}
}

func TestNotifCacheKey_RuneSafeTruncation(t *testing.T) {
	// 120 Cyrillic runes = 240 bytes UTF-8, should truncate to 100 runes without panic.
	text := strings.Repeat("Ю", 120)
	key := notifCacheKey("u1", "s1", text)

	if !strings.HasPrefix(key, "notif:classify:") {
		t.Fatalf("unexpected key prefix: %s", key)
	}

	// Same 100-rune prefix must produce the same key.
	key2 := notifCacheKey("u1", "s1", strings.Repeat("Ю", 100))
	if key != key2 {
		t.Fatal("truncated key should match exact 100-rune input key")
	}
}

func TestNotifCacheKey_ShortText(t *testing.T) {
	short := "hi"
	key := notifCacheKey("u1", "s1", short)
	if !strings.HasPrefix(key, "notif:classify:") {
		t.Fatalf("unexpected key prefix: %s", key)
	}
}
