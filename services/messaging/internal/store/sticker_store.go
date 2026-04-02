package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
)

// StickerStore manages sticker packs and user sticker collections.
type StickerStore interface {
	// GetPack returns a sticker pack by ID with all its stickers.
	GetPack(ctx context.Context, packID uuid.UUID) (*model.StickerPack, error)
	// GetPackByShortName returns a sticker pack by its short name.
	GetPackByShortName(ctx context.Context, shortName string) (*model.StickerPack, error)
	// ListFeatured returns recommended/official sticker packs.
	ListFeatured(ctx context.Context, limit int) ([]model.StickerPack, error)
	// Search searches sticker packs by title or short_name.
	Search(ctx context.Context, query string, limit int) ([]model.StickerPack, error)
	// ListInstalled returns the user's installed sticker packs ordered by position.
	ListInstalled(ctx context.Context, userID uuid.UUID) ([]model.StickerPack, error)
	// Install installs a sticker pack for a user.
	Install(ctx context.Context, userID, packID uuid.UUID) error
	// Uninstall removes a sticker pack from a user's collection.
	Uninstall(ctx context.Context, userID, packID uuid.UUID) error
	// IsInstalled checks if a user has installed a sticker pack.
	IsInstalled(ctx context.Context, userID, packID uuid.UUID) (bool, error)
	// ListRecent returns the user's recently used stickers.
	ListRecent(ctx context.Context, userID uuid.UUID, limit int) ([]model.Sticker, error)
	// AddRecent adds or updates a sticker in the user's recent list.
	AddRecent(ctx context.Context, userID, stickerID uuid.UUID) error
	// RemoveRecent removes a sticker from the user's recent list.
	RemoveRecent(ctx context.Context, userID, stickerID uuid.UUID) error
	// ClearRecent removes all stickers from the user's recent list.
	ClearRecent(ctx context.Context, userID uuid.UUID) error
	// CreatePack creates a new sticker pack (for TG import).
	CreatePack(ctx context.Context, pack *model.StickerPack, stickers []model.Sticker) error
}

type stickerStore struct {
	pool *pgxpool.Pool
}

func NewStickerStore(pool *pgxpool.Pool) StickerStore {
	return &stickerStore{pool: pool}
}

func (s *stickerStore) GetPack(ctx context.Context, packID uuid.UUID) (*model.StickerPack, error) {
	return s.getPack(ctx, `
		SELECT id, title, short_name, author_id, thumbnail_url, is_official,
		       is_animated, sticker_count, created_at, updated_at
		FROM sticker_packs
		WHERE id = $1`,
		packID,
	)
}

func (s *stickerStore) GetPackByShortName(ctx context.Context, shortName string) (*model.StickerPack, error) {
	return s.getPack(ctx, `
		SELECT id, title, short_name, author_id, thumbnail_url, is_official,
		       is_animated, sticker_count, created_at, updated_at
		FROM sticker_packs
		WHERE short_name = $1`,
		shortName,
	)
}

func (s *stickerStore) ListFeatured(ctx context.Context, limit int) ([]model.StickerPack, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	rows, err := s.pool.Query(ctx, `
		SELECT id, title, short_name, author_id, thumbnail_url, is_official,
		       is_animated, sticker_count, created_at, updated_at
		FROM sticker_packs
		WHERE is_official = true
		ORDER BY created_at DESC
		LIMIT $1`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list featured sticker packs: %w", err)
	}
	defer rows.Close()

	return scanStickerPacks(rows)
}

func (s *stickerStore) Search(ctx context.Context, query string, limit int) ([]model.StickerPack, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	rows, err := s.pool.Query(ctx, `
		SELECT id, title, short_name, author_id, thumbnail_url, is_official,
		       is_animated, sticker_count, created_at, updated_at
		FROM sticker_packs
		WHERE title ILIKE '%' || $1 || '%'
		   OR short_name ILIKE '%' || $1 || '%'
		ORDER BY is_official DESC, title ASC, created_at DESC
		LIMIT $2`,
		query, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("search sticker packs: %w", err)
	}
	defer rows.Close()

	return scanStickerPacks(rows)
}

