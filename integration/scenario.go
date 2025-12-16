// Copyright 2025 CruxStack
// SPDX-License-Identifier: MIT

//go:build integration

package integration

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/cruxstack/github-app-setup-go/configstore"
	"github.com/cruxstack/github-app-setup-go/installer"
)

// Scenario defines a single integration test case.
type Scenario struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description,omitempty"`

	// Config overrides for the installer
	Config ScenarioConfig `yaml:"config,omitempty"`

	// Mock responses from GitHub API
	MockResponses []MockResponse `yaml:"mock_responses,omitempty"`

	// Preset credentials to seed the store before the test
	PresetCredentials *PresetCredentials `yaml:"preset_credentials,omitempty"`

	// Test steps to execute
	Steps []Step `yaml:"steps"`

	// Expected state after test completion
	ExpectedStore *ExpectedStore `yaml:"expected_store,omitempty"`

	// Expected HTTP calls to mock GitHub
	ExpectedCalls []ExpectedCall `yaml:"expected_calls,omitempty"`

	// Whether a reload should have been triggered
	ExpectReload bool `yaml:"expect_reload,omitempty"`
}

// ScenarioConfig holds installer configuration overrides.
type ScenarioConfig struct {
	AppDisplayName string `yaml:"app_display_name,omitempty"`
	GitHubOrg      string `yaml:"github_org,omitempty"`
	WebhookURL     string `yaml:"webhook_url,omitempty"`
}

// PresetCredentials allows seeding the store with existing credentials.
type PresetCredentials struct {
	AppID         int64  `yaml:"app_id"`
	AppSlug       string `yaml:"app_slug"`
	ClientID      string `yaml:"client_id,omitempty"`
	ClientSecret  string `yaml:"client_secret,omitempty"`
	WebhookSecret string `yaml:"webhook_secret,omitempty"`
	PrivateKey    string `yaml:"private_key,omitempty"`
	HTMLURL       string `yaml:"html_url,omitempty"`
}

// Step defines a single action in the test scenario.
type Step struct {
	Action             string   `yaml:"action"`
	Method             string   `yaml:"method,omitempty"`
	Path               string   `yaml:"path,omitempty"`
	ExpectStatus       int      `yaml:"expect_status,omitempty"`
	ExpectBodyContains []string `yaml:"expect_body_contains,omitempty"`
	ExpectRedirect     string   `yaml:"expect_redirect,omitempty"`
}

// ExpectedStore defines the expected state of the store after the test.
type ExpectedStore struct {
	Registered        bool   `yaml:"registered"`
	InstallerDisabled bool   `yaml:"installer_disabled,omitempty"`
	AppID             int64  `yaml:"app_id,omitempty"`
	AppSlug           string `yaml:"app_slug,omitempty"`
}

// ExpectedCall defines an expected HTTP call to the mock server.
type ExpectedCall struct {
	Method string `yaml:"method"`
	Path   string `yaml:"path"`
}

// LoadScenarios reads scenarios from a YAML file.
func LoadScenarios(path string) ([]Scenario, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read scenarios file: %w", err)
	}

	var scenarios []Scenario
	if err := yaml.Unmarshal(data, &scenarios); err != nil {
		return nil, fmt.Errorf("parse scenarios: %w", err)
	}

	return scenarios, nil
}

// ScenarioRunner executes integration test scenarios.
type ScenarioRunner struct {
	t       *testing.T
	verbose bool
}

// NewScenarioRunner creates a new scenario runner.
func NewScenarioRunner(t *testing.T, verbose bool) *ScenarioRunner {
	return &ScenarioRunner{t: t, verbose: verbose}
}

