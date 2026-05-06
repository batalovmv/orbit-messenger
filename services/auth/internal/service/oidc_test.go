// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package service

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/mst-corp/orbit/services/auth/internal/model"
	"github.com/mst-corp/orbit/services/auth/internal/store"
)

// ---------------------------------------------------------------------------
// Mock OIDC provider — minimal /.well-known/openid-configuration + JWKS +
// /token endpoint. We don't simulate /authorize because we drive the flow
// directly from HandleCallback (bypassing the browser hop), which is what
// the unit test actually wants to exercise.
// ---------------------------------------------------------------------------

type mockIdP struct {
	server     *httptest.Server
	rsaKey     *rsa.PrivateKey
	keyID      string
	clientID   string
	clientSec  string
	subject    string
	email      string
	emailVerif bool
	nonce      string
	tokenCalls int32
}

func newMockIdP(t *testing.T) *mockIdP {
	t.Helper()
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa keygen: %v", err)
	}
	m := &mockIdP{
		rsaKey:     rsaKey,
		keyID:      "test-key-1",
		clientID:   "orbit-test-client",
		clientSec:  "orbit-test-secret",
		subject:    "subject-12345",
		email:      "alice@example.com",
		emailVerif: true,
	}
	mux := http.NewServeMux()
	m.server = httptest.NewServer(mux)

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                 m.server.URL,
			"authorization_endpoint": m.server.URL + "/authorize",
			"token_endpoint":         m.server.URL + "/token",
			"jwks_uri":               m.server.URL + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
			"response_types_supported":              []string{"code"},
			"subject_types_supported":               []string{"public"},
		})
	})

	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		jwk := jose.JSONWebKey{Key: &m.rsaKey.PublicKey, KeyID: m.keyID, Algorithm: "RS256", Use: "sig"}
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{jwk}})
	})

	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&m.tokenCalls, 1)
		// Validate the basic auth / form params just enough that the
		// success path proves the client supplied PKCE etc. We don't
		// try to faithfully implement RFC 6749 here.
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if r.Form.Get("client_id") != m.clientID && r.PostForm.Get("client_id") != m.clientID {
			user, _, ok := r.BasicAuth()
			if !ok || user != m.clientID {
				http.Error(w, "bad client_id", 401)
				return
			}
		}
		if r.Form.Get("code_verifier") == "" {
			http.Error(w, "missing code_verifier", 400)
			return
		}
		idToken := m.signIDToken(map[string]any{
			"iss":            m.server.URL,
			"aud":            m.clientID,
			"sub":            m.subject,
			"email":          m.email,
			"email_verified": m.emailVerif,
			"name":           "Alice Tester",
			"iat":            time.Now().Unix(),
			"exp":            time.Now().Add(5 * time.Minute).Unix(),
			"nonce":          m.nonce,
		})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "ignored-by-tests",
			"token_type":   "Bearer",
			"expires_in":   300,
			"id_token":     idToken,
		})
	})

	t.Cleanup(m.server.Close)
	return m
}

func (m *mockIdP) signIDToken(claims map[string]any) string {
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: m.rsaKey},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", m.keyID),
	)
	if err != nil {
		panic(err)
	}
	tok, err := jwt.Signed(signer).Claims(claims).Serialize()
	if err != nil {
		panic(err)
	}
	return tok
}

// ---------------------------------------------------------------------------
// Helpers shared by all the cases below.
// ---------------------------------------------------------------------------

func setupOIDCTest(t *testing.T) (*AuthService, *Provider, *mockIdP, *redis.Client, *recordingUserStore) {
	t.Helper()
	mock := newMockIdP(t)

	cfg := &OIDCConfig{
		ProviderKey:  "google",
		Issuer:       mock.server.URL,
		ClientID:     mock.clientID,
		ClientSecret: mock.clientSec,
		RedirectURL:  "http://orbit.local/api/v1/auth/oidc/google/callback",
		FrontendURL:  "http://orbit.local/",
	}
	prov, err := NewProvider(context.Background(), cfg)
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}

	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	users := newRecordingUserStore()
	svc := &AuthService{
		users:    users,
		sessions: &noopSessionStore{},
		invites:  &fakeInviteStore{},
		redis:    rdb,
		cfg: &Config{
			JWTSecret:  "test-jwt-secret-32-chars-minimum!!",
			AccessTTL:  15 * time.Minute,
			RefreshTTL: 24 * time.Hour,
		},
		logger:     slog.Default(),
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	return svc, prov, mock, rdb, users
}

