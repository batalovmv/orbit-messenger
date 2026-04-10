package tenor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
	"github.com/redis/go-redis/v9"
)

const (
	baseURL            = "https://tenor.googleapis.com/v2"
	defaultClientKey   = "orbit-messaging"
	requestTimeout     = 10 * time.Second
	trendingCacheTTL   = 5 * time.Minute
	rateLimitWindow    = time.Minute
	rateLimitPerWindow = 100
	defaultResultLimit = 20
	maxResultLimit     = 50
)

type Client struct {
	apiKey    string
	clientKey string
	http      *http.Client
	redis     *redis.Client
	logger    *slog.Logger
}

func NewClient(apiKey string, redisClient *redis.Client, logger *slog.Logger) *Client {
	if logger == nil {
		logger = slog.Default()
	}

	return &Client{
		apiKey:    strings.TrimSpace(apiKey),
		clientKey: defaultClientKey,
		http: &http.Client{
			Timeout: requestTimeout,
		},
		redis:  redisClient,
		logger: logger,
	}
}

func NewClientFromEnv(redisClient *redis.Client, logger *slog.Logger) *Client {
	return NewClient(os.Getenv("TENOR_API_KEY"), redisClient, logger)
}

func (c *Client) Search(ctx context.Context, query string, limit int, pos string) ([]model.TenorGIF, string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, "", apperror.BadRequest("Query is required")
	}

	params := url.Values{}
	params.Set("q", query)
	params.Set("limit", strconv.Itoa(normalizeResultLimit(limit)))
	if pos = strings.TrimSpace(pos); pos != "" {
		params.Set("pos", pos)
	}

	return c.fetch(ctx, "/search", params)
}

func (c *Client) Trending(ctx context.Context, limit int, pos string) ([]model.TenorGIF, string, error) {
	limit = normalizeResultLimit(limit)
	pos = strings.TrimSpace(pos)

	cacheKey := trendingCacheKey(limit, pos)
	if cached, nextPos, ok := c.getTrendingCache(ctx, cacheKey); ok {
		return cached, nextPos, nil
	}

	params := url.Values{}
	params.Set("limit", strconv.Itoa(limit))
	if pos != "" {
		params.Set("pos", pos)
	}

	gifs, nextPos, err := c.fetch(ctx, "/featured", params)
	if err != nil {
		return nil, "", err
	}

	c.setTrendingCache(ctx, cacheKey, gifs, nextPos)
	return gifs, nextPos, nil
}

type trendingCachePayload struct {
	GIFs    []model.TenorGIF `json:"gifs"`
	NextPos string           `json:"next_pos"`
}

func (c *Client) getTrendingCache(ctx context.Context, key string) ([]model.TenorGIF, string, bool) {
	if c.redis == nil {
		return nil, "", false
	}

	cached, err := c.redis.Get(ctx, key).Result()
	if err != nil {
		return nil, "", false
	}

	var payload trendingCachePayload
	if err := json.Unmarshal([]byte(cached), &payload); err != nil {
		c.logger.Warn("failed to decode Tenor trending cache", "error", err)
		return nil, "", false
	}

	return payload.GIFs, payload.NextPos, true
}

func (c *Client) setTrendingCache(ctx context.Context, key string, gifs []model.TenorGIF, nextPos string) {
	if c.redis == nil {
		return
	}

	payload, err := json.Marshal(trendingCachePayload{
		GIFs:    gifs,
		NextPos: nextPos,
	})
	if err != nil {
		c.logger.Warn("failed to encode Tenor trending cache", "error", err)
		return
	}

	if err := c.redis.Set(ctx, key, payload, trendingCacheTTL).Err(); err != nil {
		c.logger.Warn("failed to cache Tenor trending response", "error", err)
	}
}

func (c *Client) fetch(ctx context.Context, endpoint string, params url.Values) ([]model.TenorGIF, string, error) {
	if c.apiKey == "" {
		return nil, "", apperror.Internal("Tenor API key is not configured")
	}
	if c.http == nil {
		return nil, "", apperror.Internal("Tenor HTTP client is not configured")
	}

	if err := c.checkRateLimit(ctx); err != nil {
		return nil, "", err
	}

	params = cloneValues(params)
	params.Set("key", c.apiKey)
	params.Set("client_key", c.clientKey)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+endpoint+"?"+params.Encode(), nil)
	if err != nil {
		return nil, "", fmt.Errorf("create Tenor request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "OrbitMessenger/1.0")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("request Tenor API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, "", apperror.TooManyRequests("Tenor API rate limit exceeded")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		c.logger.Warn("Tenor API returned non-success status", "status", resp.StatusCode, "body", string(body))
		return nil, "", apperror.Internal("Tenor API request failed")
	}

	var payload tenorResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, "", fmt.Errorf("decode Tenor response: %w", err)
	}

	return mapTenorResults(payload.Results), payload.Next, nil
}

