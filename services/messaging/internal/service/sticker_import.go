// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"

	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
)

var telegramStickerPackSourcePattern = regexp.MustCompile(`^[A-Za-z0-9_]{3,64}$`)

const maxStickerBytes = 10 * 1024 * 1024

// TelegramStickerClient fetches sticker pack metadata and files from Telegram Bot API.
type TelegramStickerClient interface {
	GetStickerSet(ctx context.Context, shortName string) (*TelegramStickerSet, error)
	GetFile(ctx context.Context, fileID string) (*TelegramFile, error)
	DownloadFile(ctx context.Context, filePath string) ([]byte, error)
}

// StickerMediaUploader stores imported sticker assets in media service / R2.
type StickerMediaUploader interface {
	UploadSticker(ctx context.Context, uploaderID uuid.UUID, fileName string, data []byte) (*UploadedStickerMedia, error)
}

// UploadedStickerMedia is the minimal media upload result needed by sticker import.
type UploadedStickerMedia struct {
	MediaID string
}

// TelegramStickerSet mirrors the subset of Telegram Bot API we need for imports.
type TelegramStickerSet struct {
	Name        string             `json:"name"`
	Title       string             `json:"title"`
	StickerType string             `json:"sticker_type"`
	Stickers    []TelegramSticker  `json:"stickers"`
	Thumbnail   *TelegramPhotoSize `json:"thumbnail"`
}

// TelegramSticker mirrors the Telegram Bot API sticker payload.
type TelegramSticker struct {
	FileID     string             `json:"file_id"`
	Emoji      string             `json:"emoji"`
	Width      int                `json:"width"`
	Height     int                `json:"height"`
	IsAnimated bool               `json:"is_animated"`
	IsVideo    bool               `json:"is_video"`
	SetName    string             `json:"set_name"`
	Thumbnail  *TelegramPhotoSize `json:"thumbnail"`
}

