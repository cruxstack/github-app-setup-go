// Copyright 2025 CruxStack
// SPDX-License-Identifier: MIT

// Package configwait provides utilities for waiting on configuration availability
// during startup.
package configwait

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chainguard-dev/clog"
)

const (
	EnvMaxRetries    = "CONFIG_WAIT_MAX_RETRIES"
	EnvRetryInterval = "CONFIG_WAIT_RETRY_INTERVAL"
)

const (
	DefaultMaxRetries    = 30
	DefaultRetryInterval = 2 * time.Second
)

// Config configures the wait behavior.
type Config struct {
	MaxRetries    int
	RetryInterval time.Duration
}

// NewConfigFromEnv creates a Config from environment variables.
func NewConfigFromEnv() Config {
	cfg := Config{
		MaxRetries:    DefaultMaxRetries,
		RetryInterval: DefaultRetryInterval,
	}

	if v := os.Getenv(EnvMaxRetries); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.MaxRetries = n
		}
	}

	if v := os.Getenv(EnvRetryInterval); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			cfg.RetryInterval = d
		}
	}

	return cfg
}

// LoadFunc attempts to load configuration; returns nil on success.
type LoadFunc func(ctx context.Context) error

// Wait blocks until load succeeds or max retries is reached. Load is always
// attempted at least once, even if MaxRetries is zero or negative.
func Wait(ctx context.Context, cfg Config, load LoadFunc) error {
	log := clog.FromContext(ctx)
	var lastErr error
	maxAttempts := max(cfg.MaxRetries, 1)

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := load(ctx); err != nil {
			lastErr = err
			log.Warnf("[configwait] attempt %d/%d failed: %v", attempt, maxAttempts, err)

			if attempt < maxAttempts {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(cfg.RetryInterval):

				}
			}
		} else {
			if attempt > 1 {
				log.Infof("[configwait] configuration loaded successfully after %d attempts", attempt)
			}
			return nil
		}
	}

	return lastErr
}

// ReadyGate gates HTTP requests until the service is ready.
type ReadyGate struct {
	inner        http.Handler
	allowedPaths []string
	ready        atomic.Bool
	handler      atomic.Value // stores http.Handler once ready

	mu           sync.Mutex
	handlerReady chan struct{}
}

// NewReadyGate creates a ReadyGate wrapping the given handler.
//
// The allowedPaths are always allowed through, even before the gate is ready.
// Each entry matches the path itself and anything beneath it on a path-segment
// boundary: "/setup" allows "/setup", "/setup/", and "/setup/disable", but not
// "/setupadmin". The entry "/" matches only the root path. Requests whose path
// is not canonical (dot segments, repeated slashes) never bypass the gate, so
// a path such as "/setup/../webhook" cannot reach a gated handler early.
//
// The inner handler can be nil initially; call SetHandler() once ready.
func NewReadyGate(inner http.Handler, allowedPaths []string) *ReadyGate {
	normalized := make([]string, 0, len(allowedPaths))
	for _, p := range allowedPaths {
		if p == "" {
			continue
		}
		if p != "/" {
			p = strings.TrimRight(p, "/")
			if p == "" {
				p = "/"
			}
		}
		normalized = append(normalized, p)
	}

	rg := &ReadyGate{
		inner:        inner,
		allowedPaths: normalized,
		handlerReady: make(chan struct{}),
	}
	if inner != nil {
		rg.handler.Store(inner)
	}
	return rg
}

// SetReady marks the service as ready to handle all requests.
func (rg *ReadyGate) SetReady() {
	rg.ready.Store(true)
}

// SetHandler sets the main handler to use once ready.
func (rg *ReadyGate) SetHandler(h http.Handler) {
	rg.handler.Store(h)
	rg.mu.Lock()
	defer rg.mu.Unlock()
	select {
	case <-rg.handlerReady:
	default:
		close(rg.handlerReady)
	}
}

// IsReady returns true if the service is ready.
func (rg *ReadyGate) IsReady() bool {
	return rg.ready.Load()
}

// ServeHTTP implements http.Handler.
func (rg *ReadyGate) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if rg.isAllowedPath(r.URL.Path) {
		h := rg.getHandler()
		if h != nil {
			h.ServeHTTP(w, r)
			return
		}
		rg.serveUnavailable(w, r, "service starting up")
		return
	}

	if !rg.ready.Load() {
		rg.serveUnavailable(w, r, "service not ready, configuration loading")
		return
	}

	h := rg.getHandler()
	if h == nil {
		rg.serveUnavailable(w, r, "service starting up")
		return
	}
	h.ServeHTTP(w, r)
}

// isAllowedPath reports whether a request path may bypass the gate. Only
// canonical paths qualify: otherwise the inner router could resolve the path
// differently than this check does (e.g., "/setup/../webhook").
func (rg *ReadyGate) isAllowedPath(p string) bool {
	if p == "" || p != cleanPath(p) {
		return false
	}
	for _, allowed := range rg.allowedPaths {
		if allowed == "/" {
			if p == "/" {
				return true
			}
			continue
		}
		if p == allowed || strings.HasPrefix(p, allowed+"/") {
			return true
		}
	}
	return false
}

// cleanPath returns the canonical form of p, mirroring net/http.ServeMux:
// it resolves dot segments and repeated slashes and keeps a trailing slash.
func cleanPath(p string) string {
	if p == "" {
		return "/"
	}
	if p[0] != '/' {
		p = "/" + p
	}
	np := path.Clean(p)
	if p[len(p)-1] == '/' && np != "/" {
		np += "/"
	}
	return np
}

// getHandler returns the current handler.
func (rg *ReadyGate) getHandler() http.Handler {
	h := rg.handler.Load()
	if h == nil {
		return nil
	}
	return h.(http.Handler)
}

// serveUnavailable writes a 503 response.
func (rg *ReadyGate) serveUnavailable(w http.ResponseWriter, r *http.Request, message string) {
	log := clog.FromContext(r.Context())

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Retry-After", "5")
	w.WriteHeader(http.StatusServiceUnavailable)
	if err := json.NewEncoder(w).Encode(map[string]string{
		"error":   "service_unavailable",
		"message": message,
	}); err != nil {
		log.Errorf("[configwait] failed to write unavailable response: %v", err)
	}
}
