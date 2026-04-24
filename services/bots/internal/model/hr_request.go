// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package model

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// HR request status transitions:
//
//	pending → approved (approver sets approver_id, optional decision_note)
//	pending → rejected (approver sets approver_id + decision_note)
//	approved|rejected are terminal — no further transitions allowed.
const (
	HRStatusPending  = "pending"
	HRStatusApproved = "approved"
	HRStatusRejected = "rejected"
)

const (
	HRRequestTypeVacation  = "vacation"
	HRRequestTypeSickLeave = "sick_leave"
	HRRequestTypeDayOff    = "day_off"
)

var (
	ErrHRRequestNotFound     = errors.New("hr request not found")
	ErrHRRequestAlreadyFinal = errors.New("hr request already decided")
	ErrHRInvalidRequestType  = errors.New("invalid request type")
	ErrHRInvalidDateRange    = errors.New("invalid date range")
)

// HRRequest is a single time-off / sick-leave entry created by an employee and
// decided by an HR role member of the bot's HR chat.
type HRRequest struct {
	ID           uuid.UUID  `json:"id"`
	BotID        uuid.UUID  `json:"bot_id"`
	ChatID       uuid.UUID  `json:"chat_id"`
	UserID       uuid.UUID  `json:"user_id"`
	RequestType  string     `json:"request_type"`
	StartDate    time.Time  `json:"start_date"`
	EndDate      time.Time  `json:"end_date"`
	Reason       *string    `json:"reason,omitempty"`
	Status       string     `json:"status"`
	ApproverID   *uuid.UUID `json:"approver_id,omitempty"`
	DecisionNote *string    `json:"decision_note,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// IsValidHRRequestType reports whether s matches one of the accepted enum values.
func IsValidHRRequestType(s string) bool {
	return s == HRRequestTypeVacation || s == HRRequestTypeSickLeave || s == HRRequestTypeDayOff
}
