// Copyright 2025 CruxStack
// SPDX-License-Identifier: MIT

package installer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cruxstack/github-app-setup-go/configstore"
)

func TestGetBaseURL(t *testing.T) {
	tests := []struct {
		name            string
		host            string
		xForwardedHost  string
		xForwardedProto string
		want            string
	}{
		{
			name: "localhost defaults to http",
			host: "localhost:8080",
			want: "http://localhost:8080",
		},
		{
			name: "localhost without port",
			host: "localhost",
			want: "http://localhost",
		},
		{
			name: "127.0.0.1 defaults to http",
			host: "127.0.0.1:8080",
			want: "http://127.0.0.1:8080",
		},
		{
			name: "non-localhost defaults to https",
			host: "example.com",
			want: "https://example.com",
		},
		{
			name: "non-localhost with port defaults to https",
			host: "example.com:8443",
			want: "https://example.com:8443",
		},
		{
			name:           "X-Forwarded-Host takes precedence",
			host:           "internal-lb:8080",
			xForwardedHost: "api.example.com",
			want:           "https://api.example.com",
		},
		{
			name:            "X-Forwarded-Proto http allowed for localhost",
			host:            "localhost:3000",
			xForwardedProto: "http",
			want:            "http://localhost:3000",
		},
		{
			name:            "X-Forwarded-Proto http upgraded to https for non-localhost",
			host:            "example.com",
			xForwardedProto: "http",
			want:            "https://example.com",
		},
		{
			name:            "X-Forwarded-Proto https respected",
			host:            "example.com",
			xForwardedProto: "https",
			want:            "https://example.com",
		},
		{
			name:            "both forwarded headers",
			host:            "internal:8080",
			xForwardedHost:  "app.example.com",
			xForwardedProto: "https",
			want:            "https://app.example.com",
		},
		{
			name:            "X-Forwarded-Proto http with localhost forwarded host",
			host:            "production:8080",
			xForwardedHost:  "localhost:3000",
			xForwardedProto: "http",
			want:            "http://localhost:3000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Host = tt.host

			if tt.xForwardedHost != "" {
				req.Header.Set("X-Forwarded-Host", tt.xForwardedHost)
			}
			if tt.xForwardedProto != "" {
				req.Header.Set("X-Forwarded-Proto", tt.xForwardedProto)
			}

			got := getBaseURL(context.Background(), req)
			if got != tt.want {
				t.Errorf("getBaseURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestInstallURLFor(t *testing.T) {
	tests := []struct {
		name      string
		githubURL string
		slug      string
		htmlURL   string
		want      string
	}{
		{
			name:      "slug provided uses GitHub URL",
			githubURL: "https://github.com",
			slug:      "my-app",
			htmlURL:   "",
			want:      "https://github.com/apps/my-app/installations/new",
		},
		{
			name:      "slug with GHE URL",
			githubURL: "https://github.mycompany.com",
			slug:      "internal-app",
			htmlURL:   "",
			want:      "https://github.mycompany.com/apps/internal-app/installations/new",
		},
		{
			name:      "no slug uses htmlURL",
			githubURL: "https://github.com",
			slug:      "",
			htmlURL:   "https://github.com/apps/my-app",
			want:      "https://github.com/apps/my-app/installations/new",
		},
		{
			name:      "htmlURL with trailing slash",
			githubURL: "https://github.com",
			slug:      "",
			htmlURL:   "https://github.com/apps/my-app/",
			want:      "https://github.com/apps/my-app/installations/new",
		},
		{
			name:      "neither slug nor htmlURL",
			githubURL: "https://github.com",
			slug:      "",
			htmlURL:   "",
			want:      "",
		},
		{
			name:      "slug takes precedence over htmlURL",
			githubURL: "https://github.com",
			slug:      "preferred-app",
			htmlURL:   "https://github.com/apps/other-app",
			want:      "https://github.com/apps/preferred-app/installations/new",
		},
		{
			name:      "empty githubURL defaults to github.com",
			githubURL: "",
			slug:      "my-app",
			htmlURL:   "",
			want:      "https://github.com/apps/my-app/installations/new",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &Handler{
				config: Config{
					GitHubURL: tt.githubURL,
				},
			}

			got := h.installURLFor(tt.slug, tt.htmlURL)
			if got != tt.want {
				t.Errorf("installURLFor(%q, %q) = %q, want %q", tt.slug, tt.htmlURL, got, tt.want)
			}
		})
	}
}

func TestManifestClone(t *testing.T) {
	t.Run("nil manifest returns nil", func(t *testing.T) {
		var m *Manifest = nil
		got := m.Clone()
		if got != nil {
			t.Errorf("Clone() = %v, want nil", got)
		}
	})

	t.Run("clones all scalar fields", func(t *testing.T) {
		original := &Manifest{
			Name:        "test-app",
			URL:         "https://example.com",
			RedirectURL: "https://example.com/callback",
			Public:      true,
			HookAttributes: HookAttributes{
				URL:    "https://example.com/webhook",
				Active: true,
			},
		}

		clone := original.Clone()

		if clone.Name != original.Name {
			t.Errorf("Clone().Name = %q, want %q", clone.Name, original.Name)
		}
		if clone.URL != original.URL {
			t.Errorf("Clone().URL = %q, want %q", clone.URL, original.URL)
		}
		if clone.RedirectURL != original.RedirectURL {
			t.Errorf("Clone().RedirectURL = %q, want %q", clone.RedirectURL, original.RedirectURL)
		}
		if clone.Public != original.Public {
			t.Errorf("Clone().Public = %v, want %v", clone.Public, original.Public)
		}
		if clone.HookAttributes.URL != original.HookAttributes.URL {
			t.Errorf("Clone().HookAttributes.URL = %q, want %q", clone.HookAttributes.URL, original.HookAttributes.URL)
		}
		if clone.HookAttributes.Active != original.HookAttributes.Active {
			t.Errorf("Clone().HookAttributes.Active = %v, want %v", clone.HookAttributes.Active, original.HookAttributes.Active)
		}
	})

	t.Run("DefaultPerms is deep copied", func(t *testing.T) {
		original := &Manifest{
			DefaultPerms: map[string]string{
				"contents":      "read",
				"pull_requests": "write",
			},
		}

		clone := original.Clone()

		// Verify values are the same
		if clone.DefaultPerms["contents"] != "read" {
			t.Error("Clone().DefaultPerms missing expected value")
		}

		// Modify clone, original should be unchanged
		clone.DefaultPerms["contents"] = "write"
		clone.DefaultPerms["new_perm"] = "read"

		if original.DefaultPerms["contents"] != "read" {
			t.Error("Modifying clone affected original DefaultPerms")
		}
		if _, exists := original.DefaultPerms["new_perm"]; exists {
			t.Error("Adding to clone affected original DefaultPerms")
		}
	})

	t.Run("DefaultEvents is deep copied", func(t *testing.T) {
		original := &Manifest{
			DefaultEvents: []string{"push", "pull_request"},
		}

		clone := original.Clone()

		// Verify values are the same
		if len(clone.DefaultEvents) != 2 {
			t.Error("Clone().DefaultEvents has wrong length")
		}

		// Modify clone, original should be unchanged
		clone.DefaultEvents[0] = "modified"
		clone.DefaultEvents = append(clone.DefaultEvents, "new_event")

		if original.DefaultEvents[0] != "push" {
			t.Error("Modifying clone affected original DefaultEvents")
		}
		if len(original.DefaultEvents) != 2 {
			t.Error("Appending to clone affected original DefaultEvents")
		}
	})

	t.Run("nil maps and slices handled", func(t *testing.T) {
		original := &Manifest{
			Name:          "test",
			DefaultPerms:  nil,
			DefaultEvents: nil,
		}

		clone := original.Clone()

		if clone.DefaultPerms != nil {
			t.Error("Clone() should have nil DefaultPerms when original is nil")
		}
		if clone.DefaultEvents != nil {
			t.Error("Clone() should have nil DefaultEvents when original is nil")
		}
	})
}

func TestNew_Validation(t *testing.T) {
	t.Run("nil store returns error", func(t *testing.T) {
		_, err := New(Config{Store: nil})
		if err == nil {
			t.Error("New() with nil store should return error")
		}
	})

	t.Run("valid config succeeds", func(t *testing.T) {
		store := &mockStore{}
		h, err := New(Config{Store: store})
		if err != nil {
			t.Errorf("New() error = %v, want nil", err)
		}
		if h == nil {
			t.Error("New() returned nil handler")
		}
	})

	t.Run("empty GitHubURL defaults to github.com", func(t *testing.T) {
		store := &mockStore{}
		h, _ := New(Config{Store: store, GitHubURL: ""})
		if h.config.GitHubURL != "https://github.com" {
			t.Errorf("GitHubURL = %q, want %q", h.config.GitHubURL, "https://github.com")
		}
	})

	t.Run("empty AppDisplayName defaults", func(t *testing.T) {
		store := &mockStore{}
		h, _ := New(Config{Store: store, AppDisplayName: ""})
		if h.config.AppDisplayName != "GitHub App" {
			t.Errorf("AppDisplayName = %q, want %q", h.config.AppDisplayName, "GitHub App")
		}
	})
}

func TestHandler_ServeHTTP_Routing(t *testing.T) {
	store := &mockStore{
		statusFunc: func(ctx context.Context) (*configstore.InstallerStatus, error) {
			return &configstore.InstallerStatus{Registered: false}, nil
		},
	}

	h, _ := New(Config{Store: store})

	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
	}{
		{"GET root redirects to setup", http.MethodGet, "/", http.StatusFound},
		{"GET /setup returns page", http.MethodGet, "/setup", http.StatusOK},
		{"GET /setup/ returns page", http.MethodGet, "/setup/", http.StatusOK},
		{"GET /callback without code returns 400", http.MethodGet, "/callback", http.StatusBadRequest},
		{"POST /setup/disable without registration returns 400", http.MethodPost, "/setup/disable", http.StatusBadRequest},
		{"unknown path returns 404", http.MethodGet, "/unknown", http.StatusNotFound},
		{"POST to GET-only path returns 404", http.MethodPost, "/setup", http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			rec := httptest.NewRecorder()

			h.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("ServeHTTP(%s %s) status = %d, want %d", tt.method, tt.path, rec.Code, tt.wantStatus)
			}
		})
	}
}

