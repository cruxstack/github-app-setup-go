// Copyright 2025 CruxStack
// SPDX-License-Identifier: MIT

package configwait

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/chainguard-dev/clog"
)

// ReloadFunc is called when a reload is triggered.
type ReloadFunc func(ctx context.Context) error

// Reloader manages configuration reloading via SIGHUP or programmatic triggers.
type Reloader struct {
	gate       *ReadyGate
	reloadFunc ReloadFunc
	ctx        context.Context

	mu        sync.Mutex
	reloading bool
	reloadCh  chan struct{}
}

// NewReloader creates a Reloader that calls reloadFunc when triggered.
func NewReloader(ctx context.Context, gate *ReadyGate, reloadFunc ReloadFunc) *Reloader {
	return &Reloader{
		gate:       gate,
		reloadFunc: reloadFunc,
		ctx:        ctx,
		reloadCh:   make(chan struct{}, 1),
	}
}

// Start begins listening for SIGHUP signals and programmatic triggers.
// Returns a channel that closes when the reloader stops.
func (r *Reloader) Start() <-chan struct{} {
	done := make(chan struct{})
	log := clog.FromContext(r.ctx)

	sighupCh := make(chan os.Signal, 1)
	signal.Notify(sighupCh, syscall.SIGHUP)

	go func() {
		defer close(done)
		defer signal.Stop(sighupCh)

		for {
			select {
			case <-r.ctx.Done():
				return
			case <-sighupCh:
				log.Infof("[reloader] received SIGHUP, triggering reload")
				r.doReload()
			case <-r.reloadCh:
				log.Infof("[reloader] programmatic reload triggered")
				r.doReload()
			}
		}
	}()

	return done
}

// Trigger requests a configuration reload. Safe to call from any goroutine.
func (r *Reloader) Trigger() {
	log := clog.FromContext(r.ctx)

	select {
	case r.reloadCh <- struct{}{}:
	default:
		log.Infof("[reloader] reload already pending, ignoring trigger")
	}
}

// doReload performs the reload operation.
func (r *Reloader) doReload() {
	log := clog.FromContext(r.ctx)

	r.mu.Lock()
	if r.reloading {
		r.mu.Unlock()
		log.Infof("[reloader] reload already in progress, skipping")
		return
	}
	r.reloading = true
	r.mu.Unlock()

	defer func() {
		r.mu.Lock()
		r.reloading = false
		r.mu.Unlock()
	}()

	log.Infof("[reloader] starting configuration reload...")

	if err := r.reloadFunc(r.ctx); err != nil {
		log.Errorf("[reloader] reload failed: %v", err)
		return
	}

	log.Infof("[reloader] configuration reloaded successfully")
}

var (
	globalReloaderMu sync.RWMutex
	globalReloader   *Reloader

	reloadCounter   int64
	reloadCounterMu sync.Mutex
)

// SetGlobalReloader sets the global reloader instance.
func SetGlobalReloader(r *Reloader) {
	globalReloaderMu.Lock()
	defer globalReloaderMu.Unlock()
	globalReloader = r
}

// TriggerReload triggers a reload using the global reloader (no-op if unset).
func TriggerReload() {
	reloadCounterMu.Lock()
	reloadCounter++
	reloadCounterMu.Unlock()

	globalReloaderMu.RLock()
	r := globalReloader
	globalReloaderMu.RUnlock()

	if r != nil {
		r.Trigger()
	}
}

// GetReloadCount returns the number of times TriggerReload has been called.
func GetReloadCount() int64 {
	reloadCounterMu.Lock()
	defer reloadCounterMu.Unlock()
	return reloadCounter
}

// ResetReloadCounter resets the reload counter to zero.
func ResetReloadCounter() {
	reloadCounterMu.Lock()
	defer reloadCounterMu.Unlock()
	reloadCounter = 0
}
