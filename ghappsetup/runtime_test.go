// Copyright 2025 CruxStack
// SPDX-License-Identifier: MIT

package ghappsetup

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/cruxstack/github-app-setup-go/configstore"
)

func TestNewRuntime_RequiresLoadFunc(t *testing.T) {
	_, err := NewRuntime(Config{})
	if err == nil {
		t.Error("NewRuntime() should return error when LoadFunc is nil")
	}
}

func TestNewRuntime_CreatesStoreWhenNil(t *testing.T) {
	// Set up environment for LocalEnvFileStore
	tempDir := t.TempDir()
	envFile := tempDir + "/.env"
	os.Setenv("STORAGE_MODE", "envfile")
	os.Setenv("STORAGE_DIR", envFile)
	defer os.Unsetenv("STORAGE_MODE")
	defer os.Unsetenv("STORAGE_DIR")

	runtime, err := NewRuntime(Config{
		LoadFunc: func(ctx context.Context) error { return nil },
	})
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}

	if runtime.Store() == nil {
		t.Error("Runtime.Store() should not be nil when auto-created")
	}
}

func TestNewRuntime_UsesProvidedStore(t *testing.T) {
	store := &mockStore{}

	runtime, err := NewRuntime(Config{
		Store:    store,
		LoadFunc: func(ctx context.Context) error { return nil },
	})
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}

	if runtime.Store() != store {
		t.Error("Runtime.Store() should return the provided store")
	}
}

func TestNewRuntime_DetectsHTTPEnvironment(t *testing.T) {
	// Ensure Lambda env var is not set
	os.Unsetenv("AWS_LAMBDA_FUNCTION_NAME")

	runtime, err := NewRuntime(Config{
		Store:    &mockStore{},
		LoadFunc: func(ctx context.Context) error { return nil },
	})
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}

	if runtime.Environment() != EnvironmentHTTP {
		t.Errorf("Environment() = %v, want EnvironmentHTTP", runtime.Environment())
	}
}

func TestNewRuntime_DetectsLambdaEnvironment(t *testing.T) {
	os.Setenv("AWS_LAMBDA_FUNCTION_NAME", "test-function")
	defer os.Unsetenv("AWS_LAMBDA_FUNCTION_NAME")

	runtime, err := NewRuntime(Config{
		Store:    &mockStore{},
		LoadFunc: func(ctx context.Context) error { return nil },
	})
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}

	if runtime.Environment() != EnvironmentLambda {
		t.Errorf("Environment() = %v, want EnvironmentLambda", runtime.Environment())
	}
}

func TestNewRuntime_HTTPDefaults(t *testing.T) {
	os.Unsetenv("AWS_LAMBDA_FUNCTION_NAME")

	runtime, err := NewRuntime(Config{
		Store:    &mockStore{},
		LoadFunc: func(ctx context.Context) error { return nil },
	})
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}

	if runtime.config.MaxRetries != defaultHTTPMaxRetries {
		t.Errorf("MaxRetries = %d, want %d", runtime.config.MaxRetries, defaultHTTPMaxRetries)
	}
	if runtime.config.RetryInterval != defaultHTTPRetryInterval {
		t.Errorf("RetryInterval = %v, want %v", runtime.config.RetryInterval, defaultHTTPRetryInterval)
	}
}

func TestNewRuntime_LambdaDefaults(t *testing.T) {
	os.Setenv("AWS_LAMBDA_FUNCTION_NAME", "test-function")
	defer os.Unsetenv("AWS_LAMBDA_FUNCTION_NAME")

	runtime, err := NewRuntime(Config{
		Store:    &mockStore{},
		LoadFunc: func(ctx context.Context) error { return nil },
	})
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}

	if runtime.config.MaxRetries != defaultLambdaMaxRetries {
		t.Errorf("MaxRetries = %d, want %d", runtime.config.MaxRetries, defaultLambdaMaxRetries)
	}
	if runtime.config.RetryInterval != defaultLambdaRetryInterval {
		t.Errorf("RetryInterval = %v, want %v", runtime.config.RetryInterval, defaultLambdaRetryInterval)
	}
}

func TestRuntime_IsReady(t *testing.T) {
	runtime, err := NewRuntime(Config{
		Store:    &mockStore{},
		LoadFunc: func(ctx context.Context) error { return nil },
	})
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}

	if runtime.IsReady() {
		t.Error("IsReady() should be false initially")
	}

	runtime.setReady(true)

	if !runtime.IsReady() {
		t.Error("IsReady() should be true after setReady(true)")
	}
}

func TestRuntime_ReloadCallback(t *testing.T) {
	runtime, err := NewRuntime(Config{
		Store:    &mockStore{},
		LoadFunc: func(ctx context.Context) error { return nil },
	})
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}

	callback := runtime.ReloadCallback()

	// Callback should send to reloadCh
	callback()

	select {
	case <-runtime.reloadCh:
		// Good, received signal
	case <-time.After(100 * time.Millisecond):
		t.Error("ReloadCallback() did not send to reloadCh")
	}

	// Calling multiple times should not block (buffered channel)
	callback()
	callback()
}

func TestRuntime_Reload(t *testing.T) {
	var called bool
	runtime, err := NewRuntime(Config{
		Store: &mockStore{},
		LoadFunc: func(ctx context.Context) error {
			called = true
			return nil
		},
	})
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}

	err = runtime.Reload(context.Background())
	if err != nil {
		t.Errorf("Reload() error = %v", err)
	}
	if !called {
		t.Error("Reload() should call LoadFunc")
	}
}

func TestRuntime_Reload_Error(t *testing.T) {
	expectedErr := errors.New("load failed")
	runtime, err := NewRuntime(Config{
		Store: &mockStore{},
		LoadFunc: func(ctx context.Context) error {
			return expectedErr
		},
	})
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}

	err = runtime.Reload(context.Background())
	if err != expectedErr {
		t.Errorf("Reload() error = %v, want %v", err, expectedErr)
	}
}

// mockStore is a minimal Store implementation for testing.
type mockStore struct{}

func (m *mockStore) Save(ctx context.Context, creds *configstore.AppCredentials) error {
	return nil
}

func (m *mockStore) Status(ctx context.Context) (*configstore.InstallerStatus, error) {
	return nil, nil
}

func (m *mockStore) DisableInstaller(ctx context.Context) error {
	return nil
}