// TelegramPhotoSize mirrors the Telegram Bot API file reference used by set thumbnails.
type TelegramPhotoSize struct {
	FileID string `json:"file_id"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// TelegramFile mirrors the Telegram Bot API getFile result.
type TelegramFile struct {
	FileID   string `json:"file_id"`
	FilePath string `json:"file_path"`
	FileSize int64  `json:"file_size"`
}

type telegramBotEnvelope struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	ErrorCode   int             `json:"error_code"`
	Description string          `json:"description"`
}

type mediaUploadResponse struct {
	ID string `json:"id"`
}

// ImportTelegramPack downloads a sticker pack from Telegram and persists it in Orbit.
func (s *StickerService) ImportTelegramPack(ctx context.Context, userID uuid.UUID, source string) (*model.StickerPack, error) {
	if s.telegram == nil || s.mediaUploader == nil {
		return nil, apperror.Internal("Telegram sticker import is not configured")
	}

	shortName, err := normalizeTelegramStickerPackSource(source)
	if err != nil {
		return nil, err
	}

	set, err := s.telegram.GetStickerSet(ctx, shortName)
	if err != nil {
		return nil, err
	}
	if set == nil || len(set.Stickers) == 0 {
		return nil, apperror.BadRequest("Telegram sticker pack is empty")
	}

	packShortName := normalizeStickerPackShortName(set.Name)
	if !stickerPackShortNamePattern.MatchString(packShortName) {
		return nil, apperror.BadRequest("Telegram sticker pack short_name is not supported")
	}

	existingPack, err := s.stickers.GetPackByShortName(ctx, packShortName)
	if err != nil {
		return nil, fmt.Errorf("get sticker pack by short name: %w", err)
	}

	stickers, err := s.importTelegramStickers(ctx, userID, set.Stickers)
	if err != nil {
		return nil, err
	}

	thumbnailURL, err := s.importTelegramStickerSetThumbnail(ctx, userID, set)
	if err != nil {
		return nil, err
	}
	if thumbnailURL == nil && len(stickers) > 0 {
		value := decorateStickerFormatURL(stickers[0].FileURL, stickers[0].FileType)
		thumbnailURL = &value
	}

	description := "Imported from Telegram"
	title := strings.TrimSpace(set.Title)
	if title == "" {
		title = packShortName
	}

	authorID := &userID
	if existingPack != nil && existingPack.AuthorID != nil {
		authorID = existingPack.AuthorID
	}

	pack := &model.StickerPack{
		Title:        title,
		ShortName:    packShortName,
		Description:  &description,
		AuthorID:     authorID,
		ThumbnailURL: thumbnailURL,
		IsOfficial:   false,
	}
	if existingPack != nil {
		pack.ID = existingPack.ID
	}

	if err := validatePackInput(pack); err != nil {
		return nil, err
	}
	for i := range stickers {
		if err := validateStickerInput(&stickers[i]); err != nil {
			return nil, err
		}
	}

	if err := s.stickers.CreatePack(ctx, pack, stickers); err != nil {
		return nil, s.mapWriteError("import telegram sticker pack", err)
	}

	s.logger.Info("telegram sticker pack imported",
		"pack_id", pack.ID,
		"short_name", pack.ShortName,
		"stickers", len(stickers),
	)

	return pack, nil
}

func (s *StickerService) importTelegramStickers(
	ctx context.Context,
	uploaderID uuid.UUID,
	sourceStickers []TelegramSticker,
) ([]model.Sticker, error) {
	stickers := make([]model.Sticker, len(sourceStickers))
	group, groupCtx := errgroup.WithContext(ctx)
	limit := make(chan struct{}, 4)

	for index, sourceSticker := range sourceStickers {
		index := index
		sourceSticker := sourceSticker

		group.Go(func() error {
			limit <- struct{}{}
			defer func() {
				<-limit
			}()

			sticker, err := s.importTelegramSticker(groupCtx, uploaderID, sourceSticker, index)
			if err != nil {
				return err
			}

			stickers[index] = *sticker
			return nil
		})
	}

	if err := group.Wait(); err != nil {
		return nil, err
	}

	return stickers, nil
}

func (s *StickerService) importTelegramSticker(
	ctx context.Context,
	uploaderID uuid.UUID,
	sourceSticker TelegramSticker,
	position int,
) (*model.Sticker, error) {
	fileType := inferTelegramStickerFileType(sourceSticker)
	uploaded, _, err := s.uploadTelegramFile(
		ctx,
		uploaderID,
		sourceSticker.FileID,
		fmt.Sprintf("telegram-sticker-%d.%s", position, fileType),
		fileType,
	)
	if err != nil {
		return nil, err
	}

	emoji := strings.TrimSpace(sourceSticker.Emoji)
	if emoji == "" {
		emoji = "🙂"
	}

	width := sourceSticker.Width
	if width <= 0 {
		width = 512
	}
	height := sourceSticker.Height
	if height <= 0 {
		height = 512
	}

	return &model.Sticker{
		Emoji:    &emoji,
		FileURL:  buildManagedMediaURL(uploaded.MediaID),
		FileType: fileType,
		Width:    &width,
		Height:   &height,
		Position: position,
	}, nil
}

func (s *StickerService) importTelegramStickerSetThumbnail(
	ctx context.Context,
	uploaderID uuid.UUID,
	set *TelegramStickerSet,
) (*string, error) {
	if set == nil || set.Thumbnail == nil || strings.TrimSpace(set.Thumbnail.FileID) == "" {
		return nil, nil
	}

	uploaded, formatHint, err := s.uploadTelegramFile(
		ctx,
		uploaderID,
		set.Thumbnail.FileID,
		fmt.Sprintf("telegram-pack-cover-%s.webp", normalizeStickerPackShortName(set.Name)),
		"webp",
	)
	if err != nil {
		return nil, err
	}

	value := decorateStickerFormatURL(buildManagedMediaURL(uploaded.MediaID), formatHint)
	return &value, nil
}

func (s *StickerService) uploadTelegramFile(
	ctx context.Context,
	uploaderID uuid.UUID,
	fileID string,
	fallbackFileName string,
	fallbackFormat string,
) (*UploadedStickerMedia, string, error) {
	fileInfo, err := s.telegram.GetFile(ctx, fileID)
	if err != nil {
		return nil, "", err
	}

	data, err := s.telegram.DownloadFile(ctx, fileInfo.FilePath)
	if err != nil {
		return nil, "", err
	}

	fileName := strings.TrimSpace(fallbackFileName)
	if baseName := path.Base(strings.TrimSpace(fileInfo.FilePath)); baseName != "." && baseName != "/" && baseName != "" {
		fileName = baseName
	}
	if fileName == "" {
		fileName = fmt.Sprintf("telegram-sticker.%s", fallbackFormat)
	}

	uploaded, err := s.mediaUploader.UploadSticker(ctx, uploaderID, fileName, data)
	if err != nil {
		return nil, "", err
	}

	formatHint := inferStickerFormatHint(fileInfo.FilePath, fallbackFormat)
	return uploaded, formatHint, nil
}

func normalizeTelegramStickerPackSource(source string) (string, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		return "", apperror.BadRequest("sticker pack source is required")
	}

	if strings.Contains(source, "://") || strings.Contains(source, "t.me/") || strings.Contains(source, "telegram.me/") {
		if !strings.Contains(source, "://") {
			source = "https://" + source
		}

		parsed, err := url.Parse(source)
		if err != nil {
			return "", apperror.BadRequest("Invalid Telegram sticker pack URL")
		}

		host := strings.ToLower(parsed.Hostname())
		if host != "t.me" && host != "telegram.me" && host != "www.t.me" && host != "www.telegram.me" {
			return "", apperror.BadRequest("Telegram sticker pack URL must use t.me or telegram.me")
		}

		segments := strings.FieldsFunc(parsed.Path, func(char rune) bool {
			return char == '/'
		})
		if len(segments) < 2 {
			return "", apperror.BadRequest("Telegram sticker pack URL must look like t.me/addstickers/<short_name>")
		}

		route := strings.ToLower(segments[0])
		if route != "addstickers" && route != "addemoji" {
			return "", apperror.BadRequest("Telegram sticker pack URL must look like t.me/addstickers/<short_name>")
		}

		source = segments[1]
	}

	if !telegramStickerPackSourcePattern.MatchString(source) {
		return "", apperror.BadRequest("Invalid Telegram sticker pack short_name")
	}

	return source, nil
}

func inferTelegramStickerFileType(sticker TelegramSticker) string {
	switch {
	case sticker.IsAnimated:
		return "tgs"
	case sticker.IsVideo:
		return "webm"
	default:
		return "webp"
	}
}

func inferStickerFormatHint(filePath string, fallback string) string {
	switch strings.ToLower(path.Ext(strings.TrimSpace(filePath))) {
	case ".tgs":
		return "tgs"
	case ".webm":
		return "webm"
	case ".svg":
		return "svg"
	case ".png":
		return "png"
	case ".jpg":
		return "jpg"
	case ".jpeg":
		return "jpeg"
	case ".webp":
		return "webp"
	default:
		return strings.ToLower(strings.TrimSpace(fallback))
	}
}

func buildManagedMediaURL(mediaID string) string {
	return fmt.Sprintf("/media/%s", mediaID)
}

func decorateStickerFormatURL(rawURL, formatHint string) string {
	formatHint = strings.ToLower(strings.TrimSpace(formatHint))
	if rawURL == "" || formatHint == "" {
		return rawURL
	}

	if strings.Contains(rawURL, "#") {
		return rawURL + "&orbit-format=" + formatHint
	}

	return rawURL + "#orbit-format=" + formatHint
}

// TelegramBotStickerClient is a minimal Telegram Bot API client for sticker imports.
type TelegramBotStickerClient struct {
	token  string
	client *http.Client
	logger *slog.Logger
}

// NewTelegramBotStickerClient creates a Telegram sticker import client.
func NewTelegramBotStickerClient(token string, logger *slog.Logger) *TelegramBotStickerClient {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}

	return &TelegramBotStickerClient{
		token: token,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		logger: logger,
	}
}

func (c *TelegramBotStickerClient) GetStickerSet(ctx context.Context, shortName string) (*TelegramStickerSet, error) {
	query := url.Values{}
	query.Set("name", shortName)

	var result TelegramStickerSet
	if err := c.callBotAPI(ctx, "getStickerSet", query, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

func (c *TelegramBotStickerClient) GetFile(ctx context.Context, fileID string) (*TelegramFile, error) {
	query := url.Values{}
	query.Set("file_id", fileID)

	var result TelegramFile
	if err := c.callBotAPI(ctx, "getFile", query, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

func (c *TelegramBotStickerClient) DownloadFile(ctx context.Context, filePath string) ([]byte, error) {
	cleanPath := strings.TrimLeft(path.Clean("/"+strings.TrimSpace(filePath)), "/")
	if cleanPath == "" {
		return nil, apperror.BadRequest("Telegram sticker file path is missing")
	}

	endpoint := fmt.Sprintf("https://api.telegram.org/file/bot%s/%s", c.token, cleanPath)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create telegram file request: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download telegram sticker file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		rawBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("telegram file download returned %d: %s", resp.StatusCode, strings.TrimSpace(string(rawBody)))
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxStickerBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read telegram sticker file: %w", err)
	}
	if len(data) > maxStickerBytes {
		c.logger.Warn("telegram sticker file exceeds size limit", "file_path", cleanPath, "bytes", len(data), "limit", maxStickerBytes)
		return nil, fmt.Errorf("telegram sticker file exceeds %d bytes", maxStickerBytes)
	}

	return data, nil
}

func (c *TelegramBotStickerClient) callBotAPI(ctx context.Context, method string, query url.Values, target any) error {
	endpoint := fmt.Sprintf("https://api.telegram.org/bot%s/%s", c.token, method)
	if encodedQuery := query.Encode(); encodedQuery != "" {
		endpoint += "?" + encodedQuery
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("create telegram %s request: %w", method, err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("telegram %s request failed: %w", method, err)
	}
	defer resp.Body.Close()

	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read telegram %s response: %w", method, err)
	}

	var envelope telegramBotEnvelope
	if err := json.Unmarshal(rawBody, &envelope); err != nil {
		return fmt.Errorf("decode telegram %s response: %w", method, err)
	}

	if !envelope.OK {
		return mapTelegramBotError(method, envelope.Description)
	}
	if target == nil {
		return nil
	}
	if err := json.Unmarshal(envelope.Result, target); err != nil {
		return fmt.Errorf("decode telegram %s payload: %w", method, err)
	}

	return nil
}

func mapTelegramBotError(method string, description string) error {
	description = strings.TrimSpace(description)
	lowerDescription := strings.ToLower(description)

	switch {
	case method == "getStickerSet" && (strings.Contains(lowerDescription, "stickerset_invalid") || strings.Contains(lowerDescription, "not found")):
		return apperror.NotFound("Telegram sticker pack not found")
	case strings.Contains(lowerDescription, "file is too big"):
		return apperror.BadRequest("Telegram sticker file is too large")
	case description != "":
		return apperror.BadRequest(description)
	default:
		return apperror.Internal("Telegram sticker import failed")
	}
}

// MediaServiceStickerUploader uploads imported assets through Orbit media service.
type MediaServiceStickerUploader struct {
	baseURL        string
	internalSecret string
	client         *http.Client
	logger         *slog.Logger
}

// NewMediaServiceStickerUploader creates an uploader backed by media service.
func NewMediaServiceStickerUploader(baseURL string, internalSecret string, logger *slog.Logger) *MediaServiceStickerUploader {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	internalSecret = strings.TrimSpace(internalSecret)
	if baseURL == "" || internalSecret == "" {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}

	return &MediaServiceStickerUploader{
		baseURL:        baseURL,
		internalSecret: internalSecret,
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
		logger: logger,
	}
}

func (u *MediaServiceStickerUploader) UploadSticker(
	ctx context.Context,
	uploaderID uuid.UUID,
	fileName string,
	data []byte,
) (*UploadedStickerMedia, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	fileWriter, err := writer.CreateFormFile("file", path.Base(strings.TrimSpace(fileName)))
	if err != nil {
		return nil, fmt.Errorf("create multipart sticker file: %w", err)
	}
	if _, err := fileWriter.Write(data); err != nil {
		return nil, fmt.Errorf("write multipart sticker file: %w", err)
	}
	if err := writer.WriteField("type", "file"); err != nil {
		return nil, fmt.Errorf("write multipart sticker type: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("close multipart sticker body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.baseURL+"/media/upload", &body)
	if err != nil {
		return nil, fmt.Errorf("create sticker upload request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-Internal-Token", u.internalSecret)
	req.Header.Set("X-User-ID", uploaderID.String())

	resp, err := u.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("upload sticker asset: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		rawBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("media upload returned %d: %s", resp.StatusCode, strings.TrimSpace(string(rawBody)))
	}

	var uploadResponse mediaUploadResponse
	if err := json.NewDecoder(resp.Body).Decode(&uploadResponse); err != nil {
		return nil, fmt.Errorf("decode media upload response: %w", err)
	}
	if strings.TrimSpace(uploadResponse.ID) == "" {
		return nil, fmt.Errorf("media upload response missing id")
	}

	return &UploadedStickerMedia{MediaID: uploadResponse.ID}, nil
}
