package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mst-corp/orbit/pkg/config"
)

var defaultPacks = []string{
	"HotCherry",
	"Animals",
	"MrCat",
	"TuxedoCat",
	"menhera_chan",
	"froggypack",
	"BearLove",
	"PopCat",
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	botToken := flag.String("token", "", "Telegram Bot API token (required)")
	packsFlag := flag.String("packs", "", "Comma-separated sticker set names (default: popular packs)")
	cleanOld := flag.Bool("clean", false, "Remove old seeded SVG packs before import")
	flag.Parse()

	if *botToken == "" {
		*botToken = os.Getenv("TELEGRAM_BOT_TOKEN")
	}
	if *botToken == "" {
		logger.Error("bot token required: use -token flag or TELEGRAM_BOT_TOKEN env")
		os.Exit(1)
	}

	packs := defaultPacks
	if *packsFlag != "" {
		packs = strings.Split(*packsFlag, ",")
		for i := range packs {
			packs[i] = strings.TrimSpace(packs[i])
		}
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

	httpClient := &http.Client{Timeout: 60 * time.Second}
	mediaBaseURL := strings.TrimRight(config.EnvOr("MEDIA_SERVICE_URL", "http://localhost:8083"), "/")
	internalSecret := config.MustEnv("INTERNAL_SECRET")

	if *cleanOld {
		logger.Info("cleaning old seeded SVG packs")
		if err := cleanOldPacks(ctx, pool); err != nil {
			logger.Warn("clean old packs", "error", err)
		}
	}

	tgAPI := &telegramAPI{token: *botToken, client: httpClient}

	imported := 0
	for _, packName := range packs {
		logger.Info("importing pack", "name", packName)
		if err := importPack(ctx, tgAPI, pool, httpClient, mediaBaseURL, internalSecret, uploaderID, packName, logger); err != nil {
			logger.Error("import failed", "pack", packName, "error", err)
			continue
		}
		imported++
		logger.Info("pack imported successfully", "name", packName)
	}

	logger.Info("import completed", "imported", imported, "total", len(packs))
}

// --- Telegram Bot API ---

type telegramAPI struct {
	token  string
	client *http.Client
}

type tgStickerSet struct {
	Name       string      `json:"name"`
	Title      string      `json:"title"`
	IsAnimated bool        `json:"is_animated"`
	IsVideo    bool        `json:"is_video"`
	Stickers   []tgSticker `json:"stickers"`
}

type tgSticker struct {
	FileID     string       `json:"file_id"`
	Width      int          `json:"width"`
	Height     int          `json:"height"`
	IsAnimated bool         `json:"is_animated"`
	IsVideo    bool         `json:"is_video"`
	Emoji      string       `json:"emoji"`
	Thumbnail  *tgPhotoSize `json:"thumbnail"`
}

type tgPhotoSize struct {
	FileID string `json:"file_id"`
}

type tgAPIResponse[T any] struct {
	OK     bool   `json:"ok"`
	Result T      `json:"result"`
	Desc   string `json:"description"`
}

type tgFile struct {
	FilePath string `json:"file_path"`
}

func (api *telegramAPI) getStickerSet(name string) (*tgStickerSet, error) {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/getStickerSet?name=%s", api.token, name)
	resp, err := api.client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result tgAPIResponse[tgStickerSet]
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if !result.OK {
		return nil, fmt.Errorf("telegram: %s", result.Desc)
	}
	return &result.Result, nil
}

func (api *telegramAPI) downloadFileByID(fileID string) ([]byte, string, error) {
	// Step 1: get file path
	url := fmt.Sprintf("https://api.telegram.org/bot%s/getFile?file_id=%s", api.token, fileID)
	resp, err := api.client.Get(url)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	var result tgAPIResponse[tgFile]
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, "", err
	}
	if !result.OK {
		return nil, "", fmt.Errorf("telegram: %s", result.Desc)
	}

	// Step 2: download file
	dlURL := fmt.Sprintf("https://api.telegram.org/file/bot%s/%s", api.token, result.Result.FilePath)
	dlResp, err := api.client.Get(dlURL)
	if err != nil {
		return nil, "", err
	}
	defer dlResp.Body.Close()

	data, err := io.ReadAll(dlResp.Body)
	if err != nil {
		return nil, "", err
	}
	return data, result.Result.FilePath, nil
}

// --- Import logic ---

