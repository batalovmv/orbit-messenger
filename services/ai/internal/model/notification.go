// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package model

// ClassifyNotificationRequest is sent by the messaging service when it needs
// to determine the priority of an incoming message notification.
type ClassifyNotificationRequest struct {
	SenderID    string `json:"sender_id"`
	SenderRole  string `json:"sender_role"`
	ChatType    string `json:"chat_type"`
	ChatContext string `json:"chat_context"`
	MessageText string `json:"message_text"`
	HasMention  bool   `json:"has_mention"`
	ReplyToMe   bool   `json:"reply_to_me"`
}

// ClassifyNotificationResponse carries the classification result.
type ClassifyNotificationResponse struct {
	Priority  string `json:"priority"` // urgent|important|normal|low
	Reasoning string `json:"reasoning"`
	Source    string `json:"source"` // "rule" or "ai"
	Cached   bool   `json:"cached"`
}

// NotificationFeedbackRequest lets users correct a mis-classified priority.
type NotificationFeedbackRequest struct {
	MessageID          string `json:"message_id"`
	ClassifiedPriority string `json:"classified_priority"`
	UserOverride       string `json:"user_override_priority"`
}

// NotificationStatsResponse aggregates classification accuracy metrics.
type NotificationStatsResponse struct {
	TotalClassified int                      `json:"total_classified"`
	TotalOverridden int                      `json:"total_overridden"`
	MatchRate       float64                  `json:"match_rate"`
	PerPriority     map[string]PriorityStats `json:"per_priority"`
}

// PriorityStats holds per-priority classification counts.
type PriorityStats struct {
	Classified int `json:"classified"`
	Overridden int `json:"overridden"`
}
