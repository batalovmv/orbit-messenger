// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/permissions"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
	"github.com/mst-corp/orbit/services/messaging/internal/store"
)

type InviteService struct {
	invites store.InviteStore
	chats   store.ChatStore
	nats    Publisher
}

func NewInviteService(invites store.InviteStore, chats store.ChatStore, nats Publisher) *InviteService {
	return &InviteService{
		invites: invites,
		chats:   chats,
		nats:    nats,
	}
}

func generateInviteHash() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate invite hash: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// CreateInviteLink creates a new invite link for the given chat.
// Caller must be a member with CanInviteViaLink permission.
func (s *InviteService) CreateInviteLink(
	ctx context.Context,
	chatID, callerID uuid.UUID,
	title *string,
	expireAt *time.Time,
	usageLimit int,
	requiresApproval bool,
) (*model.InviteLink, error) {
	member, err := s.chats.GetMember(ctx, chatID, callerID)
	if err != nil {
		return nil, fmt.Errorf("check membership: %w", err)
	}
	if member == nil {
		return nil, apperror.Forbidden("not a member of this chat")
	}
	chat, err := s.chats.GetByID(ctx, chatID)
	if err != nil || chat == nil {
		return nil, apperror.NotFound("chat not found")
	}
	if !permissions.CanPerform(member.Role, chat.Type, member.Permissions, chat.DefaultPermissions, permissions.CanInviteViaLink) {
		return nil, apperror.Forbidden("insufficient permissions to create invite links")
	}

	hash, err := generateInviteHash()
	if err != nil {
		slog.ErrorContext(ctx, "failed to generate invite hash", "error", err)
		return nil, fmt.Errorf("create invite link: %w", err)
	}

	link := &model.InviteLink{
		ChatID:    chatID,
		CreatorID: callerID,
		Hash:             hash,
		Title:            title,
		ExpireAt:         expireAt,
		UsageLimit:       usageLimit,
		UsageCount:       0,
		RequiresApproval: requiresApproval,
		IsRevoked:        false,
		CreatedAt:        time.Now(),
	}

	if err := s.invites.Create(ctx, link); err != nil {
		return nil, fmt.Errorf("store invite link: %w", err)
	}

	return link, nil
}

// ListInviteLinks returns all invite links for the given chat.
// Caller must be an admin or owner.
func (s *InviteService) ListInviteLinks(ctx context.Context, chatID, callerID uuid.UUID) ([]model.InviteLink, error) {
	_, role, err := s.chats.IsMember(ctx, chatID, callerID)
	if err != nil {
		return nil, fmt.Errorf("check membership: %w", err)
	}
	if !permissions.IsAdminOrOwner(role) {
		return nil, apperror.Forbidden("only admins and owners can list invite links")
	}

	links, err := s.invites.ListByChatID(ctx, chatID)
	if err != nil {
		return nil, fmt.Errorf("list invite links: %w", err)
	}

	return links, nil
}

// EditInviteLink updates mutable fields of an invite link.
// Caller must be the link creator or an admin/owner of the chat.
func (s *InviteService) EditInviteLink(
	ctx context.Context,
	linkID, callerID uuid.UUID,
	title *string,
	expireAt *time.Time,
	usageLimit *int,
	requiresApproval *bool,
) error {
	link, err := s.invites.GetByID(ctx, linkID)
	if err != nil {
		return fmt.Errorf("get invite link: %w", err)
	}
	if link == nil {
		return apperror.NotFound("invite link not found")
	}

	if link.CreatorID != callerID {
		_, role, err := s.chats.IsMember(ctx, link.ChatID, callerID)
		if err != nil {
			return fmt.Errorf("check membership: %w", err)
		}
		if !permissions.IsAdminOrOwner(role) {
			return apperror.Forbidden("only the link creator or an admin can edit this invite link")
		}
	}

	if err := s.invites.Update(ctx, linkID, title, expireAt, usageLimit, requiresApproval); err != nil {
		return fmt.Errorf("update invite link: %w", err)
	}

	return nil
}

