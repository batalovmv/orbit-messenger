// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package service

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
	"github.com/mst-corp/orbit/services/messaging/internal/store"
)

// ---------------------------------------------------------------------------
// mockChatStore
// ---------------------------------------------------------------------------

type mockChatStore struct {
	listByUserFn           func(ctx context.Context, userID uuid.UUID, cursor string, limit int) ([]model.ChatListItem, string, bool, error)
	getUserChatIDsFn       func(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error)
	isFeatureEnabledFn     func(ctx context.Context, key string) (bool, error)
	getByIDFn              func(ctx context.Context, chatID uuid.UUID) (*model.Chat, error)
	createFn               func(ctx context.Context, chat *model.Chat) error
	getDirectChatFn        func(ctx context.Context, user1, user2 uuid.UUID) (*uuid.UUID, error)
	createDirectFn         func(ctx context.Context, user1, user2 uuid.UUID) (*model.Chat, error)
	getMembersFn           func(ctx context.Context, chatID uuid.UUID, cursor string, limit int) ([]model.ChatMember, string, bool, error)
	searchMembersFn        func(ctx context.Context, chatID uuid.UUID, query string, limit int) ([]model.ChatMember, error)
	getMemberIDsFn         func(ctx context.Context, chatID uuid.UUID) ([]string, error)
	addMemberFn            func(ctx context.Context, chatID, userID uuid.UUID, role string) error
	addMembersFn           func(ctx context.Context, chatID uuid.UUID, userIDs []uuid.UUID, role string) error
	isMemberFn             func(ctx context.Context, chatID, userID uuid.UUID) (bool, string, error)
	getMemberFn            func(ctx context.Context, chatID, userID uuid.UUID) (*model.ChatMember, error)
	getAdminsFn            func(ctx context.Context, chatID uuid.UUID) ([]model.ChatMember, error)
	updateChatFn           func(ctx context.Context, chatID uuid.UUID, name, description, avatarURL *string) error
	deleteChatFn           func(ctx context.Context, chatID uuid.UUID) error
	removeMemberFn         func(ctx context.Context, chatID, userID uuid.UUID) error
	updateMemberRoleFn     func(ctx context.Context, chatID, userID uuid.UUID, role string, perms int64, customTitle *string) error
	updateDefaultPermsFn   func(ctx context.Context, chatID uuid.UUID, perms int64) error
	updateMemberPermsFn    func(ctx context.Context, chatID, userID uuid.UUID, perms int64) error
	updateMemberPrefsFn    func(ctx context.Context, chatID, userID uuid.UUID, prefs model.ChatMemberPreferences) (*model.ChatMember, error)
	setSlowModeFn          func(ctx context.Context, chatID uuid.UUID, seconds int) error
	setSignaturesFn        func(ctx context.Context, chatID uuid.UUID, enabled bool) error
	getContactIDsFn        func(ctx context.Context, userID uuid.UUID) ([]string, error)
	getOrCreateSavedChatFn func(ctx context.Context, userID uuid.UUID) (*model.Chat, error)

	// Welcome flow (mig 069).
	joinUserToDefaultsFn         func(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error)
	setChatDefaultStatusFn       func(ctx context.Context, chatID uuid.UUID, isDefault bool, joinOrder int) error
	backfillDefaultMembershipsFn func(ctx context.Context) ([]store.DefaultBackfillInsert, error)
	previewDefaultMembershipsFn  func(ctx context.Context) (*store.DefaultMembershipPreview, error)

	// Smart Notifications classifier hint (per-user role tag).
	getUserClassifierHintFn func(ctx context.Context, userID uuid.UUID) (string, error)
}

