// Copyright 2025 CruxStack
// SPDX-License-Identifier: MIT

package ghappsetup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/cruxstack/github-app-setup-go/configstore"
	"github.com/cruxstack/github-app-setup-go/configwait"
)

const (
	// Environment variable used to detect Lambda runtime.
	envLambdaFunctionName = "AWS_LAMBDA_FUNCTION_NAME"

	// Default retry settings for HTTP servers.
	defaultHTTPMaxRetries    = 30
	defaultHTTPRetryInterval = 2 * time.Second

	// Default retry settings for Lambda functions.
	defaultLambdaMaxRetries    = 5
	defaultLambdaRetryInterval = 1 * time.Second
)

// Environment represents the detected runtime environment.
type Environment int

const (
	// EnvironmentHTTP indicates a long-running HTTP server environment.
	EnvironmentHTTP Environment = iota
	// EnvironmentLambda indicates an AWS Lambda function environment.
	EnvironmentLambda
)

// LoadFunc is the function called to load application configuration.
// It should return an error if configuration is not yet available,
// which will trigger a retry according to the configured retry policy.
type LoadFunc func(ctx context.Context) error

// Config configures the Runtime behavior.
type Config struct {
	// Store is the credential storage backend. If nil, one will be created
	// automatically using configstore.NewFromEnv().
	Store configstore.Store

	// LoadFunc is called to load application configuration. This is required.
	// The function should read configuration from environment variables or
	// other sources and initialize application state. It will be called
	// during startup and on reload triggers.
	LoadFunc LoadFunc

	// AllowedPaths specifies HTTP paths that should be served even before
	// configuration is loaded. This is typically used for health checks and
	// installer endpoints. Only applicable in HTTP environments.
	AllowedPaths []string

	// MaxRetries is the maximum number of times to retry loading configuration.
	// If zero, defaults are used based on detected environment:
	// HTTP: 30 retries, Lambda: 5 retries.
	MaxRetries int

	// RetryInterval is the time to wait between retry attempts.
	// If zero, defaults are used based on detected environment:
	// HTTP: 2 seconds, Lambda: 1 second.
	RetryInterval time.Duration
}

// Runtime coordinates GitHub App configuration loading, readiness gating,
// and hot reloading. It provides a unified interface for both HTTP servers
// and Lambda functions.
type Runtime struct {
	config Config
	store  configstore.Store
	gate   *configwait.ReadyGate
	env    Environment

	mu       sync.RWMutex
	ready    bool
	reloadCh chan struct{}
}

// NewRuntime creates a new Runtime with the given configuration.
// It auto-detects the runtime environment (HTTP vs Lambda) and applies
// appropriate defaults for retry behavior.
func NewRuntime(cfg Config) (*Runtime, error) {
	if cfg.LoadFunc == nil {
		return nil, errors.New("ghappsetup: LoadFunc is required")
	}

	// Auto-detect environment
	env := detectEnvironment()

	// Apply defaults based on environment
	if cfg.MaxRetries == 0 {
		if env == EnvironmentLambda {
			cfg.MaxRetries = defaultLambdaMaxRetries
		} else {
			cfg.MaxRetries = defaultHTTPMaxRetries
		}
	}
	if cfg.RetryInterval == 0 {
		if env == EnvironmentLambda {
			cfg.RetryInterval = defaultLambdaRetryInterval
		} else {
			cfg.RetryInterval = defaultHTTPRetryInterval
		}
	}

	// Create store if not provided
	store := cfg.Store
	if store == nil {
		var err error
		store, err = configstore.NewFromEnv()
		if err != nil {
			return nil, fmt.Errorf("ghappsetup: failed to create store: %w", err)
		}
	}

	// Create ready gate for HTTP environments
	var gate *configwait.ReadyGate
	if env == EnvironmentHTTP {
		gate = configwait.NewReadyGate(nil, cfg.AllowedPaths)
	}

	return &Runtime{
		config:   cfg,
		store:    store,
		gate:     gate,
		env:      env,
		reloadCh: make(chan struct{}, 1),
	}, nil
}

// Store returns the credential storage backend used by this Runtime.
// This is useful when manually wiring up the installer.
func (r *Runtime) Store() configstore.Store {
	return r.store
}

// Environment returns the detected runtime environment.
func (r *Runtime) Environment() Environment {
	return r.env
}

// IsReady returns true if configuration has been successfully loaded.
func (r *Runtime) IsReady() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.ready
}

// setReady marks the runtime as ready and updates the HTTP gate if present.
func (r *Runtime) setReady(ready bool) {
	r.mu.Lock()
	r.ready = ready
	r.mu.Unlock()

	if ready && r.gate != nil {
		r.gate.SetReady()
	}
}

// Reload triggers a configuration reload by calling LoadFunc.
// This is safe to call from multiple goroutines; concurrent reload
// requests are coalesced.
func (r *Runtime) Reload(ctx context.Context) error {
	return r.config.LoadFunc(ctx)
}

// ReloadCallback returns a function suitable for use as installer.Config.OnReloadNeeded.
// The returned function triggers an asynchronous reload.
func (r *Runtime) ReloadCallback() func() {
	return func() {
		select {
		case r.reloadCh <- struct{}{}:
		default:
			// Reload already pending
		}
	}
}

// waitConfig returns a configwait.Config based on the Runtime configuration.
func (r *Runtime) waitConfig() configwait.Config {
	return configwait.Config{
		MaxRetries:    r.config.MaxRetries,
		RetryInterval: r.config.RetryInterval,
	}
}

// detectEnvironment checks for Lambda environment indicators.
func detectEnvironment() Environment {
	if os.Getenv(envLambdaFunctionName) != "" {
		return EnvironmentLambda
	}
	return EnvironmentHTTP
}