// RevokeInviteLink marks an invite link as revoked.
// Caller must be the link creator or an admin/owner of the chat.
func (s *InviteService) RevokeInviteLink(ctx context.Context, linkID, callerID uuid.UUID) error {
	link, err := s.invites.GetByID(ctx, linkID)
	if err != nil {
		return fmt.Errorf("get invite link: %w", err)
	}
	if link == nil {
		return apperror.NotFound("invite link not found")
	}

	if link.CreatorID != callerID {
		_, role, err := s.chats.IsMember(ctx, link.ChatID, callerID)
		if err != nil {
			return fmt.Errorf("check membership: %w", err)
		}
		if !permissions.IsAdminOrOwner(role) {
			return apperror.Forbidden("only the link creator or an admin can revoke this invite link")
		}
	}

	if err := s.invites.Revoke(ctx, linkID); err != nil {
		return fmt.Errorf("revoke invite link: %w", err)
	}

	return nil
}

// GetInviteInfo returns public info about the chat associated with the invite link.
// No authentication required.
func (s *InviteService) GetInviteInfo(ctx context.Context, hash string) (map[string]interface{}, error) {
	link, err := s.invites.GetByHash(ctx, hash)
	if err != nil {
		return nil, fmt.Errorf("get invite link by hash: %w", err)
	}
	if link == nil || link.IsRevoked {
		return nil, apperror.NotFound("invite link not found or revoked")
	}
	if link.ExpireAt != nil && time.Now().After(*link.ExpireAt) {
		return nil, apperror.NotFound("invite link has expired")
	}

	chat, err := s.chats.GetByID(ctx, link.ChatID)
	if err != nil {
		return nil, fmt.Errorf("get chat: %w", err)
	}
	if chat == nil {
		return nil, apperror.NotFound("chat not found")
	}

	memberIDs, err := s.chats.GetMemberIDs(ctx, link.ChatID)
	if err != nil {
		return nil, fmt.Errorf("get member count: %w", err)
	}

	return map[string]interface{}{
		"chat_id":      chat.ID,
		"chat_name":    chat.Name,
		"member_count": len(memberIDs),
	}, nil
}

// JoinByInvite adds the user to the chat via the invite link, or creates a join request
// if the link requires approval.
func (s *InviteService) JoinByInvite(ctx context.Context, hash string, userID uuid.UUID) (map[string]interface{}, error) {
	link, err := s.invites.GetByHash(ctx, hash)
	if err != nil {
		return nil, fmt.Errorf("get invite link: %w", err)
	}
	if link == nil || link.IsRevoked {
		return nil, apperror.NotFound("invite link not found or revoked")
	}
	if link.ExpireAt != nil && time.Now().After(*link.ExpireAt) {
		return nil, apperror.NotFound("invite link has expired")
	}
	// Usage limit is enforced atomically by IncrementUsage SQL below (WHERE usage_count < usage_limit).
	// No Go-level early-return here to avoid TOCTOU race on concurrent requests.

	isMember, _, err := s.chats.IsMember(ctx, link.ChatID, userID)
	if err != nil {
		return nil, fmt.Errorf("check membership: %w", err)
	}
	if isMember {
		return map[string]interface{}{"status": "already_member"}, nil
	}

	if link.RequiresApproval {
		req := &model.JoinRequest{
			ChatID: link.ChatID,
			UserID: userID,
			Status: "pending",
		}
		if err := s.invites.CreateJoinRequest(ctx, req); err != nil {
			return nil, fmt.Errorf("create join request: %w", err)
		}
		return map[string]interface{}{"status": "pending"}, nil
	}

	// Atomically claim the slot FIRST — prevents over-admission on concurrent requests.
	if err := s.invites.IncrementUsage(ctx, link.ID); err != nil {
		return nil, apperror.BadRequest("Invite link usage limit reached or link invalid")
	}

	if err := s.chats.AddMember(ctx, link.ChatID, userID, "member"); err != nil {
		// Rollback usage count on AddMember failure to prevent slot leak.
		if rbErr := s.invites.DecrementUsage(ctx, link.ID); rbErr != nil {
			slog.WarnContext(ctx, "failed to rollback invite usage", "link_id", link.ID, "error", rbErr)
		}
		return nil, fmt.Errorf("add member: %w", err)
	}

	memberIDs, err := s.chats.GetMemberIDs(ctx, link.ChatID)
	if err != nil {
		slog.WarnContext(ctx, "failed to get member IDs for NATS publish", "chat_id", link.ChatID, "error", err)
	} else {
		s.nats.Publish(
			fmt.Sprintf("orbit.chat.%s.member.added", link.ChatID),
			"chat_member_added",
			map[string]interface{}{
				"chat_id": link.ChatID,
				"user_id": userID,
			},
			memberIDs,
		)
	}

	return map[string]interface{}{"status": "joined"}, nil
}