// Run executes a single scenario.
func (r *ScenarioRunner) Run(scenario Scenario) {
	r.t.Run(scenario.Name, func(t *testing.T) {
		if r.verbose {
			t.Logf("Running scenario: %s", scenario.Name)
			if scenario.Description != "" {
				t.Logf("  Description: %s", scenario.Description)
			}
		}

		// Get shared TLS certificate (generated once at test startup)
		tlsCert, certPool, err := getSharedTLSCert()
		if err != nil {
			t.Fatalf("get TLS cert: %v", err)
		}

		// Create mock GitHub server
		mockGitHub := NewMockGitHubServer(scenario.MockResponses, r.verbose)
		githubServer := httptest.NewUnstartedServer(mockGitHub)
		githubServer.TLS = &tls.Config{
			Certificates: []tls.Certificate{tlsCert},
		}
		githubServer.StartTLS()
		defer githubServer.Close()

		// Create temp directory for .env file
		tempDir := t.TempDir()
		envFilePath := filepath.Join(tempDir, ".env")

		// Create store
		store := configstore.NewLocalEnvFileStore(envFilePath)

		// Preset credentials if specified
		if scenario.PresetCredentials != nil {
			creds := &configstore.AppCredentials{
				AppID:         scenario.PresetCredentials.AppID,
				AppSlug:       scenario.PresetCredentials.AppSlug,
				ClientID:      scenario.PresetCredentials.ClientID,
				ClientSecret:  scenario.PresetCredentials.ClientSecret,
				WebhookSecret: scenario.PresetCredentials.WebhookSecret,
				PrivateKey:    scenario.PresetCredentials.PrivateKey,
				HTMLURL:       scenario.PresetCredentials.HTMLURL,
			}
			// Fill in defaults for required fields
			if creds.PrivateKey == "" {
				rsaKey, err := getSharedRSAKeyPEM()
				if err != nil {
					t.Fatalf("get RSA key: %v", err)
				}
				creds.PrivateKey = rsaKey
			}
			if err := store.Save(context.Background(), creds); err != nil {
				t.Fatalf("preset credentials: %v", err)
			}
		}

		// Track reload calls using atomic counter
		var reloadCount atomic.Int64

		// Create installer handler
		cfg := installer.Config{
			Store:          store,
			GitHubURL:      githubServer.URL,
			AppDisplayName: "GitHub App",
		}
		if scenario.Config.AppDisplayName != "" {
			cfg.AppDisplayName = scenario.Config.AppDisplayName
		}
		if scenario.Config.GitHubOrg != "" {
			cfg.GitHubOrg = scenario.Config.GitHubOrg
		}
		if scenario.Config.WebhookURL != "" {
			cfg.WebhookURL = scenario.Config.WebhookURL
		}

		// Set up reload callback to track reload calls
		cfg.OnReloadNeeded = func() {
			reloadCount.Add(1)
		}

		handler, err := installer.New(cfg)
		if err != nil {
			t.Fatalf("create installer: %v", err)
		}

		// Create test server for installer (also HTTPS)
		installerServer := httptest.NewUnstartedServer(handler)
		installerServer.TLS = &tls.Config{
			Certificates: []tls.Certificate{tlsCert},
		}
		installerServer.StartTLS()
		defer installerServer.Close()

		// Create HTTP client that trusts our self-signed cert
		httpClient := &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					RootCAs: certPool,
				},
			},
			// Don't follow redirects automatically - we want to inspect them
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
			Timeout: 10 * time.Second,
		}

		originalTransport := http.DefaultTransport
		http.DefaultTransport = &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs: certPool,
			},
		}
		defer func() { http.DefaultTransport = originalTransport }()

		// Execute test steps
		for i, step := range scenario.Steps {
			if r.verbose {
				t.Logf("  Step %d: %s %s", i+1, step.Method, step.Path)
			}

			switch step.Action {
			case "request":
				r.executeRequestStep(t, httpClient, installerServer.URL, step)
			default:
				t.Fatalf("unknown action: %s", step.Action)
			}
		}

		time.Sleep(50 * time.Millisecond)

		// Verify expected store state
		if scenario.ExpectedStore != nil {
			status, err := store.Status(context.Background())
			if err != nil {
				t.Fatalf("get store status: %v", err)
			}

			if status.Registered != scenario.ExpectedStore.Registered {
				t.Errorf("store.Registered = %v, want %v", status.Registered, scenario.ExpectedStore.Registered)
			}
			if status.InstallerDisabled != scenario.ExpectedStore.InstallerDisabled {
				t.Errorf("store.InstallerDisabled = %v, want %v", status.InstallerDisabled, scenario.ExpectedStore.InstallerDisabled)
			}
			if scenario.ExpectedStore.AppID != 0 && status.AppID != scenario.ExpectedStore.AppID {
				t.Errorf("store.AppID = %d, want %d", status.AppID, scenario.ExpectedStore.AppID)
			}
			if scenario.ExpectedStore.AppSlug != "" && status.AppSlug != scenario.ExpectedStore.AppSlug {
				t.Errorf("store.AppSlug = %q, want %q", status.AppSlug, scenario.ExpectedStore.AppSlug)
			}
		}

		// Verify expected HTTP calls to mock GitHub
		if len(scenario.ExpectedCalls) > 0 {
			requests := mockGitHub.GetRequests()
			for _, expected := range scenario.ExpectedCalls {
				found := false
				for _, req := range requests {
					if req.Method == expected.Method && matchPath(req.Path, expected.Path) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected call not found: %s %s", expected.Method, expected.Path)
					t.Logf("actual calls:")
					for _, req := range requests {
						t.Logf("  %s %s", req.Method, req.Path)
					}
				}
			}
		}

		// Verify reload was triggered if expected
		if scenario.ExpectReload {
			count := reloadCount.Load()
			if count == 0 {
				t.Errorf("expected reload to be triggered, but it was not")
			}
		}
	})
}

func (r *ScenarioRunner) executeRequestStep(t *testing.T, client *http.Client, baseURL string, step Step) {
	url := baseURL + step.Path
	req, err := http.NewRequest(step.Method, url, nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("execute request: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}

	// Check status code
	if step.ExpectStatus != 0 && resp.StatusCode != step.ExpectStatus {
		t.Errorf("%s %s: status = %d, want %d\nBody: %s", step.Method, step.Path, resp.StatusCode, step.ExpectStatus, string(body))
	}

	// Check body contains expected strings
	for _, expected := range step.ExpectBodyContains {
		if !strings.Contains(string(body), expected) {
			t.Errorf("%s %s: body does not contain %q\nBody: %s", step.Method, step.Path, expected, string(body))
		}
	}

	// Check redirect location
	if step.ExpectRedirect != "" {
		location := resp.Header.Get("Location")
		if location != step.ExpectRedirect {
			t.Errorf("%s %s: redirect = %q, want %q", step.Method, step.Path, location, step.ExpectRedirect)
		}
	}
}
