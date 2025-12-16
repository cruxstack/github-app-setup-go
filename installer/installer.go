// Copyright 2025 CruxStack
// SPDX-License-Identifier: MIT

// Package installer provides a web-based installer for creating GitHub Apps
// using the GitHub App Manifest flow.
package installer

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/chainguard-dev/clog"

	"github.com/cruxstack/github-app-setup-go/configstore"
)

//go:embed templates/*
var templateFS embed.FS

var indexTemplate = template.Must(template.ParseFS(templateFS, "templates/index.html"))
var successTemplate = template.Must(template.ParseFS(templateFS, "templates/success.html"))

const (
	httpClientTimeout = 30 * time.Second
	EnvGitHubURL      = "GITHUB_URL"
	EnvGitHubOrg      = "GITHUB_ORG"
	disableSetupPath  = "/setup/disable"
)

// CredentialsSavedFunc is called after credentials are saved.
type CredentialsSavedFunc func(ctx context.Context, creds *configstore.AppCredentials) error

// Config holds the installer configuration.
type Config struct {
	Store              configstore.Store
	Manifest           Manifest
	AppDisplayName     string
	GitHubURL          string
	GitHubOrg          string
	RedirectURL        string
	WebhookURL         string
	OnCredentialsSaved CredentialsSavedFunc

	// OnReloadNeeded is called after credentials are saved to trigger
	// a configuration reload. This should be wired to the Runtime's
	// ReloadCallback() or a custom reload function.
	OnReloadNeeded func()
}

// NewConfigFromEnv creates a Config from environment variables.
func NewConfigFromEnv() Config {
	return Config{
		GitHubURL: configstore.GetEnvDefault(EnvGitHubURL, "https://github.com"),
		GitHubOrg: os.Getenv(EnvGitHubOrg),
	}
}

// Handler handles the GitHub App manifest installation flow.
type Handler struct {
	config Config
}

type indexTemplateData struct {
	AppDisplayName string
	GitHubURL      string
	GitHubOrg      string
	FormActionURL  string
	ManifestJSON   template.JS
	WebhookURL     string
	NeedsWebhook   bool
	DefaultAppName string
}

type successTemplateData struct {
	AppDisplayName    string
	AppID             int64
	AppSlug           string
	HTMLURL           string
	InstallURL        string
	DisableActionURL  string
	InstallerDisabled bool
}

// New creates a new installer Handler with the given configuration.
func New(cfg Config) (*Handler, error) {
	if cfg.Store == nil {
		return nil, fmt.Errorf("store is required")
	}
	if cfg.GitHubURL == "" {
		cfg.GitHubURL = "https://github.com"
	}
	if cfg.AppDisplayName == "" {
		cfg.AppDisplayName = "GitHub App"
	}
	return &Handler{config: cfg}, nil
}

// ServeHTTP implements http.Handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	switch {
	case (r.Method == http.MethodGet || r.Method == http.MethodHead) && (path == "/" || path == ""):
		h.handleRoot(w, r)
	case (r.Method == http.MethodGet || r.Method == http.MethodHead) && (path == "/setup" || path == "/setup/"):
		h.handleIndex(w, r)
	case (r.Method == http.MethodGet || r.Method == http.MethodHead) && path == "/callback":
		h.handleCallback(w, r)

	case r.Method == http.MethodPost && (path == disableSetupPath || path == disableSetupPath+"/"):
		h.handleDisable(w, r)
	default:
		http.NotFound(w, r)
	}
}

// handleRoot redirects to /setup if enabled, otherwise returns 404.
func (h *Handler) handleRoot(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := clog.FromContext(ctx)

	status, err := h.config.Store.Status(ctx)
	if err != nil {
		log.Errorf("[installer] failed to read installer status: %v", err)
		http.Error(w, "Failed to load installer status", http.StatusInternalServerError)
		return
	}

	if status != nil && status.InstallerDisabled {
		http.NotFound(w, r)
		return
	}

	http.Redirect(w, r, "/setup", http.StatusFound)
}

