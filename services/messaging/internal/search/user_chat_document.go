// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package search

// BuildUserDocument creates a normalized user document for Meilisearch.
// Fields match the searchable/filterable attributes configured for the "users" index:
// searchable: display_name, email — filterable: role
func BuildUserDocument(id, displayName, email, role string) map[string]interface{} {
	return map[string]interface{}{
		"id":           id,
		"display_name": displayName,
		"email":        email,
		"role":         role,
	}
}

// BuildChatDocument creates a normalized chat document for Meilisearch.
// Fields match the searchable/filterable attributes configured for the "chats" index:
// searchable: name, description — filterable: id, type
func BuildChatDocument(id, chatType, name, description string) map[string]interface{} {
	return map[string]interface{}{
		"id":          id,
		"type":        chatType,
		"name":        name,
		"description": description,
	}
}
