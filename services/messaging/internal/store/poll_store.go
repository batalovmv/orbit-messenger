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
	"github.com/mst-corp/orbit/services/messaging/internal/model"
)

// PollStore manages polls, options, and votes.
type PollStore interface {
	// Create creates a new poll with its options. Sets poll.ID and option IDs.
	Create(ctx context.Context, poll *model.Poll) error
	// GetByID returns a poll with options and vote counts.
	GetByID(ctx context.Context, pollID uuid.UUID) (*model.Poll, error)
	// GetByMessageID returns the poll attached to a message.
	GetByMessageID(ctx context.Context, messageID uuid.UUID) (*model.Poll, error)
	// ListByMessageIDs returns polls keyed by message ID for batch hydration.
	ListByMessageIDs(ctx context.Context, messageIDs []uuid.UUID) (map[uuid.UUID]*model.Poll, error)
	// Vote records a vote. Returns error if poll is closed or option invalid.
	Vote(ctx context.Context, pollID, optionID, userID uuid.UUID) error
	// VoteAtomic records one or more votes atomically for a user.
	VoteAtomic(ctx context.Context, pollID, userID uuid.UUID, optionIDs []uuid.UUID, isMultiple bool) error
	// Unvote removes a user's vote on a specific option.
	Unvote(ctx context.Context, pollID, optionID, userID uuid.UUID) error
	// UnvoteAll removes all of a user's votes on a poll.
	UnvoteAll(ctx context.Context, pollID, userID uuid.UUID) error
	// Close marks a poll as closed.
	Close(ctx context.Context, pollID uuid.UUID) error
	// GetVoters returns users who voted for a specific option (non-anonymous polls only).
	GetVoters(ctx context.Context, pollID, optionID uuid.UUID, limit int, cursor string) ([]model.PollVote, string, bool, error)
	// HasVoted checks if a user has voted on any option in a poll.
	HasVoted(ctx context.Context, pollID, userID uuid.UUID) (bool, error)
	// GetUserVotes returns the option IDs the user voted for in a poll.
	GetUserVotes(ctx context.Context, pollID, userID uuid.UUID) ([]uuid.UUID, error)
	// ListUserVotesByPollIDs returns option IDs keyed by poll ID for batch hydration.
	ListUserVotesByPollIDs(ctx context.Context, pollIDs []uuid.UUID, userID uuid.UUID) (map[uuid.UUID][]uuid.UUID, error)
}

type pollStore struct {
	pool *pgxpool.Pool
}

func NewPollStore(pool *pgxpool.Pool) PollStore {
	return &pollStore{pool: pool}
}