// noopSessionStore is enough for tests that don't care about sessions table
// content — createTokenPair calls Create() and we just acknowledge.
type noopSessionStore struct{}

func (n *noopSessionStore) Create(_ context.Context, sess *model.Session) error {
	if sess.ID == uuid.Nil {
		sess.ID = uuid.New()
	}
	return nil
}
func (n *noopSessionStore) GetByTokenHash(_ context.Context, _ string) (*model.Session, error) {
	return nil, nil
}
func (n *noopSessionStore) GetByID(_ context.Context, _ uuid.UUID) (*model.Session, error) {
	return nil, nil
}
func (n *noopSessionStore) ListByUser(_ context.Context, _ uuid.UUID) ([]model.Session, error) {
	return nil, nil
}
func (n *noopSessionStore) DeleteByID(_ context.Context, _ uuid.UUID, _ uuid.UUID) error {
	return nil
}
func (n *noopSessionStore) DeleteByTokenHash(_ context.Context, _ string) error { return nil }
func (n *noopSessionStore) DeleteAndReturnByTokenHash(_ context.Context, _ string) (*model.Session, error) {
	return nil, nil
}
func (n *noopSessionStore) DeleteAllByUser(_ context.Context, _ uuid.UUID) error { return nil }

// recordingUserStore tracks which OIDC store methods were hit so each test
// case can assert on resolution path (subject hit / email hit / created).
type recordingUserStore struct {
	bySubject map[string]*model.User // key: provider+":"+subject
	byEmail   map[string]*model.User
	created   []*model.User
	linked    []linkRecord
}

type linkRecord struct {
	UserID   uuid.UUID
	Provider string
	Subject  string
}

func newRecordingUserStore() *recordingUserStore {
	return &recordingUserStore{
		bySubject: map[string]*model.User{},
		byEmail:   map[string]*model.User{},
	}
}

func (r *recordingUserStore) Create(_ context.Context, _ *model.User) error { return nil }
func (r *recordingUserStore) CreateIfNoAdmins(_ context.Context, _ *model.User) error {
	return nil
}
func (r *recordingUserStore) GetByID(_ context.Context, id uuid.UUID) (*model.User, error) {
	for _, u := range r.byEmail {
		if u.ID == id {
			return u, nil
		}
	}
	return nil, nil
}
func (r *recordingUserStore) GetByEmail(_ context.Context, email string) (*model.User, error) {
	if u, ok := r.byEmail[email]; ok {
		return u, nil
	}
	return nil, nil
}
func (r *recordingUserStore) GetNotificationPriorityMode(_ context.Context, _ uuid.UUID) (string, error) {
	return "smart", nil
}
func (r *recordingUserStore) Update(_ context.Context, _ *model.User) error { return nil }
func (r *recordingUserStore) CountAdmins(_ context.Context) (int, error)    { return 0, nil }
func (r *recordingUserStore) UpdatePassword(_ context.Context, _ uuid.UUID, _ string) error {
	return nil
}
func (r *recordingUserStore) UpdateTOTP(_ context.Context, _ uuid.UUID, _ *string, _ bool) error {
	return nil
}
func (r *recordingUserStore) EnableTOTPAndRevokeSessions(_ context.Context, _ uuid.UUID, _ string) error {
	return nil
}
func (r *recordingUserStore) UpdateNotificationPriorityMode(_ context.Context, _ uuid.UUID, _ string) error {
	return nil
}

