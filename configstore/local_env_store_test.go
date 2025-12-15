// Copyright 2025 CruxStack
// SPDX-License-Identifier: MIT

package configstore

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseEnvFile(t *testing.T) {
	tests := []struct {
		name       string
		content    string
		wantValues map[string]string
		wantLines  int
	}{
		{
			name:    "simple key-value pairs",
			content: "FOO=bar\nBAZ=qux",
			wantValues: map[string]string{
				"FOO": "bar",
				"BAZ": "qux",
			},
			wantLines: 2,
		},
		{
			name:    "double-quoted values",
			content: `KEY="value with spaces"`,
			wantValues: map[string]string{
				"KEY": "value with spaces",
			},
			wantLines: 1,
		},
		{
			name:    "single-quoted values",
			content: `KEY='value with spaces'`,
			wantValues: map[string]string{
				"KEY": "value with spaces",
			},
			wantLines: 1,
		},
		{
			name:    "unquoted value with equals sign",
			content: "KEY=value=with=equals",
			wantValues: map[string]string{
				"KEY": "value=with=equals",
			},
			wantLines: 1,
		},
		{
			name:    "comments are ignored",
			content: "# This is a comment\nKEY=value\n# Another comment",
			wantValues: map[string]string{
				"KEY": "value",
			},
			wantLines: 3,
		},
		{
			name:    "empty lines preserved",
			content: "FOO=bar\n\nBAZ=qux",
			wantValues: map[string]string{
				"FOO": "bar",
				"BAZ": "qux",
			},
			wantLines: 3,
		},
		{
			name:    "whitespace around equals",
			content: "KEY = value",
			wantValues: map[string]string{
				"KEY": "value",
			},
			wantLines: 1,
		},
		{
			name:    "PEM key with escaped newlines",
			content: `PRIVATE_KEY="-----BEGIN RSA PRIVATE KEY-----\nMIIE...\n-----END RSA PRIVATE KEY-----"`,
			wantValues: map[string]string{
				"PRIVATE_KEY": `-----BEGIN RSA PRIVATE KEY-----\nMIIE...\n-----END RSA PRIVATE KEY-----`,
			},
			wantLines: 1,
		},
		{
			name:       "empty file",
			content:    "",
			wantValues: map[string]string{},
			wantLines:  0,
		},
		{
			name:       "only comments",
			content:    "# comment 1\n# comment 2",
			wantValues: map[string]string{},
			wantLines:  2,
		},
		{
			name:    "line without equals ignored",
			content: "INVALID_LINE\nVALID=true",
			wantValues: map[string]string{
				"VALID": "true",
			},
			wantLines: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			envPath := filepath.Join(tmpDir, ".env")

			if err := os.WriteFile(envPath, []byte(tt.content), 0600); err != nil {
				t.Fatalf("Failed to write test file: %v", err)
			}

			values, lines, err := parseEnvFile(envPath)
			if err != nil {
				t.Fatalf("parseEnvFile() error = %v", err)
			}

			if len(values) != len(tt.wantValues) {
				t.Errorf("parseEnvFile() got %d values, want %d", len(values), len(tt.wantValues))
			}

			for k, want := range tt.wantValues {
				got, ok := values[k]
				if !ok {
					t.Errorf("parseEnvFile() missing key %q", k)
					continue
				}
				if got != want {
					t.Errorf("parseEnvFile()[%q] = %q, want %q", k, got, want)
				}
			}

			if len(lines) != tt.wantLines {
				t.Errorf("parseEnvFile() got %d lines, want %d", len(lines), tt.wantLines)
			}
		})
	}
}

func TestParseEnvFile_NotExists(t *testing.T) {
	values, lines, err := parseEnvFile("/nonexistent/path/.env")

	if !os.IsNotExist(err) {
		t.Errorf("parseEnvFile() error = %v, want os.IsNotExist", err)
	}
	if values != nil {
		t.Errorf("parseEnvFile() values = %v, want nil", values)
	}
	if lines != nil {
		t.Errorf("parseEnvFile() lines = %v, want nil", lines)
	}
}

