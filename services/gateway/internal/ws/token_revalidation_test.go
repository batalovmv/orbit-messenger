// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ws

import (
	"context"
	"errors"
	"testing"
	"time"
)

type stubRevalidationTarget struct {
	expiry       time.Time
	done         chan struct{}
	revalidateFn func(context.Context) error
	closeFn      func(code int, text string) error
}

func (s *stubRevalidationTarget) TokenExpiry() time.Time {
	return s.expiry
}

func (s *stubRevalidationTarget) Revalidate(ctx context.Context) error {
	if s.revalidateFn != nil {
		return s.revalidateFn(ctx)
	}
	return nil
}

func (s *stubRevalidationTarget) Close(code int, text string) error {
	if s.closeFn != nil {
		return s.closeFn(code, text)
	}
	return nil
}

func (s *stubRevalidationTarget) Done() <-chan struct{} {
	return s.done
}

func TestRunTokenRevalidation_ClosesExpiredToken(t *testing.T) {
	ticks := make(chan time.Time, 1)
	closed := make(chan struct{}, 1)
	target := &stubRevalidationTarget{
		expiry: time.Now().Add(-time.Second),
		done:   make(chan struct{}),
		closeFn: func(code int, text string) error {
			if code != closeCodePolicyViolation {
				t.Fatalf("unexpected close code: %d", code)
			}
			if text != "token expired" {
				t.Fatalf("unexpected close text: %q", text)
			}
			closed <- struct{}{}
			return nil
		},
		revalidateFn: func(context.Context) error {
			t.Fatal("revalidate must not run for expired tokens")
			return nil
		},
	}

	done := make(chan struct{})
	go func() {
		runTokenRevalidation(target, ticks, nil, time.Now)
		close(done)
	}()

	ticks <- time.Now()

	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for close")
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for loop exit")
	}
}

func TestRunTokenRevalidation_ClosesRevokedToken(t *testing.T) {
	ticks := make(chan time.Time, 1)
	closed := make(chan struct{}, 1)
	target := &stubRevalidationTarget{
		expiry: time.Now().Add(time.Minute),
		done:   make(chan struct{}),
		revalidateFn: func(context.Context) error {
			return errors.New("revoked")
		},
		closeFn: func(code int, text string) error {
			if code != closeCodePolicyViolation {
				t.Fatalf("unexpected close code: %d", code)
			}
			if text != "token revoked" {
				t.Fatalf("unexpected close text: %q", text)
			}
			closed <- struct{}{}
			return nil
		},
	}

	done := make(chan struct{})
	go func() {
		runTokenRevalidation(target, ticks, nil, time.Now)
		close(done)
	}()

	ticks <- time.Now()

	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for close")
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for loop exit")
	}
}
