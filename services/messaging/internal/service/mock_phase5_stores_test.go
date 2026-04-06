package service

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
)

// ---------------------------------------------------------------------------
// Mock ReactionStore
// ---------------------------------------------------------------------------

type mockReactionStore struct {
	addFn                   func(ctx context.Context, messageID, userID uuid.UUID, emoji string) error
	removeFn                func(ctx context.Context, messageID, userID uuid.UUID, emoji string) error
	removeAllByUserFn       func(ctx context.Context, messageID, userID uuid.UUID) error
	replaceUserReactionFn   func(ctx context.Context, messageID, userID uuid.UUID, emoji string) error
	listByMessageFn         func(ctx context.Context, messageID uuid.UUID) ([]model.ReactionSummary, error)
	listByMessageIDsFn      func(ctx context.Context, messageIDs []uuid.UUID) (map[uuid.UUID][]model.ReactionSummary, error)
	listUsersByEmojiFn      func(ctx context.Context, messageID uuid.UUID, emoji string, cursor string, limit int) ([]model.Reaction, string, bool, error)
	countByMessageFn        func(ctx context.Context, messageID uuid.UUID) (int, error)
	getAvailableReactionsFn func(ctx context.Context, chatID uuid.UUID) (*model.ChatAvailableReactions, error)
	setAvailableReactionsFn func(ctx context.Context, chatID uuid.UUID, mode string, emojis []string) error
}

func (m *mockReactionStore) Add(ctx context.Context, messageID, userID uuid.UUID, emoji string) error {
	if m.addFn != nil {
		return m.addFn(ctx, messageID, userID, emoji)
	}
	return nil
}

func (m *mockReactionStore) Remove(ctx context.Context, messageID, userID uuid.UUID, emoji string) error {
	if m.removeFn != nil {
		return m.removeFn(ctx, messageID, userID, emoji)
	}
	return nil
}

func (m *mockReactionStore) RemoveAllByUser(ctx context.Context, messageID, userID uuid.UUID) error {
	if m.removeAllByUserFn != nil {
		return m.removeAllByUserFn(ctx, messageID, userID)
	}
	return nil
}

func (m *mockReactionStore) ReplaceUserReaction(ctx context.Context, messageID, userID uuid.UUID, emoji string) error {
	if m.replaceUserReactionFn != nil {
		return m.replaceUserReactionFn(ctx, messageID, userID, emoji)
	}
	return nil
}

func (m *mockReactionStore) ListByMessage(ctx context.Context, messageID uuid.UUID) ([]model.ReactionSummary, error) {
	if m.listByMessageFn != nil {
		return m.listByMessageFn(ctx, messageID)
	}
	return nil, nil
}

func (m *mockReactionStore) ListByMessageIDs(ctx context.Context, messageIDs []uuid.UUID) (map[uuid.UUID][]model.ReactionSummary, error) {
	if m.listByMessageIDsFn != nil {
		return m.listByMessageIDsFn(ctx, messageIDs)
	}
	result := make(map[uuid.UUID][]model.ReactionSummary, len(messageIDs))
	if m.listByMessageFn == nil {
		return result, nil
	}
	for _, messageID := range messageIDs {
		reactions, err := m.listByMessageFn(ctx, messageID)
		if err != nil {
			return nil, err
		}
		result[messageID] = reactions
	}
	return result, nil
}

func (m *mockReactionStore) ListUsersByEmoji(ctx context.Context, messageID uuid.UUID, emoji string, cursor string, limit int) ([]model.Reaction, string, bool, error) {
	if m.listUsersByEmojiFn != nil {
		return m.listUsersByEmojiFn(ctx, messageID, emoji, cursor, limit)
	}
	return nil, "", false, nil
}

func (m *mockReactionStore) CountByMessage(ctx context.Context, messageID uuid.UUID) (int, error) {
	if m.countByMessageFn != nil {
		return m.countByMessageFn(ctx, messageID)
	}
	return 0, nil
}

func (m *mockReactionStore) GetAvailableReactions(ctx context.Context, chatID uuid.UUID) (*model.ChatAvailableReactions, error) {
	if m.getAvailableReactionsFn != nil {
		return m.getAvailableReactionsFn(ctx, chatID)
	}
	return &model.ChatAvailableReactions{ChatID: chatID, Mode: "all"}, nil
}

