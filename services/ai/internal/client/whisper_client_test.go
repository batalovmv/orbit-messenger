// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package client

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestWhisperClient_SetBaseURL ensures the override path is honoured so we
// can route through Groq (or any OpenAI-compatible Whisper provider) by
// setting OPENAI_BASE_URL on Saturn.
func TestWhisperClient_SetBaseURL(t *testing.T) {
	var hitPath string
	var hitAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitPath = r.URL.Path
		hitAuth = r.Header.Get("Authorization")
		// Drain body so multipart isn't logged as broken.
		_, _ = io.Copy(io.Discard, r.Body)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"text":     "hello world",
			"language": "en",
		})
	}))
	defer srv.Close()

	c := NewWhisperClient("test-key", "whisper-large-v3-turbo", slog.Default())
	c.SetBaseURL(srv.URL + "/openai/v1")

	result, err := c.TranscribeAudio(context.Background(), []byte("dummy-audio"), "voice.ogg", "")
	if err != nil {
		t.Fatalf("TranscribeAudio: %v", err)
	}
	if result.Text != "hello world" {
		t.Errorf("Text = %q, want %q", result.Text, "hello world")
	}
	if !strings.HasSuffix(hitPath, "/audio/transcriptions") {
		t.Errorf("hit path = %q, want suffix /audio/transcriptions", hitPath)
	}
	if !strings.HasPrefix(hitPath, "/openai/v1/") {
		t.Errorf("hit path = %q, want prefix /openai/v1/ (proves SetBaseURL was used)", hitPath)
	}
	if hitAuth != "Bearer test-key" {
		t.Errorf("Authorization = %q, want %q", hitAuth, "Bearer test-key")
	}
}

// TestWhisperClient_DefaultBaseURL guards against accidentally pointing
// every deploy at api.openai.com when no override is set.
func TestWhisperClient_DefaultBaseURL(t *testing.T) {
	c := NewWhisperClient("test-key", "whisper-1", slog.Default())
	if got := c.BaseURL(); got != "https://api.openai.com/v1" {
		t.Errorf("BaseURL() = %q, want %q", got, "https://api.openai.com/v1")
	}
}

// TestWhisperClient_TrailingSlashTolerance ensures a Saturn operator setting
// OPENAI_BASE_URL with or without a trailing slash gets the same behaviour.
func TestWhisperClient_TrailingSlashTolerance(t *testing.T) {
	var hitPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitPath = r.URL.Path
		_, _ = io.Copy(io.Discard, r.Body)
		_ = json.NewEncoder(w).Encode(map[string]any{"text": "ok"})
	}))
	defer srv.Close()

	c := NewWhisperClient("k", "whisper-large-v3-turbo", slog.Default())
	c.SetBaseURL(srv.URL + "/openai/v1/")

	if _, err := c.TranscribeAudio(context.Background(), []byte("x"), "voice.ogg", ""); err != nil {
		t.Fatalf("TranscribeAudio: %v", err)
	}
	if strings.Contains(hitPath, "//audio") {
		t.Errorf("hit path %q contains double slash — base URL trim broken", hitPath)
	}
	if !strings.HasSuffix(hitPath, "/audio/transcriptions") {
		t.Errorf("hit path = %q, want suffix /audio/transcriptions", hitPath)
	}
}