func (s *pollStore) Create(ctx context.Context, poll *model.Poll) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	err = tx.QueryRow(ctx,
		`INSERT INTO polls (
		     message_id, question, is_anonymous, is_multiple, is_quiz,
		     correct_option, solution, solution_entities, close_at
		 )
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9)
		 RETURNING id, is_closed, created_at`,
		poll.MessageID,
		poll.Question,
		poll.IsAnonymous,
		poll.IsMultiple,
		poll.IsQuiz,
		poll.CorrectOption,
		poll.Solution,
		poll.SolutionEntities,
		poll.CloseAt,
	).Scan(&poll.ID, &poll.IsClosed, &poll.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert poll: %w", err)
	}

	for i := range poll.Options {
		option := &poll.Options[i]
		option.PollID = poll.ID
		if err := tx.QueryRow(ctx,
			`INSERT INTO poll_options (id, poll_id, text, position)
			 VALUES (gen_random_uuid(), $1, $2, $3)
			 RETURNING id`,
			poll.ID, option.Text, option.Position,
		).Scan(&option.ID); err != nil {
			return fmt.Errorf("insert poll option: %w", err)
		}
		option.PollID = poll.ID
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

func (s *pollStore) GetByID(ctx context.Context, pollID uuid.UUID) (*model.Poll, error) {
	return s.getPollByQuery(ctx,
		`SELECT id, message_id, question, is_anonymous, is_multiple, is_quiz, correct_option,
		        solution, solution_entities, is_closed, close_at, created_at
		 FROM polls WHERE id = $1`,
		pollID,
	)
}

func (s *pollStore) GetByMessageID(ctx context.Context, messageID uuid.UUID) (*model.Poll, error) {
	return s.getPollByQuery(ctx,
		`SELECT id, message_id, question, is_anonymous, is_multiple, is_quiz, correct_option,
		        solution, solution_entities, is_closed, close_at, created_at
		 FROM polls WHERE message_id = $1`,
		messageID,
	)
}

func (s *pollStore) ListByMessageIDs(ctx context.Context, messageIDs []uuid.UUID) (map[uuid.UUID]*model.Poll, error) {
	if len(messageIDs) == 0 {
		return map[uuid.UUID]*model.Poll{}, nil
	}

	rows, err := s.pool.Query(ctx,
		`SELECT id, message_id, question, is_anonymous, is_multiple, is_quiz, correct_option,
		        solution, solution_entities, is_closed, close_at, created_at
		 FROM polls
		 WHERE message_id = ANY($1)`,
		messageIDs,
	)
	if err != nil {
		return nil, fmt.Errorf("list polls by message IDs: %w", err)
	}
	defer rows.Close()

	pollsByMessageID := make(map[uuid.UUID]*model.Poll)
	pollIDs := make([]uuid.UUID, 0, len(messageIDs))
	pollsByID := make(map[uuid.UUID]*model.Poll)

	for rows.Next() {
		poll := &model.Poll{}
		if err := rows.Scan(
			&poll.ID,
			&poll.MessageID,
			&poll.Question,
			&poll.IsAnonymous,
			&poll.IsMultiple,
			&poll.IsQuiz,
			&poll.CorrectOption,
			&poll.Solution,
			&poll.SolutionEntities,
			&poll.IsClosed,
			&poll.CloseAt,
			&poll.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan poll by message IDs: %w", err)
		}

		pollsByMessageID[poll.MessageID] = poll
		pollsByID[poll.ID] = poll
		pollIDs = append(pollIDs, poll.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(pollIDs) == 0 {
		return pollsByMessageID, nil
	}

	totalRows, err := s.pool.Query(ctx,
		`SELECT poll_id, COUNT(DISTINCT user_id)::int
		 FROM poll_votes
		 WHERE poll_id = ANY($1)
		 GROUP BY poll_id`,
		pollIDs,
	)
	if err != nil {
		return nil, fmt.Errorf("list poll totals by message IDs: %w", err)
	}
	defer totalRows.Close()

	for totalRows.Next() {
		var pollID uuid.UUID
		var total int
		if err := totalRows.Scan(&pollID, &total); err != nil {
			return nil, fmt.Errorf("scan poll total by message IDs: %w", err)
		}
		if poll := pollsByID[pollID]; poll != nil {
			poll.TotalVoters = total
		}
	}
	if err := totalRows.Err(); err != nil {
		return nil, err
	}

	optionRows, err := s.pool.Query(ctx,
		`SELECT po.id, po.poll_id, po.text, po.position, COUNT(pv.user_id)::int AS voters
		 FROM poll_options po
		 LEFT JOIN poll_votes pv ON pv.poll_id = po.poll_id AND pv.option_id = po.id
		 WHERE po.poll_id = ANY($1)
		 GROUP BY po.id, po.poll_id, po.text, po.position
		 ORDER BY po.poll_id, po.position`,
		pollIDs,
	)
	if err != nil {
		return nil, fmt.Errorf("list poll options by message IDs: %w", err)
	}
	defer optionRows.Close()

	for optionRows.Next() {
		var option model.PollOption
		if err := optionRows.Scan(&option.ID, &option.PollID, &option.Text, &option.Position, &option.Voters); err != nil {
			return nil, fmt.Errorf("scan poll option by message IDs: %w", err)
		}
		if poll := pollsByID[option.PollID]; poll != nil {
			poll.Options = append(poll.Options, option)
		}
	}
	if err := optionRows.Err(); err != nil {
		return nil, err
	}

	return pollsByMessageID, nil
}

func (s *pollStore) Vote(ctx context.Context, pollID, optionID, userID uuid.UUID) error {
	tag, err := s.pool.Exec(ctx,
		`INSERT INTO poll_votes (poll_id, option_id, user_id)
		 SELECT p.id, po.id, $3
		 FROM polls p
		 JOIN poll_options po ON po.id = $2 AND po.poll_id = p.id
		 WHERE p.id = $1 AND p.is_closed = FALSE
		 ON CONFLICT (poll_id, user_id, option_id) DO NOTHING`,
		pollID, optionID, userID,
	)
	if err != nil {
		return fmt.Errorf("insert poll vote: %w", err)
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	return s.explainVoteNoop(ctx, pollID, optionID)
}

func (s *pollStore) VoteAtomic(ctx context.Context, pollID, userID uuid.UUID, optionIDs []uuid.UUID, isMultiple bool) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if !isMultiple {
		_, err = tx.Exec(ctx,
			`DELETE FROM poll_votes WHERE poll_id = $1 AND user_id = $2`,
			pollID, userID,
		)
		if err != nil {
			return fmt.Errorf("clear votes: %w", err)
		}
	}

	for _, optionID := range optionIDs {
		_, err = tx.Exec(ctx,
			`INSERT INTO poll_votes (poll_id, option_id, user_id)
			 SELECT p.id, po.id, $3
			 FROM polls p
			 JOIN poll_options po ON po.id = $2 AND po.poll_id = p.id
			 WHERE p.id = $1 AND p.is_closed = FALSE
			 ON CONFLICT (poll_id, user_id, option_id) DO NOTHING`,
			pollID, optionID, userID,
		)
		if err != nil {
			return fmt.Errorf("vote: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}

	return nil
}

func (s *pollStore) Unvote(ctx context.Context, pollID, optionID, userID uuid.UUID) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM poll_votes WHERE poll_id = $1 AND option_id = $2 AND user_id = $3`,
		pollID, optionID, userID,
	)
	if err != nil {
		return fmt.Errorf("delete poll vote: %w", err)
	}
	return nil
}

func (s *pollStore) UnvoteAll(ctx context.Context, pollID, userID uuid.UUID) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM poll_votes WHERE poll_id = $1 AND user_id = $2`,
		pollID, userID,
	)
	if err != nil {
		return fmt.Errorf("delete all poll votes: %w", err)
	}
	return nil
}

func (s *pollStore) Close(ctx context.Context, pollID uuid.UUID) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE polls SET is_closed = TRUE WHERE id = $1 AND is_closed = FALSE`,
		pollID,
	)
	if err != nil {
		return fmt.Errorf("close poll: %w", err)
	}
	if tag.RowsAffected() > 0 {
		return nil
	}

	var exists bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM polls WHERE id = $1)`,
		pollID,
	).Scan(&exists); err != nil {
		return fmt.Errorf("check poll existence: %w", err)
	}
	if !exists {
		return pgx.ErrNoRows
	}
	return nil
}

func (s *pollStore) GetVoters(
	ctx context.Context,
	pollID, optionID uuid.UUID,
	limit int,
	cursor string,
) ([]model.PollVote, string, bool, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	args := []any{pollID, optionID, limit + 1}
	query := `SELECT poll_id, option_id, user_id, voted_at
		  FROM poll_votes
		  WHERE poll_id = $1 AND option_id = $2`

	if cursor != "" {
		cursorTime, err := time.Parse(time.RFC3339Nano, cursor)
		if err == nil {
			query += ` AND voted_at < $4`
			args = append(args, cursorTime)
		}
	}

	query += ` ORDER BY voted_at DESC LIMIT $3`

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, "", false, fmt.Errorf("list poll voters: %w", err)
	}
	defer rows.Close()

	var votes []model.PollVote
	for rows.Next() {
		var vote model.PollVote
		if err := rows.Scan(&vote.PollID, &vote.OptionID, &vote.UserID, &vote.VotedAt); err != nil {
			return nil, "", false, fmt.Errorf("scan poll voter: %w", err)
		}
		votes = append(votes, vote)
	}
	if err := rows.Err(); err != nil {
		return nil, "", false, err
	}

	hasMore := len(votes) > limit
	if hasMore {
		votes = votes[:limit]
	}

	var nextCursor string
	if hasMore && len(votes) > 0 {
		nextCursor = votes[len(votes)-1].VotedAt.Format(time.RFC3339Nano)
	}

	return votes, nextCursor, hasMore, nil
}

func (s *pollStore) HasVoted(ctx context.Context, pollID, userID uuid.UUID) (bool, error) {
	var voted bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM poll_votes WHERE poll_id = $1 AND user_id = $2)`,
		pollID, userID,
	).Scan(&voted)
	if err != nil {
		return false, fmt.Errorf("check poll vote: %w", err)
	}
	return voted, nil
}

func (s *pollStore) GetUserVotes(ctx context.Context, pollID, userID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT option_id
		 FROM poll_votes
		 WHERE poll_id = $1 AND user_id = $2
		 ORDER BY voted_at, option_id`,
		pollID, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("get user poll votes: %w", err)
	}
	defer rows.Close()

	var optionIDs []uuid.UUID
	for rows.Next() {
		var optionID uuid.UUID
		if err := rows.Scan(&optionID); err != nil {
			return nil, fmt.Errorf("scan user poll vote: %w", err)
		}
		optionIDs = append(optionIDs, optionID)
	}
	return optionIDs, rows.Err()
}