// ListJoinRequests returns pending join requests for the given chat.
// Caller must be an admin or owner.
func (s *InviteService) ListJoinRequests(ctx context.Context, chatID, callerID uuid.UUID) ([]model.JoinRequest, error) {
	_, role, err := s.chats.IsMember(ctx, chatID, callerID)
	if err != nil {
		return nil, fmt.Errorf("check membership: %w", err)
	}
	if !permissions.IsAdminOrOwner(role) {
		return nil, apperror.Forbidden("only admins and owners can view join requests")
	}

	requests, err := s.invites.ListJoinRequests(ctx, chatID)
	if err != nil {
		return nil, fmt.Errorf("list join requests: %w", err)
	}

	return requests, nil
}

// ApproveJoinRequest approves a pending join request, adding the user to the chat.
// Caller must be an admin or owner.
func (s *InviteService) ApproveJoinRequest(ctx context.Context, chatID, callerID, targetUserID uuid.UUID) error {
	_, role, err := s.chats.IsMember(ctx, chatID, callerID)
	if err != nil {
		return fmt.Errorf("check membership: %w", err)
	}
	if !permissions.IsAdminOrOwner(role) {
		return apperror.Forbidden("only admins and owners can approve join requests")
	}

	if err := s.invites.UpdateJoinRequestStatus(ctx, chatID, targetUserID, "approved", callerID); err != nil {
		return fmt.Errorf("update join request status: %w", err)
	}

	if err := s.chats.AddMember(ctx, chatID, targetUserID, "member"); err != nil {
		// Rollback: revert status to "pending" so the user can be re-approved.
		if rbErr := s.invites.UpdateJoinRequestStatus(ctx, chatID, targetUserID, "pending", callerID); rbErr != nil {
			slog.WarnContext(ctx, "failed to rollback join request status after AddMember failure",
				"chat_id", chatID, "user_id", targetUserID, "error", rbErr)
		}
		return fmt.Errorf("add member: %w", err)
	}

	memberIDs, err := s.chats.GetMemberIDs(ctx, chatID)
	if err != nil {
		slog.WarnContext(ctx, "failed to get member IDs for NATS publish", "chat_id", chatID, "error", err)
	} else {
		s.nats.Publish(
			fmt.Sprintf("orbit.chat.%s.member.added", chatID),
			"chat_member_added",
			map[string]interface{}{
				"chat_id": chatID,
				"user_id": targetUserID,
			},
			memberIDs,
			callerID.String(),
		)
	}

	return nil
}

// RejectJoinRequest rejects a pending join request.
// Caller must be an admin or owner.
func (s *InviteService) RejectJoinRequest(ctx context.Context, chatID, callerID, targetUserID uuid.UUID) error {
	_, role, err := s.chats.IsMember(ctx, chatID, callerID)
	if err != nil {
		return fmt.Errorf("check membership: %w", err)
	}
	if !permissions.IsAdminOrOwner(role) {
		return apperror.Forbidden("only admins and owners can reject join requests")
	}

	if err := s.invites.UpdateJoinRequestStatus(ctx, chatID, targetUserID, "rejected", callerID); err != nil {
		return fmt.Errorf("update join request status: %w", err)
	}

	return nil
}
