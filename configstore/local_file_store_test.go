// Copyright 2025 CruxStack
// SPDX-License-Identifier: MIT

package configstore

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalFileStore_Save(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewLocalFileStore(tmpDir)

	creds := &AppCredentials{
		AppID:         12345,
		AppSlug:       "my-app",
		ClientID:      "Iv1.abc123",
		ClientSecret:  "secret123",
		WebhookSecret: "whsec_123",
		PrivateKey:    "-----BEGIN RSA PRIVATE KEY-----\nMIIE...\n-----END RSA PRIVATE KEY-----\n",
		HTMLURL:       "https://github.com/apps/my-app",
		CustomFields: map[string]string{
			"STS_DOMAIN":   "sts.example.com",
			"CUSTOM_VALUE": "custom123",
		},
	}

	err := store.Save(context.Background(), creds)
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Verify core files exist with correct content
	checks := map[string]struct {
		content string
		mode    os.FileMode
	}{
		"app-id":          {content: "12345", mode: 0644},
		"app-slug":        {content: "my-app", mode: 0644},
		"client-id":       {content: "Iv1.abc123", mode: 0644},
		"client-secret":   {content: "secret123", mode: 0600},
		"webhook-secret":  {content: "whsec_123", mode: 0600},
		"private-key.pem": {content: "-----BEGIN RSA PRIVATE KEY-----\nMIIE...\n-----END RSA PRIVATE KEY-----\n", mode: 0600},
		"app-html-url":    {content: "https://github.com/apps/my-app", mode: 0644},
		"sts-domain":      {content: "sts.example.com", mode: 0644},
		"custom-value":    {content: "custom123", mode: 0644},
	}

	for name, want := range checks {
		path := filepath.Join(tmpDir, name)

		// Check file exists
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("File %q not created: %v", name, err)
			continue
		}

		// Check permissions
		if info.Mode().Perm() != want.mode {
			t.Errorf("File %q permissions = %o, want %o", name, info.Mode().Perm(), want.mode)
		}

		// Check content
		content, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("Failed to read %q: %v", name, err)
			continue
		}
		if string(content) != want.content {
			t.Errorf("File %q content = %q, want %q", name, string(content), want.content)
		}
	}
}

func TestLocalFileStore_Save_CreatesDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	nestedDir := filepath.Join(tmpDir, "nested", "config", "dir")

	store := NewLocalFileStore(nestedDir)
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

	// Directory should be created with 0700 permissions
	info, err := os.Stat(nestedDir)
	if err != nil {
		t.Fatalf("Directory not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("Expected directory, got file")
	}
	if info.Mode().Perm() != 0700 {
		t.Errorf("Directory permissions = %o, want 0700", info.Mode().Perm())
	}
}

func TestLocalFileStore_Save_OmitsEmptyOptionalFields(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewLocalFileStore(tmpDir)

	creds := &AppCredentials{
		AppID:         1,
		ClientID:      "client",
		ClientSecret:  "secret",
		WebhookSecret: "webhook",
		PrivateKey:    "key",
		// AppSlug and HTMLURL are empty
	}

	err := store.Save(context.Background(), creds)
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Optional files should not exist
	for _, name := range []string{"app-slug", "app-html-url"} {
		path := filepath.Join(tmpDir, name)
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("File %q should not exist for empty value", name)
		}
	}
}

func TestLocalFileStore_Save_SkipsEmptyCustomFields(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewLocalFileStore(tmpDir)

	creds := &AppCredentials{
		AppID:         1,
		ClientID:      "client",
		ClientSecret:  "secret",
		WebhookSecret: "webhook",
		PrivateKey:    "key",
		CustomFields: map[string]string{
			"EMPTY_FIELD": "",
			"VALID_FIELD": "value",
		},
	}

	err := store.Save(context.Background(), creds)
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Empty custom field should not be created
	if _, err := os.Stat(filepath.Join(tmpDir, "empty-field")); !os.IsNotExist(err) {
		t.Error("Empty custom field should not create a file")
	}

	// Valid custom field should exist
	if _, err := os.Stat(filepath.Join(tmpDir, "valid-field")); err != nil {
		t.Errorf("Valid custom field not created: %v", err)
	}
}

func TestLocalFileStore_Status_Registered(t *testing.T) {
	tmpDir := t.TempDir()

	// Create all required files
	files := map[string]string{
		"app-id":          "12345",
		"app-slug":        "test-app",
		"app-html-url":    "https://github.com/apps/test-app",
		"client-id":       "client123",
		"client-secret":   "secret123",
		"webhook-secret":  "webhook123",
		"private-key.pem": "-----BEGIN RSA-----\n...\n-----END RSA-----",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(tmpDir, name), []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create %s: %v", name, err)
		}
	}

	store := NewLocalFileStore(tmpDir)
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
	if status.AppSlug != "test-app" {
		t.Errorf("Status.AppSlug = %q, want %q", status.AppSlug, "test-app")
	}
	if status.HTMLURL != "https://github.com/apps/test-app" {
		t.Errorf("Status.HTMLURL = %q, want %q", status.HTMLURL, "https://github.com/apps/test-app")
	}
	if status.InstallerDisabled {
		t.Error("Status.InstallerDisabled = true, want false")
	}
}

func TestLocalFileStore_Status_NotRegistered_MissingAppID(t *testing.T) {
	tmpDir := t.TempDir()

	// Directory exists but no app-id file
	store := NewLocalFileStore(tmpDir)
	status, err := store.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}

	if status.Registered {
		t.Error("Status.Registered = true, want false (no app-id)")
	}
}

