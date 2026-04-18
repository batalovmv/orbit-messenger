package crypto

import "testing"

func TestDecryptContentField_RoundTrip(t *testing.T) {
	key := DeriveKey("test-key")
	ct, err := Encrypt("hello world", key)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	field := ct
	DecryptContentField(&field, key)
	if field != "hello world" {
		t.Fatalf("want plaintext, got %q", field)
	}
}

func TestDecryptContentField_NilAndEmpty(t *testing.T) {
	key := DeriveKey("test-key")
	DecryptContentField(nil, key)

	empty := ""
	DecryptContentField(&empty, key)
	if empty != "" {
		t.Fatalf("want empty, got %q", empty)
	}
}

func TestDecryptContentField_LegacyPlaintextFallback(t *testing.T) {
	key := DeriveKey("test-key")
	field := "legacy plain text written before at-rest encryption"
	DecryptContentField(&field, key)
	if field != "legacy plain text written before at-rest encryption" {
		t.Fatalf("legacy plaintext must be preserved, got %q", field)
	}
}

func TestDecryptContentField_WrongKeyFallback(t *testing.T) {
	goodKey := DeriveKey("good")
	wrongKey := DeriveKey("wrong")
	ct, err := Encrypt("secret", goodKey)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	field := ct
	DecryptContentField(&field, wrongKey)
	if field != ct {
		t.Fatalf("wrong key must leave ciphertext as-is, got %q", field)
	}
}

func TestEncryptContentField_PreservesNil(t *testing.T) {
	key := DeriveKey("k")
	got, err := EncryptContentField(nil, key)
	if err != nil {
		t.Fatalf("encrypt nil: %v", err)
	}
	if got != nil {
		t.Fatalf("want nil, got %v", got)
	}
}

func TestEncryptContentField_RoundTripsViaDecrypt(t *testing.T) {
	key := DeriveKey("k")
	in := "message"
	ct, err := EncryptContentField(&in, key)
	if err != nil || ct == nil {
		t.Fatalf("encrypt: %v ct=%v", err, ct)
	}
	DecryptContentField(ct, key)
	if *ct != "message" {
		t.Fatalf("round trip failed, got %q", *ct)
	}
}