func (r *recordingUserStore) GetByOIDCSubject(_ context.Context, provider, subject string) (*model.User, error) {
	if u, ok := r.bySubject[provider+":"+subject]; ok {
		return u, nil
	}
	return nil, nil
}
func (r *recordingUserStore) LinkOIDCSubject(_ context.Context, userID uuid.UUID, provider, subject string) error {
	r.linked = append(r.linked, linkRecord{userID, provider, subject})
	for _, u := range r.byEmail {
		if u.ID == userID {
			r.bySubject[provider+":"+subject] = u
			return nil
		}
	}
	return nil
}
func (r *recordingUserStore) CreateOIDCUser(_ context.Context, u *model.User, provider, subject string) error {
	if u.ID == uuid.Nil {
		u.ID = uuid.New()
	}
	u.IsActive = true
	r.byEmail[u.Email] = u
	r.bySubject[provider+":"+subject] = u
	r.created = append(r.created, u)
	return nil
}
func (r *recordingUserStore) Deactivate(_ context.Context, id uuid.UUID) error {
	for _, u := range r.byEmail {
		if u.ID == id {
			u.IsActive = false
		}
	}
	return nil
}
func (r *recordingUserStore) ListOIDCActiveUsers(_ context.Context, _ string) ([]store.OIDCActiveUser, error) {
	return nil, nil
}

// driveCallback walks the AuthorizeURL → /token → HandleCallback path
// without spinning up a real browser. Returns the result and the issued
// state, so each case can vary the input.
func driveCallback(t *testing.T, svc *AuthService, prov *Provider, mock *mockIdP) (*CallbackResult, error) {
	t.Helper()
	authURL, err := svc.AuthorizeURL(context.Background(), prov, "/inbox")
	if err != nil {
		t.Fatalf("AuthorizeURL: %v", err)
	}
	parsed, _ := url.Parse(authURL)
	state := parsed.Query().Get("state")
	mock.nonce = parsed.Query().Get("nonce")
	if state == "" || mock.nonce == "" {
		t.Fatalf("authorize URL missing state/nonce: %s", authURL)
	}
	if got := parsed.Query().Get("code_challenge_method"); got != "S256" {
		t.Fatalf("expected code_challenge_method=S256, got %q", got)
	}
	if c := parsed.Query().Get("code_challenge"); len(c) < 43 {
		t.Fatalf("code_challenge too short: %q", c)
	}
	return svc.HandleCallback(context.Background(), prov, state, "test-auth-code", "127.0.0.1", "go-test")
}

// ---------------------------------------------------------------------------
// Cases
// ---------------------------------------------------------------------------

func TestOIDC_Authorize_StoresStateInRedis(t *testing.T) {
	svc, prov, _, rdb, _ := setupOIDCTest(t)

	authURL, err := svc.AuthorizeURL(context.Background(), prov, "/some/path")
	if err != nil {
		t.Fatalf("AuthorizeURL: %v", err)
	}
	parsed, _ := url.Parse(authURL)
	state := parsed.Query().Get("state")
	if state == "" {
		t.Fatal("state empty")
	}
	if val, err := rdb.Get(context.Background(), stateKey(state)).Result(); err != nil || val == "" {
		t.Fatalf("state not in redis: val=%q err=%v", val, err)
	}
}

func TestOIDC_Callback_CreatesNewUser(t *testing.T) {
	svc, prov, mock, _, users := setupOIDCTest(t)

	res, err := driveCallback(t, svc, prov, mock)
	if err != nil {
		t.Fatalf("HandleCallback: %v", err)
	}
	if res.User.Email != mock.email {
		t.Errorf("expected email %s, got %s", mock.email, res.User.Email)
	}
	if len(users.created) != 1 {
		t.Fatalf("expected 1 created user, got %d", len(users.created))
	}
	if res.Tokens.AccessToken == "" {
		t.Error("access token empty")
	}
	if res.ReturnTo != "http://orbit.local/inbox" {
		t.Errorf("unexpected return_to: %s", res.ReturnTo)
	}
}