func TestLocalFileStore_Status_NotRegistered_MissingRequiredFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Create app-id but missing required files
	if err := os.WriteFile(filepath.Join(tmpDir, "app-id"), []byte("12345"), 0644); err != nil {
		t.Fatalf("Failed to create app-id: %v", err)
	}

	store := NewLocalFileStore(tmpDir)
	status, err := store.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}

	if status.Registered {
		t.Error("Status.Registered = true, want false (missing required files)")
	}
}

func TestLocalFileStore_Status_InstallerDisabled(t *testing.T) {
	tmpDir := t.TempDir()

	// Create all required files plus installer-disabled marker
	files := map[string]string{
		"app-id":             "12345",
		"client-id":          "client",
		"client-secret":      "secret",
		"webhook-secret":     "webhook",
		"private-key.pem":    "key",
		"installer-disabled": "disabled",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(tmpDir, name), []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create %s: %v", name, err)
		}
	}

	store := NewLocalFileStore(tmpDir)
	status, err := store.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}

	if !status.InstallerDisabled {
		t.Error("Status.InstallerDisabled = false, want true")
	}
}

func TestLocalFileStore_Status_DirectoryNotExists(t *testing.T) {
	store := NewLocalFileStore("/nonexistent/directory")
	status, err := store.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v, want nil for nonexistent directory", err)
	}

	if status.Registered {
		t.Error("Status.Registered = true, want false for nonexistent directory")
	}
}

func TestLocalFileStore_Status_WhitespaceInAppID(t *testing.T) {
	tmpDir := t.TempDir()

	// Create app-id with whitespace and newline
	files := map[string]string{
		"app-id":          "  12345\n",
		"client-id":       "client",
		"client-secret":   "secret",
		"webhook-secret":  "webhook",
		"private-key.pem": "key",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(tmpDir, name), []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create %s: %v", name, err)
		}
	}

	store := NewLocalFileStore(tmpDir)
	status, err := store.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}

	if status.AppID != 12345 {
		t.Errorf("Status.AppID = %d, want 12345 (whitespace should be trimmed)", status.AppID)
	}
}

func TestLocalFileStore_DisableInstaller(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewLocalFileStore(tmpDir)

	err := store.DisableInstaller(context.Background())
	if err != nil {
		t.Fatalf("DisableInstaller() error = %v", err)
	}

	// Check marker file exists
	markerPath := filepath.Join(tmpDir, "installer-disabled")
	info, err := os.Stat(markerPath)
	if err != nil {
		t.Fatalf("Marker file not created: %v", err)
	}

	// Check permissions (should be 0600 for security)
	if info.Mode().Perm() != 0600 {
		t.Errorf("Marker file permissions = %o, want 0600", info.Mode().Perm())
	}
}

func TestLocalFileStore_DisableInstaller_CreatesDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	nestedDir := filepath.Join(tmpDir, "nested", "dir")
	store := NewLocalFileStore(nestedDir)

	err := store.DisableInstaller(context.Background())
	if err != nil {
		t.Fatalf("DisableInstaller() error = %v", err)
	}

	// Directory should be created
	if _, err := os.Stat(nestedDir); err != nil {
		t.Errorf("Directory not created: %v", err)
	}

	// Marker file should exist
	if _, err := os.Stat(filepath.Join(nestedDir, "installer-disabled")); err != nil {
		t.Errorf("Marker file not created: %v", err)
	}
}

func TestLocalFileStore_RoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewLocalFileStore(tmpDir)

	original := &AppCredentials{
		AppID:         99999,
		AppSlug:       "roundtrip-app",
		ClientID:      "Iv1.roundtrip",
		ClientSecret:  "roundtrip-secret",
		WebhookSecret: "whsec_roundtrip",
		PrivateKey:    "-----BEGIN RSA PRIVATE KEY-----\nroundtrip-key-content\n-----END RSA PRIVATE KEY-----\n",
		HTMLURL:       "https://github.com/apps/roundtrip-app",
		CustomFields: map[string]string{
			"CUSTOM_FIELD": "custom-value",
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

	// Verify custom field was saved
	content, err := os.ReadFile(filepath.Join(tmpDir, "custom-field"))
	if err != nil {
		t.Fatalf("Custom field file not found: %v", err)
	}
	if string(content) != "custom-value" {
		t.Errorf("Custom field content = %q, want %q", string(content), "custom-value")
	}
}

func TestReadTrimmedFile(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name    string
		content string
		want    string
	}{
		{"no whitespace", "value", "value"},
		{"trailing newline", "value\n", "value"},
		{"leading and trailing spaces", "  value  ", "value"},
		{"mixed whitespace", "\t value \n", "value"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(tmpDir, "test-"+tt.name)
			if err := os.WriteFile(path, []byte(tt.content), 0644); err != nil {
				t.Fatalf("Failed to write file: %v", err)
			}

			got, err := readTrimmedFile(path)
			if err != nil {
				t.Fatalf("readTrimmedFile() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("readTrimmedFile() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestReadTrimmedFile_NotExists(t *testing.T) {
	_, err := readTrimmedFile("/nonexistent/file")
	if !os.IsNotExist(err) {
		t.Errorf("readTrimmedFile() error = %v, want os.IsNotExist", err)
	}
}