func (s *stickerStore) ListInstalled(ctx context.Context, userID uuid.UUID) ([]model.StickerPack, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT sp.id, sp.title, sp.short_name, sp.author_id, sp.thumbnail_url,
		       sp.is_official, sp.is_animated, sp.sticker_count, sp.created_at, sp.updated_at
		FROM user_installed_stickers uis
		JOIN sticker_packs sp ON sp.id = uis.pack_id
		WHERE uis.user_id = $1
		ORDER BY uis.position ASC, uis.installed_at ASC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list installed sticker packs: %w", err)
	}
	defer rows.Close()

	return scanStickerPacks(rows)
}

func (s *stickerStore) Install(ctx context.Context, userID, packID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO user_installed_stickers (user_id, pack_id, position)
		SELECT $1, $2, COALESCE(MAX(position) + 1, 0)
		FROM user_installed_stickers
		WHERE user_id = $1`,
		userID, packID,
	)
	if err != nil {
		return fmt.Errorf("install sticker pack: %w", err)
	}
	return nil
}

func (s *stickerStore) Uninstall(ctx context.Context, userID, packID uuid.UUID) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM user_installed_stickers WHERE user_id = $1 AND pack_id = $2`,
		userID, packID,
	)
	if err != nil {
		return fmt.Errorf("uninstall sticker pack: %w", err)
	}
	return nil
}

func (s *stickerStore) IsInstalled(ctx context.Context, userID, packID uuid.UUID) (bool, error) {
	var installed bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM user_installed_stickers WHERE user_id = $1 AND pack_id = $2)`,
		userID, packID,
	).Scan(&installed)
	if err != nil {
		return false, fmt.Errorf("check sticker pack install status: %w", err)
	}
	return installed, nil
}

func (s *stickerStore) ListRecent(ctx context.Context, userID uuid.UUID, limit int) ([]model.Sticker, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}

	rows, err := s.pool.Query(ctx, `
		SELECT s.id, s.pack_id, s.emoji, s.file_url, s.file_type, s.width, s.height, s.position
		FROM recent_stickers rs
		JOIN stickers s ON s.id = rs.sticker_id
		WHERE rs.user_id = $1
		ORDER BY rs.used_at DESC
		LIMIT $2`,
		userID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list recent stickers: %w", err)
	}
	defer rows.Close()

	stickers := make([]model.Sticker, 0)
	for rows.Next() {
		var sticker model.Sticker
		if err := rows.Scan(
			&sticker.ID,
			&sticker.PackID,
			&sticker.Emoji,
			&sticker.FileURL,
			&sticker.FileType,
			&sticker.Width,
			&sticker.Height,
			&sticker.Position,
		); err != nil {
			return nil, fmt.Errorf("scan recent sticker: %w", err)
		}
		stickers = append(stickers, sticker)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recent stickers: %w", err)
	}
	return stickers, nil
}

func (s *stickerStore) AddRecent(ctx context.Context, userID, stickerID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO recent_stickers (user_id, sticker_id, used_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (user_id, sticker_id)
		DO UPDATE SET used_at = EXCLUDED.used_at`,
		userID, stickerID,
	)
	if err != nil {
		return fmt.Errorf("add recent sticker: %w", err)
	}
	return nil
}

func (s *stickerStore) RemoveRecent(ctx context.Context, userID, stickerID uuid.UUID) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM recent_stickers WHERE user_id = $1 AND sticker_id = $2`,
		userID, stickerID,
	)
	if err != nil {
		return fmt.Errorf("remove recent sticker: %w", err)
	}
	return nil
}

func (s *stickerStore) ClearRecent(ctx context.Context, userID uuid.UUID) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM recent_stickers WHERE user_id = $1`,
		userID,
	)
	if err != nil {
		return fmt.Errorf("clear recent stickers: %w", err)
	}
	return nil
}

