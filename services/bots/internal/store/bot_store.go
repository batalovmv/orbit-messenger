package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mst-corp/orbit/services/bots/internal/model"
)

const botSelectColumns = `
	b.id, b.user_id, b.owner_id, u.username, u.display_name, u.avatar_url,
	b.description, b.short_description, b.about_text,
	b.is_system, b.is_inline, b.inline_placeholder,
	b.is_privacy_enabled, b.can_join_groups, b.can_read_all_group_messages,
	b.menu_button,
	b.webhook_url, b.webhook_secret_hash,
	b.is_active, b.created_at, b.updated_at
`

type BotStore interface {
	Create(ctx context.Context, bot *model.Bot) error
	GetByID(ctx context.Context, id uuid.UUID) (*model.Bot, error)
	GetByUserID(ctx context.Context, userID uuid.UUID) (*model.Bot, error)
	GetByUsername(ctx context.Context, username string) (*model.Bot, error)
	GetBotUserIDByUsername(ctx context.Context, username string) (uuid.UUID, error)
	List(ctx context.Context, ownerID *uuid.UUID, limit int, offset int) ([]model.Bot, int, error)
	Update(ctx context.Context, bot *model.Bot) error
	Delete(ctx context.Context, id uuid.UUID) error
	CreateBotUser(ctx context.Context, username, displayName string) (uuid.UUID, error)
}

type botStore struct {
	pool *pgxpool.Pool
}

type botScanner interface {
	Scan(dest ...any) error
}

func NewBotStore(pool *pgxpool.Pool) BotStore {
	return &botStore{pool: pool}
}

func scanBot(scanner botScanner, bot *model.Bot) error {
	var menuButtonRaw []byte
	if err := scanner.Scan(
		&bot.ID,
		&bot.UserID,
		&bot.OwnerID,
		&bot.Username,
		&bot.DisplayName,
		&bot.AvatarURL,
		&bot.Description,
		&bot.ShortDescription,
		&bot.AboutText,
		&bot.IsSystem,
		&bot.IsInline,
		&bot.InlinePlaceholder,
		&bot.IsPrivacyEnabled,
		&bot.CanJoinGroups,
		&bot.CanReadAllGroupMessages,
		&menuButtonRaw,
		&bot.WebhookURL,
		&bot.WebhookSecretHash,
		&bot.IsActive,
		&bot.CreatedAt,
		&bot.UpdatedAt,
	); err != nil {
		return err
	}
	if len(menuButtonRaw) > 0 {
		mb := &model.MenuButton{}
		if err := json.Unmarshal(menuButtonRaw, mb); err != nil {
			return fmt.Errorf("decode menu_button: %w", err)
		}
		bot.MenuButton = mb
	}
	return nil
}

func (s *botStore) GetBotUserIDByUsername(ctx context.Context, username string) (uuid.UUID, error) {
	var userID uuid.UUID
	err := s.pool.QueryRow(ctx, `
		SELECT id FROM users WHERE username = $1 AND account_type = 'bot'
	`, username).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, model.ErrBotNotFound
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("get bot user by username: %w", err)
	}
	return userID, nil
}

func (s *botStore) CreateBotUser(ctx context.Context, username, displayName string) (uuid.UUID, error) {
	userID := uuid.New()
	email := fmt.Sprintf("bot-%s@orbit.internal", userID.String())

	_, err := s.pool.Exec(ctx, `
		INSERT INTO users (id, email, password_hash, display_name, role, account_type, username)
		VALUES ($1, $2, '', $3, 'member', 'bot', $4)
	`, userID, email, displayName, username)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return uuid.Nil, model.ErrBotAlreadyExists
		}
		return uuid.Nil, fmt.Errorf("create bot user: %w", err)
	}

	return userID, nil
}

func (s *botStore) Create(ctx context.Context, bot *model.Bot) error {
	// New privacy/inline/menu fields use DB defaults on insert; Update handles mutations.
	err := s.pool.QueryRow(ctx, `
		INSERT INTO bots (
			user_id, owner_id, description, short_description, is_system,
			is_inline, webhook_url, webhook_secret_hash, is_active
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, created_at, updated_at
	`,
		bot.UserID,
		bot.OwnerID,
		bot.Description,
		bot.ShortDescription,
		bot.IsSystem,
		bot.IsInline,
		bot.WebhookURL,
		bot.WebhookSecretHash,
		bot.IsActive,
	).Scan(&bot.ID, &bot.CreatedAt, &bot.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return model.ErrBotAlreadyExists
		}
		return fmt.Errorf("create bot: %w", err)
	}

	return nil
}