// handleIndex serves the main page.
func (h *Handler) handleIndex(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := clog.FromContext(ctx)

	status, err := h.config.Store.Status(ctx)

	if err != nil {
		log.Errorf("[installer] failed to read installer status: %v", err)
		http.Error(w, "Failed to load installer status", http.StatusInternalServerError)
		return
	}
	if status != nil && status.Registered {
		data := h.successDataFromStatus(status)
		h.renderSuccess(w, r, data)
		return
	}

	redirectURL := h.config.RedirectURL
	if redirectURL == "" {
		redirectURL = getBaseURL(ctx, r)
		log.Infof("[installer] auto-detected redirect url: url=%s host=%s forwarded_host=%s",
			redirectURL, r.Host, r.Header.Get("X-Forwarded-Host"))
	}

	webhookURL := h.config.WebhookURL
	if webhookURL == "" {
		webhookURL = r.FormValue("webhook_url")
		if webhookURL == "" {
			webhookURL = getBaseURL(ctx, r) + "/webhook"
			log.Infof("[installer] auto-detected webhook url: url=%s", webhookURL)
		}
	}

	manifest := h.config.Manifest.Clone()
	if manifest == nil {
		manifest = &Manifest{}
	}
	manifest.RedirectURL = redirectURL + "/callback"
	manifest.HookAttributes.URL = webhookURL
	manifest.HookAttributes.Active = webhookURL != ""

	log.Infof("[installer] manifest redirect_url: %s", manifest.RedirectURL)
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		http.Error(w, "Failed to generate manifest", http.StatusInternalServerError)
		return
	}

	var formActionURL string
	if h.config.GitHubOrg != "" {
		formActionURL = fmt.Sprintf("%s/organizations/%s/settings/apps/new",
			h.config.GitHubURL, h.config.GitHubOrg)
	} else {
		formActionURL = fmt.Sprintf("%s/settings/apps/new", h.config.GitHubURL)
	}

	defaultAppName := manifest.Name
	if defaultAppName == "" {
		defaultAppName = strings.ToLower(strings.ReplaceAll(h.config.AppDisplayName, " ", "-"))
	}

	data := indexTemplateData{
		AppDisplayName: h.config.AppDisplayName,
		GitHubURL:      h.config.GitHubURL,
		GitHubOrg:      h.config.GitHubOrg,
		FormActionURL:  formActionURL,
		ManifestJSON:   template.JS(manifestJSON),
		WebhookURL:     webhookURL,
		NeedsWebhook:   h.config.WebhookURL == "",
		DefaultAppName: defaultAppName,
	}

	var buf bytes.Buffer
	if err := indexTemplate.Execute(&buf, data); err != nil {
		log.Errorf("[installer] failed to render index template: %v", err)
		http.Error(w, "Failed to render page", http.StatusInternalServerError)
		return
	}
	setSecurityHeaders(w)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if _, err := buf.WriteTo(w); err != nil {
		log.Errorf("[installer] failed to write response: %v", err)
	}
}

// handleCallback handles the GitHub redirect after app creation.
func (h *Handler) handleCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := clog.FromContext(ctx)

	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "Missing code parameter", http.StatusBadRequest)
		return
	}
	if !isValidOAuthCode(code) {
		http.Error(w, "Invalid code parameter", http.StatusBadRequest)
		return
	}

	var customDomain string
	if cookie, err := r.Cookie("custom_domain"); err == nil {
		customDomain = cookie.Value
		http.SetCookie(w, &http.Cookie{
			Name:   "custom_domain",
			Value:  "",
			Path:   "/",
			MaxAge: -1,
		})
	}

	creds, err := h.exchangeCode(ctx, code)
	if err != nil {
		log.Errorf("[installer] failed to exchange code: %v", err)
		http.Error(w, "Failed to exchange code", http.StatusInternalServerError)
		return
	}

	if creds.CustomFields == nil {
		creds.CustomFields = make(map[string]string)
	}

	if customDomain != "" {
		creds.CustomFields["CUSTOM_DOMAIN"] = customDomain
	}

	if h.config.OnCredentialsSaved != nil {
		if err := h.config.OnCredentialsSaved(ctx, creds); err != nil {
			log.Errorf("[installer] OnCredentialsSaved callback failed: %v", err)
		}
	}

	if err := h.config.Store.Save(ctx, creds); err != nil {
		log.Errorf("[installer] failed to save credentials: %v", err)
		http.Error(w, "Failed to save credentials", http.StatusInternalServerError)
		return
	}

	log.Infof("[installer] successfully created github app: slug=%s app_id=%d", creds.AppSlug, creds.AppID)

	if h.config.OnReloadNeeded != nil {
		log.Infof("[installer] triggering configuration reload")
		h.config.OnReloadNeeded()
	}

	data := h.successDataFromCreds(creds)
	h.renderSuccess(w, r, data)
}

