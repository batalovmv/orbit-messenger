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
