package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mst-corp/orbit/pkg/config"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
	"github.com/mst-corp/orbit/services/messaging/internal/store"
)

//go:embed packs
var embeddedAssets embed.FS

type seedManifest struct {
	Packs []seedPack `json:"packs"`
}

type seedPack struct {
	ID          string        `json:"id"`
	Title       string        `json:"title"`
	ShortName   string        `json:"short_name"`
	Description string        `json:"description"`
	Thumbnail   string        `json:"thumbnail"`
	Stickers    []seedSticker `json:"stickers"`
}

type seedSticker struct {
	ID     string `json:"id"`
	Emoji  string `json:"emoji"`
	File   string `json:"file"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type mediaUploadResponse struct {
	ID string `json:"id"`
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	manifestPath := flag.String("manifest", "", "Optional path to a manifest JSON file")
	flag.Parse()

	manifest, assetReader, err := loadManifest(*manifestPath)
	if err != nil {
		logger.Error("load manifest", "error", err)
		os.Exit(1)
	}

	ctx := context.Background()
	pool, err := openDatabase(ctx)
	if err != nil {
		logger.Error("open database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	uploaderID, err := resolveUploaderID(ctx, pool)
	if err != nil {
		logger.Error("resolve uploader id", "error", err)
		os.Exit(1)
	}

	stickerStore := store.NewStickerStore(pool)
	httpClient := &http.Client{Timeout: 30 * time.Second}
	mediaBaseURL := strings.TrimRight(config.EnvOr("MEDIA_SERVICE_URL", "http://localhost:8083"), "/")
	internalSecret := config.MustEnv("INTERNAL_SECRET")
	r2BaseURL := buildR2BaseURL()

	logger.Info("starting sticker seed", "packs", len(manifest.Packs), "uploader_id", uploaderID)

	for _, pack := range manifest.Packs {
		if err := seedPackData(ctx, stickerStore, httpClient, assetReader, mediaBaseURL, internalSecret, uploaderID, r2BaseURL, pack, logger); err != nil {
			logger.Error("seed pack failed", "pack", pack.ShortName, "error", err)
			os.Exit(1)
		}
	}

	logger.Info("sticker seed completed", "packs", len(manifest.Packs))
}

func seedPackData(
	ctx context.Context,
	stickerStore store.StickerStore,
	httpClient *http.Client,
	assetReader func(string) ([]byte, error),
	mediaBaseURL string,
	internalSecret string,
	uploaderID uuid.UUID,
	r2BaseURL string,
	pack seedPack,
	logger *slog.Logger,
) error {
	packID, err := uuid.Parse(pack.ID)
	if err != nil {
		return fmt.Errorf("parse pack id %q: %w", pack.ID, err)
	}

	existingPack, err := stickerStore.GetPackByShortName(ctx, pack.ShortName)
	if err != nil {
		return fmt.Errorf("get existing pack %s: %w", pack.ShortName, err)
	}

	existingURLs := make(map[uuid.UUID]string)
	if existingPack != nil {
		for _, sticker := range existingPack.Stickers {
			existingURLs[sticker.ID] = sticker.FileURL
		}
	}

	uploadedByFile := make(map[string]string)
	stickers := make([]model.Sticker, 0, len(pack.Stickers))
	var thumbnailURL *string

	for _, item := range pack.Stickers {
		stickerID, err := uuid.Parse(item.ID)
		if err != nil {
			return fmt.Errorf("parse sticker id %q: %w", item.ID, err)
		}

		fileURL, err := resolveStickerURL(ctx, httpClient, assetReader, mediaBaseURL, internalSecret, uploaderID, r2BaseURL, uploadedByFile, item, existingURLs[stickerID])
		if err != nil {
			return fmt.Errorf("resolve sticker url %s: %w", item.File, err)
		}

		emoji := item.Emoji
		width := item.Width
		height := item.Height
		sticker := model.Sticker{
			ID:       stickerID,
			PackID:   packID,
			Emoji:    &emoji,
			FileURL:  fileURL,
			FileType: "svg",
			Width:    &width,
			Height:   &height,
		}
		stickers = append(stickers, sticker)

		if normalizeAssetPath(item.File) == normalizeAssetPath(pack.Thumbnail) {
			value := fileURL
			thumbnailURL = &value
		}
	}

	if thumbnailURL == nil && len(stickers) > 0 {
		value := stickers[0].FileURL
		thumbnailURL = &value
	}

	description := pack.Description
	modelPack := &model.StickerPack{
		ID:           packID,
		Title:        strings.TrimSpace(pack.Title),
		ShortName:    strings.TrimSpace(pack.ShortName),
		Description:  strPtrOrNil(description),
		ThumbnailURL: thumbnailURL,
		IsOfficial:   true,
	}

	if err := stickerStore.CreatePack(ctx, modelPack, stickers); err != nil {
		return fmt.Errorf("create pack %s: %w", pack.ShortName, err)
	}

	logger.Info("seeded sticker pack",
		"pack_id", modelPack.ID,
		"short_name", modelPack.ShortName,
		"stickers", len(stickers),
	)
	return nil
}

func resolveStickerURL(
	ctx context.Context,
	httpClient *http.Client,
	assetReader func(string) ([]byte, error),
	mediaBaseURL string,
	internalSecret string,
	uploaderID uuid.UUID,
	r2BaseURL string,
	cache map[string]string,
	item seedSticker,
	existingURL string,
) (string, error) {
	assetPath := normalizeAssetPath(item.File)
	if cachedURL, ok := cache[assetPath]; ok {
		return cachedURL, nil
	}

	if isReusableRemoteURL(existingURL) {
		cache[assetPath] = existingURL
		return existingURL, nil
	}

	data, err := assetReader(assetPath)
	if err != nil {
		return "", fmt.Errorf("read asset %s: %w", assetPath, err)
	}

	mediaID, err := uploadStickerAsset(ctx, httpClient, mediaBaseURL, internalSecret, uploaderID, path.Base(assetPath), data)
	if err != nil {
		return "", err
	}

	fileURL := buildStickerObjectURL(r2BaseURL, mediaID, path.Ext(assetPath))
	cache[assetPath] = fileURL
	return fileURL, nil
}

func uploadStickerAsset(
	ctx context.Context,
	httpClient *http.Client,
	mediaBaseURL string,
	internalSecret string,
	uploaderID uuid.UUID,
	filename string,
	data []byte,
) (string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	fileWriter, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return "", fmt.Errorf("create multipart file: %w", err)
	}
	if _, err := fileWriter.Write(data); err != nil {
		return "", fmt.Errorf("write multipart file: %w", err)
	}
	if err := writer.WriteField("type", "file"); err != nil {
		return "", fmt.Errorf("write multipart field: %w", err)
	}
	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("close multipart writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, mediaBaseURL+"/media/upload", &body)
	if err != nil {
		return "", fmt.Errorf("create upload request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-Internal-Token", internalSecret)
	req.Header.Set("X-User-ID", uploaderID.String())

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("perform upload request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		rawBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("media upload returned %d: %s", resp.StatusCode, strings.TrimSpace(string(rawBody)))
	}

	var uploadResp mediaUploadResponse
	if err := json.NewDecoder(resp.Body).Decode(&uploadResp); err != nil {
		return "", fmt.Errorf("decode media upload response: %w", err)
	}
	if uploadResp.ID == "" {
		return "", errors.New("media upload response missing id")
	}

	return uploadResp.ID, nil
}

func loadManifest(manifestPath string) (*seedManifest, func(string) ([]byte, error), error) {
	var (
		rawManifest []byte
		err         error
		assetReader func(string) ([]byte, error)
	)

	if strings.TrimSpace(manifestPath) == "" {
		rawManifest, err = embeddedAssets.ReadFile("packs/manifest.json")
		if err != nil {
			return nil, nil, fmt.Errorf("read embedded manifest: %w", err)
		}
		assetReader = func(rel string) ([]byte, error) {
			return embeddedAssets.ReadFile(path.Join("packs", normalizeAssetPath(rel)))
		}
	} else {
		rawManifest, err = os.ReadFile(manifestPath)
		if err != nil {
			return nil, nil, fmt.Errorf("read manifest file: %w", err)
		}
		baseDir := filepath.Dir(manifestPath)
		assetReader = func(rel string) ([]byte, error) {
			return os.ReadFile(filepath.Join(baseDir, filepath.FromSlash(normalizeAssetPath(rel))))
		}
	}

	var manifest seedManifest
	if err := json.Unmarshal(rawManifest, &manifest); err != nil {
		return nil, nil, fmt.Errorf("decode manifest: %w", err)
	}

	if len(manifest.Packs) == 0 {
		return nil, nil, errors.New("manifest does not contain any packs")
	}

	return &manifest, assetReader, nil
}

func openDatabase(ctx context.Context) (*pgxpool.Pool, error) {
	dbDSN, dbPassword, dbRawPassword := config.DatabaseDSN()
	poolCfg, err := pgxpool.ParseConfig(dbDSN)
	if err != nil {
		return nil, fmt.Errorf("parse database config: %w", err)
	}

	passwords := []string{dbPassword}
	if noBackslashes := strings.ReplaceAll(dbPassword, `\`, ""); noBackslashes != dbPassword {
		passwords = append(passwords, noBackslashes)
	}
	if doubledBackslashes := strings.ReplaceAll(dbPassword, `\`, `\\`); doubledBackslashes != dbPassword {
		passwords = append(passwords, doubledBackslashes)
	}
	if dbRawPassword != "" && dbRawPassword != dbPassword {
		passwords = append(passwords, dbRawPassword)
	}

	var pool *pgxpool.Pool
	for _, password := range passwords {
		poolCfg.ConnConfig.Password = password
		pool, err = pgxpool.NewWithConfig(ctx, poolCfg)
		if err != nil {
			continue
		}
		if err := pool.Ping(ctx); err != nil {
			pool.Close()
			continue
		}
		return pool, nil
	}

	return nil, errors.New("all database connection attempts failed")
}

func resolveUploaderID(ctx context.Context, pool *pgxpool.Pool) (uuid.UUID, error) {
	if rawID := strings.TrimSpace(os.Getenv("STICKER_SEED_UPLOADER_ID")); rawID != "" {
		uploaderID, err := uuid.Parse(rawID)
		if err != nil {
			return uuid.Nil, fmt.Errorf("parse STICKER_SEED_UPLOADER_ID: %w", err)
		}
		return uploaderID, nil
	}

	var uploaderID uuid.UUID
	err := pool.QueryRow(ctx, `
		SELECT id
		FROM users
		WHERE role = 'admin'
		ORDER BY created_at ASC
		LIMIT 1`,
	).Scan(&uploaderID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("select admin uploader: %w", err)
	}

	return uploaderID, nil
}

func buildR2BaseURL() string {
	endpoint := config.EnvOr("R2_PUBLIC_ENDPOINT", config.MustEnv("R2_ENDPOINT"))
	endpoint = strings.TrimRight(endpoint, "/")
	bucket := config.EnvOr("R2_BUCKET", "orbit-media")
	return endpoint + "/" + bucket
}

func buildStickerObjectURL(r2BaseURL string, mediaID string, ext string) string {
	if ext == "" {
		ext = ".bin"
	}
	return fmt.Sprintf("%s/file/%s/original%s", r2BaseURL, mediaID, ext)
}

func normalizeAssetPath(rel string) string {
	cleaned := path.Clean(strings.TrimSpace(rel))
	cleaned = strings.TrimPrefix(cleaned, "./")
	return strings.TrimPrefix(cleaned, "/")
}

func isReusableRemoteURL(raw string) bool {
	raw = strings.TrimSpace(raw)
	return strings.HasPrefix(raw, "https://") || strings.HasPrefix(raw, "http://")
}

func strPtrOrNil(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}

	return &trimmed
}

var _ fs.FS = embeddedAssets