func TestOIDC_Callback_LinksExistingEmailUser(t *testing.T) {
	svc, prov, mock, _, users := setupOIDCTest(t)

	pre := &model.User{
		ID:       uuid.New(),
		Email:    mock.email,
		IsActive: true,
		Role:     "member",
	}
	users.byEmail[mock.email] = pre

	res, err := driveCallback(t, svc, prov, mock)
	if err != nil {
		t.Fatalf("HandleCallback: %v", err)
	}
	if res.User.ID != pre.ID {
		t.Errorf("expected to reuse existing user; got %v vs %v", res.User.ID, pre.ID)
	}
	if len(users.created) != 0 {
		t.Errorf("must not create when email matches; created=%d", len(users.created))
	}
	if len(users.linked) != 1 || users.linked[0].UserID != pre.ID {
		t.Errorf("expected one link record for %v; got %+v", pre.ID, users.linked)
	}
}

func TestOIDC_Callback_ReusesByOIDCSubject(t *testing.T) {
	svc, prov, mock, _, users := setupOIDCTest(t)

	pre := &model.User{ID: uuid.New(), Email: mock.email, IsActive: true, Role: "member"}
	users.bySubject["google:"+mock.subject] = pre
	users.byEmail[mock.email] = pre // also reachable via GetByID for token pair issue

	res, err := driveCallback(t, svc, prov, mock)
	if err != nil {
		t.Fatalf("HandleCallback: %v", err)
	}
	if res.User.ID != pre.ID {
		t.Errorf("expected existing-by-subject reuse")
	}
	if len(users.created) != 0 || len(users.linked) != 0 {
		t.Errorf("subject hit must not link or create; created=%d linked=%d",
			len(users.created), len(users.linked))
	}
}

func TestOIDC_Callback_RejectsUnknownState(t *testing.T) {
	svc, prov, _, _, _ := setupOIDCTest(t)

	_, err := svc.HandleCallback(context.Background(), prov, "totally-bogus-state", "code", "ip", "ua")
	if err == nil || !strings.Contains(err.Error(), "OIDC state") {
		t.Errorf("expected unknown-state rejection, got %v", err)
	}
}

func TestOIDC_Callback_RejectsDisallowedDomain(t *testing.T) {
	svc, prov, mock, _, _ := setupOIDCTest(t)
	prov.cfg.AllowedEmailDomains = []string{"example.org"}

	_, err := driveCallback(t, svc, prov, mock)
	if err == nil || !strings.Contains(err.Error(), "domain") {
		t.Errorf("expected domain rejection, got %v", err)
	}
}

func TestOIDC_Callback_RejectsUnverifiedEmail(t *testing.T) {
	svc, prov, mock, _, _ := setupOIDCTest(t)
	mock.emailVerif = false

	_, err := driveCallback(t, svc, prov, mock)
	if err == nil || !strings.Contains(err.Error(), "verified") {
		t.Errorf("expected unverified-email rejection, got %v", err)
	}
}

func TestOIDC_Callback_StateIsSingleUse(t *testing.T) {
	svc, prov, mock, _, _ := setupOIDCTest(t)

	authURL, _ := svc.AuthorizeURL(context.Background(), prov, "/")
	parsed, _ := url.Parse(authURL)
	state := parsed.Query().Get("state")
	mock.nonce = parsed.Query().Get("nonce")

	if _, err := svc.HandleCallback(context.Background(), prov, state, "code", "", ""); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if _, err := svc.HandleCallback(context.Background(), prov, state, "code", "", ""); err == nil {
		t.Fatal("second call must fail — state should have been single-use via GETDEL")
	}
}