func importPack(
	ctx context.Context,
	tgAPI *telegramAPI,
	pool *pgxpool.Pool,
	httpClient *http.Client,
	mediaBaseURL, internalSecret string,
	uploaderID uuid.UUID,
	packName string,
	logger *slog.Logger,
) error {
	// Check if already imported
	var existingCount int
	_ = pool.QueryRow(ctx, `SELECT COUNT(*) FROM sticker_packs WHERE short_name = $1`, packName).Scan(&existingCount)
	if existingCount > 0 {
		logger.Info("pack already exists, skipping", "name", packName)
		return nil
	}

	// Fetch from Telegram
	set, err := tgAPI.getStickerSet(packName)
	if err != nil {
		return fmt.Errorf("fetch set: %w", err)
	}
	if len(set.Stickers) == 0 {
		return fmt.Errorf("empty set")
	}

	fileType := "webp"
	if set.IsAnimated {
		fileType = "tgs"
	} else if set.IsVideo {
		fileType = "webm"
	}

	packID := uuid.New()
	r2BaseURL := buildR2BaseURL()
	var thumbnailURL string

	// Insert pack first
	_, err = pool.Exec(ctx, `
		INSERT INTO sticker_packs (id, title, short_name, is_official, is_animated, sticker_count, is_featured)
		VALUES ($1, $2, $3, true, $4, $5, true)
	`, packID, set.Title, set.Name, set.IsAnimated || set.IsVideo, len(set.Stickers))
	if err != nil {
		return fmt.Errorf("insert pack: %w", err)
	}

	imported := 0
	for i, tgStk := range set.Stickers {
		if i > 0 && i%10 == 0 {
			logger.Info("progress", "pack", packName, "sticker", i, "total", len(set.Stickers))
		}

		data, filePath, err := tgAPI.downloadFileByID(tgStk.FileID)
		if err != nil {
			logger.Warn("download failed", "index", i, "error", err)
			continue
		}

		// Determine filename and MIME
		ext := fileType
		if strings.HasSuffix(filePath, ".tgs") {
			ext = "tgs"
		} else if strings.HasSuffix(filePath, ".webm") {
			ext = "webm"
		} else if strings.HasSuffix(filePath, ".webp") {
			ext = "webp"
		}
		filename := fmt.Sprintf("sticker_%d.%s", i, ext)
		mime := mimeForExt(ext)

		// Upload to media service
		mediaID, err := uploadToMedia(httpClient, mediaBaseURL, internalSecret, uploaderID.String(), data, filename, mime)
		if err != nil {
			logger.Warn("upload failed", "index", i, "error", err)
			continue
		}

		fileURL := fmt.Sprintf("%s/file/%s/original.%s", r2BaseURL, mediaID, "octet-stream")

		if i == 0 {
			thumbnailURL = fileURL
		}

		stickerID := uuid.New()
		_, err = pool.Exec(ctx, `
			INSERT INTO stickers (id, pack_id, emoji, file_url, file_type, width, height, position)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		`, stickerID, packID, tgStk.Emoji, fileURL, ext, tgStk.Width, tgStk.Height, i)
		if err != nil {
			logger.Warn("insert sticker failed", "index", i, "error", err)
			continue
		}

		imported++
		time.Sleep(50 * time.Millisecond)
	}

	// Update pack thumbnail and sticker count
	_, _ = pool.Exec(ctx, `UPDATE sticker_packs SET thumbnail_url = $1, sticker_count = $2 WHERE id = $3`,
		thumbnailURL, imported, packID)

	if imported == 0 {
		_, _ = pool.Exec(ctx, `DELETE FROM sticker_packs WHERE id = $1`, packID)
		return fmt.Errorf("no stickers imported")
	}

	return nil
}

func uploadToMedia(client *http.Client, baseURL, internalSecret, uploaderID string, data []byte, filename, mime string) (string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("file", filename)
	if err != nil {
		return "", err
	}
	if _, err := part.Write(data); err != nil {
		return "", err
	}
	w.Close()

	req, err := http.NewRequest("POST", baseURL+"/media/upload", &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("X-User-ID", uploaderID)
	req.Header.Set("X-Internal-Token", internalSecret)

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 201 && resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("upload %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	return result.ID, nil
}

func mimeForExt(ext string) string {
	switch ext {
	case "tgs":
		return "application/x-tgsticker"
	case "webm":
		return "video/webm"
	default:
		return "image/webp"
	}
}

func buildR2BaseURL() string {
	publicURL := config.EnvOr("R2_PUBLIC_URL", "")
	if publicURL != "" {
		return strings.TrimRight(publicURL, "/")
	}
	endpoint := config.EnvOr("R2_ENDPOINT", "http://localhost:9000")
	bucket := config.EnvOr("R2_BUCKET", "orbit-media")
	return strings.TrimRight(endpoint, "/") + "/" + bucket
}

func cleanOldPacks(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `
		DELETE FROM sticker_packs WHERE id IN (
			SELECT DISTINCT sp.id FROM sticker_packs sp
			JOIN stickers s ON s.pack_id = sp.id
			WHERE s.file_type = 'svg'
		)
	`)
	return err
}

func openDatabase(ctx context.Context) (*pgxpool.Pool, error) {
	dsn, _, _ := config.DatabaseDSN()
	return pgxpool.New(ctx, dsn)
}

func resolveUploaderID(ctx context.Context, pool *pgxpool.Pool) (uuid.UUID, error) {
	var id uuid.UUID
	err := pool.QueryRow(ctx, `SELECT id FROM users WHERE role = 'admin' LIMIT 1`).Scan(&id)
	return id, err
}
