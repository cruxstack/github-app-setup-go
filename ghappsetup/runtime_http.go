// Copyright 2025 CruxStack
// SPDX-License-Identifier: MIT

package ghappsetup

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/cruxstack/github-app-setup-go/configwait"
)

// Start blocks until configuration is successfully loaded, then marks the
// runtime as ready. It uses the configured retry policy to attempt loading.
// Returns an error if configuration cannot be loaded after all retries.
//
// This method is intended for HTTP server environments. For Lambda, use
// EnsureLoaded instead.
func (r *Runtime) Start(ctx context.Context) error {
	err := configwait.Wait(ctx, r.waitConfig(), configwait.LoadFunc(r.config.LoadFunc))
	if err != nil {
		return err
	}
	r.setReady(true)
	return nil
}

// StartAsync begins configuration loading in the background and returns
// immediately. The returned channel receives the result of the loading
// operation - nil on success or an error if loading failed after all retries.
// The channel is closed after sending the result.
//
// This method is intended for HTTP server environments where you want to
// start serving immediately (for health checks and installer endpoints)
// while configuration loads in the background.
func (r *Runtime) StartAsync(ctx context.Context) <-chan error {
	errCh := make(chan error, 1)
	go func() {
		defer close(errCh)
		errCh <- r.Start(ctx)
	}()
	return errCh
}

// Handler wraps the given http.Handler with a ReadyGate that returns 503
// Service Unavailable for requests to non-allowed paths before the runtime
// is ready. Paths specified in Config.AllowedPaths are always forwarded
// to the inner handler.
//
// The returned handler should be used as the server's main handler.
func (r *Runtime) Handler(inner http.Handler) http.Handler {
	if r.gate == nil {
		// No gate (e.g., Lambda environment) - return inner directly
		return inner
	}
	r.gate.SetHandler(inner)
	return r.gate
}

// ListenForReloads starts listening for SIGHUP signals and reload triggers
// from ReloadCallback. When a reload is triggered, LoadFunc is called.
// The returned channel is closed when the context is canceled.
//
// This should be called after Start() completes successfully.
func (r *Runtime) ListenForReloads(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})

	// Set up SIGHUP signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGHUP)

	go func() {
		defer close(done)
		defer signal.Stop(sigCh)

		for {
			select {
			case <-ctx.Done():
				return
			case <-sigCh:
				r.doReload(ctx)
			case <-r.reloadCh:
				r.doReload(ctx)
			}
		}
	}()

	return done
}

// doReload performs the actual reload operation.
func (r *Runtime) doReload(ctx context.Context) {
	if err := r.config.LoadFunc(ctx); err != nil {
		// Log error but don't crash - reload failures are non-fatal
		// The application continues running with the previous configuration
		return
	}
}

// HealthHandler returns an http.HandlerFunc that reports the runtime's
// readiness status. It returns 200 OK with body "ok" when ready, or
// 503 Service Unavailable with body "not ready" when not ready.
func (r *Runtime) HealthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if r.IsReady() {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("not ready"))
		}
	}
}
