// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

type MediaClient struct {
	baseURL       string
	internalToken string
	httpClient    *http.Client
}

type UploadResponse struct {
	ID       string `json:"id"`
	URL      string `json:"url"`
	MimeType string `json:"mime_type"`
	Size     int64  `json:"size"`
}

// MediaInfo mirrors the subset of media-service /media/:id/info we consume.
type MediaInfo struct {
	ID               string  `json:"id"`
	Type             string  `json:"type"`
	MimeType         string  `json:"mime_type"`
	OriginalFilename string  `json:"original_filename,omitempty"`
	SizeBytes        int64   `json:"size_bytes"`
	Width            *int    `json:"width,omitempty"`
	Height           *int    `json:"height,omitempty"`
	DurationSeconds  *float64 `json:"duration_seconds,omitempty"`
	URL              string  `json:"url,omitempty"`
	ThumbnailURL     string  `json:"thumbnail_url,omitempty"`
}

func NewMediaClient(baseURL, internalToken string) *MediaClient {
	return &MediaClient{
		baseURL:       strings.TrimRight(baseURL, "/"),
		internalToken: internalToken,
		httpClient:    &http.Client{Timeout: 60 * time.Second},
	}
}

// UploadFile uploads a file to the media service and returns the media ID.
func (c *MediaClient) UploadFile(ctx context.Context, botUserID uuid.UUID, filename, mediaType string, data []byte) (string, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return "", fmt.Errorf("create form file: %w", err)
	}
	if _, err := part.Write(data); err != nil {
		return "", fmt.Errorf("write file data: %w", err)
	}

	if mediaType != "" {
		if err := writer.WriteField("type", mediaType); err != nil {
			return "", fmt.Errorf("write type field: %w", err)
		}
	}

	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("close multipart writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/media/upload", &buf)
	if err != nil {
		return "", fmt.Errorf("create upload request: %w", err)
	}

	req.Header.Set("X-Internal-Token", c.internalToken)
	req.Header.Set("X-User-ID", botUserID.String())
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("upload request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read upload response: %w", err)
	}

	if resp.StatusCode >= http.StatusBadRequest {
		return "", &ClientError{StatusCode: resp.StatusCode, Message: string(respBody)}
	}

	var result UploadResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("decode upload response: %w", err)
	}

	return result.ID, nil
}

// GetInfo fetches metadata for a media item. The bot user must be able to
// access the media (CanAccess: uploader or media is linked to any message).
func (c *MediaClient) GetInfo(ctx context.Context, botUserID, mediaID uuid.UUID) (*MediaInfo, error) {
	url := fmt.Sprintf("%s/media/%s/info", c.baseURL, mediaID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create info request: %w", err)
	}
	req.Header.Set("X-Internal-Token", c.internalToken)
	req.Header.Set("X-User-ID", botUserID.String())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("info request failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read info response: %w", err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, &ClientError{StatusCode: resp.StatusCode, Message: string(body)}
	}
	var info MediaInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, fmt.Errorf("decode info response: %w", err)
	}
	return &info, nil
}

// StreamFile opens an HTTP stream to the media service for the original file.
// Caller must close the body. Returns Content-Type for relaying to the bot.
func (c *MediaClient) StreamFile(ctx context.Context, botUserID, mediaID uuid.UUID, rangeHeader string) (io.ReadCloser, http.Header, int, error) {
	url := fmt.Sprintf("%s/media/%s", c.baseURL, mediaID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("create stream request: %w", err)
	}
	req.Header.Set("X-Internal-Token", c.internalToken)
	req.Header.Set("X-User-ID", botUserID.String())
	if rangeHeader != "" {
		req.Header.Set("Range", rangeHeader)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("stream request failed: %w", err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return nil, nil, resp.StatusCode, &ClientError{StatusCode: resp.StatusCode, Message: string(body)}
	}
	return resp.Body, resp.Header, resp.StatusCode, nil
}
