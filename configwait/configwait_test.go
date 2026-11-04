// Copyright 2025 CruxStack
// SPDX-License-Identifier: MIT

package configwait

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestWait_ImmediateSuccess(t *testing.T) {
	ctx := context.Background()
	cfg := Config{
		MaxRetries:    3,
		RetryInterval: 10 * time.Millisecond,
	}

	callCount := 0
	err := Wait(ctx, cfg, func(ctx context.Context) error {
		callCount++
		return nil
	})

	if err != nil {
		t.Errorf("Wait() error = %v, want nil", err)
	}
	if callCount != 1 {
		t.Errorf("Load function called %d times, want 1", callCount)
	}
}

func TestWait_RetryThenSuccess(t *testing.T) {
	ctx := context.Background()
	cfg := Config{
		MaxRetries:    5,
		RetryInterval: 10 * time.Millisecond,
	}

	callCount := 0
	err := Wait(ctx, cfg, func(ctx context.Context) error {
		callCount++
		if callCount < 3 {
			return errors.New("not ready")
		}
		return nil
	})

	if err != nil {
		t.Errorf("Wait() error = %v, want nil", err)
	}
	if callCount != 3 {
		t.Errorf("Load function called %d times, want 3", callCount)
	}
}

func TestWait_MaxRetriesExceeded(t *testing.T) {
	ctx := context.Background()
	cfg := Config{
		MaxRetries:    3,
		RetryInterval: 10 * time.Millisecond,
	}

	callCount := 0
	expectedErr := errors.New("always fail")
	err := Wait(ctx, cfg, func(ctx context.Context) error {
		callCount++
		return expectedErr
	})

	if err != expectedErr {
		t.Errorf("Wait() error = %v, want %v", err, expectedErr)
	}
	if callCount != 3 {
		t.Errorf("Load function called %d times, want 3", callCount)
	}
}

func TestWait_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cfg := Config{
		MaxRetries:    100,
		RetryInterval: 100 * time.Millisecond,
	}

	callCount := 0
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err := Wait(ctx, cfg, func(ctx context.Context) error {
		callCount++
		return errors.New("not ready")
	})

	if err != context.Canceled {
		t.Errorf("Wait() error = %v, want %v", err, context.Canceled)
	}
	// Should have been cancelled after 1-2 attempts
	if callCount > 2 {
		t.Errorf("Load function called %d times, expected <= 2", callCount)
	}
}

func TestReadyGate_NotReadyReturns503(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	gate := NewReadyGate(inner, nil)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	gate.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestReadyGate_ReadyPassesThrough(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	gate := NewReadyGate(inner, nil)
	gate.SetReady()

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	gate.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Body.String() != "ok" {
		t.Errorf("Body = %q, want %q", rec.Body.String(), "ok")
	}
}

func TestReadyGate_AllowedPathsPassThrough(t *testing.T) {
	var called atomic.Bool
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called.Store(true)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("setup page"))
	})

	gate := NewReadyGate(inner, []string{"/setup", "/healthz"})
	// Not ready, but /setup should pass through

	tests := []struct {
		path       string
		wantStatus int
		wantBody   string
	}{
		{"/setup", http.StatusOK, "setup page"},
		{"/setup/", http.StatusOK, "setup page"},
		{"/setup/callback", http.StatusOK, "setup page"},
		{"/healthz", http.StatusOK, "setup page"},
		{"/other", http.StatusServiceUnavailable, ""},
		{"/webhook", http.StatusServiceUnavailable, ""},

		// Allowed paths match on segment boundaries, not raw string prefixes.
		{"/setupadmin", http.StatusServiceUnavailable, ""},
		{"/healthzz", http.StatusServiceUnavailable, ""},

		// Non-canonical paths never bypass the gate, in either direction.
		{"/setup/../webhook", http.StatusServiceUnavailable, ""},
		{"/setup/%2e%2e/webhook", http.StatusServiceUnavailable, ""},
		{"/setup/%2E%2E%2Fwebhook", http.StatusServiceUnavailable, ""},
		{"/webhook/../setup", http.StatusServiceUnavailable, ""},
		{"/setup/./callback", http.StatusServiceUnavailable, ""},
		{"//setup", http.StatusServiceUnavailable, ""},
		{"/setup//callback", http.StatusServiceUnavailable, ""},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			called.Store(false)
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()

			gate.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("Status = %d, want %d", rec.Code, tt.wantStatus)
			}

			if tt.wantStatus == http.StatusOK && !called.Load() {
				t.Error("Inner handler was not called for allowed path")
			}
		})
	}
}

func TestReadyGate_AllowedPathNormalization(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	tests := []struct {
		name    string
		allowed []string
		path    string
		want    int
	}{
		{"trailing slash entry allows bare path", []string{"/setup/"}, "/setup", http.StatusOK},
		{"trailing slash entry allows subtree", []string{"/setup/"}, "/setup/disable", http.StatusOK},
		{"trailing slash entry rejects sibling", []string{"/setup/"}, "/setupadmin", http.StatusServiceUnavailable},
		{"root entry allows only root", []string{"/"}, "/", http.StatusOK},
		{"root entry does not allow subpaths", []string{"/"}, "/webhook", http.StatusServiceUnavailable},
		{"empty entry is ignored", []string{""}, "/webhook", http.StatusServiceUnavailable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gate := NewReadyGate(inner, tt.allowed)
			rec := httptest.NewRecorder()
			gate.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.path, nil))
			if rec.Code != tt.want {
				t.Errorf("path %q with allowed %q: status = %d, want %d", tt.path, tt.allowed, rec.Code, tt.want)
			}
		})
	}
}