func TestHandler_handleRoot_DisabledInstaller(t *testing.T) {
	store := &mockStore{
		statusFunc: func(ctx context.Context) (*configstore.InstallerStatus, error) {
			return &configstore.InstallerStatus{InstallerDisabled: true}, nil
		},
	}

	h, _ := New(Config{Store: store})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("handleRoot() with disabled installer status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestHandler_handleIndex_AlreadyRegistered(t *testing.T) {
	store := &mockStore{
		statusFunc: func(ctx context.Context) (*configstore.InstallerStatus, error) {
			return &configstore.InstallerStatus{
				Registered: true,
				AppID:      12345,
				AppSlug:    "my-app",
			}, nil
		},
	}

	h, _ := New(Config{Store: store})

	req := httptest.NewRequest(http.MethodGet, "/setup", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	// Should show success page
	if rec.Code != http.StatusOK {
		t.Errorf("handleIndex() with registered app status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestIsValidOAuthCode(t *testing.T) {
	tests := []struct {
		name string
		code string
		want bool
	}{
		{"valid alphanumeric code", "abc123DEF456xyz789", true},
		{"valid 20 character code", "abcdefghij1234567890", true},
		{"code too short", "abc", false},
		{"code too long", string(make([]byte, 101)), false},
		{"contains hyphen", "abc-123", false},
		{"contains underscore", "abc_123", false},
		{"contains space", "abc 123", false},
		{"contains special chars", "abc!@#123", false},
		{"empty string", "", false},
		{"minimum valid length", "abcdefghij", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isValidOAuthCode(tt.code)
			if got != tt.want {
				t.Errorf("isValidOAuthCode(%q) = %v, want %v", tt.code, got, tt.want)
			}
		})
	}
}

func TestHandler_handleCallback_InvalidCode(t *testing.T) {
	store := &mockStore{
		statusFunc: func(ctx context.Context) (*configstore.InstallerStatus, error) {
			return &configstore.InstallerStatus{Registered: false}, nil
		},
	}

	h, _ := New(Config{Store: store})

	tests := []struct {
		name       string
		code       string
		wantStatus int
	}{
		{"missing code", "", http.StatusBadRequest},
		{"invalid code with special chars", "abc!@#123", http.StatusBadRequest},
		{"code too short", "abc", http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := "/callback"
			if tt.code != "" {
				url += "?code=" + tt.code
			}
			req := httptest.NewRequest(http.MethodGet, url, nil)
			rec := httptest.NewRecorder()

			h.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("handleCallback() with code %q status = %d, want %d", tt.code, rec.Code, tt.wantStatus)
			}
		})
	}
}

// mockStore implements configstore.Store for testing
type mockStore struct {
	saveFunc             func(ctx context.Context, creds *configstore.AppCredentials) error
	statusFunc           func(ctx context.Context) (*configstore.InstallerStatus, error)
	disableInstallerFunc func(ctx context.Context) error
}

func (m *mockStore) Save(ctx context.Context, creds *configstore.AppCredentials) error {
	if m.saveFunc != nil {
		return m.saveFunc(ctx, creds)
	}
	return nil
}

func (m *mockStore) Status(ctx context.Context) (*configstore.InstallerStatus, error) {
	if m.statusFunc != nil {
		return m.statusFunc(ctx)
	}
	return &configstore.InstallerStatus{}, nil
}

func (m *mockStore) DisableInstaller(ctx context.Context) error {
	if m.disableInstallerFunc != nil {
		return m.disableInstallerFunc(ctx)
	}
	return nil
}
