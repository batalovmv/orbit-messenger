package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
	"github.com/mst-corp/orbit/services/messaging/internal/store"
)

type UserService struct {
	users store.UserStore
}

func NewUserService(users store.UserStore) *UserService {
	return &UserService{users: users}
}

func (s *UserService) GetMe(ctx context.Context, userID uuid.UUID) (*model.User, error) {
	u, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	if u == nil {
		return nil, apperror.NotFound("User not found")
	}
	return u, nil
}

func (s *UserService) GetUser(ctx context.Context, userID uuid.UUID) (*model.User, error) {
	u, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	if u == nil {
		return nil, apperror.NotFound("User not found")
	}
	return u, nil
}

func (s *UserService) UpdateProfile(ctx context.Context, userID uuid.UUID, displayName string, bio, phone, avatarURL, customStatus, customStatusEmoji *string) (*model.User, error) {
	u, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	if u == nil {
		return nil, apperror.NotFound("User not found")
	}

	if displayName != "" {
		u.DisplayName = displayName
	}
	if bio != nil {
		u.Bio = bio
	}
	if phone != nil {
		u.Phone = phone
	}
	if avatarURL != nil {
		u.AvatarURL = avatarURL
	}
	if customStatus != nil {
		u.CustomStatus = customStatus
	}
	if customStatusEmoji != nil {
		u.CustomStatusEmoji = customStatusEmoji
	}

	if err := s.users.Update(ctx, u); err != nil {
		return nil, fmt.Errorf("update user: %w", err)
	}
	return u, nil
}

func (s *UserService) SearchUsers(ctx context.Context, query string, limit int) ([]model.User, error) {
	if query == "" {
		return nil, apperror.BadRequest("Search query is required")
	}
	return s.users.Search(ctx, query, limit)
}