func TestFormatEnvLine(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
		want  string
	}{
		{
			name:  "simple value",
			key:   "KEY",
			value: "value",
			want:  "KEY=value",
		},
		{
			name:  "value with spaces needs quotes",
			key:   "KEY",
			value: "value with spaces",
			want:  `KEY="value with spaces"`,
		},
		{
			name:  "value with escaped newlines needs quotes",
			key:   "PEM",
			value: `-----BEGIN RSA-----\nMIIE\n-----END RSA-----`,
			want:  `PEM="-----BEGIN RSA-----\nMIIE\n-----END RSA-----"`,
		},
		{
			name:  "value with hash needs quotes",
			key:   "KEY",
			value: "value#comment",
			want:  `KEY="value#comment"`,
		},
		{
			name:  "value with double quote is escaped",
			key:   "KEY",
			value: `value"quoted`,
			want:  `KEY="value\"quoted"`,
		},
		{
			name:  "value with single quote needs quotes",
			key:   "KEY",
			value: "it's",
			want:  `KEY="it's"`,
		},
		{
			name:  "value with tab needs quotes",
			key:   "KEY",
			value: "value\twith\ttabs",
			want:  `KEY="value	with	tabs"`,
		},
		{
			name:  "empty value",
			key:   "KEY",
			value: "",
			want:  "KEY=",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatEnvLine(tt.key, tt.value)
			if got != tt.want {
				t.Errorf("formatEnvLine(%q, %q) = %q, want %q", tt.key, tt.value, got, tt.want)
			}
		})
	}
}

func TestWriteEnvFile_PreservesComments(t *testing.T) {
	tmpDir := t.TempDir()
	envPath := filepath.Join(tmpDir, ".env")

	originalContent := `# Database config
DB_HOST=localhost

# App config
APP_NAME=test
`
	if err := os.WriteFile(envPath, []byte(originalContent), 0600); err != nil {
		t.Fatalf("Failed to write initial file: %v", err)
	}

	values, lines, err := parseEnvFile(envPath)
	if err != nil {
		t.Fatalf("parseEnvFile() error = %v", err)
	}

	// Update one value and add a new one
	values["APP_NAME"] = "updated"
	values["NEW_KEY"] = "new_value"

	if err := writeEnvFile(envPath, values, lines); err != nil {
		t.Fatalf("writeEnvFile() error = %v", err)
	}

	content, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	result := string(content)

	// Comments should be preserved
	if !strings.Contains(result, "# Database config") {
		t.Error("writeEnvFile() lost comment '# Database config'")
	}
	if !strings.Contains(result, "# App config") {
		t.Error("writeEnvFile() lost comment '# App config'")
	}

	// Updated value should be present
	if !strings.Contains(result, "APP_NAME=updated") {
		t.Error("writeEnvFile() did not update APP_NAME")
	}

	// New key should be appended
	if !strings.Contains(result, "NEW_KEY=new_value") {
		t.Error("writeEnvFile() did not add NEW_KEY")
	}

	// Original structure should be maintained
	if !strings.Contains(result, "DB_HOST=localhost") {
		t.Error("writeEnvFile() lost DB_HOST")
	}
}