func (s *stickerStore) CreatePack(ctx context.Context, pack *model.StickerPack, stickers []model.Sticker) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin sticker pack tx: %w", err)
	}
	defer tx.Rollback(ctx)

	err = tx.QueryRow(ctx, `
		INSERT INTO sticker_packs (
			title, short_name, author_id, thumbnail_url, is_official, is_animated, sticker_count
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, sticker_count, created_at, updated_at`,
		pack.Title,
		pack.ShortName,
		pack.AuthorID,
		pack.ThumbnailURL,
		pack.IsOfficial,
		pack.IsAnimated,
		len(stickers),
	).Scan(&pack.ID, &pack.StickerCount, &pack.CreatedAt, &pack.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert sticker pack: %w", err)
	}

	if len(stickers) > 0 {
		batch := &pgx.Batch{}
		for i := range stickers {
			batch.Queue(`
				INSERT INTO stickers (pack_id, emoji, file_url, file_type, width, height, position)
				VALUES ($1, $2, $3, $4, $5, $6, $7)
				RETURNING id`,
				pack.ID,
				stickers[i].Emoji,
				stickers[i].FileURL,
				stickers[i].FileType,
				stickers[i].Width,
				stickers[i].Height,
				stickers[i].Position,
			)
		}

		results := tx.SendBatch(ctx, batch)
		for i := range stickers {
			stickers[i].PackID = pack.ID
			if err := results.QueryRow().Scan(&stickers[i].ID); err != nil {
				_ = results.Close()
				return fmt.Errorf("insert sticker %d: %w", i, err)
			}
		}
		if err := results.Close(); err != nil {
			return fmt.Errorf("close sticker batch: %w", err)
		}
	}

	pack.Stickers = append([]model.Sticker(nil), stickers...)
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit sticker pack tx: %w", err)
	}
	return nil
}

func (s *stickerStore) getPack(ctx context.Context, query string, arg interface{}) (*model.StickerPack, error) {
	var pack model.StickerPack
	err := s.pool.QueryRow(ctx, query, arg).Scan(
		&pack.ID,
		&pack.Title,
		&pack.ShortName,
		&pack.AuthorID,
		&pack.ThumbnailURL,
		&pack.IsOfficial,
		&pack.IsAnimated,
		&pack.StickerCount,
		&pack.CreatedAt,
		&pack.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get sticker pack: %w", err)
	}

	stickers, err := s.listPackStickers(ctx, pack.ID)
	if err != nil {
		return nil, err
	}
	pack.Stickers = stickers
	pack.StickerCount = len(stickers)

	return &pack, nil
}

func (s *stickerStore) listPackStickers(ctx context.Context, packID uuid.UUID) ([]model.Sticker, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, pack_id, emoji, file_url, file_type, width, height, position
		FROM stickers
		WHERE pack_id = $1
		ORDER BY position ASC`,
		packID,
	)
	if err != nil {
		return nil, fmt.Errorf("list stickers for pack: %w", err)
	}
	defer rows.Close()

	stickers := make([]model.Sticker, 0)
	for rows.Next() {
		var sticker model.Sticker
		if err := rows.Scan(
			&sticker.ID,
			&sticker.PackID,
			&sticker.Emoji,
			&sticker.FileURL,
			&sticker.FileType,
			&sticker.Width,
			&sticker.Height,
			&sticker.Position,
		); err != nil {
			return nil, fmt.Errorf("scan sticker: %w", err)
		}
		stickers = append(stickers, sticker)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate stickers for pack: %w", err)
	}
	return stickers, nil
}

func scanStickerPacks(rows pgx.Rows) ([]model.StickerPack, error) {
	packs := make([]model.StickerPack, 0)
	for rows.Next() {
		var pack model.StickerPack
		if err := rows.Scan(
			&pack.ID,
			&pack.Title,
			&pack.ShortName,
			&pack.AuthorID,
			&pack.ThumbnailURL,
			&pack.IsOfficial,
			&pack.IsAnimated,
			&pack.StickerCount,
			&pack.CreatedAt,
			&pack.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan sticker pack: %w", err)
		}
		packs = append(packs, pack)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sticker packs: %w", err)
	}
	return packs, nil
}
