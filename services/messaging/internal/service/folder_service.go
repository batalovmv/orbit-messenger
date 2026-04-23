// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/mst-corp/orbit/services/messaging/internal/store"
)

// FolderService handles business logic for chat folders.
type FolderService struct {
	store store.FolderStore
}

// NewFolderService creates a FolderService backed by the given store.
func NewFolderService(s store.FolderStore) *FolderService {
	return &FolderService{store: s}
}

// List returns all folders for a user, ordered by position.
func (s *FolderService) List(ctx context.Context, userID uuid.UUID) ([]*store.ChatFolder, error) {
	folders, err := s.store.List(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list folders: %w", err)
	}
	return folders, nil
}

// Get returns a single folder by ID. Returns NotFound if the folder does not
// exist or does not belong to userID.
func (s *FolderService) Get(ctx context.Context, id int, userID uuid.UUID) (*store.ChatFolder, error) {
	folder, err := s.store.Get(ctx, id, userID)
	if err != nil {
		return nil, err
	}
	return folder, nil
}

// Create adds a new folder for the user. The store enforces the per-user folder limit.
func (s *FolderService) Create(ctx context.Context, f *store.ChatFolder) error {
	if err := s.store.Create(ctx, f); err != nil {
		return err
	}
	return nil
}

// Update modifies an existing folder's metadata and chat memberships. Returns
// NotFound if the folder does not exist or does not belong to userID.
func (s *FolderService) Update(ctx context.Context, f *store.ChatFolder) error {
	if err := s.store.Update(ctx, f); err != nil {
		return err
	}
	return nil
}

// Delete removes a folder by ID. Returns NotFound if the folder does not exist
// or does not belong to userID.
func (s *FolderService) Delete(ctx context.Context, id int, userID uuid.UUID) error {
	if err := s.store.Delete(ctx, id, userID); err != nil {
		return err
	}
	return nil
}

// UpdateOrder reorders folders for a user by assigning each folder in folderIDs
// a position equal to its slice index. Returns NotFound if any folder ID does
// not belong to userID.
func (s *FolderService) UpdateOrder(ctx context.Context, userID uuid.UUID, folderIDs []int) error {
	if err := s.store.UpdateOrder(ctx, userID, folderIDs); err != nil {
		return err
	}
	return nil
}