// exchangeCode exchanges the temporary code for app credentials.
func (h *Handler) exchangeCode(ctx context.Context, code string) (*configstore.AppCredentials, error) {
	url := fmt.Sprintf("%s/api/v3/app-manifests/%s/conversions", h.config.GitHubURL, code)

	if h.config.GitHubURL == "https://github.com" {
		url = fmt.Sprintf("https://api.github.com/app-manifests/%s/conversions", code)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	client := &http.Client{
		Timeout: httpClientTimeout,
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call GitHub API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("GitHub API returned %d: %s", resp.StatusCode, string(body))
	}

	var creds configstore.AppCredentials
	if err := json.Unmarshal(body, &creds); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &creds, nil
}

func (h *Handler) handleDisable(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := clog.FromContext(ctx)

	status, err := h.config.Store.Status(ctx)
	if err != nil {
		log.Errorf("[installer] failed to check status: %v", err)
		http.Error(w, "Failed to check installer status", http.StatusInternalServerError)
		return
	}
	if status == nil || !status.Registered {
		http.Error(w, "Cannot disable installer before app is registered", http.StatusBadRequest)
		return
	}

	if err := h.config.Store.DisableInstaller(ctx); err != nil {
		log.Errorf("[installer] failed to disable installer: %v", err)
		http.Error(w, "Failed to disable installer", http.StatusInternalServerError)
		return
	}

	log.Infof("[installer] installer disabled via setup UI")
	http.Redirect(w, r, "/healthz", http.StatusSeeOther)
}

func (h *Handler) successDataFromCreds(creds *configstore.AppCredentials) successTemplateData {
	data := successTemplateData{
		AppDisplayName:   h.config.AppDisplayName,
		AppID:            creds.AppID,
		AppSlug:          creds.AppSlug,
		HTMLURL:          creds.HTMLURL,
		DisableActionURL: disableSetupPath,
	}
	data.InstallURL = h.installURLFor(creds.AppSlug, creds.HTMLURL)
	return data
}

func (h *Handler) successDataFromStatus(status *configstore.InstallerStatus) successTemplateData {
	if status == nil {
		return successTemplateData{AppDisplayName: h.config.AppDisplayName}
	}
	data := successTemplateData{
		AppDisplayName:    h.config.AppDisplayName,
		AppID:             status.AppID,
		AppSlug:           status.AppSlug,
		HTMLURL:           status.HTMLURL,
		InstallerDisabled: status.InstallerDisabled,
		DisableActionURL:  disableSetupPath,
	}
	data.InstallURL = h.installURLFor(status.AppSlug, status.HTMLURL)
	return data
}

func (h *Handler) installURLFor(slug, htmlURL string) string {
	if slug != "" {
		githubURL := h.config.GitHubURL
		if githubURL == "" {
			githubURL = "https://github.com"
		}
		return fmt.Sprintf("%s/apps/%s/installations/new", githubURL, slug)
	}
	if htmlURL != "" {
		trimmed := strings.TrimRight(htmlURL, "/")
		return trimmed + "/installations/new"
	}
	return ""
}

func (h *Handler) renderSuccess(w http.ResponseWriter, r *http.Request, data successTemplateData) {
	ctx := r.Context()
	log := clog.FromContext(ctx)

	var buf bytes.Buffer
	if err := successTemplate.Execute(&buf, data); err != nil {
		log.Errorf("[installer] failed to render success template: %v", err)
		http.Error(w, "Failed to render page", http.StatusInternalServerError)
		return
	}
	setSecurityHeaders(w)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if _, err := buf.WriteTo(w); err != nil {
		log.Errorf("[installer] failed to write response: %v", err)
	}
}

// setSecurityHeaders sets common security headers.
func setSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
}

// isValidOAuthCode validates the OAuth code format from GitHub.
func isValidOAuthCode(code string) bool {
	if len(code) < 10 || len(code) > 100 {
		return false
	}
	for _, c := range code {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
			return false
		}
	}
	return true
}

// getBaseURL derives the base URL from the request headers.
func getBaseURL(ctx context.Context, r *http.Request) string {
	log := clog.FromContext(ctx)

	host := r.Header.Get("X-Forwarded-Host")
	if host == "" {
		host = r.Host
	}

	scheme := r.Header.Get("X-Forwarded-Proto")
	if scheme == "" {
		scheme = "https"
		if host == "localhost" || strings.HasPrefix(host, "localhost:") ||
			host == "127.0.0.1" || strings.HasPrefix(host, "127.0.0.1:") {
			scheme = "http"
		}
	} else if scheme == "http" && !strings.HasPrefix(host, "localhost") && !strings.HasPrefix(host, "127.0.0.1") {
		scheme = "https"
	}

	baseURL := scheme + "://" + host
	log.Debugf("[installer] getBaseURL: scheme=%s host=%s r.Host=%s X-Forwarded-Proto=%s X-Forwarded-Host=%s result=%s",
		scheme, host, r.Host, r.Header.Get("X-Forwarded-Proto"), r.Header.Get("X-Forwarded-Host"), baseURL)
	return baseURL
}
