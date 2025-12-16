// Copyright 2025 CruxStack
// SPDX-License-Identifier: MIT

// Example demonstrating a GitHub App with webhook handling using the
// ghappsetup.Runtime for unified lifecycle management.
package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/cruxstack/github-app-setup-go/configstore"
	"github.com/cruxstack/github-app-setup-go/ghappsetup"
	"github.com/cruxstack/github-app-setup-go/installer"
)

const (
	defaultPort              = 8080
	defaultReadHeaderTimeout = 10 * time.Second
	defaultShutdownTimeout   = 30 * time.Second
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	log := setupLogger()
	ctx = withLogger(ctx, log)

	port := defaultPort
	if p := os.Getenv("PORT"); p != "" {
		fmt.Sscanf(p, "%d", &port)
	}

	installerEnabled := configstore.InstallerEnabled()

	// Determine allowed paths for the ReadyGate
	allowedPaths := []string{"/healthz"}
	if installerEnabled {
		allowedPaths = append(allowedPaths, "/setup", "/callback", "/")
	}

	// Create the Runtime with unified lifecycle management
	runtime, err := ghappsetup.NewRuntime(ghappsetup.Config{
		LoadFunc:     func(ctx context.Context) error { return loadConfig(ctx, log) },
		AllowedPaths: allowedPaths,
	})
	if err != nil {
		log.Error("failed to create runtime", "error", err)
		os.Exit(1)
	}

	// Set up HTTP routes
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", runtime.HealthHandler())
	mux.HandleFunc("/webhook", webhookHandler(log))

	// Set up installer if enabled (using Option B: convenience method)
	if installerEnabled {
		manifest := installer.Manifest{
			URL:    "https://github.com/cruxstack/github-app-setup-go",
			Public: false,
			DefaultPerms: map[string]string{
				"contents":      "read",
				"pull_requests": "read",
			},
			DefaultEvents: []string{
				"push",
				"pull_request",
			},
		}

		installerHandler, err := runtime.InstallerHandler(installer.Config{
			Manifest:       manifest,
			AppDisplayName: "Simple Webhook App",
			GitHubURL:      configstore.GetEnvDefault("GITHUB_URL", "https://github.com"),
			GitHubOrg:      os.Getenv("GITHUB_ORG"),
		})
		if err != nil {
			log.Error("failed to create installer handler", "error", err)
			os.Exit(1)
		}

		mux.Handle("/setup", installerHandler)
		mux.Handle("/setup/", installerHandler)
		mux.Handle("/callback", installerHandler)
		mux.Handle("/", installerHandler)

		log.Info("installer enabled, visit /setup to create GitHub App")
	}

	// Create server with Runtime's handler (includes ReadyGate)
	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		ReadHeaderTimeout: defaultReadHeaderTimeout,
		Handler:           runtime.Handler(mux),
	}

	log.Info("starting HTTP server", "port", port, "installer_enabled", installerEnabled)

	// Start the HTTP server
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	// Start configuration loading (blocks until config is loaded)
	go func() {
		if err := runtime.Start(ctx); err != nil {
			log.Error("failed to load configuration after retries", "error", err)
			os.Exit(1)
		}
		log.Info("configuration loaded, service is ready")

		// Listen for reload triggers (SIGHUP or installer callback)
		done := runtime.ListenForReloads(ctx)
		log.Info("configuration reloader started (send SIGHUP to reload)")
		<-done
	}()

	<-ctx.Done()
	log.Info("shutting down server...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), defaultShutdownTimeout)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("server shutdown error", "error", err)
		os.Exit(1)
	}
}

// loadConfig loads configuration from environment variables.
func loadConfig(_ context.Context, log *slog.Logger) error {
	webhookSecret := os.Getenv(configstore.EnvGitHubWebhookSecret)
	if webhookSecret == "" {
		return fmt.Errorf("%s is not set", configstore.EnvGitHubWebhookSecret)
	}

	appID := os.Getenv(configstore.EnvGitHubAppID)
	if appID == "" {
		return fmt.Errorf("%s is not set", configstore.EnvGitHubAppID)
	}

	log.Info("loaded GitHub App configuration", "app_id", appID)
	return nil
}

// webhookHandler returns an HTTP handler that processes GitHub webhooks.
func webhookHandler(log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			log.Error("failed to read webhook body", "error", err)
			http.Error(w, "failed to read body", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		// Get webhook secret from environment (loaded by loadConfig)
		secret := os.Getenv(configstore.EnvGitHubWebhookSecret)
		signature := r.Header.Get("X-Hub-Signature-256")
		if !validateSignature(body, signature, secret) {
			log.Warn("webhook signature validation failed",
				"remote_addr", r.RemoteAddr,
				"has_signature", signature != "",
			)
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}

		eventType := r.Header.Get("X-GitHub-Event")
		deliveryID := r.Header.Get("X-GitHub-Delivery")

		var payload struct {
			Action     string `json:"action"`
			Repository struct {
				FullName string `json:"full_name"`
			} `json:"repository"`
			Sender struct {
				Login string `json:"login"`
			} `json:"sender"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			log.Warn("failed to parse webhook payload", "error", err)
		}

		log.Info("received webhook",
			"event", eventType,
			"action", payload.Action,
			"delivery_id", deliveryID,
			"repository", payload.Repository.FullName,
			"sender", payload.Sender.Login,
			"payload_size", len(body),
		)

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}
}

// validateSignature validates the GitHub webhook signature.
func validateSignature(payload []byte, signature, secret string) bool {
	if signature == "" || secret == "" {
		return false
	}

	if !strings.HasPrefix(signature, "sha256=") {
		return false
	}
	sig := strings.TrimPrefix(signature, "sha256=")

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	expected := hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(sig), []byte(expected))
}

// setupLogger creates a slog.Logger based on LOG_FORMAT environment variable.
func setupLogger() *slog.Logger {
	format := strings.ToLower(os.Getenv("LOG_FORMAT"))

	var handler slog.Handler
	opts := &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}

	switch format {
	case "json":
		handler = slog.NewJSONHandler(os.Stderr, opts)
	default:
		handler = slog.NewTextHandler(os.Stderr, opts)
	}

	return slog.New(handler)
}

type loggerKey struct{}

// withLogger adds a logger to the context.
func withLogger(ctx context.Context, log *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey{}, log)
}
