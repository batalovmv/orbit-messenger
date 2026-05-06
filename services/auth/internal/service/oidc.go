// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

// Package service — OIDC SSO (ADR 006).
//
// This file is the env-configured single-provider OIDC implementation that
// issues normal access+refresh tokens once a corporate OIDC sign-in succeeds.
// Multi-provider support, admin UI, and directory-sync deactivation are
// deliberately out of scope for this slice — see ADR 006 for the cuts.

package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
	"golang.org/x/oauth2"

	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/services/auth/internal/model"
)

// OIDCConfig is populated from env at startup. ProviderKey == "" disables
// the routes (handler returns 404). All non-empty fields are required.
type OIDCConfig struct {
	ProviderKey         string   // path segment, e.g. "google"
	Issuer              string   // discovery base, e.g. https://accounts.google.com
	ClientID            string
	ClientSecret        string
	RedirectURL         string   // must match what's registered with the IdP
	AllowedEmailDomains []string // empty = any domain
	FrontendURL         string   // post-callback redirect target
}

// Enabled reports whether the auth service should expose /oidc routes.
func (c *OIDCConfig) Enabled() bool {
	return c != nil && c.ProviderKey != "" && c.Issuer != "" &&
		c.ClientID != "" && c.ClientSecret != "" && c.RedirectURL != ""
}

// Provider wraps the discovered OIDC provider + verified id_token validator
// and the oauth2 exchange client. Built once at startup so /authorize and
// /callback share the same JWKS cache.
type Provider struct {
	cfg      *OIDCConfig
	provider *oidc.Provider
	verifier *oidc.IDTokenVerifier
	oauth    *oauth2.Config
}

// NewProvider runs OIDC discovery against cfg.Issuer and returns a ready
// Provider. ctx is honoured for the discovery HTTP call; pass a short timeout
// at startup so the service doesn't hang forever if the IdP is down.
func NewProvider(ctx context.Context, cfg *OIDCConfig) (*Provider, error) {
	if !cfg.Enabled() {
		return nil, errors.New("oidc: provider not configured")
	}
	p, err := oidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery %q: %w", cfg.Issuer, err)
	}
	return &Provider{
		cfg:      cfg,
		provider: p,
		verifier: p.Verifier(&oidc.Config{ClientID: cfg.ClientID}),
		oauth: &oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  cfg.RedirectURL,
			Endpoint:     p.Endpoint(),
			Scopes:       []string{oidc.ScopeOpenID, "email", "profile"},
		},
	}, nil
}

// stateEntry is what we stash in Redis between /authorize and /callback.
type stateEntry struct {
	Verifier string `json:"v"`
	Nonce    string `json:"n"`
	ReturnTo string `json:"r"`
}

const stateTTL = 5 * time.Minute

func stateKey(state string) string { return "oidc:state:" + state }

