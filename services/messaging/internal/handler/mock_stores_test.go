package handler

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
)

// ---------------------------------------------------------------------------
// Mock ChatStore
// ---------------------------------------------------------------------------

type mockChatStore struct {
	listByUserFn      func(ctx context.Context, userID uuid.UUID, cursor string, limit int) ([]model.ChatListItem, string, bool, error)
	getByIDFn         func(ctx context.Context, chatID uuid.UUID) (*model.Chat, error)
	createFn          func(ctx context.Context, chat *model.Chat) error
	getDirectChatFn   func(ctx context.Context, user1, user2 uuid.UUID) (*uuid.UUID, error)
	createDirectFn    func(ctx context.Context, user1, user2 uuid.UUID) (*model.Chat, error)
	getMembersFn      func(ctx context.Context, chatID uuid.UUID, cursor string, limit int) ([]model.ChatMember, string, bool, error)
	getMemberIDsFn    func(ctx context.Context, chatID uuid.UUID) ([]string, error)
	addMemberFn       func(ctx context.Context, chatID, userID uuid.UUID, role string) error
	isMemberFn        func(ctx context.Context, chatID, userID uuid.UUID) (bool, string, error)
}

func (m *mockChatStore) ListByUser(ctx context.Context, userID uuid.UUID, cursor string, limit int) ([]model.ChatListItem, string, bool, error) {
	if m.listByUserFn != nil {
		return m.listByUserFn(ctx, userID, cursor, limit)
	}
	return nil, "", false, nil
}

func (m *mockChatStore) GetByID(ctx context.Context, chatID uuid.UUID) (*model.Chat, error) {
	if m.getByIDFn != nil {
		return m.getByIDFn(ctx, chatID)
	}
	return nil, nil
}

func (m *mockChatStore) Create(ctx context.Context, chat *model.Chat) error {
	if m.createFn != nil {
		return m.createFn(ctx, chat)
	}
	chat.ID = uuid.New()
	chat.CreatedAt = time.Now()
	chat.UpdatedAt = time.Now()
	return nil
}

func (m *mockChatStore) GetDirectChat(ctx context.Context, user1, user2 uuid.UUID) (*uuid.UUID, error) {
	if m.getDirectChatFn != nil {
		return m.getDirectChatFn(ctx, user1, user2)
	}
	return nil, nil
}

func (m *mockChatStore) CreateDirectChat(ctx context.Context, user1, user2 uuid.UUID) (*model.Chat, error) {
	if m.createDirectFn != nil {
		return m.createDirectFn(ctx, user1, user2)
	}
	id := uuid.New()
	return &model.Chat{ID: id, Type: "direct", CreatedAt: time.Now(), UpdatedAt: time.Now()}, nil
}

func (m *mockChatStore) GetMembers(ctx context.Context, chatID uuid.UUID, cursor string, limit int) ([]model.ChatMember, string, bool, error) {
	if m.getMembersFn != nil {
		return m.getMembersFn(ctx, chatID, cursor, limit)
	}
	return nil, "", false, nil
}

func (m *mockChatStore) GetMemberIDs(ctx context.Context, chatID uuid.UUID) ([]string, error) {
	if m.getMemberIDsFn != nil {
		return m.getMemberIDsFn(ctx, chatID)
	}
	return nil, nil
}

func (m *mockChatStore) AddMember(ctx context.Context, chatID, userID uuid.UUID, role string) error {
	if m.addMemberFn != nil {
		return m.addMemberFn(ctx, chatID, userID, role)
	}
	return nil
}

func (m *mockChatStore) IsMember(ctx context.Context, chatID, userID uuid.UUID) (bool, string, error) {
	if m.isMemberFn != nil {
		return m.isMemberFn(ctx, chatID, userID)
	}
	return true, "member", nil
}

// ---------------------------------------------------------------------------
// Mock MessageStore
// ---------------------------------------------------------------------------

type mockMessageStore struct {
	createFn             func(ctx context.Context, msg *model.Message) error
	getByIDFn            func(ctx context.Context, id uuid.UUID) (*model.Message, error)
	listByChatFn         func(ctx context.Context, chatID uuid.UUID, cursor string, limit int) ([]model.Message, string, bool, error)
	findByChatAndDateFn  func(ctx context.Context, chatID uuid.UUID, date time.Time, limit int) ([]model.Message, string, bool, error)
	updateFn             func(ctx context.Context, msg *model.Message) error
	softDeleteFn         func(ctx context.Context, id uuid.UUID) error
	listPinnedFn         func(ctx context.Context, chatID uuid.UUID) ([]model.Message, error)
	pinFn                func(ctx context.Context, chatID, msgID uuid.UUID) error
	unpinFn              func(ctx context.Context, chatID, msgID uuid.UUID) error
	unpinAllFn           func(ctx context.Context, chatID uuid.UUID) error
	updateReadPointerFn  func(ctx context.Context, chatID, userID, lastReadMsgID uuid.UUID) error
	createForwardedFn    func(ctx context.Context, msgs []model.Message) ([]model.Message, error)
}

