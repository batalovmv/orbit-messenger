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
	// GetByIDs returns stickers for the provided IDs in the same order.
	GetByIDs(ctx context.Context, stickerIDs []uuid.UUID) ([]model.Sticker, error)
	// GetPack returns a sticker pack by ID with all its stickers.
	GetPack(ctx context.Context, packID uuid.UUID) (*model.StickerPack, error)
	// GetPackByShortName returns a sticker pack by its short name.
	GetPackByShortName(ctx context.Context, shortName string) (*model.StickerPack, error)
	// ListFeatured returns recommended sticker packs.
	ListFeatured(ctx context.Context, limit int) ([]model.StickerPack, error)
	// Search searches sticker packs by title, short_name, or description.
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
	// CreatePack creates or updates a sticker pack and synchronizes its stickers.
	CreatePack(ctx context.Context, pack *model.StickerPack, stickers []model.Sticker) error
	// AddSticker appends a sticker to the end of a pack.
	AddSticker(ctx context.Context, packID uuid.UUID, sticker *model.Sticker) error
	// UpdatePack updates pack metadata.
	UpdatePack(ctx context.Context, pack *model.StickerPack) error
	// DeletePack removes a pack and its stickers.
	DeletePack(ctx context.Context, packID uuid.UUID) error
}

type stickerStore struct {
	pool *pgxpool.Pool
}

func NewStickerStore(pool *pgxpool.Pool) StickerStore {
	return &stickerStore{pool: pool}
}

func (s *stickerStore) GetByIDs(ctx context.Context, stickerIDs []uuid.UUID) ([]model.Sticker, error) {
	if len(stickerIDs) == 0 {
		return []model.Sticker{}, nil
	}

	rows, err := s.pool.Query(ctx, `
		SELECT id, pack_id, emoji, file_url, file_type, width, height, position
		FROM stickers
		WHERE id = ANY($1)
		ORDER BY array_position($1::uuid[], id) ASC`,
		stickerIDs,
	)
	if err != nil {
		return nil, fmt.Errorf("get stickers by ids: %w", err)
	}
	defer rows.Close()

	stickers := make([]model.Sticker, 0, len(stickerIDs))
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
			return nil, fmt.Errorf("scan sticker by id: %w", err)
		}
		stickers = append(stickers, sticker)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate stickers by id: %w", err)
	}

	return stickers, nil
}

func (s *stickerStore) GetPack(ctx context.Context, packID uuid.UUID) (*model.StickerPack, error) {
	return s.getPack(ctx, `
		SELECT id, title, short_name, description, author_id, thumbnail_url, is_official,
		       is_featured, is_animated, sticker_count, created_at, updated_at
		FROM sticker_packs
		WHERE id = $1`,
		packID,
	)
}

func (s *stickerStore) GetPackByShortName(ctx context.Context, shortName string) (*model.StickerPack, error) {
	return s.getPack(ctx, `
		SELECT id, title, short_name, description, author_id, thumbnail_url, is_official,
		       is_featured, is_animated, sticker_count, created_at, updated_at
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
		SELECT id, title, short_name, description, author_id, thumbnail_url, is_official,
		       is_featured, is_animated, sticker_count, created_at, updated_at
		FROM sticker_packs
		WHERE is_featured = true
		ORDER BY is_official DESC, updated_at DESC, created_at DESC
		LIMIT $1`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list featured sticker packs: %w", err)
	}
	defer rows.Close()

	packs, err := scanStickerPacks(rows)
	if err != nil {
		return nil, err
	}
	if err := s.hydratePackStickers(ctx, packs); err != nil {
		return nil, err
	}
	return packs, nil
}

func (s *stickerStore) Search(ctx context.Context, query string, limit int) ([]model.StickerPack, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	rows, err := s.pool.Query(ctx, `
		SELECT id, title, short_name, description, author_id, thumbnail_url, is_official,
		       is_featured, is_animated, sticker_count, created_at, updated_at
		FROM sticker_packs
		WHERE title ILIKE '%' || $1 || '%'
		   OR short_name ILIKE '%' || $1 || '%'
		   OR COALESCE(description, '') ILIKE '%' || $1 || '%'
		ORDER BY is_featured DESC, is_official DESC, title ASC, created_at DESC
		LIMIT $2`,
		query, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("search sticker packs: %w", err)
	}
	defer rows.Close()

	packs, err := scanStickerPacks(rows)
	if err != nil {
		return nil, err
	}
	if err := s.hydratePackStickers(ctx, packs); err != nil {
		return nil, err
	}
	return packs, nil
}