// AuthorizeURL is called by the /authorize handler. It allocates a fresh
// state + PKCE verifier + nonce, persists them in Redis under the state key
// (single-use via GETDEL on callback), and returns the URL the browser
// should be redirected to. returnTo is sanitised by the caller.
func (s *AuthService) AuthorizeURL(ctx context.Context, p *Provider, returnTo string) (string, error) {
	if p == nil {
		return "", apperror.NotFound("OIDC not configured")
	}
	state, err := randomURLBytes(32)
	if err != nil {
		return "", err
	}
	verifier, err := randomURLBytes(48) // 64-char base64url, comfortably above the 43-char min
	if err != nil {
		return "", err
	}
	nonce, err := randomURLBytes(24)
	if err != nil {
		return "", err
	}
	challenge := pkceS256(verifier)

	entry := stateEntry{Verifier: verifier, Nonce: nonce, ReturnTo: returnTo}
	if err := s.redis.Set(ctx, stateKey(state), encodeState(entry), stateTTL).Err(); err != nil {
		return "", fmt.Errorf("oidc: persist state: %w", err)
	}

	return p.oauth.AuthCodeURL(state,
		oauth2.SetAuthURLParam("code_challenge", challenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
		oidc.Nonce(nonce),
	), nil
}

// CallbackResult is what the handler needs to issue cookies + redirect.
type CallbackResult struct {
	Tokens   *TokenPair
	User     *model.User
	ReturnTo string // safe path under FrontendURL
}

// HandleCallback validates state, exchanges the code, parses the id_token,
// resolves or creates the local user, and issues a normal token pair.
//
// ip / userAgent are forwarded to createTokenPair for the sessions row so
// the new SSO session shows up alongside password sessions in the user's
// "active sessions" list (consistent revocation semantics).
func (s *AuthService) HandleCallback(ctx context.Context, p *Provider, state, code, ip, userAgent string) (*CallbackResult, error) {
	if p == nil {
		return nil, apperror.NotFound("OIDC not configured")
	}
	if state == "" || code == "" {
		return nil, apperror.BadRequest("Missing state or code")
	}

	raw, err := s.redis.GetDel(ctx, stateKey(state)).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, apperror.BadRequest("Unknown or expired OIDC state")
		}
		return nil, fmt.Errorf("oidc: load state: %w", err)
	}
	entry, err := decodeState(raw)
	if err != nil {
		return nil, apperror.BadRequest("Corrupt OIDC state")
	}

	tokens, err := p.oauth.Exchange(ctx, code,
		oauth2.SetAuthURLParam("code_verifier", entry.Verifier),
	)
	if err != nil {
		return nil, fmt.Errorf("oidc: token exchange: %w", err)
	}
	rawIDToken, ok := tokens.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return nil, apperror.BadRequest("OIDC response missing id_token")
	}
	idToken, err := p.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, fmt.Errorf("oidc: id_token verify: %w", err)
	}
	if idToken.Nonce != entry.Nonce {
		return nil, apperror.BadRequest("OIDC nonce mismatch")
	}

	var claims struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return nil, fmt.Errorf("oidc: parse claims: %w", err)
	}
	email := strings.ToLower(strings.TrimSpace(claims.Email))
	if email == "" {
		return nil, apperror.BadRequest("OIDC id_token missing email")
	}
	// EmailVerified is optional in the spec but we require it — an
	// unverified address is a phishing surface (anyone could mint an
	// id_token claiming any address against an unscrupulous IdP).
	if !claims.EmailVerified {
		return nil, apperror.Forbidden("OIDC email not verified by provider")
	}
	if !emailDomainAllowed(email, p.cfg.AllowedEmailDomains) {
		slog.WarnContext(ctx, "oidc: email domain not allowed",
			"email", email, "subject", idToken.Subject)
		return nil, apperror.Forbidden("Email domain not allowed for SSO")
	}

	u, err := s.resolveOIDCUser(ctx, p.cfg.ProviderKey, idToken.Subject, email, claims.Name)
	if err != nil {
		return nil, err
	}
	if !u.IsActive {
		return nil, apperror.Forbidden("Account is deactivated")
	}

	pair, err := s.createTokenPair(ctx, u.ID, nil, ip, userAgent)
	if err != nil {
		return nil, err
	}

	return &CallbackResult{
		Tokens:   pair,
		User:     u,
		ReturnTo: sanitiseReturnTo(entry.ReturnTo, p.cfg.FrontendURL),
	}, nil
}

// resolveOIDCUser implements the three-step lookup described in ADR 006:
// (1) by (provider, subject), (2) by email — if the existing user is not
// already linked to a different OIDC identity, bind this subject to them,
// (3) create a fresh OIDC user and run the welcome flow.
func (s *AuthService) resolveOIDCUser(ctx context.Context, providerKey, subject, email, displayName string) (*model.User, error) {
	if existing, err := s.users.GetByOIDCSubject(ctx, providerKey, subject); err != nil {
		return nil, fmt.Errorf("oidc: lookup by subject: %w", err)
	} else if existing != nil {
		return existing, nil
	}

	if byEmail, err := s.users.GetByEmail(ctx, email); err != nil {
		return nil, fmt.Errorf("oidc: lookup by email: %w", err)
	} else if byEmail != nil {
		if err := s.users.LinkOIDCSubject(ctx, byEmail.ID, providerKey, subject); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				// Row exists but already bound to a different (provider, subject).
				// Refuse rather than silently re-bind — could be a hijack attempt.
				return nil, apperror.Conflict("Account is already linked to a different SSO identity")
			}
			return nil, fmt.Errorf("oidc: link subject: %w", err)
		}
		slog.InfoContext(ctx, "oidc: linked existing user", "user_id", byEmail.ID, "provider", providerKey)
		return byEmail, nil
	}

	name := strings.TrimSpace(displayName)
	if name == "" {
		// Best-effort fallback so the UI doesn't render an empty name strip.
		name = strings.SplitN(email, "@", 2)[0]
	}
	u := &model.User{
		Email:       email,
		DisplayName: name,
		Role:        "member",
	}
	if err := s.users.CreateOIDCUser(ctx, u, providerKey, subject); err != nil {
		return nil, fmt.Errorf("oidc: create user: %w", err)
	}
	slog.InfoContext(ctx, "oidc: created new user", "user_id", u.ID, "provider", providerKey)

	// Reuse the existing welcome flow — see auth_service.Register for the
	// rationale (best-effort, retry once, admin backfill is the safety net).
	s.joinDefaultChatsBestEffort(ctx, u.ID)
	return u, nil
}

