// Copyright 2025 CruxStack
// SPDX-License-Identifier: MIT

package ghappsetup

import (
	"context"
	"sync"
	"time"

	"github.com/chainguard-dev/clog"
)

// lambdaState tracks Lambda-specific initialization state.
type lambdaState struct {
	mu        sync.Mutex
	loading   bool
	loaded    bool
	lastError error
}

var lambdaStates = struct {
	mu     sync.Mutex
	states map[*Runtime]*lambdaState
}{
	states: make(map[*Runtime]*lambdaState),
}

func (r *Runtime) getLambdaState() *lambdaState {
	lambdaStates.mu.Lock()
	defer lambdaStates.mu.Unlock()

	if state, ok := lambdaStates.states[r]; ok {
		return state
	}
	state := &lambdaState{}
	lambdaStates.states[r] = state
	return state
}

// EnsureLoaded ensures that configuration has been loaded, performing
// lazy initialization if needed. It is safe to call from multiple goroutines.
//
// The first call loads configuration using the configured retry policy.
// Once a load succeeds, subsequent calls return nil immediately. While a load
// is in progress, concurrent callers wait for it and receive its result. If a
// load fails, its error is returned and the next call starts a fresh load
// (again with the full retry policy), so a later invocation can succeed once
// configuration becomes available, e.g. after the installer saves credentials.
//
// This method is intended for Lambda environments where configuration
// loading happens lazily on first request. For HTTP servers, use Start instead.
//
// Example usage in a Lambda handler:
//
//	func handler(ctx context.Context, req events.APIGatewayV2HTTPRequest) (Response, error) {
//	    if err := runtime.EnsureLoaded(ctx); err != nil {
//	        return serviceUnavailableResponse(), nil
//	    }
//	    return handleRequest(ctx, req)
//	}
func (r *Runtime) EnsureLoaded(ctx context.Context) error {
	state := r.getLambdaState()

	// Fast path: already loaded
	state.mu.Lock()
	if state.loaded {
		state.mu.Unlock()
		return nil
	}

	// Check if another goroutine is loading
	if state.loading {
		state.mu.Unlock()
		// Wait and retry - another goroutine is loading
		return r.waitForLoad(ctx, state)
	}

	// We're the loader
	state.loading = true
	state.mu.Unlock()

	err := r.loadWithRetry(ctx)

	state.mu.Lock()
	state.loading = false
	if err == nil {
		state.loaded = true
		r.setReady(true)
	} else {
		state.lastError = err
	}
	state.mu.Unlock()

	return err
}

// waitForLoad waits for another goroutine to finish loading.
func (r *Runtime) waitForLoad(ctx context.Context, state *lambdaState) error {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			state.mu.Lock()
			if !state.loading {
				loaded := state.loaded
				lastErr := state.lastError
				state.mu.Unlock()

				if loaded {
					return nil
				}
				return lastErr
			}
			state.mu.Unlock()
		}
	}
}

// loadWithRetry attempts to load configuration with retry logic. LoadFunc is
// always attempted at least once.
func (r *Runtime) loadWithRetry(ctx context.Context) error {
	log := clog.FromContext(ctx)
	var lastErr error
	maxAttempts := max(r.config.MaxRetries, 1)

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := r.config.LoadFunc(ctx); err != nil {
			lastErr = err
			log.Warnf("[ghappsetup] attempt %d/%d failed: %v", attempt, maxAttempts, err)

			if attempt < maxAttempts {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(r.config.RetryInterval):
				}
			}
		} else {
			if attempt > 1 {
				log.Infof("[ghappsetup] configuration loaded successfully after %d attempts", attempt)
			}
			return nil
		}
	}

	return lastErr
}

// ResetLoadState resets the Lambda loading state, allowing EnsureLoaded to
// attempt loading again. This is primarily useful for testing.
func (r *Runtime) ResetLoadState() {
	state := r.getLambdaState()
	state.mu.Lock()
	defer state.mu.Unlock()
	state.loaded = false
	state.loading = false
	state.lastError = nil

	r.mu.Lock()
	r.ready = false
	r.mu.Unlock()
}