func (s *stickerStore) ListInstalled(ctx context.Context, userID uuid.UUID) ([]model.StickerPack, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT sp.id, sp.title, sp.short_name, sp.description, sp.author_id, sp.thumbnail_url,
		       sp.is_official, sp.is_featured, sp.is_animated, sp.sticker_count, sp.created_at, sp.updated_at
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

	packs, err := scanStickerPacks(rows)
	if err != nil {
		return nil, err
	}
	if err := s.hydratePackStickers(ctx, packs); err != nil {
		return nil, err
	}
	return packs, nil
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

	pack.IsFeatured = true
	for i := range stickers {
		if stickers[i].ID == uuid.Nil {
			stickers[i].ID = uuid.New()
		}
		stickers[i].PackID = pack.ID
		stickers[i].Position = i
	}

	err = tx.QueryRow(ctx, `
		INSERT INTO sticker_packs (
			id, title, short_name, description, author_id, thumbnail_url,
			is_official, is_featured, is_animated, sticker_count
		)
		VALUES (
			COALESCE(NULLIF($1, '00000000-0000-0000-0000-000000000000'::uuid), gen_random_uuid()),
			$2, $3, $4, $5, $6, $7, true, $8, $9
		)
		ON CONFLICT (short_name) DO UPDATE SET
			title = EXCLUDED.title,
			description = EXCLUDED.description,
			author_id = EXCLUDED.author_id,
			thumbnail_url = EXCLUDED.thumbnail_url,
			is_official = EXCLUDED.is_official,
			is_featured = EXCLUDED.is_featured,
			is_animated = EXCLUDED.is_animated,
			sticker_count = EXCLUDED.sticker_count,
			updated_at = NOW()
		RETURNING id, title, short_name, description, author_id, thumbnail_url,
		          is_official, is_featured, is_animated, sticker_count, created_at, updated_at`,
		pack.ID,
		pack.Title,
		pack.ShortName,
		pack.Description,
		pack.AuthorID,
		pack.ThumbnailURL,
		pack.IsOfficial,
		pack.IsAnimated,
		len(stickers),
	).Scan(
		&pack.ID,
		&pack.Title,
		&pack.ShortName,
		&pack.Description,
		&pack.AuthorID,
		&pack.ThumbnailURL,
		&pack.IsOfficial,
		&pack.IsFeatured,
		&pack.IsAnimated,
		&pack.StickerCount,
		&pack.CreatedAt,
		&pack.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert sticker pack: %w", err)
	}

	for i := range stickers {
		stickers[i].PackID = pack.ID
	}
	if err := s.syncPackStickers(ctx, tx, pack.ID, stickers); err != nil {
		return err
	}
	if err := s.refreshPackStats(ctx, tx, pack); err != nil {
		return err
	}

	pack.Stickers = append([]model.Sticker(nil), stickers...)
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit sticker pack tx: %w", err)
	}
	return nil
}

func (s *stickerStore) AddSticker(ctx context.Context, packID uuid.UUID, sticker *model.Sticker) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin add sticker tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if sticker.ID == uuid.Nil {
		sticker.ID = uuid.New()
	}
	sticker.PackID = packID

	err = tx.QueryRow(ctx, `
		WITH next_position AS (
			SELECT COALESCE(MAX(position) + 1, 0) AS value
			FROM stickers
			WHERE pack_id = $1
		)
		INSERT INTO stickers (id, pack_id, emoji, file_url, file_type, width, height, position)
		SELECT $2, $1, $3, $4, $5, $6, $7, value
		FROM next_position
		RETURNING position`,
		packID,
		sticker.ID,
		sticker.Emoji,
		sticker.FileURL,
		sticker.FileType,
		sticker.Width,
		sticker.Height,
	).Scan(&sticker.Position)
	if err != nil {
		return fmt.Errorf("insert sticker: %w", err)
	}

	pack := &model.StickerPack{ID: packID}
	if err := s.refreshPackStats(ctx, tx, pack); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit add sticker tx: %w", err)
	}
	return nil
}

func (s *stickerStore) UpdatePack(ctx context.Context, pack *model.StickerPack) error {
	pack.IsFeatured = true
	err := s.pool.QueryRow(ctx, `
		UPDATE sticker_packs
		SET title = $2,
		    short_name = $3,
		    description = $4,
		    thumbnail_url = $5,
		    is_featured = true,
		    updated_at = NOW()
		WHERE id = $1
		RETURNING id, title, short_name, description, author_id, thumbnail_url,
		          is_official, is_featured, is_animated, sticker_count, created_at, updated_at`,
		pack.ID,
		pack.Title,
		pack.ShortName,
		pack.Description,
		pack.ThumbnailURL,
	).Scan(
		&pack.ID,
		&pack.Title,
		&pack.ShortName,
		&pack.Description,
		&pack.AuthorID,
		&pack.ThumbnailURL,
		&pack.IsOfficial,
		&pack.IsFeatured,
		&pack.IsAnimated,
		&pack.StickerCount,
		&pack.CreatedAt,
		&pack.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("update sticker pack: %w", err)
	}
	return nil
}

