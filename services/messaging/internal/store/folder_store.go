// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mst-corp/orbit/pkg/apperror"
)

const maxFoldersPerUser = 100

// lockUserFolders acquires a transaction-scoped advisory lock for the given user,
// serializing all position-mutating folder operations for that user.
// The lock key uses two int32 halves of the UUID to avoid hashtext collision.
func lockUserFolders(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	b := userID[:]
	hi := int32(b[0])<<24 | int32(b[1])<<16 | int32(b[2])<<8 | int32(b[3])
	lo := int32(b[4])<<24 | int32(b[5])<<16 | int32(b[6])<<8 | int32(b[7])
	if _, err := tx.Exec(ctx,
		`SELECT pg_advisory_xact_lock($1, $2)`,
		hi, lo,
	); err != nil {
		return fmt.Errorf("acquire folder lock: %w", err)
	}
	return nil
}

// ChatFolder represents a user-defined chat folder with its chat membership lists.
type ChatFolder struct {
	ID              int
	UserID          uuid.UUID
	Title           string
	Emoticon        *string
	Color           *int
	Position        int
	CreatedAt       time.Time
	UpdatedAt       time.Time
	IncludedChatIDs []string
	ExcludedChatIDs []string
	PinnedChatIDs   []string
}

// FolderStore manages chat folders and their chat memberships.
type FolderStore interface {
	List(ctx context.Context, userID uuid.UUID) ([]*ChatFolder, error)
	Get(ctx context.Context, id int, userID uuid.UUID) (*ChatFolder, error)
	Create(ctx context.Context, f *ChatFolder) error
	Update(ctx context.Context, f *ChatFolder) error
	Delete(ctx context.Context, id int, userID uuid.UUID) error
	UpdateOrder(ctx context.Context, userID uuid.UUID, folderIDs []int) error
	SetChats(ctx context.Context, folderID int, userID uuid.UUID, included, excluded, pinned []string) error
}

type folderStore struct {
	pool *pgxpool.Pool
}

// NewFolderStore creates a FolderStore backed by the given connection pool.
func NewFolderStore(pool *pgxpool.Pool) FolderStore {
	return &folderStore{pool: pool}
}

// List returns all folders for a user ordered by position, with chat membership lists populated.
func (s *folderStore) List(ctx context.Context, userID uuid.UUID) ([]*ChatFolder, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, user_id, title, emoticon, color, position, created_at, updated_at
		 FROM chat_folders
		 WHERE user_id = $1
		 ORDER BY position ASC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list folders: %w", err)
	}
	defer rows.Close()

	var folders []*ChatFolder
	for rows.Next() {
		f := &ChatFolder{}
		if err := rows.Scan(
			&f.ID, &f.UserID, &f.Title, &f.Emoticon, &f.Color,
			&f.Position, &f.CreatedAt, &f.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan folder: %w", err)
		}
		folders = append(folders, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate folders: %w", err)
	}

	for _, f := range folders {
		if err := s.loadChats(ctx, f); err != nil {
			return nil, err
		}
	}
	return folders, nil
}

