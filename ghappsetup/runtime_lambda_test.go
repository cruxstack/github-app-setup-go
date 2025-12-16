// Copyright 2025 CruxStack
// SPDX-License-Identifier: MIT

package ghappsetup

import (
	"context"
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cruxstack/github-app-setup-go/configstore"
)

func TestRuntime_EnsureLoaded_Success(t *testing.T) {
	os.Setenv("AWS_LAMBDA_FUNCTION_NAME", "test-function")
	defer os.Unsetenv("AWS_LAMBDA_FUNCTION_NAME")

	var called atomic.Bool
	runtime, err := NewRuntime(Config{
		Store: &lambdaMockStore{},
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
	err = runtime.EnsureLoaded(ctx)
	if err != nil {
		t.Errorf("EnsureLoaded() error = %v", err)
	}

	if !called.Load() {
		t.Error("EnsureLoaded() should call LoadFunc")
	}

	if !runtime.IsReady() {
		t.Error("IsReady() should be true after EnsureLoaded()")
	}
}

func TestRuntime_EnsureLoaded_Idempotent(t *testing.T) {
	os.Setenv("AWS_LAMBDA_FUNCTION_NAME", "test-function")
	defer os.Unsetenv("AWS_LAMBDA_FUNCTION_NAME")

	var callCount atomic.Int32
	runtime, err := NewRuntime(Config{
		Store: &lambdaMockStore{},
		LoadFunc: func(ctx context.Context) error {
			callCount.Add(1)
			return nil
		},
		MaxRetries:    3,
		RetryInterval: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}

	ctx := context.Background()

	// Call EnsureLoaded multiple times
	for i := 0; i < 5; i++ {
		err = runtime.EnsureLoaded(ctx)
		if err != nil {
			t.Errorf("EnsureLoaded() call %d error = %v", i, err)
		}
	}

	// LoadFunc should only be called once
	if callCount.Load() != 1 {
		t.Errorf("LoadFunc called %d times, want 1", callCount.Load())
	}
}

func TestRuntime_EnsureLoaded_RetryThenSuccess(t *testing.T) {
	os.Setenv("AWS_LAMBDA_FUNCTION_NAME", "test-function")
	defer os.Unsetenv("AWS_LAMBDA_FUNCTION_NAME")

	var callCount atomic.Int32
	runtime, err := NewRuntime(Config{
		Store: &lambdaMockStore{},
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
	err = runtime.EnsureLoaded(ctx)
	if err != nil {
		t.Errorf("EnsureLoaded() error = %v", err)
	}

	if callCount.Load() != 3 {
		t.Errorf("LoadFunc called %d times, want 3", callCount.Load())
	}

	if !runtime.IsReady() {
		t.Error("IsReady() should be true after successful retry")
	}
}

func TestRuntime_EnsureLoaded_MaxRetriesExceeded(t *testing.T) {
	os.Setenv("AWS_LAMBDA_FUNCTION_NAME", "test-function")
	defer os.Unsetenv("AWS_LAMBDA_FUNCTION_NAME")

	expectedErr := errors.New("always fail")
	runtime, err := NewRuntime(Config{
		Store: &lambdaMockStore{},
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
	err = runtime.EnsureLoaded(ctx)
	if err != expectedErr {
		t.Errorf("EnsureLoaded() error = %v, want %v", err, expectedErr)
	}

	if runtime.IsReady() {
		t.Error("IsReady() should be false after failed loading")
	}
}

func TestRuntime_EnsureLoaded_ContextCancellation(t *testing.T) {
	os.Setenv("AWS_LAMBDA_FUNCTION_NAME", "test-function")
	defer os.Unsetenv("AWS_LAMBDA_FUNCTION_NAME")

	runtime, err := NewRuntime(Config{
		Store: &lambdaMockStore{},
		LoadFunc: func(ctx context.Context) error {
			return errors.New("not ready")
		},
		MaxRetries:    100,
		RetryInterval: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err = runtime.EnsureLoaded(ctx)
	if err != context.Canceled {
		t.Errorf("EnsureLoaded() error = %v, want %v", err, context.Canceled)
	}
}

func TestRuntime_ResetLoadState(t *testing.T) {
	os.Setenv("AWS_LAMBDA_FUNCTION_NAME", "test-function")
	defer os.Unsetenv("AWS_LAMBDA_FUNCTION_NAME")

	var callCount atomic.Int32
	runtime, err := NewRuntime(Config{
		Store: &lambdaMockStore{},
		LoadFunc: func(ctx context.Context) error {
			callCount.Add(1)
			return nil
		},
		MaxRetries:    3,
		RetryInterval: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}

	ctx := context.Background()

	// First load
	err = runtime.EnsureLoaded(ctx)
	if err != nil {
		t.Errorf("First EnsureLoaded() error = %v", err)
	}

	if callCount.Load() != 1 {
		t.Errorf("LoadFunc called %d times, want 1", callCount.Load())
	}

	// Reset and load again
	runtime.ResetLoadState()

	if runtime.IsReady() {
		t.Error("IsReady() should be false after ResetLoadState()")
	}

	err = runtime.EnsureLoaded(ctx)
	if err != nil {
		t.Errorf("Second EnsureLoaded() error = %v", err)
	}

	if callCount.Load() != 2 {
		t.Errorf("LoadFunc called %d times, want 2 after reset", callCount.Load())
	}
}

func TestRuntime_EnsureLoaded_ConcurrentCalls(t *testing.T) {
	os.Setenv("AWS_LAMBDA_FUNCTION_NAME", "test-function")
	defer os.Unsetenv("AWS_LAMBDA_FUNCTION_NAME")

	var callCount atomic.Int32
	runtime, err := NewRuntime(Config{
		Store: &lambdaMockStore{},
		LoadFunc: func(ctx context.Context) error {
			callCount.Add(1)
			// Simulate slow loading
			time.Sleep(50 * time.Millisecond)
			return nil
		},
		MaxRetries:    3,
		RetryInterval: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}

	ctx := context.Background()

	// Start multiple concurrent EnsureLoaded calls
	done := make(chan error, 10)
	for i := 0; i < 10; i++ {
		go func() {
			done <- runtime.EnsureLoaded(ctx)
		}()
	}

	// Wait for all to complete
	for i := 0; i < 10; i++ {
		if err := <-done; err != nil {
			t.Errorf("Concurrent EnsureLoaded() error = %v", err)
		}
	}

	// LoadFunc should only be called once despite concurrent calls
	if callCount.Load() != 1 {
		t.Errorf("LoadFunc called %d times, want 1", callCount.Load())
	}
}

// lambdaMockStore for Lambda tests
type lambdaMockStore struct{}

func (m *lambdaMockStore) Save(ctx context.Context, creds *configstore.AppCredentials) error {
	return nil
}

func (m *lambdaMockStore) Status(ctx context.Context) (*configstore.InstallerStatus, error) {
	return nil, nil
}

func (m *lambdaMockStore) DisableInstaller(ctx context.Context) error {
	return nil
}