func (s *stickerStore) DeletePack(ctx context.Context, packID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM sticker_packs WHERE id = $1`, packID)
	if err != nil {
		return fmt.Errorf("delete sticker pack: %w", err)
	}
	return nil
}

func (s *stickerStore) getPack(ctx context.Context, query string, arg interface{}) (*model.StickerPack, error) {
	var pack model.StickerPack
	err := s.pool.QueryRow(ctx, query, arg).Scan(
		&pack.ID,
		&pack.Title,
		&pack.ShortName,
		&pack.Description,
		&pack.AuthorID,
		&pack.ThumbnailURL,
		&pack.IsOfficial,
		&pack.IsFeatured,
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

func (s *stickerStore) syncPackStickers(ctx context.Context, tx pgx.Tx, packID uuid.UUID, stickers []model.Sticker) error {
	stickerIDs := make([]uuid.UUID, 0, len(stickers))
	for i := range stickers {
		stickerIDs = append(stickerIDs, stickers[i].ID)
	}

	if len(stickerIDs) == 0 {
		if _, err := tx.Exec(ctx, `DELETE FROM stickers WHERE pack_id = $1`, packID); err != nil {
			return fmt.Errorf("clear stickers for pack: %w", err)
		}
		return nil
	}

	if _, err := tx.Exec(ctx, `
		DELETE FROM stickers
		WHERE pack_id = $1
		  AND NOT (id = ANY($2))`,
		packID, stickerIDs,
	); err != nil {
		return fmt.Errorf("delete stale stickers: %w", err)
	}

	batch := &pgx.Batch{}
	for i := range stickers {
		batch.Queue(`
			INSERT INTO stickers (id, pack_id, emoji, file_url, file_type, width, height, position)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (id) DO UPDATE SET
				pack_id = EXCLUDED.pack_id,
				emoji = EXCLUDED.emoji,
				file_url = EXCLUDED.file_url,
				file_type = EXCLUDED.file_type,
				width = EXCLUDED.width,
				height = EXCLUDED.height,
				position = EXCLUDED.position`,
			stickers[i].ID,
			packID,
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
		if _, err := results.Exec(); err != nil {
			_ = results.Close()
			return fmt.Errorf("upsert sticker %d: %w", i, err)
		}
	}
	if err := results.Close(); err != nil {
		return fmt.Errorf("close sticker batch: %w", err)
	}

	return nil
}

func (s *stickerStore) refreshPackStats(ctx context.Context, tx pgx.Tx, pack *model.StickerPack) error {
	err := tx.QueryRow(ctx, `
		UPDATE sticker_packs
		SET sticker_count = stats.sticker_count,
		    is_animated = stats.is_animated,
		    is_featured = true,
		    updated_at = NOW()
		FROM (
			SELECT $1::uuid AS pack_id,
			       COUNT(*)::int AS sticker_count,
			       COALESCE(BOOL_OR(file_type IN ('tgs', 'webm')), false) AS is_animated
			FROM stickers
			WHERE pack_id = $1
		) AS stats
		WHERE sticker_packs.id = stats.pack_id
		RETURNING sticker_packs.sticker_count, sticker_packs.is_animated, sticker_packs.is_featured, sticker_packs.updated_at`,
		pack.ID,
	).Scan(&pack.StickerCount, &pack.IsAnimated, &pack.IsFeatured, &pack.UpdatedAt)
	if err != nil {
		return fmt.Errorf("refresh sticker pack stats: %w", err)
	}
	return nil
}

func scanStickerPacks(rows pgx.Rows) ([]model.StickerPack, error) {
	packs := make([]model.StickerPack, 0)
	for rows.Next() {
		var pack model.StickerPack
		if err := rows.Scan(
			&pack.ID,
			&pack.Title,
			&pack.ShortName,
			&pack.Description,
			&pack.AuthorID,
			&pack.ThumbnailURL,
			&pack.IsOfficial,
			&pack.IsFeatured,
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

func (s *stickerStore) hydratePackStickers(ctx context.Context, packs []model.StickerPack) error {
	if len(packs) == 0 {
		return nil
	}

	packIDs := make([]uuid.UUID, len(packs))
	for i, p := range packs {
		packIDs[i] = p.ID
	}

	rows, err := s.pool.Query(ctx, `
		SELECT id, pack_id, emoji, file_url, file_type, width, height, position
		FROM stickers
		WHERE pack_id = ANY($1)
		ORDER BY pack_id, position ASC`,
		packIDs,
	)
	if err != nil {
		return fmt.Errorf("hydrate pack stickers: %w", err)
	}
	defer rows.Close()

	stickersByPack := make(map[uuid.UUID][]model.Sticker)
	for rows.Next() {
		var sticker model.Sticker
		if err := rows.Scan(
			&sticker.ID, &sticker.PackID, &sticker.Emoji,
			&sticker.FileURL, &sticker.FileType,
			&sticker.Width, &sticker.Height, &sticker.Position,
		); err != nil {
			return fmt.Errorf("scan sticker: %w", err)
		}
		stickersByPack[sticker.PackID] = append(stickersByPack[sticker.PackID], sticker)
	}

	for i := range packs {
		packs[i].Stickers = stickersByPack[packs[i].ID]
	}

	return nil
}