func TestOIDC_LoadConfigFromEnv(t *testing.T) {
	env := map[string]string{
		"OIDC_PROVIDER_KEY":          "google",
		"OIDC_ISSUER":                "https://accounts.google.com",
		"OIDC_CLIENT_ID":             "cid",
		"OIDC_CLIENT_SECRET":         "secret",
		"OIDC_REDIRECT_URL":          "https://app/callback",
		"OIDC_ALLOWED_EMAIL_DOMAINS": "Example.com, foo.example.org , ",
		"OIDC_FRONTEND_URL":          "https://app/",
	}
	cfg := LoadOIDCConfigFromEnv(func(k string) string { return env[k] })
	if !cfg.Enabled() {
		t.Fatal("expected enabled config")
	}
	want := []string{"example.com", "foo.example.org"}
	if fmt.Sprintf("%v", cfg.AllowedEmailDomains) != fmt.Sprintf("%v", want) {
		t.Errorf("domain CSV not normalised: got %v want %v", cfg.AllowedEmailDomains, want)
	}
}

func TestOIDC_DisplayLabel(t *testing.T) {
	cases := []struct {
		name     string
		cfg      *OIDCConfig
		expected string
	}{
		{"explicit display name", &OIDCConfig{ProviderKey: "google", DisplayName: "Google Workspace"}, "Google Workspace"},
		{"fallback title-case", &OIDCConfig{ProviderKey: "okta"}, "Okta"},
		{"empty provider", &OIDCConfig{}, ""},
		{"nil config", nil, ""},
		{"display name with spaces trimmed", &OIDCConfig{ProviderKey: "google", DisplayName: "  Acme SSO  "}, "Acme SSO"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.cfg.DisplayLabel(); got != c.expected {
				t.Errorf("DisplayLabel()=%q want %q", got, c.expected)
			}
		})
	}
}

func TestOIDC_LoadConfigFromEnv_DisplayName(t *testing.T) {
	env := map[string]string{
		"OIDC_PROVIDER_KEY":          "google",
		"OIDC_PROVIDER_DISPLAY_NAME": " Acme Corp SSO ",
		"OIDC_ISSUER":                "https://accounts.google.com",
		"OIDC_CLIENT_ID":             "cid",
		"OIDC_CLIENT_SECRET":         "secret",
		"OIDC_REDIRECT_URL":          "https://app/callback",
	}
	cfg := LoadOIDCConfigFromEnv(func(k string) string { return env[k] })
	if cfg.DisplayName != "Acme Corp SSO" {
		t.Errorf("DisplayName not trimmed: got %q", cfg.DisplayName)
	}
	if cfg.DisplayLabel() != "Acme Corp SSO" {
		t.Errorf("DisplayLabel: got %q", cfg.DisplayLabel())
	}
}

func TestOIDC_SanitiseReturnTo(t *testing.T) {
	cases := []struct {
		in, frontend, want string
	}{
		{"/inbox", "http://orbit.local/", "http://orbit.local/inbox"},
		{"//evil.example.com/x", "http://orbit.local/", "http://orbit.local/"},
		{"https://evil.example.com/x", "http://orbit.local/", "http://orbit.local/"},
		{"", "http://orbit.local/", "http://orbit.local/"},
		{"/foo?bar=1", "http://orbit.local", "http://orbit.local/foo?bar=1"},
	}
	for _, c := range cases {
		if got := sanitiseReturnTo(c.in, c.frontend); got != c.want {
			t.Errorf("sanitiseReturnTo(%q,%q)=%q want %q", c.in, c.frontend, got, c.want)
		}
	}
}

// Sanity: pkce challenge must be 43 chars (SHA256 → 32 bytes → base64url no
// padding = 43 chars). Catch any future regression where we accidentally
// switch to a different hash.
func TestOIDC_PKCEChallengeLength(t *testing.T) {
	v, _ := randomURLBytes(48)
	c := pkceS256(v)
	if len(c) != 43 {
		t.Errorf("PKCE challenge len=%d, want 43; v=%q c=%q", len(c), v, c)
	}
	// Ensure base64url decoding is clean (no padding chars).
	if _, err := base64.RawURLEncoding.DecodeString(c); err != nil {
		t.Errorf("PKCE challenge not clean base64url: %v", err)
	}
}

// Guard against accidental noop on the io import: keeps the test file's
// imports honest if the test set is rearranged later.
var _ = io.Discard
