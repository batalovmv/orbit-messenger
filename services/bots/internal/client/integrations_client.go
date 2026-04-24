// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

// IntegrationsClient communicates with the integrations service.
type IntegrationsClient struct {
	baseURL       string
	internalToken string
	httpClient    *http.Client
}

// ConnectorInfo is a summary of an integration connector.
type ConnectorInfo struct {
	ID          uuid.UUID  `json:"id"`
	Name        string     `json:"name"`
	DisplayName string     `json:"display_name"`
	Type        string     `json:"type"`
	BotID       *uuid.UUID `json:"bot_id,omitempty"`
	IsActive    bool       `json:"is_active"`
	CreatedBy   uuid.UUID  `json:"created_by"`
}

func NewIntegrationsClient(baseURL, internalToken string) *IntegrationsClient {
	return &IntegrationsClient{
		baseURL:       strings.TrimRight(baseURL, "/") + "/api/v1",
		internalToken: internalToken,
		httpClient:    &http.Client{Timeout: 10 * time.Second},
	}
}

// ListConnectors returns all active connectors.
func (c *IntegrationsClient) ListConnectors(ctx context.Context) ([]ConnectorInfo, error) {
	url := fmt.Sprintf("%s/integrations/connectors?limit=100&offset=0", c.baseURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create list connectors request: %w", err)
	}
	req.Header.Set("X-Internal-Token", c.internalToken)
	// Use a system-level user ID (zero UUID) since this is an internal call
	req.Header.Set("X-User-ID", uuid.Nil.String())
	req.Header.Set("X-User-Role", "superadmin")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list connectors request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read connectors response: %w", err)
	}

	if resp.StatusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("list connectors error (%d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data []ConnectorInfo `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode connectors response: %w", err)
	}

	return result.Data, nil
}

// UpdateConnectorBotID links or unlinks a bot from a connector.
func (c *IntegrationsClient) UpdateConnectorBotID(ctx context.Context, connectorID uuid.UUID, botUserID *uuid.UUID) error {
	url := fmt.Sprintf("%s/integrations/connectors/%s", c.baseURL, connectorID)

	payload := map[string]any{}
	if botUserID != nil {
		payload["bot_id"] = botUserID.String()
	} else {
		payload["bot_id"] = nil
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal update connector body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, url, bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("create update connector request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Token", c.internalToken)
	req.Header.Set("X-User-ID", uuid.Nil.String())
	req.Header.Set("X-User-Role", "superadmin")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("update connector request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("update connector error (%d): %s", resp.StatusCode, string(body))
	}

	return nil
}
