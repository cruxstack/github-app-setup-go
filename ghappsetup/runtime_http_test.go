// Copyright 2025 CruxStack
// SPDX-License-Identifier: MIT

package ghappsetup

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cruxstack/github-app-setup-go/configstore"
)

func TestRuntime_Start_Success(t *testing.T) {
	os.Unsetenv("AWS_LAMBDA_FUNCTION_NAME")

	var called atomic.Bool
	runtime, err := NewRuntime(Config{
		Store: &mockStore{},
		LoadFunc: func(ctx context.Context) error {
			called.Store(true)
			return nil
		},
		MaxRetries:    3,
		RetryInterval: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}

	ctx := context.Background()
	err = runtime.Start(ctx)
	if err != nil {
		t.Errorf("Start() error = %v", err)
	}

	if !called.Load() {
		t.Error("Start() should call LoadFunc")
	}

	if !runtime.IsReady() {
		t.Error("IsReady() should be true after Start()")
	}
}

func TestRuntime_Start_RetryThenSuccess(t *testing.T) {
	os.Unsetenv("AWS_LAMBDA_FUNCTION_NAME")

	var callCount atomic.Int32
	runtime, err := NewRuntime(Config{
		Store: &mockStore{},
		LoadFunc: func(ctx context.Context) error {
			count := callCount.Add(1)
			if count < 3 {
				return errors.New("not ready")
			}
			return nil
		},
		MaxRetries:    5,
		RetryInterval: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}

	ctx := context.Background()
	err = runtime.Start(ctx)
	if err != nil {
		t.Errorf("Start() error = %v", err)
	}

	if callCount.Load() != 3 {
		t.Errorf("LoadFunc called %d times, want 3", callCount.Load())
	}
}

func TestRuntime_Start_MaxRetriesExceeded(t *testing.T) {
	os.Unsetenv("AWS_LAMBDA_FUNCTION_NAME")

	expectedErr := errors.New("always fail")
	runtime, err := NewRuntime(Config{
		Store: &mockStore{},
		LoadFunc: func(ctx context.Context) error {
			return expectedErr
		},
		MaxRetries:    3,
		RetryInterval: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}

	ctx := context.Background()
	err = runtime.Start(ctx)
	if err != expectedErr {
		t.Errorf("Start() error = %v, want %v", err, expectedErr)
	}

	if runtime.IsReady() {
		t.Error("IsReady() should be false after failed Start()")
	}
}

func TestRuntime_StartAsync(t *testing.T) {
	os.Unsetenv("AWS_LAMBDA_FUNCTION_NAME")

	runtime, err := NewRuntime(Config{
		Store: &mockStore{},
		LoadFunc: func(ctx context.Context) error {
			return nil
		},
		MaxRetries:    3,
		RetryInterval: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}

	ctx := context.Background()
	errCh := runtime.StartAsync(ctx)

	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("StartAsync() error = %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Error("StartAsync() did not complete in time")
	}

	if !runtime.IsReady() {
		t.Error("IsReady() should be true after StartAsync()")
	}
}

func TestRuntime_Handler_GatesRequests(t *testing.T) {
	os.Unsetenv("AWS_LAMBDA_FUNCTION_NAME")

	runtime, err := NewRuntime(Config{
		Store:        &mockStore{},
		LoadFunc:     func(ctx context.Context) error { return nil },
		AllowedPaths: []string{"/healthz"},
	})
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	handler := runtime.Handler(inner)

	// Before ready, non-allowed paths return 503
	req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("Status = %d, want %d before ready", rec.Code, http.StatusServiceUnavailable)
	}

	// Allowed paths pass through
	req = httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d for allowed path", rec.Code, http.StatusOK)
	}

	// After ready, all paths pass through
	runtime.setReady(true)

	req = httptest.NewRequest(http.MethodGet, "/api/data", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d after ready", rec.Code, http.StatusOK)
	}
}

func TestRuntime_HealthHandler(t *testing.T) {
	os.Unsetenv("AWS_LAMBDA_FUNCTION_NAME")

	runtime, err := NewRuntime(Config{
		Store:    &mockStore{},
		LoadFunc: func(ctx context.Context) error { return nil },
	})
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}

	handler := runtime.HealthHandler()

	// Not ready
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("Status = %d, want %d when not ready", rec.Code, http.StatusServiceUnavailable)
	}
	if rec.Body.String() != "not ready" {
		t.Errorf("Body = %q, want %q", rec.Body.String(), "not ready")
	}

	// After ready
	runtime.setReady(true)

	req = httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec = httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d when ready", rec.Code, http.StatusOK)
	}
	if rec.Body.String() != "ok" {
		t.Errorf("Body = %q, want %q", rec.Body.String(), "ok")
	}
}

func TestRuntime_ListenForReloads(t *testing.T) {
	os.Unsetenv("AWS_LAMBDA_FUNCTION_NAME")

	var reloadCount atomic.Int32
	runtime, err := NewRuntime(Config{
		Store: &mockStore{},
		LoadFunc: func(ctx context.Context) error {
			reloadCount.Add(1)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := runtime.ListenForReloads(ctx)

	// Trigger reload via callback
	callback := runtime.ReloadCallback()
	callback()

	// Wait for reload to process
	time.Sleep(50 * time.Millisecond)

	if reloadCount.Load() != 1 {
		t.Errorf("Reload count = %d, want 1", reloadCount.Load())
	}

	// Cancel context should stop listener
	cancel()

	select {
	case <-done:
		// Good, listener stopped
	case <-time.After(100 * time.Millisecond):
		t.Error("ListenForReloads did not stop after context cancellation")
	}
}

// mockStore for HTTP tests
type httpMockStore struct{}

func (m *httpMockStore) Save(ctx context.Context, creds *configstore.AppCredentials) error {
	return nil
}

func (m *httpMockStore) Status(ctx context.Context) (*configstore.InstallerStatus, error) {
	return nil, nil
}

func (m *httpMockStore) DisableInstaller(ctx context.Context) error {
	return nil
}