func TestReadyGate_IsReady(t *testing.T) {
	gate := NewReadyGate(nil, nil)

	if gate.IsReady() {
		t.Error("IsReady() = true, want false initially")
	}

	gate.SetReady()

	if !gate.IsReady() {
		t.Error("IsReady() = false, want true after SetReady()")
	}
}

func TestReadyGate_SetHandler(t *testing.T) {
	gate := NewReadyGate(nil, nil)

	// Initially no handler
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	gate.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("Status = %d, want %d when not ready", rec.Code, http.StatusServiceUnavailable)
	}

	// Set handler and mark ready
	gate.SetHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("dynamic handler"))
	}))
	gate.SetReady()

	req = httptest.NewRequest(http.MethodGet, "/test", nil)
	rec = httptest.NewRecorder()
	gate.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d after SetReady()", rec.Code, http.StatusOK)
	}
	if rec.Body.String() != "dynamic handler" {
		t.Errorf("Body = %q, want %q", rec.Body.String(), "dynamic handler")
	}
}

func TestNewConfigFromEnv_Defaults(t *testing.T) {
	cfg := NewConfigFromEnv()

	if cfg.MaxRetries != DefaultMaxRetries {
		t.Errorf("MaxRetries = %d, want %d", cfg.MaxRetries, DefaultMaxRetries)
	}
	if cfg.RetryInterval != DefaultRetryInterval {
		t.Errorf("RetryInterval = %v, want %v", cfg.RetryInterval, DefaultRetryInterval)
	}
}

func TestReloader_Trigger(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	gate := NewReadyGate(nil, nil)

	var reloadCount atomic.Int32
	reloadFunc := func(ctx context.Context) error {
		reloadCount.Add(1)
		return nil
	}

	reloader := NewReloader(ctx, gate, reloadFunc)
	reloader.Start()

	// Trigger a reload
	reloader.Trigger()

	// Wait for reload to complete
	time.Sleep(50 * time.Millisecond)

	if got := reloadCount.Load(); got != 1 {
		t.Errorf("Reload count = %d, want 1", got)
	}
}

func TestReloader_MultipleTriggers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	gate := NewReadyGate(nil, nil)

	var reloadCount atomic.Int32
	reloadFunc := func(ctx context.Context) error {
		reloadCount.Add(1)
		// Simulate some work
		time.Sleep(20 * time.Millisecond)
		return nil
	}

	reloader := NewReloader(ctx, gate, reloadFunc)
	reloader.Start()

	// Trigger multiple reloads rapidly
	reloader.Trigger()
	reloader.Trigger()
	reloader.Trigger()

	// Wait for reloads to complete
	time.Sleep(100 * time.Millisecond)

	// Only one or two reloads should have occurred due to deduplication
	got := reloadCount.Load()
	if got > 2 {
		t.Errorf("Reload count = %d, want <= 2 (due to deduplication)", got)
	}
	if got < 1 {
		t.Errorf("Reload count = %d, want >= 1", got)
	}
}

func TestReloader_ReloadError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	gate := NewReadyGate(nil, nil)

	var reloadCount atomic.Int32
	reloadFunc := func(ctx context.Context) error {
		reloadCount.Add(1)
		return errors.New("reload failed")
	}

	reloader := NewReloader(ctx, gate, reloadFunc)
	reloader.Start()

	// Trigger a reload
	reloader.Trigger()

	// Wait for reload to complete
	time.Sleep(50 * time.Millisecond)

	// Reload should have been attempted
	if got := reloadCount.Load(); got != 1 {
		t.Errorf("Reload count = %d, want 1", got)
	}
}

func TestReloader_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	gate := NewReadyGate(nil, nil)

	var reloadCount atomic.Int32
	reloadFunc := func(ctx context.Context) error {
		reloadCount.Add(1)
		return nil
	}

	reloader := NewReloader(ctx, gate, reloadFunc)
	done := reloader.Start()

	// Cancel context
	cancel()

	// Wait for reloader to stop
	select {
	case <-done:
		// Good, reloader stopped
	case <-time.After(100 * time.Millisecond):
		t.Error("Reloader did not stop after context cancellation")
	}
}

func TestWait_NonPositiveMaxRetriesStillLoads(t *testing.T) {
	for _, maxRetries := range []int{0, -1} {
		var calls atomic.Int32
		wantErr := errors.New("not configured")
		err := Wait(context.Background(), Config{MaxRetries: maxRetries}, func(ctx context.Context) error {
			calls.Add(1)
			return wantErr
		})
		if !errors.Is(err, wantErr) {
			t.Errorf("MaxRetries=%d: Wait() error = %v, want %v", maxRetries, err, wantErr)
		}
		if got := calls.Load(); got != 1 {
			t.Errorf("MaxRetries=%d: load called %d times, want 1", maxRetries, got)
		}
	}
}

func TestReloader_SuccessMarksGateReady(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	gate := NewReadyGate(nil, nil)
	reloader := NewReloader(ctx, gate, func(ctx context.Context) error { return nil })
	reloader.Start()
	reloader.Trigger()

	deadline := time.Now().Add(time.Second)
	for !gate.IsReady() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !gate.IsReady() {
		t.Error("gate.IsReady() = false after successful reload, want true")
	}
}
