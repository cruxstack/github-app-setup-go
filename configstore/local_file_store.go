// Copyright 2025 CruxStack
// SPDX-License-Identifier: MIT

package configstore

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// LocalFileStore saves credentials as individual files in a directory.
type LocalFileStore struct {
	Dir string
}

// NewLocalFileStore creates a store that saves credentials as files in dir.
func NewLocalFileStore(dir string) *LocalFileStore {
	return &LocalFileStore{Dir: dir}
}

// Save writes credentials to individual files in the store directory.
func (s *LocalFileStore) Save(ctx context.Context, creds *AppCredentials) error {
	if err := os.MkdirAll(s.Dir, 0700); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", s.Dir, err)
	}

	files := map[string]struct {
		content string
		mode    os.FileMode
	}{
		"app-id":          {content: fmt.Sprintf("%d", creds.AppID), mode: 0644},
		"private-key.pem": {content: creds.PrivateKey, mode: 0600},
		"webhook-secret":  {content: creds.WebhookSecret, mode: 0600},
		"client-id":       {content: creds.ClientID, mode: 0644},
		"client-secret":   {content: creds.ClientSecret, mode: 0600},
	}

	if creds.AppSlug != "" {
		files["app-slug"] = struct {
			content string
			mode    os.FileMode
		}{content: creds.AppSlug, mode: 0644}
	}
	if creds.HTMLURL != "" {
		files["app-html-url"] = struct {
			content string
			mode    os.FileMode
		}{content: creds.HTMLURL, mode: 0644}
	}

	for key, value := range creds.CustomFields {
		if value != "" {
			filename := strings.ToLower(strings.ReplaceAll(key, "_", "-"))
			files[filename] = struct {
				content string
				mode    os.FileMode
			}{content: value, mode: 0644}
		}
	}

	for name, file := range files {
		path := filepath.Join(s.Dir, name)
		if err := os.WriteFile(path, []byte(file.content), file.mode); err != nil {
			return fmt.Errorf("failed to write %s: %w", path, err)
		}
	}

	return nil
}

// Status returns the current registration state by checking required files.
func (s *LocalFileStore) Status(ctx context.Context) (*InstallerStatus, error) {
	status := &InstallerStatus{}

	appIDBytes, err := os.ReadFile(filepath.Join(s.Dir, "app-id"))
	if err != nil {
		if os.IsNotExist(err) {
			return status, nil
		}
		return nil, err
	}

	if id, err := strconv.ParseInt(strings.TrimSpace(string(appIDBytes)), 10, 64); err == nil {
		status.AppID = id
	}

	required := []string{"client-id", "client-secret", "webhook-secret", "private-key.pem"}
	for _, name := range required {
		if _, err := os.Stat(filepath.Join(s.Dir, name)); err != nil {
			if os.IsNotExist(err) {
				return status, nil
			}
			return nil, err
		}
	}
	status.Registered = true

	if slug, err := readTrimmedFile(filepath.Join(s.Dir, "app-slug")); err == nil {
		status.AppSlug = slug
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	if html, err := readTrimmedFile(filepath.Join(s.Dir, "app-html-url")); err == nil {
		status.HTMLURL = html
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	if _, err := os.Stat(filepath.Join(s.Dir, "installer-disabled")); err == nil {
		status.InstallerDisabled = true
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	return status, nil
}

// DisableInstaller creates a marker file to disable the installer.
func (s *LocalFileStore) DisableInstaller(ctx context.Context) error {
	if err := os.MkdirAll(s.Dir, 0700); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", s.Dir, err)
	}

	path := filepath.Join(s.Dir, "installer-disabled")
	if err := os.WriteFile(path, []byte("disabled"), 0600); err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}

	return nil
}

func readTrimmedFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}
