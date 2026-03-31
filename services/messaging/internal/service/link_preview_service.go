package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"golang.org/x/net/html"
)

const (
	linkPreviewCacheTTL = 24 * time.Hour
	linkPreviewTimeout  = 10 * time.Second
	maxBodySize         = 2 * 1024 * 1024 // 2 MB
)

// LinkPreview represents parsed OG tags for a URL.
type LinkPreview struct {
	URL         string `json:"url"`
	DisplayURL  string `json:"display_url"`
	SiteName    string `json:"site_name,omitempty"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	ImageURL    string `json:"image,omitempty"`
	Type        string `json:"type,omitempty"`
}

type LinkPreviewService struct {
	client *http.Client
	redis  *redis.Client
	logger *slog.Logger
}

func NewLinkPreviewService(rdb *redis.Client, logger *slog.Logger) *LinkPreviewService {
	// Custom dialer that checks resolved IPs at connection time (prevents DNS rebinding)
	safeDialer := &net.Dialer{Timeout: 5 * time.Second}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, fmt.Errorf("invalid address: %w", err)
			}
			ips, err := net.DefaultResolver.LookupHost(ctx, host)
			if err != nil {
				return nil, fmt.Errorf("DNS lookup failed: %w", err)
			}
			for _, ipStr := range ips {
				ip := net.ParseIP(ipStr)
				if ip == nil || ip.IsLoopback() || ip.IsPrivate() ||
					ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
					return nil, fmt.Errorf("private address blocked: %s", ipStr)
				}
			}
			// Connect to the first safe IP directly (no second DNS lookup)
			return safeDialer.DialContext(ctx, network, net.JoinHostPort(ips[0], port))
		},
	}

	return &LinkPreviewService{
		client: &http.Client{
			Timeout:   linkPreviewTimeout,
			Transport: transport,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 5 {
					return fmt.Errorf("too many redirects")
				}
				return nil
			},
		},
		redis:  rdb,
		logger: logger,
	}
}

// CheckRateLimit enforces a per-user rate limit for link previews (30 req/min).
// Returns true if the request is allowed, false if rate limited.
func (s *LinkPreviewService) CheckRateLimit(ctx context.Context, userID string) bool {
	key := fmt.Sprintf("ratelimit:linkpreview:%s", userID)
	count, err := s.redis.Incr(ctx, key).Result()
	if err != nil {
		// Fail-closed: deny on Redis error
		s.logger.Error("link preview rate limit Redis error", "error", err)
		return false
	}
	if count == 1 {
		s.redis.Expire(ctx, key, 60*time.Second)
	}
	return count <= 30
}

func (s *LinkPreviewService) FetchPreview(ctx context.Context, rawURL string) (*LinkPreview, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}

	// Only allow http/https
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("unsupported scheme: %s", parsed.Scheme)
	}

	// Block obvious private hostnames early (SSRF protection).
	// Full IP-level check happens in the custom DialContext to prevent DNS rebinding.
	host := strings.ToLower(parsed.Hostname())
	if host == "localhost" || host == "0.0.0.0" {
		return nil, fmt.Errorf("private addresses are not allowed")
	}

	// Check cache
	cacheKey := linkPreviewCacheKey(rawURL)
	if cached, err := s.redis.Get(ctx, cacheKey).Result(); err == nil {
		var preview LinkPreview
		if json.Unmarshal([]byte(cached), &preview) == nil {
			return &preview, nil
		}
	}

	// Fetch the page
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "OrbitBot/1.0")
	req.Header.Set("Accept", "text/html")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "text/html") {
		return nil, fmt.Errorf("not HTML: %s", contentType)
	}

	body := io.LimitReader(resp.Body, maxBodySize)

	preview, err := parseOGTags(body, rawURL, parsed)
	if err != nil {
		return nil, fmt.Errorf("parse OG tags: %w", err)
	}

	// Cache result
	if data, err := json.Marshal(preview); err == nil {
		if err := s.redis.Set(ctx, cacheKey, string(data), linkPreviewCacheTTL).Err(); err != nil {
			s.logger.Warn("failed to cache link preview", "error", err)
		}
	}

	return preview, nil
}

func parseOGTags(r io.Reader, rawURL string, parsed *url.URL) (*LinkPreview, error) {
	tokenizer := html.NewTokenizer(r)

	preview := &LinkPreview{
		URL:        rawURL,
		DisplayURL: parsed.Host + parsed.Path,
	}

	var titleFromTag string
	var inTitle bool
	var descFromMeta string

	for {
		tt := tokenizer.Next()
		switch tt {
		case html.ErrorToken:
			// EOF or error — finalize
			if preview.Title == "" {
				preview.Title = titleFromTag
			}
			if preview.Description == "" {
				preview.Description = descFromMeta
			}
			if preview.Title == "" && preview.Description == "" {
				return nil, fmt.Errorf("no metadata found")
			}
			return preview, nil

		case html.StartTagToken, html.SelfClosingTagToken:
			tn, hasAttr := tokenizer.TagName()
			tag := string(tn)

			if tag == "title" {
				inTitle = true
				continue
			}

			if tag == "meta" && hasAttr {
				attrs := collectAttrs(tokenizer)
				prop := attrs["property"]
				name := attrs["name"]
				content := attrs["content"]

				switch prop {
				case "og:title":
					preview.Title = content
				case "og:description":
					preview.Description = content
				case "og:image":
					if imgURL, err := url.Parse(content); err == nil &&
						(imgURL.Scheme == "http" || imgURL.Scheme == "https") {
						// Block private/internal hostnames in og:image to prevent internal IP probing
						imgHost := strings.ToLower(imgURL.Hostname())
						ip := net.ParseIP(imgHost)
						blocked := imgHost == "localhost" || imgHost == "0.0.0.0" ||
							(ip != nil && (ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast()))
						if !blocked {
							preview.ImageURL = content
						}
					}
				case "og:site_name":
					preview.SiteName = content
				case "og:type":
					preview.Type = content
				case "og:url":
					// Validate og:url to prevent URL spoofing / phishing
					if content != "" {
						if ogURL, err := url.Parse(content); err == nil &&
							(ogURL.Scheme == "http" || ogURL.Scheme == "https") {
							preview.URL = content
						}
					}
				}

				if name == "description" && content != "" {
					descFromMeta = content
				}
			}

			// Stop after </head> to avoid parsing the body
			if tag == "body" {
				if preview.Title == "" {
					preview.Title = titleFromTag
				}
				if preview.Description == "" {
					preview.Description = descFromMeta
				}
				if preview.Title == "" && preview.Description == "" {
					return nil, fmt.Errorf("no metadata found")
				}
				return preview, nil
			}

		case html.TextToken:
			if inTitle {
				titleFromTag = strings.TrimSpace(string(tokenizer.Text()))
				inTitle = false
			}

		case html.EndTagToken:
			tn, _ := tokenizer.TagName()
			if string(tn) == "title" {
				inTitle = false
			}
		}
	}
}

func collectAttrs(z *html.Tokenizer) map[string]string {
	attrs := make(map[string]string)
	for {
		key, val, more := z.TagAttr()
		if len(key) > 0 {
			attrs[string(key)] = string(val)
		}
		if !more {
			break
		}
	}
	return attrs
}

func linkPreviewCacheKey(rawURL string) string {
	h := sha256.Sum256([]byte(rawURL))
	return fmt.Sprintf("linkpreview:%x", h[:8])
}