func (s *pollStore) ListUserVotesByPollIDs(ctx context.Context, pollIDs []uuid.UUID, userID uuid.UUID) (map[uuid.UUID][]uuid.UUID, error) {
	if len(pollIDs) == 0 {
		return map[uuid.UUID][]uuid.UUID{}, nil
	}

	rows, err := s.pool.Query(ctx,
		`SELECT poll_id, option_id
		 FROM poll_votes
		 WHERE poll_id = ANY($1) AND user_id = $2
		 ORDER BY poll_id, voted_at, option_id`,
		pollIDs, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list user poll votes by poll IDs: %w", err)
	}
	defer rows.Close()

	result := make(map[uuid.UUID][]uuid.UUID)
	for rows.Next() {
		var pollID uuid.UUID
		var optionID uuid.UUID
		if err := rows.Scan(&pollID, &optionID); err != nil {
			return nil, fmt.Errorf("scan user poll vote by poll IDs: %w", err)
		}
		result[pollID] = append(result[pollID], optionID)
	}

	return result, rows.Err()
}

func (s *pollStore) getPollByQuery(ctx context.Context, query string, arg uuid.UUID) (*model.Poll, error) {
	poll := &model.Poll{}
	err := s.pool.QueryRow(ctx, query, arg).Scan(
		&poll.ID,
		&poll.MessageID,
		&poll.Question,
		&poll.IsAnonymous,
		&poll.IsMultiple,
		&poll.IsQuiz,
		&poll.CorrectOption,
		&poll.Solution,
		&poll.SolutionEntities,
		&poll.IsClosed,
		&poll.CloseAt,
		&poll.CreatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load poll: %w", err)
	}

	totalVoters, err := s.totalVoters(ctx, poll.ID)
	if err != nil {
		return nil, err
	}
	poll.TotalVoters = totalVoters

	rows, err := s.pool.Query(ctx,
		`SELECT po.id, po.poll_id, po.text, po.position, COUNT(pv.user_id) AS voters
		 FROM poll_options po
		 LEFT JOIN poll_votes pv ON pv.poll_id = po.poll_id AND pv.option_id = po.id
		 WHERE po.poll_id = $1
		 GROUP BY po.id, po.poll_id, po.text, po.position
		 ORDER BY po.position`,
		poll.ID,
	)
	if err != nil {
		return nil, fmt.Errorf("load poll options: %w", err)
	}
	defer rows.Close()

	poll.Options = make([]model.PollOption, 0)
	for rows.Next() {
		var option model.PollOption
		if err := rows.Scan(&option.ID, &option.PollID, &option.Text, &option.Position, &option.Voters); err != nil {
			return nil, fmt.Errorf("scan poll option: %w", err)
		}
		poll.Options = append(poll.Options, option)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return poll, nil
}

func (s *pollStore) totalVoters(ctx context.Context, pollID uuid.UUID) (int, error) {
	var total int
	if err := s.pool.QueryRow(ctx,
		`SELECT COUNT(DISTINCT user_id) FROM poll_votes WHERE poll_id = $1`,
		pollID,
	).Scan(&total); err != nil {
		return 0, fmt.Errorf("count poll voters: %w", err)
	}
	return total, nil
}

func (s *pollStore) explainVoteNoop(ctx context.Context, pollID, optionID uuid.UUID) error {
	var isClosed bool
	err := s.pool.QueryRow(ctx,
		`SELECT is_closed FROM polls WHERE id = $1`,
		pollID,
	).Scan(&isClosed)
	if err == pgx.ErrNoRows {
		return pgx.ErrNoRows
	}
	if err != nil {
		return fmt.Errorf("check poll status: %w", err)
	}
	if isClosed {
		return fmt.Errorf("poll closed")
	}

	var optionExists bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM poll_options WHERE poll_id = $1 AND id = $2)`,
		pollID, optionID,
	).Scan(&optionExists); err != nil {
		return fmt.Errorf("check poll option: %w", err)
	}
	if !optionExists {
		return fmt.Errorf("invalid option")
	}
	return nil
}