func (m *mockMessageStore) Create(ctx context.Context, msg *model.Message) error {
	if m.createFn != nil {
		return m.createFn(ctx, msg)
	}
	msg.ID = uuid.New()
	msg.CreatedAt = time.Now()
	return nil
}

func (m *mockMessageStore) GetByID(ctx context.Context, id uuid.UUID) (*model.Message, error) {
	if m.getByIDFn != nil {
		return m.getByIDFn(ctx, id)
	}
	return nil, nil
}

func (m *mockMessageStore) ListByChat(ctx context.Context, chatID uuid.UUID, cursor string, limit int) ([]model.Message, string, bool, error) {
	if m.listByChatFn != nil {
		return m.listByChatFn(ctx, chatID, cursor, limit)
	}
	return nil, "", false, nil
}

func (m *mockMessageStore) FindByChatAndDate(ctx context.Context, chatID uuid.UUID, date time.Time, limit int) ([]model.Message, string, bool, error) {
	if m.findByChatAndDateFn != nil {
		return m.findByChatAndDateFn(ctx, chatID, date, limit)
	}
	return nil, "", false, nil
}

func (m *mockMessageStore) Update(ctx context.Context, msg *model.Message) error {
	if m.updateFn != nil {
		return m.updateFn(ctx, msg)
	}
	return nil
}

func (m *mockMessageStore) SoftDelete(ctx context.Context, id uuid.UUID) error {
	if m.softDeleteFn != nil {
		return m.softDeleteFn(ctx, id)
	}
	return nil
}

func (m *mockMessageStore) ListPinned(ctx context.Context, chatID uuid.UUID) ([]model.Message, error) {
	if m.listPinnedFn != nil {
		return m.listPinnedFn(ctx, chatID)
	}
	return nil, nil
}

func (m *mockMessageStore) Pin(ctx context.Context, chatID, msgID uuid.UUID) error {
	if m.pinFn != nil {
		return m.pinFn(ctx, chatID, msgID)
	}
	return nil
}

func (m *mockMessageStore) Unpin(ctx context.Context, chatID, msgID uuid.UUID) error {
	if m.unpinFn != nil {
		return m.unpinFn(ctx, chatID, msgID)
	}
	return nil
}

func (m *mockMessageStore) UnpinAll(ctx context.Context, chatID uuid.UUID) error {
	if m.unpinAllFn != nil {
		return m.unpinAllFn(ctx, chatID)
	}
	return nil
}

func (m *mockMessageStore) UpdateReadPointer(ctx context.Context, chatID, userID, lastReadMsgID uuid.UUID) error {
	if m.updateReadPointerFn != nil {
		return m.updateReadPointerFn(ctx, chatID, userID, lastReadMsgID)
	}
	return nil
}

func (m *mockMessageStore) CreateForwarded(ctx context.Context, msgs []model.Message) ([]model.Message, error) {
	if m.createForwardedFn != nil {
		return m.createForwardedFn(ctx, msgs)
	}
	result := make([]model.Message, len(msgs))
	for i, msg := range msgs {
		msg.ID = uuid.New()
		msg.CreatedAt = time.Now()
		msg.IsForwarded = true
		result[i] = msg
	}
	return result, nil
}

// ---------------------------------------------------------------------------
// Mock UserStore
// ---------------------------------------------------------------------------

type mockUserStore struct {
	getByIDFn func(ctx context.Context, id uuid.UUID) (*model.User, error)
	updateFn  func(ctx context.Context, u *model.User) error
	searchFn  func(ctx context.Context, query string, limit int) ([]model.User, error)
}

func (m *mockUserStore) GetByID(ctx context.Context, id uuid.UUID) (*model.User, error) {
	if m.getByIDFn != nil {
		return m.getByIDFn(ctx, id)
	}
	return nil, nil
}

func (m *mockUserStore) Update(ctx context.Context, u *model.User) error {
	if m.updateFn != nil {
		return m.updateFn(ctx, u)
	}
	return nil
}

func (m *mockUserStore) Search(ctx context.Context, query string, limit int) ([]model.User, error) {
	if m.searchFn != nil {
		return m.searchFn(ctx, query, limit)
	}
	return nil, nil
}