func (s *botStore) GetByID(ctx context.Context, id uuid.UUID) (*model.Bot, error) {
	bot := &model.Bot{}
	err := scanBot(s.pool.QueryRow(ctx, `
		SELECT `+botSelectColumns+`
		FROM bots b
		JOIN users u ON u.id = b.user_id
		WHERE b.id = $1
	`, id), bot)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get bot by id: %w", err)
	}

	return bot, nil
}

func (s *botStore) GetByUserID(ctx context.Context, userID uuid.UUID) (*model.Bot, error) {
	bot := &model.Bot{}
	err := scanBot(s.pool.QueryRow(ctx, `
		SELECT `+botSelectColumns+`
		FROM bots b
		JOIN users u ON u.id = b.user_id
		WHERE b.user_id = $1
	`, userID), bot)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get bot by user id: %w", err)
	}

	return bot, nil
}

func (s *botStore) GetByUsername(ctx context.Context, username string) (*model.Bot, error) {
	bot := &model.Bot{}
	err := scanBot(s.pool.QueryRow(ctx, `
		SELECT `+botSelectColumns+`
		FROM bots b
		JOIN users u ON u.id = b.user_id
		WHERE u.username = $1
	`, username), bot)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get bot by username: %w", err)
	}

	return bot, nil
}

func (s *botStore) List(ctx context.Context, ownerID *uuid.UUID, limit int, offset int) ([]model.Bot, int, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	var ownerArg any
	if ownerID != nil {
		ownerArg = *ownerID
	}

	var total int
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM bots
		WHERE ($1::uuid IS NULL OR owner_id = $1)
	`, ownerArg).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count bots: %w", err)
	}

	rows, err := s.pool.Query(ctx, `
		SELECT `+botSelectColumns+`
		FROM bots b
		JOIN users u ON u.id = b.user_id
		WHERE ($1::uuid IS NULL OR b.owner_id = $1)
		ORDER BY b.created_at DESC
		LIMIT $2 OFFSET $3
	`, ownerArg, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list bots: %w", err)
	}
	defer rows.Close()

	bots := make([]model.Bot, 0, limit)
	for rows.Next() {
		var bot model.Bot
		if err := scanBot(rows, &bot); err != nil {
			return nil, 0, fmt.Errorf("scan bot: %w", err)
		}
		bots = append(bots, bot)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate bots: %w", err)
	}

	return bots, total, nil
}

func (s *botStore) Update(ctx context.Context, bot *model.Bot) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin update bot tx: %w", err)
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx, `
		UPDATE users
		SET username = $1, display_name = $2
		WHERE id = $3
	`, bot.Username, bot.DisplayName, bot.UserID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return model.ErrBotAlreadyExists
		}
		return fmt.Errorf("update bot user: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return model.ErrBotNotFound
	}

	var menuButtonJSON []byte
	if bot.MenuButton != nil {
		raw, err := json.Marshal(bot.MenuButton)
		if err != nil {
			return fmt.Errorf("encode menu_button: %w", err)
		}
		menuButtonJSON = raw
	}

	tag, err = tx.Exec(ctx, `
		UPDATE bots
		SET description = $1,
		    short_description = $2,
		    about_text = $3,
		    is_system = $4,
		    is_inline = $5,
		    inline_placeholder = $6,
		    is_privacy_enabled = $7,
		    can_join_groups = $8,
		    can_read_all_group_messages = $9,
		    menu_button = $10,
		    webhook_url = $11,
		    webhook_secret_hash = $12,
		    is_active = $13,
		    updated_at = NOW()
		WHERE id = $14
	`,
		bot.Description, bot.ShortDescription, bot.AboutText,
		bot.IsSystem, bot.IsInline, bot.InlinePlaceholder,
		bot.IsPrivacyEnabled, bot.CanJoinGroups, bot.CanReadAllGroupMessages,
		menuButtonJSON,
		bot.WebhookURL, bot.WebhookSecretHash, bot.IsActive, bot.ID,
	)
	if err != nil {
		return fmt.Errorf("update bot: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return model.ErrBotNotFound
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit update bot tx: %w", err)
	}

	return nil
}

func (s *botStore) Delete(ctx context.Context, id uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin delete bot tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var userID uuid.UUID
	err = tx.QueryRow(ctx, `SELECT user_id FROM bots WHERE id = $1`, id).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.ErrBotNotFound
	}
	if err != nil {
		return fmt.Errorf("get bot user for delete: %w", err)
	}

	if _, err := tx.Exec(ctx, `DELETE FROM bots WHERE id = $1`, id); err != nil {
		return fmt.Errorf("delete bot: %w", err)
	}

	if _, err := tx.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID); err != nil {
		return fmt.Errorf("delete bot user: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit delete bot tx: %w", err)
	}

	return nil
}