func (m *mockChatStore) ListByUser(ctx context.Context, userID uuid.UUID, cursor string, limit int) ([]model.ChatListItem, string, bool, error) {
	if m.listByUserFn != nil {
		return m.listByUserFn(ctx, userID, cursor, limit)
	}
	return nil, "", false, nil
}
func (m *mockChatStore) GetUserChatIDs(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error) {
	if m.getUserChatIDsFn != nil {
		return m.getUserChatIDsFn(ctx, userID)
	}
	return nil, nil
}
func (m *mockChatStore) IsFeatureEnabled(ctx context.Context, key string) (bool, error) {
	if m.isFeatureEnabledFn != nil {
		return m.isFeatureEnabledFn(ctx, key)
	}
	return false, nil
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
func (m *mockChatStore) GetDirectChat(ctx context.Context, u1, u2 uuid.UUID) (*uuid.UUID, error) {
	if m.getDirectChatFn != nil {
		return m.getDirectChatFn(ctx, u1, u2)
	}
	return nil, nil
}
func (m *mockChatStore) CreateDirectChat(ctx context.Context, u1, u2 uuid.UUID) (*model.Chat, error) {
	if m.createDirectFn != nil {
		return m.createDirectFn(ctx, u1, u2)
	}
	return nil, nil
}
func (m *mockChatStore) GetMembers(ctx context.Context, chatID uuid.UUID, cursor string, limit int) ([]model.ChatMember, string, bool, error) {
	if m.getMembersFn != nil {
		return m.getMembersFn(ctx, chatID, cursor, limit)
	}
	return nil, "", false, nil
}
func (m *mockChatStore) SearchMembers(ctx context.Context, chatID uuid.UUID, query string, limit int) ([]model.ChatMember, error) {
	if m.searchMembersFn != nil {
		return m.searchMembersFn(ctx, chatID, query, limit)
	}
	return nil, nil
}
func (m *mockChatStore) GetMemberIDs(ctx context.Context, chatID uuid.UUID) ([]string, error) {
	if m.getMemberIDsFn != nil {
		return m.getMemberIDsFn(ctx, chatID)
	}
	return nil, nil
}
func (m *mockChatStore) GetUserClassifierHint(ctx context.Context, userID uuid.UUID) (string, error) {
	if m.getUserClassifierHintFn != nil {
		return m.getUserClassifierHintFn(ctx, userID)
	}
	return "member", nil
}
func (m *mockChatStore) AddMember(ctx context.Context, chatID, userID uuid.UUID, role string) error {
	if m.addMemberFn != nil {
		return m.addMemberFn(ctx, chatID, userID, role)
	}
	return nil
}
func (m *mockChatStore) AddMembers(ctx context.Context, chatID uuid.UUID, userIDs []uuid.UUID, role string) error {
	if m.addMembersFn != nil {
		return m.addMembersFn(ctx, chatID, userIDs, role)
	}
	return nil
}
func (m *mockChatStore) IsMember(ctx context.Context, chatID, userID uuid.UUID) (bool, string, error) {
	if m.isMemberFn != nil {
		return m.isMemberFn(ctx, chatID, userID)
	}
	return false, "", nil
}
func (m *mockChatStore) GetMember(ctx context.Context, chatID, userID uuid.UUID) (*model.ChatMember, error) {
	if m.getMemberFn != nil {
		return m.getMemberFn(ctx, chatID, userID)
	}
	return nil, nil
}
func (m *mockChatStore) GetAdmins(ctx context.Context, chatID uuid.UUID) ([]model.ChatMember, error) {
	if m.getAdminsFn != nil {
		return m.getAdminsFn(ctx, chatID)
	}
	return nil, nil
}
func (m *mockChatStore) UpdateChat(ctx context.Context, chatID uuid.UUID, name, description, avatarURL *string) error {
	if m.updateChatFn != nil {
		return m.updateChatFn(ctx, chatID, name, description, avatarURL)
	}
	return nil
}
func (m *mockChatStore) DeleteChat(ctx context.Context, chatID uuid.UUID) error {
	if m.deleteChatFn != nil {
		return m.deleteChatFn(ctx, chatID)
	}
	return nil
}
func (m *mockChatStore) RemoveMember(ctx context.Context, chatID, userID uuid.UUID) error {
	if m.removeMemberFn != nil {
		return m.removeMemberFn(ctx, chatID, userID)
	}
	return nil
}
func (m *mockChatStore) UpdateMemberRole(ctx context.Context, chatID, userID uuid.UUID, role string, perms int64, customTitle *string) error {
	if m.updateMemberRoleFn != nil {
		return m.updateMemberRoleFn(ctx, chatID, userID, role, perms, customTitle)
	}
	return nil
}
func (m *mockChatStore) UpdateDefaultPermissions(ctx context.Context, chatID uuid.UUID, perms int64) error {
	if m.updateDefaultPermsFn != nil {
		return m.updateDefaultPermsFn(ctx, chatID, perms)
	}
	return nil
}
func (m *mockChatStore) UpdateMemberPermissions(ctx context.Context, chatID, userID uuid.UUID, perms int64) error {
	if m.updateMemberPermsFn != nil {
		return m.updateMemberPermsFn(ctx, chatID, userID, perms)
	}
	return nil
}
func (m *mockChatStore) UpdateMemberPreferences(ctx context.Context, chatID, userID uuid.UUID, prefs model.ChatMemberPreferences) (*model.ChatMember, error) {
	if m.updateMemberPrefsFn != nil {
		return m.updateMemberPrefsFn(ctx, chatID, userID, prefs)
	}
	return nil, nil
}
func (m *mockChatStore) SetSlowMode(ctx context.Context, chatID uuid.UUID, seconds int) error {
	if m.setSlowModeFn != nil {
		return m.setSlowModeFn(ctx, chatID, seconds)
	}
	return nil
}
func (m *mockChatStore) SetSignatures(ctx context.Context, chatID uuid.UUID, enabled bool) error {
	if m.setSignaturesFn != nil {
		return m.setSignaturesFn(ctx, chatID, enabled)
	}
	return nil
}

func (m *mockChatStore) SetIsProtected(ctx context.Context, chatID uuid.UUID, enabled bool) error {
	return nil
}

func (m *mockChatStore) ClearChatPhoto(ctx context.Context, chatID uuid.UUID) error {
	return nil
}
func (m *mockChatStore) GetContactIDs(ctx context.Context, userID uuid.UUID) ([]string, error) {
	if m.getContactIDsFn != nil {
		return m.getContactIDsFn(ctx, userID)
	}
	return nil, nil
}

func (m *mockChatStore) ListAll(ctx context.Context, limit int) ([]model.Chat, error) {
	return nil, nil
}

func (m *mockChatStore) ListAllPaginated(ctx context.Context, cursor string, limit int) ([]model.Chat, string, bool, error) {
	return nil, "", false, nil
}

func (m *mockChatStore) GetCommonChats(ctx context.Context, userA, userB uuid.UUID, limit int) ([]model.Chat, error) {
	return nil, nil
}

func (m *mockChatStore) GetOrCreateSavedChat(ctx context.Context, userID uuid.UUID) (*model.Chat, error) {
	if m.getOrCreateSavedChatFn != nil {
		return m.getOrCreateSavedChatFn(ctx, userID)
	}
	return nil, nil
}

func (m *mockChatStore) SaveDraft(ctx context.Context, chatID, userID uuid.UUID, text string) error {
	return nil
}

func (m *mockChatStore) ClearDraft(ctx context.Context, chatID, userID uuid.UUID) error {
	return nil
}

func (m *mockChatStore) ExportByUserID(ctx context.Context, userID uuid.UUID, writeRow func([]byte) error) error {
	return nil
}

// Welcome-flow methods (mig 069). Tests opt in by setting the *Fn field;
// otherwise these no-op so unrelated tests stay unaffected.
func (m *mockChatStore) JoinUserToDefaults(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error) {
	if m.joinUserToDefaultsFn != nil {
		return m.joinUserToDefaultsFn(ctx, userID)
	}
	return nil, nil
}
func (m *mockChatStore) SetChatDefaultStatus(ctx context.Context, chatID uuid.UUID, isDefault bool, joinOrder int) error {
	if m.setChatDefaultStatusFn != nil {
		return m.setChatDefaultStatusFn(ctx, chatID, isDefault, joinOrder)
	}
	return nil
}
func (m *mockChatStore) BackfillDefaultMemberships(ctx context.Context) ([]store.DefaultBackfillInsert, error) {
	if m.backfillDefaultMembershipsFn != nil {
		return m.backfillDefaultMembershipsFn(ctx)
	}
	return nil, nil
}
func (m *mockChatStore) PreviewDefaultMemberships(ctx context.Context) (*store.DefaultMembershipPreview, error) {
	if m.previewDefaultMembershipsFn != nil {
		return m.previewDefaultMembershipsFn(ctx)
	}
	return &store.DefaultMembershipPreview{}, nil
}

// ---------------------------------------------------------------------------
// mockMessageStore
// ---------------------------------------------------------------------------

type mockMessageStore struct {
	createFn               func(ctx context.Context, msg *model.Message) error
	getByIDFn              func(ctx context.Context, id uuid.UUID) (*model.Message, error)
	getByIDsFn             func(ctx context.Context, ids []uuid.UUID) ([]model.Message, error)
	listByChatFn           func(ctx context.Context, chatID uuid.UUID, cursor string, limit int) ([]model.Message, string, bool, error)
	findByChatAndDateFn    func(ctx context.Context, chatID uuid.UUID, date time.Time, limit int) ([]model.Message, string, bool, error)
	updateFn               func(ctx context.Context, msg *model.Message) error
	softDeleteFn           func(ctx context.Context, id uuid.UUID) error
	markOneTimeViewedFn    func(ctx context.Context, msgID, userID uuid.UUID) (*model.Message, error)
	softDeleteAuthorizedFn func(ctx context.Context, msgID, userID uuid.UUID) (uuid.UUID, int, error)
	listPinnedFn           func(ctx context.Context, chatID uuid.UUID) ([]model.Message, error)
	pinFn                  func(ctx context.Context, chatID, msgID uuid.UUID) error
	unpinFn                func(ctx context.Context, chatID, msgID uuid.UUID) error
	unpinAllFn             func(ctx context.Context, chatID uuid.UUID) error
	updateReadPointerFn    func(ctx context.Context, chatID, userID, lastReadMsgID uuid.UUID) error
	getReadStateFn         func(ctx context.Context, chatID, userID uuid.UUID) (int64, int64, error)
	createForwardedFn      func(ctx context.Context, msgs []model.Message) ([]model.Message, error)
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
func (m *mockMessageStore) GetByIDs(ctx context.Context, ids []uuid.UUID) ([]model.Message, error) {
	if m.getByIDsFn != nil {
		return m.getByIDsFn(ctx, ids)
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
func (m *mockMessageStore) MarkOneTimeViewed(ctx context.Context, msgID, userID uuid.UUID) (*model.Message, error) {
	if m.markOneTimeViewedFn != nil {
		return m.markOneTimeViewedFn(ctx, msgID, userID)
	}
	return nil, nil
}
func (m *mockMessageStore) SoftDeleteAuthorized(ctx context.Context, msgID, userID uuid.UUID) (uuid.UUID, int, error) {
	if m.softDeleteAuthorizedFn != nil {
		return m.softDeleteAuthorizedFn(ctx, msgID, userID)
	}
	return uuid.Nil, 0, nil
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

func (m *mockMessageStore) GetReadState(ctx context.Context, chatID, userID uuid.UUID) (int64, int64, error) {
	if m.getReadStateFn != nil {
		return m.getReadStateFn(ctx, chatID, userID)
	}
	return 0, 0, nil
}
func (m *mockMessageStore) CreateForwarded(ctx context.Context, msgs []model.Message) ([]model.Message, error) {
	if m.createForwardedFn != nil {
		return m.createForwardedFn(ctx, msgs)
	}
	return nil, nil
}

func (m *mockMessageStore) CreateWithMedia(ctx context.Context, msg *model.Message, mediaIDs []uuid.UUID, isSpoiler bool) error {
	msg.ID = uuid.New()
	msg.SequenceNumber = 1
	return nil
}

func (m *mockMessageStore) GetMediaByMessageIDs(ctx context.Context, messageIDs []uuid.UUID) (map[uuid.UUID][]model.MediaAttachment, error) {
	return nil, nil
}

func (m *mockMessageStore) CopyMediaLinks(ctx context.Context, newMessageID uuid.UUID, mediaIDs []string) error {
	return nil
}

func (m *mockMessageStore) ListSharedMedia(ctx context.Context, chatID uuid.UUID, mediaType string, cursor string, limit int) ([]model.SharedMediaItem, string, bool, error) {
	return nil, "", false, nil
}

func (m *mockMessageStore) ExportByChatID(ctx context.Context, chatID uuid.UUID, writeRow func([]byte) error) error {
	return nil
}

// ---------------------------------------------------------------------------
// mockInviteStore
// ---------------------------------------------------------------------------

type mockInviteStore struct {
	createFn                  func(ctx context.Context, link *model.InviteLink) error
	getByHashFn               func(ctx context.Context, hash string) (*model.InviteLink, error)
	getByIDFn                 func(ctx context.Context, linkID uuid.UUID) (*model.InviteLink, error)
	listByChatIDFn            func(ctx context.Context, chatID uuid.UUID) ([]model.InviteLink, error)
	updateFn                  func(ctx context.Context, linkID uuid.UUID, title *string, expireAt *time.Time, usageLimit *int, requiresApproval *bool) error
	revokeFn                  func(ctx context.Context, linkID uuid.UUID) error
	incrementUsageFn          func(ctx context.Context, linkID uuid.UUID) error
	createJoinRequestFn       func(ctx context.Context, req *model.JoinRequest) error
	listJoinRequestsFn        func(ctx context.Context, chatID uuid.UUID) ([]model.JoinRequest, error)
	updateJoinRequestStatusFn func(ctx context.Context, chatID, userID uuid.UUID, status string, reviewedBy uuid.UUID) error
	deleteJoinRequestFn       func(ctx context.Context, chatID, userID uuid.UUID) error
}

func (m *mockInviteStore) Create(ctx context.Context, link *model.InviteLink) error {
	if m.createFn != nil {
		return m.createFn(ctx, link)
	}
	link.ID = uuid.New()
	return nil
}
func (m *mockInviteStore) GetByHash(ctx context.Context, hash string) (*model.InviteLink, error) {
	if m.getByHashFn != nil {
		return m.getByHashFn(ctx, hash)
	}
	return nil, nil
}
func (m *mockInviteStore) GetByID(ctx context.Context, linkID uuid.UUID) (*model.InviteLink, error) {
	if m.getByIDFn != nil {
		return m.getByIDFn(ctx, linkID)
	}
	return nil, nil
}
func (m *mockInviteStore) ListByChatID(ctx context.Context, chatID uuid.UUID) ([]model.InviteLink, error) {
	if m.listByChatIDFn != nil {
		return m.listByChatIDFn(ctx, chatID)
	}
	return nil, nil
}
func (m *mockInviteStore) Update(ctx context.Context, linkID uuid.UUID, title *string, expireAt *time.Time, usageLimit *int, requiresApproval *bool) error {
	if m.updateFn != nil {
		return m.updateFn(ctx, linkID, title, expireAt, usageLimit, requiresApproval)
	}
	return nil
}
func (m *mockInviteStore) Revoke(ctx context.Context, linkID uuid.UUID) error {
	if m.revokeFn != nil {
		return m.revokeFn(ctx, linkID)
	}
	return nil
}
func (m *mockInviteStore) IncrementUsage(ctx context.Context, linkID uuid.UUID) error {
	if m.incrementUsageFn != nil {
		return m.incrementUsageFn(ctx, linkID)
	}
	return nil
}
func (m *mockInviteStore) DecrementUsage(ctx context.Context, linkID uuid.UUID) error {
	return nil
}
func (m *mockInviteStore) CreateJoinRequest(ctx context.Context, req *model.JoinRequest) error {
	if m.createJoinRequestFn != nil {
		return m.createJoinRequestFn(ctx, req)
	}
	return nil
}
func (m *mockInviteStore) ListJoinRequests(ctx context.Context, chatID uuid.UUID) ([]model.JoinRequest, error) {
	if m.listJoinRequestsFn != nil {
		return m.listJoinRequestsFn(ctx, chatID)
	}
	return nil, nil
}
func (m *mockInviteStore) UpdateJoinRequestStatus(ctx context.Context, chatID, userID uuid.UUID, status string, reviewedBy uuid.UUID) error {
	if m.updateJoinRequestStatusFn != nil {
		return m.updateJoinRequestStatusFn(ctx, chatID, userID, status, reviewedBy)
	}
	return nil
}
func (m *mockInviteStore) DeleteJoinRequest(ctx context.Context, chatID, userID uuid.UUID) error {
	if m.deleteJoinRequestFn != nil {
		return m.deleteJoinRequestFn(ctx, chatID, userID)
	}
	return nil
}