func (m *mockReactionStore) SetAvailableReactions(ctx context.Context, chatID uuid.UUID, mode string, emojis []string) error {
	if m.setAvailableReactionsFn != nil {
		return m.setAvailableReactionsFn(ctx, chatID, mode, emojis)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Mock PollStore
// ---------------------------------------------------------------------------

type mockPollStore struct {
	createFn                 func(ctx context.Context, poll *model.Poll) error
	getByIDFn                func(ctx context.Context, pollID uuid.UUID) (*model.Poll, error)
	getByMessageIDFn         func(ctx context.Context, messageID uuid.UUID) (*model.Poll, error)
	listByMessageIDsFn       func(ctx context.Context, messageIDs []uuid.UUID) (map[uuid.UUID]*model.Poll, error)
	voteFn                   func(ctx context.Context, pollID, optionID, userID uuid.UUID) error
	voteAtomicFn             func(ctx context.Context, pollID, userID uuid.UUID, optionIDs []uuid.UUID, isMultiple bool) error
	unvoteFn                 func(ctx context.Context, pollID, optionID, userID uuid.UUID) error
	unvoteAllFn              func(ctx context.Context, pollID, userID uuid.UUID) error
	closeFn                  func(ctx context.Context, pollID uuid.UUID) error
	getVotersFn              func(ctx context.Context, pollID, optionID uuid.UUID, limit int, cursor string) ([]model.PollVote, string, bool, error)
	hasVotedFn               func(ctx context.Context, pollID, userID uuid.UUID) (bool, error)
	getUserVotesFn           func(ctx context.Context, pollID, userID uuid.UUID) ([]uuid.UUID, error)
	listUserVotesByPollIDsFn func(ctx context.Context, pollIDs []uuid.UUID, userID uuid.UUID) (map[uuid.UUID][]uuid.UUID, error)
}

func (m *mockPollStore) Create(ctx context.Context, poll *model.Poll) error {
	if m.createFn != nil {
		return m.createFn(ctx, poll)
	}
	poll.ID = uuid.New()
	for i := range poll.Options {
		poll.Options[i].ID = uuid.New()
	}
	return nil
}

func (m *mockPollStore) GetByID(ctx context.Context, pollID uuid.UUID) (*model.Poll, error) {
	if m.getByIDFn != nil {
		return m.getByIDFn(ctx, pollID)
	}
	return nil, nil
}

func (m *mockPollStore) GetByMessageID(ctx context.Context, messageID uuid.UUID) (*model.Poll, error) {
	if m.getByMessageIDFn != nil {
		return m.getByMessageIDFn(ctx, messageID)
	}
	return nil, nil
}

func (m *mockPollStore) ListByMessageIDs(ctx context.Context, messageIDs []uuid.UUID) (map[uuid.UUID]*model.Poll, error) {
	if m.listByMessageIDsFn != nil {
		return m.listByMessageIDsFn(ctx, messageIDs)
	}
	result := make(map[uuid.UUID]*model.Poll, len(messageIDs))
	if m.getByMessageIDFn == nil {
		return result, nil
	}
	for _, messageID := range messageIDs {
		poll, err := m.getByMessageIDFn(ctx, messageID)
		if err != nil {
			return nil, err
		}
		if poll != nil {
			result[messageID] = poll
		}
	}
	return result, nil
}

func (m *mockPollStore) Vote(ctx context.Context, pollID, optionID, userID uuid.UUID) error {
	if m.voteFn != nil {
		return m.voteFn(ctx, pollID, optionID, userID)
	}
	return nil
}

func (m *mockPollStore) VoteAtomic(ctx context.Context, pollID, userID uuid.UUID, optionIDs []uuid.UUID, isMultiple bool) error {
	if m.voteAtomicFn != nil {
		return m.voteAtomicFn(ctx, pollID, userID, optionIDs, isMultiple)
	}
	for _, optionID := range optionIDs {
		if m.voteFn != nil {
			if err := m.voteFn(ctx, pollID, optionID, userID); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *mockPollStore) Unvote(ctx context.Context, pollID, optionID, userID uuid.UUID) error {
	if m.unvoteFn != nil {
		return m.unvoteFn(ctx, pollID, optionID, userID)
	}
	return nil
}

func (m *mockPollStore) UnvoteAll(ctx context.Context, pollID, userID uuid.UUID) error {
	if m.unvoteAllFn != nil {
		return m.unvoteAllFn(ctx, pollID, userID)
	}
	return nil
}

func (m *mockPollStore) Close(ctx context.Context, pollID uuid.UUID) error {
	if m.closeFn != nil {
		return m.closeFn(ctx, pollID)
	}
	return nil
}

func (m *mockPollStore) GetVoters(
	ctx context.Context,
	pollID, optionID uuid.UUID,
	limit int,
	cursor string,
) ([]model.PollVote, string, bool, error) {
	if m.getVotersFn != nil {
		return m.getVotersFn(ctx, pollID, optionID, limit, cursor)
	}
	return nil, "", false, nil
}

func (m *mockPollStore) HasVoted(ctx context.Context, pollID, userID uuid.UUID) (bool, error) {
	if m.hasVotedFn != nil {
		return m.hasVotedFn(ctx, pollID, userID)
	}
	return false, nil
}

func (m *mockPollStore) GetUserVotes(ctx context.Context, pollID, userID uuid.UUID) ([]uuid.UUID, error) {
	if m.getUserVotesFn != nil {
		return m.getUserVotesFn(ctx, pollID, userID)
	}
	return nil, nil
}

func (m *mockPollStore) ListUserVotesByPollIDs(ctx context.Context, pollIDs []uuid.UUID, userID uuid.UUID) (map[uuid.UUID][]uuid.UUID, error) {
	if m.listUserVotesByPollIDsFn != nil {
		return m.listUserVotesByPollIDsFn(ctx, pollIDs, userID)
	}
	result := make(map[uuid.UUID][]uuid.UUID, len(pollIDs))
	if m.getUserVotesFn == nil {
		return result, nil
	}
	for _, pollID := range pollIDs {
		votes, err := m.getUserVotesFn(ctx, pollID, userID)
		if err != nil {
			return nil, err
		}
		result[pollID] = votes
	}
	return result, nil
}

// ---------------------------------------------------------------------------
// Mock StickerStore
// ---------------------------------------------------------------------------

type mockStickerStore struct {
	getByIDsFn           func(ctx context.Context, stickerIDs []uuid.UUID) ([]model.Sticker, error)
	getPackFn            func(ctx context.Context, packID uuid.UUID) (*model.StickerPack, error)
	getPackByShortNameFn func(ctx context.Context, shortName string) (*model.StickerPack, error)
	listFeaturedFn       func(ctx context.Context, limit int) ([]model.StickerPack, error)
	searchFn             func(ctx context.Context, query string, limit int) ([]model.StickerPack, error)
	listInstalledFn      func(ctx context.Context, userID uuid.UUID) ([]model.StickerPack, error)
	installFn            func(ctx context.Context, userID, packID uuid.UUID) error
	uninstallFn          func(ctx context.Context, userID, packID uuid.UUID) error
	isInstalledFn        func(ctx context.Context, userID, packID uuid.UUID) (bool, error)
	listRecentFn         func(ctx context.Context, userID uuid.UUID, limit int) ([]model.Sticker, error)
	addRecentFn          func(ctx context.Context, userID, stickerID uuid.UUID) error
	removeRecentFn       func(ctx context.Context, userID, stickerID uuid.UUID) error
	clearRecentFn        func(ctx context.Context, userID uuid.UUID) error
	createPackFn         func(ctx context.Context, pack *model.StickerPack, stickers []model.Sticker) error
	addStickerFn         func(ctx context.Context, packID uuid.UUID, sticker *model.Sticker) error
	updatePackFn         func(ctx context.Context, pack *model.StickerPack) error
	deletePackFn         func(ctx context.Context, packID uuid.UUID) error
}

func (m *mockStickerStore) GetByIDs(ctx context.Context, stickerIDs []uuid.UUID) ([]model.Sticker, error) {
	if m.getByIDsFn != nil {
		return m.getByIDsFn(ctx, stickerIDs)
	}
	return nil, nil
}

func (m *mockStickerStore) GetPack(ctx context.Context, packID uuid.UUID) (*model.StickerPack, error) {
	if m.getPackFn != nil {
		return m.getPackFn(ctx, packID)
	}
	return nil, nil
}

func (m *mockStickerStore) GetPackByShortName(ctx context.Context, shortName string) (*model.StickerPack, error) {
	if m.getPackByShortNameFn != nil {
		return m.getPackByShortNameFn(ctx, shortName)
	}
	return nil, nil
}

func (m *mockStickerStore) ListFeatured(ctx context.Context, limit int) ([]model.StickerPack, error) {
	if m.listFeaturedFn != nil {
		return m.listFeaturedFn(ctx, limit)
	}
	return nil, nil
}

func (m *mockStickerStore) Search(ctx context.Context, query string, limit int) ([]model.StickerPack, error) {
	if m.searchFn != nil {
		return m.searchFn(ctx, query, limit)
	}
	return nil, nil
}

func (m *mockStickerStore) ListInstalled(ctx context.Context, userID uuid.UUID) ([]model.StickerPack, error) {
	if m.listInstalledFn != nil {
		return m.listInstalledFn(ctx, userID)
	}
	return nil, nil
}

func (m *mockStickerStore) Install(ctx context.Context, userID, packID uuid.UUID) error {
	if m.installFn != nil {
		return m.installFn(ctx, userID, packID)
	}
	return nil
}

func (m *mockStickerStore) Uninstall(ctx context.Context, userID, packID uuid.UUID) error {
	if m.uninstallFn != nil {
		return m.uninstallFn(ctx, userID, packID)
	}
	return nil
}

func (m *mockStickerStore) IsInstalled(ctx context.Context, userID, packID uuid.UUID) (bool, error) {
	if m.isInstalledFn != nil {
		return m.isInstalledFn(ctx, userID, packID)
	}
	return false, nil
}

func (m *mockStickerStore) ListRecent(ctx context.Context, userID uuid.UUID, limit int) ([]model.Sticker, error) {
	if m.listRecentFn != nil {
		return m.listRecentFn(ctx, userID, limit)
	}
	return nil, nil
}

func (m *mockStickerStore) AddRecent(ctx context.Context, userID, stickerID uuid.UUID) error {
	if m.addRecentFn != nil {
		return m.addRecentFn(ctx, userID, stickerID)
	}
	return nil
}

func (m *mockStickerStore) RemoveRecent(ctx context.Context, userID, stickerID uuid.UUID) error {
	if m.removeRecentFn != nil {
		return m.removeRecentFn(ctx, userID, stickerID)
	}
	return nil
}

func (m *mockStickerStore) ClearRecent(ctx context.Context, userID uuid.UUID) error {
	if m.clearRecentFn != nil {
		return m.clearRecentFn(ctx, userID)
	}
	return nil
}

func (m *mockStickerStore) CreatePack(ctx context.Context, pack *model.StickerPack, stickers []model.Sticker) error {
	if m.createPackFn != nil {
		return m.createPackFn(ctx, pack, stickers)
	}
	pack.ID = uuid.New()
	return nil
}

func (m *mockStickerStore) AddSticker(ctx context.Context, packID uuid.UUID, sticker *model.Sticker) error {
	if m.addStickerFn != nil {
		return m.addStickerFn(ctx, packID, sticker)
	}
	sticker.ID = uuid.New()
	sticker.PackID = packID
	return nil
}

func (m *mockStickerStore) UpdatePack(ctx context.Context, pack *model.StickerPack) error {
	if m.updatePackFn != nil {
		return m.updatePackFn(ctx, pack)
	}
	return nil
}

func (m *mockStickerStore) DeletePack(ctx context.Context, packID uuid.UUID) error {
	if m.deletePackFn != nil {
		return m.deletePackFn(ctx, packID)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Mock GIFStore
// ---------------------------------------------------------------------------

type mockGIFStore struct {
	listSavedFn       func(ctx context.Context, userID uuid.UUID, limit int) ([]model.SavedGIF, error)
	saveFn            func(ctx context.Context, gif *model.SavedGIF) error
	removeFn          func(ctx context.Context, userID, gifID uuid.UUID) error
	removeByTenorIDFn func(ctx context.Context, userID uuid.UUID, tenorID string) error
}

func (m *mockGIFStore) ListSaved(ctx context.Context, userID uuid.UUID, limit int) ([]model.SavedGIF, error) {
	if m.listSavedFn != nil {
		return m.listSavedFn(ctx, userID, limit)
	}
	return nil, nil
}

func (m *mockGIFStore) Save(ctx context.Context, gif *model.SavedGIF) error {
	if m.saveFn != nil {
		return m.saveFn(ctx, gif)
	}
	gif.ID = uuid.New()
	gif.CreatedAt = time.Now()
	return nil
}

func (m *mockGIFStore) Remove(ctx context.Context, userID, gifID uuid.UUID) error {
	if m.removeFn != nil {
		return m.removeFn(ctx, userID, gifID)
	}
	return nil
}

func (m *mockGIFStore) RemoveByTenorID(ctx context.Context, userID uuid.UUID, tenorID string) error {
	if m.removeByTenorIDFn != nil {
		return m.removeByTenorIDFn(ctx, userID, tenorID)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Mock ScheduledMessageStore
// ---------------------------------------------------------------------------

type mockScheduledMessageStore struct {
	createFn               func(ctx context.Context, msg *model.ScheduledMessage) error
	getByIDFn              func(ctx context.Context, id uuid.UUID) (*model.ScheduledMessage, error)
	listByChatFn           func(ctx context.Context, chatID, senderID uuid.UUID) ([]model.ScheduledMessage, error)
	updateFn               func(ctx context.Context, id uuid.UUID, content *string, entities []byte, scheduledAt *time.Time) error
	deleteFn               func(ctx context.Context, id, senderID uuid.UUID) error
	markSentFn             func(ctx context.Context, id uuid.UUID) error
	claimAndMarkPendingFn  func(ctx context.Context, limit int) ([]model.ScheduledMessage, error)
}

func (m *mockScheduledMessageStore) Create(ctx context.Context, msg *model.ScheduledMessage) error {
	if m.createFn != nil {
		return m.createFn(ctx, msg)
	}
	msg.ID = uuid.New()
	msg.CreatedAt = time.Now()
	msg.UpdatedAt = time.Now()
	return nil
}

func (m *mockScheduledMessageStore) GetByID(ctx context.Context, id uuid.UUID) (*model.ScheduledMessage, error) {
	if m.getByIDFn != nil {
		return m.getByIDFn(ctx, id)
	}
	return nil, nil
}

func (m *mockScheduledMessageStore) ListByChat(ctx context.Context, chatID, senderID uuid.UUID) ([]model.ScheduledMessage, error) {
	if m.listByChatFn != nil {
		return m.listByChatFn(ctx, chatID, senderID)
	}
	return nil, nil
}

func (m *mockScheduledMessageStore) Update(ctx context.Context, id uuid.UUID, content *string, entities []byte, scheduledAt *time.Time) error {
	if m.updateFn != nil {
		return m.updateFn(ctx, id, content, entities, scheduledAt)
	}
	return nil
}

func (m *mockScheduledMessageStore) Delete(ctx context.Context, id, senderID uuid.UUID) error {
	if m.deleteFn != nil {
		return m.deleteFn(ctx, id, senderID)
	}
	return nil
}

func (m *mockScheduledMessageStore) MarkSent(ctx context.Context, id uuid.UUID) error {
	if m.markSentFn != nil {
		return m.markSentFn(ctx, id)
	}
	return nil
}

func (m *mockScheduledMessageStore) ClaimAndMarkPending(ctx context.Context, limit int) ([]model.ScheduledMessage, error) {
	if m.claimAndMarkPendingFn != nil {
		return m.claimAndMarkPendingFn(ctx, limit)
	}
	return nil, nil
}

// ---------------------------------------------------------------------------
// Mock TenorClient
// ---------------------------------------------------------------------------

type mockTenorClient struct {
	searchFn   func(ctx context.Context, query string, limit int, pos string) ([]model.TenorGIF, string, error)
	trendingFn func(ctx context.Context, limit int, pos string) ([]model.TenorGIF, string, error)
}

func (m *mockTenorClient) Search(ctx context.Context, query string, limit int, pos string) ([]model.TenorGIF, string, error) {
	if m.searchFn != nil {
		return m.searchFn(ctx, query, limit, pos)
	}
	return nil, "", nil
}

func (m *mockTenorClient) Trending(ctx context.Context, limit int, pos string) ([]model.TenorGIF, string, error) {
	if m.trendingFn != nil {
		return m.trendingFn(ctx, limit, pos)
	}
	return nil, "", nil
}