func (c *Client) checkRateLimit(ctx context.Context) error {
	if c.redis == nil {
		return nil
	}

	key := fmt.Sprintf("ratelimit:tenor:%s", time.Now().UTC().Format("200601021504"))
	if err := c.redis.SetNX(ctx, key, 0, rateLimitWindow).Err(); err != nil {
		c.logger.Warn("Tenor rate limit Redis init failed", "error", err)
		return nil
	}

	count, err := c.redis.Incr(ctx, key).Result()
	if err != nil {
		c.logger.Warn("Tenor rate limit Redis unavailable", "error", err)
		return nil
	}
	if count > rateLimitPerWindow {
		return apperror.TooManyRequests("Tenor rate limit exceeded")
	}
	return nil
}

type tenorResponse struct {
	Results []tenorResult `json:"results"`
	Next    string        `json:"next"`
}

type tenorResult struct {
	ID                 string                `json:"id"`
	Title              string                `json:"title"`
	ContentDescription string                `json:"content_description"`
	MediaFormats       map[string]tenorMedia `json:"media_formats"`
}

type tenorMedia struct {
	URL     string `json:"url"`
	Preview string `json:"preview"`
	Dims    []int  `json:"dims"`
}

func mapTenorResults(results []tenorResult) []model.TenorGIF {
	if len(results) == 0 {
		return nil
	}

	mapped := make([]model.TenorGIF, 0, len(results))
	for _, result := range results {
		primary := selectPrimaryMedia(result.MediaFormats)
		if primary.URL == "" {
			continue
		}

		preview := selectPreviewMedia(result.MediaFormats, primary)
		title := strings.TrimSpace(result.Title)
		if title == "" {
			title = strings.TrimSpace(result.ContentDescription)
		}

		mapped = append(mapped, model.TenorGIF{
			TenorID:    result.ID,
			URL:        primary.URL,
			PreviewURL: preview,
			Width:      mediaWidth(primary),
			Height:     mediaHeight(primary),
			Title:      title,
		})
	}

	return mapped
}

func selectPrimaryMedia(media map[string]tenorMedia) tenorMedia {
	for _, key := range []string{"mp4", "mediummp4", "nanomp4", "tinymp4", "gif", "mediumgif", "tinygif"} {
		if candidate, ok := media[key]; ok && candidate.URL != "" {
			return candidate
		}
	}
	return tenorMedia{}
}

func selectPreviewMedia(media map[string]tenorMedia, primary tenorMedia) string {
	for _, key := range []string{"tinygif", "nanogif", "gif", "mediumgif", "tinymp4", "nanomp4"} {
		if candidate, ok := media[key]; ok {
			if candidate.Preview != "" {
				return candidate.Preview
			}
			if candidate.URL != "" {
				return candidate.URL
			}
		}
	}
	if primary.Preview != "" {
		return primary.Preview
	}
	return primary.URL
}

func mediaWidth(media tenorMedia) int {
	if len(media.Dims) > 0 {
		return media.Dims[0]
	}
	return 0
}

func mediaHeight(media tenorMedia) int {
	if len(media.Dims) > 1 {
		return media.Dims[1]
	}
	return 0
}

func normalizeResultLimit(limit int) int {
	if limit <= 0 || limit > maxResultLimit {
		return defaultResultLimit
	}
	return limit
}

func trendingCacheKey(limit int, pos string) string {
	if pos == "" {
		pos = "first"
	}
	return fmt.Sprintf("tenor:trending:%d:%s", limit, pos)
}

func cloneValues(values url.Values) url.Values {
	cloned := make(url.Values, len(values))
	for key, items := range values {
		if len(items) == 0 {
			cloned[key] = nil
			continue
		}
		copyItems := make([]string, len(items))
		copy(copyItems, items)
		cloned[key] = copyItems
	}
	return cloned
}