func TestLocalEnvFileStore_Save(t *testing.T) {
	tmpDir := t.TempDir()
	envPath := filepath.Join(tmpDir, ".env")

	store := NewLocalEnvFileStore(envPath)
	creds := &AppCredentials{
		AppID:         12345,
		AppSlug:       "my-app",
		ClientID:      "Iv1.abc123",
		ClientSecret:  "secret123",
		WebhookSecret: "whsec_123",
		PrivateKey:    "-----BEGIN RSA PRIVATE KEY-----\nMIIE...\n-----END RSA PRIVATE KEY-----\n",
		HTMLURL:       "https://github.com/apps/my-app",
		CustomFields: map[string]string{
			"STS_DOMAIN": "sts.example.com",
		},
	}

	err := store.Save(context.Background(), creds)
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Verify file was created with correct permissions
	info, err := os.Stat(envPath)
	if err != nil {
		t.Fatalf("File not created: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("File permissions = %o, want 0600", info.Mode().Perm())
	}

	// Parse and verify contents
	values, _, err := parseEnvFile(envPath)
	if err != nil {
		t.Fatalf("parseEnvFile() error = %v", err)
	}

	checks := map[string]string{
		EnvGitHubAppID:         "12345",
		EnvGitHubAppSlug:       "my-app",
		EnvGitHubClientID:      "Iv1.abc123",
		EnvGitHubClientSecret:  "secret123",
		EnvGitHubWebhookSecret: "whsec_123",
		EnvGitHubAppHTMLURL:    "https://github.com/apps/my-app",
		"STS_DOMAIN":           "sts.example.com",
	}

	for key, want := range checks {
		got := values[key]
		if got != want {
			t.Errorf("values[%q] = %q, want %q", key, got, want)
		}
	}

	// PEM key should have newlines escaped
	pemValue := values[EnvGitHubAppPrivateKey]
	if strings.Contains(pemValue, "\n") && !strings.Contains(pemValue, "\\n") {
		t.Error("Private key should have literal \\n not actual newlines")
	}
}

func TestLocalEnvFileStore_Save_CreatesDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	envPath := filepath.Join(tmpDir, "nested", "dir", ".env")

	store := NewLocalEnvFileStore(envPath)
	creds := &AppCredentials{
		AppID:         1,
		ClientID:      "client",
		ClientSecret:  "secret",
		WebhookSecret: "webhook",
		PrivateKey:    "key",
	}

	err := store.Save(context.Background(), creds)
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if _, err := os.Stat(envPath); os.IsNotExist(err) {
		t.Error("Save() did not create file in nested directory")
	}
}

func TestLocalEnvFileStore_Save_PreservesExistingValues(t *testing.T) {
	tmpDir := t.TempDir()
	envPath := filepath.Join(tmpDir, ".env")

	// Write initial content
	initialContent := `# My app config
EXISTING_KEY=existing_value
PORT=8080
`
	if err := os.WriteFile(envPath, []byte(initialContent), 0600); err != nil {
		t.Fatalf("Failed to write initial file: %v", err)
	}

	store := NewLocalEnvFileStore(envPath)
	creds := &AppCredentials{
		AppID:         1,
		ClientID:      "client",
		ClientSecret:  "secret",
		WebhookSecret: "webhook",
		PrivateKey:    "key",
	}

	err := store.Save(context.Background(), creds)
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	values, _, err := parseEnvFile(envPath)
	if err != nil {
		t.Fatalf("parseEnvFile() error = %v", err)
	}

	// Existing values should be preserved
	if values["EXISTING_KEY"] != "existing_value" {
		t.Error("Save() overwrote EXISTING_KEY")
	}
	if values["PORT"] != "8080" {
		t.Error("Save() overwrote PORT")
	}

	// New values should be present
	if values[EnvGitHubAppID] != "1" {
		t.Error("Save() did not write GITHUB_APP_ID")
	}
}

func TestLocalEnvFileStore_Status(t *testing.T) {
	tmpDir := t.TempDir()
	envPath := filepath.Join(tmpDir, ".env")

	content := `GITHUB_APP_ID=12345
GITHUB_APP_SLUG=my-app
GITHUB_APP_HTML_URL=https://github.com/apps/my-app
GITHUB_APP_PRIVATE_KEY="-----BEGIN RSA-----\nMIIE\n-----END RSA-----"
GITHUB_WEBHOOK_SECRET=whsec_123
GITHUB_CLIENT_ID=Iv1.abc
GITHUB_CLIENT_SECRET=secret
`
	if err := os.WriteFile(envPath, []byte(content), 0600); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}

	store := NewLocalEnvFileStore(envPath)
	status, err := store.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}

	if !status.Registered {
		t.Error("Status.Registered = false, want true")
	}
	if status.AppID != 12345 {
		t.Errorf("Status.AppID = %d, want 12345", status.AppID)
	}
	if status.AppSlug != "my-app" {
		t.Errorf("Status.AppSlug = %q, want %q", status.AppSlug, "my-app")
	}
	if status.HTMLURL != "https://github.com/apps/my-app" {
		t.Errorf("Status.HTMLURL = %q, want %q", status.HTMLURL, "https://github.com/apps/my-app")
	}
	if status.InstallerDisabled {
		t.Error("Status.InstallerDisabled = true, want false")
	}
}