// --- helpers ---

func randomURLBytes(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("rand: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func pkceS256(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

// encodeState / decodeState use a deliberately tiny field-name format ("v",
// "n", "r") so the Redis value stays small even when ReturnTo is long.
func encodeState(e stateEntry) string {
	return e.Verifier + "|" + e.Nonce + "|" + e.ReturnTo
}

func decodeState(raw string) (stateEntry, error) {
	parts := strings.SplitN(raw, "|", 3)
	if len(parts) != 3 {
		return stateEntry{}, errors.New("oidc: bad state encoding")
	}
	return stateEntry{Verifier: parts[0], Nonce: parts[1], ReturnTo: parts[2]}, nil
}

func emailDomainAllowed(email string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	at := strings.LastIndex(email, "@")
	if at < 0 {
		return false
	}
	domain := strings.ToLower(email[at+1:])
	for _, d := range allowed {
		if strings.EqualFold(strings.TrimSpace(d), domain) {
			return true
		}
	}
	return false
}

// sanitiseReturnTo accepts only same-origin paths starting with "/" and
// without a "//" prefix (which a browser would parse as a protocol-relative
// URL). Everything else falls back to the frontend root — the open-redirect
// surface is exactly zero by construction.
func sanitiseReturnTo(returnTo, frontendURL string) string {
	if returnTo == "" || !strings.HasPrefix(returnTo, "/") || strings.HasPrefix(returnTo, "//") {
		return frontendURL
	}
	// Reject anything with a scheme or host smuggled through encoding.
	if u, err := url.Parse(returnTo); err != nil || u.Host != "" || u.Scheme != "" {
		return frontendURL
	}
	if frontendURL == "" {
		return returnTo
	}
	return strings.TrimRight(frontendURL, "/") + returnTo
}

// loadOIDCConfigFromEnv reads the OIDC_* env block. Returned config has
// Enabled() == false when OIDC_PROVIDER_KEY is empty so callers can skip
// provider discovery cleanly. Lives here (not in main.go) so the parsing
// rules — especially the CSV split — are unit-testable.
func LoadOIDCConfigFromEnv(getenv func(string) string) *OIDCConfig {
	cfg := &OIDCConfig{
		ProviderKey:  strings.TrimSpace(getenv("OIDC_PROVIDER_KEY")),
		Issuer:       strings.TrimSpace(getenv("OIDC_ISSUER")),
		ClientID:     strings.TrimSpace(getenv("OIDC_CLIENT_ID")),
		ClientSecret: getenv("OIDC_CLIENT_SECRET"),
		RedirectURL:  strings.TrimSpace(getenv("OIDC_REDIRECT_URL")),
		FrontendURL:  strings.TrimSpace(getenv("OIDC_FRONTEND_URL")),
	}
	if domains := strings.TrimSpace(getenv("OIDC_ALLOWED_EMAIL_DOMAINS")); domains != "" {
		for _, d := range strings.Split(domains, ",") {
			d = strings.ToLower(strings.TrimSpace(d))
			if d != "" {
				cfg.AllowedEmailDomains = append(cfg.AllowedEmailDomains, d)
			}
		}
	}
	return cfg
}

// EnsureUUIDv4 is exposed for the handler so it can avoid importing google/uuid.
func EnsureUUIDv4(s string) (uuid.UUID, error) { return uuid.Parse(s) }
