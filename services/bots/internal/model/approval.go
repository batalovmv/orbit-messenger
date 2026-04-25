// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package model

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

const (
	ApprovalStatusPending   = "pending"
	ApprovalStatusApproved  = "approved"
	ApprovalStatusRejected  = "rejected"
	ApprovalStatusCancelled = "cancelled"
)

var (
	ErrApprovalNotFound        = errors.New("approval request not found")
	ErrApprovalAlreadyFinal    = errors.New("approval request is already decided or cancelled")
	ErrApprovalVersionConflict = errors.New("approval request was modified concurrently")
	ErrApprovalForbidden       = errors.New("only the requester or bot owner can cancel")
)

// ApprovalRequest is a generic approval workflow entry created by any user in a
// bot-enabled chat and decided by the bot owner.
type ApprovalRequest struct {
	ID           uuid.UUID       `json:"id" db:"id"`
	BotID        uuid.UUID       `json:"bot_id" db:"bot_id"`
	ChatID       uuid.UUID       `json:"chat_id" db:"chat_id"`
	RequesterID  uuid.UUID       `json:"requester_id" db:"requester_id"`
	ApprovalType string          `json:"approval_type" db:"approval_type"`
	Subject      string          `json:"subject" db:"subject"`
	Payload      json.RawMessage `json:"payload" db:"payload"`
	Status       string          `json:"status" db:"status"`
	Version      int             `json:"version" db:"version"`
	DecidedBy    *uuid.UUID      `json:"decided_by,omitempty" db:"decided_by"`
	DecidedAt    *time.Time      `json:"decided_at,omitempty" db:"decided_at"`
	DecisionNote *string         `json:"decision_note,omitempty" db:"decision_note"`
	CreatedAt    time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at" db:"updated_at"`
}