func TestLocalEnvFileStore_Status_NotRegistered(t *testing.T) {
	tmpDir := t.TempDir()
	envPath := filepath.Join(tmpDir, ".env")

	// Missing required fields
	content := `GITHUB_APP_ID=12345
# Missing other required fields
`
	if err := os.WriteFile(envPath, []byte(content), 0600); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}

	store := NewLocalEnvFileStore(envPath)
	status, err := store.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}

	if status.Registered {
		t.Error("Status.Registered = true, want false (missing required fields)")
	}
}

func TestLocalEnvFileStore_Status_FileNotExists(t *testing.T) {
	store := NewLocalEnvFileStore("/nonexistent/.env")
	status, err := store.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v, want nil for nonexistent file", err)
	}

	if status.Registered {
		t.Error("Status.Registered = true, want false for nonexistent file")
	}
}

func TestLocalEnvFileStore_Status_InstallerDisabled(t *testing.T) {
	tmpDir := t.TempDir()
	envPath := filepath.Join(tmpDir, ".env")

	content := `GITHUB_APP_INSTALLER_ENABLED=false
`
	if err := os.WriteFile(envPath, []byte(content), 0600); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}

	store := NewLocalEnvFileStore(envPath)
	status, err := store.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}

	if !status.InstallerDisabled {
		t.Error("Status.InstallerDisabled = false, want true")
	}
}

func TestLocalEnvFileStore_DisableInstaller(t *testing.T) {
	tmpDir := t.TempDir()
	envPath := filepath.Join(tmpDir, ".env")

	store := NewLocalEnvFileStore(envPath)
	err := store.DisableInstaller(context.Background())
	if err != nil {
		t.Fatalf("DisableInstaller() error = %v", err)
	}

	values, _, err := parseEnvFile(envPath)
	if err != nil {
		t.Fatalf("parseEnvFile() error = %v", err)
	}

	if values[EnvGitHubAppInstallerEnabled] != "false" {
		t.Errorf("DisableInstaller() did not set %s=false", EnvGitHubAppInstallerEnabled)
	}
}

func TestLocalEnvFileStore_RoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	envPath := filepath.Join(tmpDir, ".env")

	store := NewLocalEnvFileStore(envPath)

	// Original credentials with complex PEM key
	original := &AppCredentials{
		AppID:         99999,
		AppSlug:       "test-app",
		ClientID:      "Iv1.complex123",
		ClientSecret:  "super-secret-value",
		WebhookSecret: "whsec_complex",
		PrivateKey: `-----BEGIN RSA PRIVATE KEY-----
MIIEowIBAAKCAQEA0Z3VS5JJcds3xfn/ygWyF8PbnGy0AHB7MhgHW1FZ
+multiline+content+here
-----END RSA PRIVATE KEY-----
`,
		HTMLURL: "https://github.com/apps/test-app",
		CustomFields: map[string]string{
			"CUSTOM_DOMAIN": "custom.example.com",
			"ANOTHER_FIELD": "another-value",
		},
	}

	// Save
	if err := store.Save(context.Background(), original); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Verify via Status
	status, err := store.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}

	if !status.Registered {
		t.Error("Status.Registered = false after Save()")
	}
	if status.AppID != original.AppID {
		t.Errorf("Status.AppID = %d, want %d", status.AppID, original.AppID)
	}
	if status.AppSlug != original.AppSlug {
		t.Errorf("Status.AppSlug = %q, want %q", status.AppSlug, original.AppSlug)
	}
	if status.HTMLURL != original.HTMLURL {
		t.Errorf("Status.HTMLURL = %q, want %q", status.HTMLURL, original.HTMLURL)
	}

	// Also verify custom fields were saved
	values, _, err := parseEnvFile(envPath)
	if err != nil {
		t.Fatalf("parseEnvFile() error = %v", err)
	}
	if values["CUSTOM_DOMAIN"] != "custom.example.com" {
		t.Error("Custom field CUSTOM_DOMAIN was not saved")
	}
	if values["ANOTHER_FIELD"] != "another-value" {
		t.Error("Custom field ANOTHER_FIELD was not saved")
	}
}
