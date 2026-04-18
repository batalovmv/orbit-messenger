package store

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mst-corp/orbit/pkg/crypto"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
)

// fakeScanner implements scheduledMessageScanner for table-driven scan tests.
// The Scan signature on scheduledMessageStore expects columns in the exact
// order produced by scheduledMessageSelectQuery — see the scan call site.
type fakeScanner struct {
	values []any
}

func (f *fakeScanner) Scan(dest ...any) error {
	for i, v := range f.values {
		assignScanDest(dest[i], v)
	}
	return nil
}

// assignScanDest writes v into *dest using the narrowest cases the scheduled
// scan path actually uses. Nils in the value slice are represented as the
// untyped `nil` (no typed-nil traps). Anything else is panicked loudly so
// the test fails rather than silently drifting from the column order.
func assignScanDest(dest any, v any) {
	switch d := dest.(type) {
	case *uuid.UUID:
		*d = v.(uuid.UUID)
	case *string:
		*d = v.(string)
	case **string:
		if v == nil {
			*d = nil
			return
		}
		s := v.(string)
		*d = &s
	case *[]byte:
		if v == nil {
			*d = nil
			return
		}
		*d = v.([]byte)
	case **uuid.UUID:
		if v == nil {
			*d = nil
			return
		}
		id := v.(uuid.UUID)
		*d = &id
	case **int64:
		if v == nil {
			*d = nil
			return
		}
		n := v.(int64)
		*d = &n
	case *json.RawMessage:
		if v == nil {
			*d = nil
			return
		}
		*d = v.(json.RawMessage)
	case *[]uuid.UUID:
		if v == nil {
			*d = nil
			return
		}
		*d = v.([]uuid.UUID)
	case *bool:
		*d = v.(bool)
	case *time.Time:
		*d = v.(time.Time)
	case **time.Time:
		if v == nil {
			*d = nil
			return
		}
		t := v.(time.Time)
		*d = &t
	default:
		panic("unhandled dest type in fakeScanner")
	}
}

func scheduledScanValues(content any) []any {
	now := time.Now()
	return []any{
		uuid.New(),                // id
		uuid.New(),                // chat_id
		uuid.New(),                // sender_id
		content,   // content *string (nil/string/ciphertext per test)
		nil,       // entities
		nil,       // reply_to_id
		nil,       // reply_to_seq
		"text",    // type
		nil,       // media_ids
		false,     // is_spoiler
		nil,       // poll_payload
		now,       // scheduled_at
		false,     // is_sent
		nil,       // sent_at
		now,       // created_at
		now,       // updated_at
	}
}

func TestScheduledScan_RoundTripsCiphertextToPlaintext(t *testing.T) {
	key := crypto.DeriveKey("test")
	s := &scheduledMessageStore{atRest: key}

	plain := "отложенное сообщение"
	ct, err := crypto.Encrypt(plain, key)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	var msg model.ScheduledMessage
	scanner := &fakeScanner{values: scheduledScanValues(ct)}
	if err := s.scanScheduledMessageRow(scanner, &msg); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if msg.Content == nil || *msg.Content != plain {
		t.Fatalf("expected plaintext %q, got %v", plain, msg.Content)
	}
}

func TestScheduledScan_NilContentStaysNil(t *testing.T) {
	s := &scheduledMessageStore{atRest: crypto.DeriveKey("test")}

	var msg model.ScheduledMessage
	scanner := &fakeScanner{values: scheduledScanValues(nil)}
	if err := s.scanScheduledMessageRow(scanner, &msg); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if msg.Content != nil {
		t.Fatalf("expected nil content, got %v", *msg.Content)
	}
}

func TestScheduledScan_LegacyPlaintextFallback(t *testing.T) {
	s := &scheduledMessageStore{atRest: crypto.DeriveKey("test")}

	legacy := "before at-rest"
	var msg model.ScheduledMessage
	scanner := &fakeScanner{values: scheduledScanValues(legacy)}
	if err := s.scanScheduledMessageRow(scanner, &msg); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if msg.Content == nil || *msg.Content != legacy {
		t.Fatalf("legacy plaintext should be preserved, got %v", msg.Content)
	}
}