// Get returns a single folder by ID, verifying ownership by userID.
func (s *folderStore) Get(ctx context.Context, id int, userID uuid.UUID) (*ChatFolder, error) {
	f := &ChatFolder{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, user_id, title, emoticon, color, position, created_at, updated_at
		 FROM chat_folders
		 WHERE id = $1 AND user_id = $2`,
		id, userID,
	).Scan(
		&f.ID, &f.UserID, &f.Title, &f.Emoticon, &f.Color,
		&f.Position, &f.CreatedAt, &f.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, apperror.NotFound("folder not found")
		}
		return nil, fmt.Errorf("get folder: %w", err)
	}

	if err := s.loadChats(ctx, f); err != nil {
		return nil, err
	}
	return f, nil
}

// Create inserts a new folder and sets its chat memberships. Sets f.ID on success.
func (s *folderStore) Create(ctx context.Context, f *ChatFolder) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if err := lockUserFolders(ctx, tx, f.UserID); err != nil {
		return err
	}

	var count int
	if err := tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM chat_folders WHERE user_id = $1`,
		f.UserID,
	).Scan(&count); err != nil {
		return fmt.Errorf("count folders: %w", err)
	}
	if count >= maxFoldersPerUser {
		return apperror.BadRequest("too many folders")
	}

	err = tx.QueryRow(ctx,
		`INSERT INTO chat_folders (user_id, title, emoticon, color, position)
		 VALUES ($1, $2, $3, $4,
		         COALESCE((SELECT MAX(position) + 1 FROM chat_folders WHERE user_id = $1), 0))
		 RETURNING id, position, created_at, updated_at`,
		f.UserID, f.Title, f.Emoticon, f.Color,
	).Scan(&f.ID, &f.Position, &f.CreatedAt, &f.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert folder: %w", err)
	}

	if err := setChatsInTx(ctx, tx, f.ID, f.UserID, f.IncludedChatIDs, f.ExcludedChatIDs, f.PinnedChatIDs); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// Update modifies an existing folder's metadata and replaces its chat memberships.
func (s *folderStore) Update(ctx context.Context, f *ChatFolder) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	tag, err := tx.Exec(ctx,
		`UPDATE chat_folders
		 SET title = $1, emoticon = $2, color = $3
		 WHERE id = $4 AND user_id = $5`,
		f.Title, f.Emoticon, f.Color, f.ID, f.UserID,
	)
	if err != nil {
		return fmt.Errorf("update folder: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return apperror.NotFound("folder not found")
	}

	if err := setChatsInTx(ctx, tx, f.ID, f.UserID, f.IncludedChatIDs, f.ExcludedChatIDs, f.PinnedChatIDs); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// Delete removes a folder by ID, verifying ownership by userID, then compacts positions.
func (s *folderStore) Delete(ctx context.Context, id int, userID uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if err := lockUserFolders(ctx, tx, userID); err != nil {
		return err
	}

	tag, err := tx.Exec(ctx,
		`DELETE FROM chat_folders WHERE id = $1 AND user_id = $2`,
		id, userID,
	)
	if err != nil {
		return fmt.Errorf("delete folder: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return apperror.NotFound("folder not found")
	}

	// Compact positions to eliminate gaps after deletion.
	if _, err := tx.Exec(ctx,
		`UPDATE chat_folders cf
		 SET position = sub.new_pos
		 FROM (
		     SELECT id, ROW_NUMBER() OVER (ORDER BY position ASC) - 1 AS new_pos
		     FROM chat_folders
		     WHERE user_id = $1
		 ) sub
		 WHERE cf.id = sub.id AND cf.user_id = $1`,
		userID,
	); err != nil {
		return fmt.Errorf("compact folder positions: %w", err)
	}

	return tx.Commit(ctx)
}

// UpdateOrder sets the position of each folder in folderIDs to its slice index.
func (s *folderStore) UpdateOrder(ctx context.Context, userID uuid.UUID, folderIDs []int) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if err := lockUserFolders(ctx, tx, userID); err != nil {
		return err
	}

	// Verify folderIDs is a complete permutation of the user's folders.
	existingRows, err := tx.Query(ctx,
		`SELECT id FROM chat_folders WHERE user_id = $1`,
		userID,
	)
	if err != nil {
		return fmt.Errorf("list folders for order check: %w", err)
	}
	existing := make(map[int]struct{})
	for existingRows.Next() {
		var fid int
		if err := existingRows.Scan(&fid); err != nil {
			existingRows.Close()
			return fmt.Errorf("scan folder id: %w", err)
		}
		existing[fid] = struct{}{}
	}
	existingRows.Close()
	if err := existingRows.Err(); err != nil {
		return fmt.Errorf("iterate folders: %w", err)
	}

	if len(folderIDs) != len(existing) {
		return apperror.BadRequest("folder_ids must contain all folders")
	}
	for _, fid := range folderIDs {
		if _, ok := existing[fid]; !ok {
			return apperror.NotFound("folder not found")
		}
	}

	// Reject duplicate IDs — the permutation check above only catches wrong counts
	// when duplicates cancel out missing IDs.
	seen := make(map[int]struct{}, len(folderIDs))
	for _, fid := range folderIDs {
		if _, dup := seen[fid]; dup {
			return apperror.BadRequest("duplicate folder ID")
		}
		seen[fid] = struct{}{}
	}

	for pos, folderID := range folderIDs {
		tag, err := tx.Exec(ctx,
			`UPDATE chat_folders SET position = $1 WHERE id = $2 AND user_id = $3`,
			pos, folderID, userID,
		)
		if err != nil {
			return fmt.Errorf("update folder position id=%d: %w", folderID, err)
		}
		if tag.RowsAffected() == 0 {
			return apperror.NotFound("folder not found")
		}
	}

	return tx.Commit(ctx)
}

// SetChats replaces all chat memberships for a folder, verifying ownership first.
func (s *folderStore) SetChats(ctx context.Context, folderID int, userID uuid.UUID, included, excluded, pinned []string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Verify ownership before mutating chats.
	var ownerID uuid.UUID
	err = tx.QueryRow(ctx,
		`SELECT user_id FROM chat_folders WHERE id = $1`,
		folderID,
	).Scan(&ownerID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return apperror.NotFound("folder not found")
		}
		return fmt.Errorf("verify folder ownership: %w", err)
	}
	if ownerID != userID {
		return apperror.NotFound("folder not found")
	}

	if err := setChatsInTx(ctx, tx, folderID, userID, included, excluded, pinned); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// loadChats populates IncludedChatIDs, ExcludedChatIDs, and PinnedChatIDs on a folder.
func (s *folderStore) loadChats(ctx context.Context, f *ChatFolder) error {
	rows, err := s.pool.Query(ctx,
		`SELECT chat_id::text, is_pinned, is_excluded
		 FROM chat_folder_chats
		 WHERE folder_id = $1`,
		f.ID,
	)
	if err != nil {
		return fmt.Errorf("load folder chats id=%d: %w", f.ID, err)
	}
	defer rows.Close()

	for rows.Next() {
		var chatID string
		var isPinned, isExcluded bool
		if err := rows.Scan(&chatID, &isPinned, &isExcluded); err != nil {
			return fmt.Errorf("scan folder chat: %w", err)
		}
		switch {
		case isPinned:
			f.PinnedChatIDs = append(f.PinnedChatIDs, chatID)
		case isExcluded:
			f.ExcludedChatIDs = append(f.ExcludedChatIDs, chatID)
		default:
			f.IncludedChatIDs = append(f.IncludedChatIDs, chatID)
		}
	}
	return rows.Err()
}

// setChatsInTx replaces all chat_folder_chats rows for folderID within an existing transaction.
// It verifies that every requested chat_id is accessible to userID via chat_members before inserting.
func setChatsInTx(ctx context.Context, tx pgx.Tx, folderID int, userID uuid.UUID, included, excluded, pinned []string) error {
	if _, err := tx.Exec(ctx,
		`DELETE FROM chat_folder_chats WHERE folder_id = $1`,
		folderID,
	); err != nil {
		return fmt.Errorf("clear folder chats: %w", err)
	}

	type chatRow struct {
		chatID     string
		isPinned   bool
		isExcluded bool
	}

	rows := make([]chatRow, 0, len(included)+len(excluded)+len(pinned))
	for _, id := range included {
		rows = append(rows, chatRow{chatID: id, isPinned: false, isExcluded: false})
	}
	for _, id := range excluded {
		rows = append(rows, chatRow{chatID: id, isPinned: false, isExcluded: true})
	}
	for _, id := range pinned {
		rows = append(rows, chatRow{chatID: id, isPinned: true, isExcluded: false})
	}

	if len(rows) > 0 {
		// Collect unique chat IDs requested.
		requested := make(map[string]struct{}, len(rows))
		allIDs := make([]string, 0, len(rows))
		for _, r := range rows {
			if _, seen := requested[r.chatID]; !seen {
				requested[r.chatID] = struct{}{}
				allIDs = append(allIDs, r.chatID)
			}
		}

		// Verify membership in a single query.
		memberRows, err := tx.Query(ctx,
			`SELECT chat_id::text FROM chat_members WHERE user_id = $1 AND chat_id = ANY($2::uuid[])`,
			userID, allIDs,
		)
		if err != nil {
			return fmt.Errorf("verify chat membership: %w", err)
		}
		accessible := make(map[string]struct{}, len(allIDs))
		for memberRows.Next() {
			var chatID string
			if err := memberRows.Scan(&chatID); err != nil {
				memberRows.Close()
				return fmt.Errorf("scan chat membership: %w", err)
			}
			accessible[chatID] = struct{}{}
		}
		memberRows.Close()
		if err := memberRows.Err(); err != nil {
			return fmt.Errorf("iterate chat membership: %w", err)
		}

		for chatID := range requested {
			if _, ok := accessible[chatID]; !ok {
				return apperror.BadRequest("chat not accessible: " + chatID)
			}
		}
	}

	for _, r := range rows {
		if _, err := tx.Exec(ctx,
			`INSERT INTO chat_folder_chats (folder_id, chat_id, is_pinned, is_excluded)
			 VALUES ($1, $2::uuid, $3, $4)
			 ON CONFLICT (folder_id, chat_id) DO UPDATE
			   SET is_pinned = EXCLUDED.is_pinned, is_excluded = EXCLUDED.is_excluded`,
			folderID, r.chatID, r.isPinned, r.isExcluded,
		); err != nil {
			return fmt.Errorf("insert folder chat %s: %w", r.chatID, err)
		}
	}
	return nil
}
