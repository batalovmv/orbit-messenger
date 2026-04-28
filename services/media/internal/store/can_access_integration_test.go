// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

//go:build integration
// +build integration

package store

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mst-corp/orbit/services/media/internal/model"
)

// TestCanAccess_RegressionMatrix verifies the IDOR fix from audit 2026-04-26
// CRITICAL #2. Run with:
//
//	TEST_DATABASE_URL=postgres://orbit:orbit@localhost:5432/orbit \
//	  go test -tags=integration ./services/media/internal/store/...
//
// Skipped without TEST_DATABASE_URL so default `go test ./...` stays green.
func TestCanAccess_RegressionMatrix(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}

	store := &MediaStore{pool: pool}

	// Fresh fixtures with random UUIDs so the test does not collide with
	// existing data and is safe to run repeatedly.
	uploaderID := uuid.New()
	memberID := uuid.New()
	outsiderID := uuid.New()
	forwardMemberID := uuid.New() // member of chat B only
	chatAID := uuid.New()
	chatBID := uuid.New()
	mediaID := uuid.New()
	msgAID := uuid.New()
	msgBID := uuid.New()

	t.Cleanup(func() {
		// Order matters due to FKs.
		_, _ = pool.Exec(ctx, `DELETE FROM message_media WHERE media_id = $1`, mediaID)
		_, _ = pool.Exec(ctx, `DELETE FROM messages WHERE id = ANY($1)`, []uuid.UUID{msgAID, msgBID})
		_, _ = pool.Exec(ctx, `DELETE FROM chat_members WHERE chat_id = ANY($1)`, []uuid.UUID{chatAID, chatBID})
		_, _ = pool.Exec(ctx, `DELETE FROM chats WHERE id = ANY($1)`, []uuid.UUID{chatAID, chatBID})
		_, _ = pool.Exec(ctx, `DELETE FROM media WHERE id = $1`, mediaID)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = ANY($1)`,
			[]uuid.UUID{uploaderID, memberID, outsiderID, forwardMemberID})
	})

	mustExec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("exec %q: %v", sql, err)
		}
	}

	// Users — minimal columns; rely on schema defaults for the rest.
	for _, u := range []struct {
		id    uuid.UUID
		label string
	}{
		{uploaderID, "uploader"},
		{memberID, "member"},
		{outsiderID, "outsider"},
		{forwardMemberID, "fwd"},
	} {
		email := u.label + "_" + u.id.String() + "@test.local"
		mustExec(`INSERT INTO users (id, email, password_hash, display_name)
		          VALUES ($1, $2, 'x', $3)`, u.id, email, u.label)
	}

	// Two chats: A contains the original message, B contains a forwarded copy.
	mustExec(`INSERT INTO chats (id, type, created_by) VALUES ($1, 'group', $2)`, chatAID, uploaderID)
	mustExec(`INSERT INTO chats (id, type, created_by) VALUES ($1, 'group', $2)`, chatBID, uploaderID)

	// Memberships:
	//   - uploaderID: in chat A (creator)
	//   - memberID: in chat A
	//   - forwardMemberID: in chat B (only)
	//   - outsiderID: nowhere
	mustExec(`INSERT INTO chat_members (chat_id, user_id, role) VALUES ($1, $2, 'owner')`, chatAID, uploaderID)
	mustExec(`INSERT INTO chat_members (chat_id, user_id, role) VALUES ($1, $2, 'member')`, chatAID, memberID)
	mustExec(`INSERT INTO chat_members (chat_id, user_id, role) VALUES ($1, $2, 'member')`, chatBID, forwardMemberID)

	// Media uploaded by uploaderID.
	filename := "test.jpg"
	m := &model.Media{
		ID:               mediaID,
		UploaderID:       uploaderID,
		Type:             "photo",
		MimeType:         "image/jpeg",
		OriginalFilename: &filename,
		SizeBytes:        1024,
		R2Key:            "test/" + mediaID.String() + ".jpg",
		ProcessingStatus: "ready",
	}
	if err := store.Create(ctx, m); err != nil {
		t.Fatalf("create media: %v", err)
	}

	// Message in chat A linking the media. sequence_number is per-chat unique
	// with no DB default in the live schema; we pass 1 explicitly because the
	// chat is fresh.
	mustExec(`INSERT INTO messages (id, chat_id, sender_id, type, sequence_number)
	          VALUES ($1, $2, $3, 'photo', 1)`, msgAID, chatAID, uploaderID)
	mustExec(`INSERT INTO message_media (message_id, media_id, position) VALUES ($1, $2, 0)`,
		msgAID, mediaID)

	// Forwarded copy in chat B linking the same media — exercises
	// the "user belongs to any chat that contains the media" path.
	mustExec(`INSERT INTO messages (id, chat_id, sender_id, type, sequence_number, is_forwarded, forwarded_from)
	          VALUES ($1, $2, $3, 'photo', 1, true, $3)`, msgBID, chatBID, uploaderID)
	mustExec(`INSERT INTO message_media (message_id, media_id, position) VALUES ($1, $2, 0)`,
		msgBID, mediaID)

	cases := []struct {
		name string
		user uuid.UUID
		want bool
	}{
		{"uploader_can_access", uploaderID, true},
		{"member_of_chat_with_attached_media_can_access", memberID, true},
		{"member_of_other_chat_with_forwarded_media_can_access", forwardMemberID, true},
		{"non_member_cannot_access_published_media", outsiderID, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := store.CanAccess(ctx, mediaID, tc.user)
			if err != nil {
				t.Fatalf("CanAccess: %v", err)
			}
			if got != tc.want {
				t.Fatalf("CanAccess(media=%s, user=%s) = %v, want %v",
					mediaID, tc.user, got, tc.want)
			}
		})
	}

	// IDOR regression specifically: outsider against an UNATTACHED media — also false.
	t.Run("non_member_cannot_access_unrelated_media", func(t *testing.T) {
		got, err := store.CanAccess(ctx, uuid.New(), outsiderID)
		if err != nil {
			t.Fatalf("CanAccess: %v", err)
		}
		if got {
			t.Fatalf("CanAccess for nonexistent media must be false")
		}
	})
}
