// Copyright 2025 CruxStack
// SPDX-License-Identifier: MIT

package configstore

import (
	"bufio"
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// LocalEnvFileStore saves credentials to a .env file.
type LocalEnvFileStore struct {
	FilePath string
}

// NewLocalEnvFileStore creates a store that saves credentials to the given path.
func NewLocalEnvFileStore(filepath string) *LocalEnvFileStore {
	return &LocalEnvFileStore{FilePath: filepath}
}

// Save writes credentials to .env format, preserving existing content.
// It also sets the environment variables in the current process so they
// are immediately available to the application.
func (s *LocalEnvFileStore) Save(ctx context.Context, creds *AppCredentials) error {
	dir := filepath.Dir(s.FilePath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	_, originalLines, err := parseEnvFile(s.FilePath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to read existing .env file: %w", err)
	}

	updates := make(map[string]string)
	for key, value := range creds.CustomFields {
		if value != "" {
			updates[key] = value
		}
	}

	singleLinePEM := strings.ReplaceAll(creds.PrivateKey, "\n", "\\n")

	updates[EnvGitHubAppID] = fmt.Sprintf("%d", creds.AppID)
	updates[EnvGitHubWebhookSecret] = creds.WebhookSecret
	updates[EnvGitHubClientID] = creds.ClientID
	updates[EnvGitHubClientSecret] = creds.ClientSecret
	updates[EnvGitHubAppPrivateKey] = singleLinePEM
	if creds.AppSlug != "" {
		updates[EnvGitHubAppSlug] = creds.AppSlug
	}
	if creds.HTMLURL != "" {
		updates[EnvGitHubAppHTMLURL] = creds.HTMLURL
	}

	if err := writeEnvFile(s.FilePath, updates, originalLines); err != nil {
		return fmt.Errorf("failed to write .env file: %w", err)
	}

	// Set environment variables in the current process so they are
	// immediately available for configuration reload.
	os.Setenv(EnvGitHubAppID, fmt.Sprintf("%d", creds.AppID))
	os.Setenv(EnvGitHubWebhookSecret, creds.WebhookSecret)
	os.Setenv(EnvGitHubClientID, creds.ClientID)
	os.Setenv(EnvGitHubClientSecret, creds.ClientSecret)
	os.Setenv(EnvGitHubAppPrivateKey, singleLinePEM)
	if creds.AppSlug != "" {
		os.Setenv(EnvGitHubAppSlug, creds.AppSlug)
	}
	if creds.HTMLURL != "" {
		os.Setenv(EnvGitHubAppHTMLURL, creds.HTMLURL)
	}
	for key, value := range creds.CustomFields {
		if value != "" {
			os.Setenv(key, value)
		}
	}

	return nil
}

func parseEnvFile(path string) (map[string]string, []string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()

	values := make(map[string]string)
	var lines []string

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		lines = append(lines, line)

		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		idx := strings.Index(line, "=")
		if idx == -1 {
			continue
		}

		key := strings.TrimSpace(line[:idx])
		value := strings.TrimSpace(line[idx+1:])

		if len(value) >= 2 {
			if (strings.HasPrefix(value, "\"") && strings.HasSuffix(value, "\"")) ||
				(strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'")) {
				value = value[1 : len(value)-1]
			}
		}

		values[key] = value
	}

	if err := scanner.Err(); err != nil {
		return nil, nil, err
	}

	return values, lines, nil
}

// writeEnvFile rewrites only the lines whose keys appear in values and appends
// any keys not already present. All other lines are preserved verbatim so that
// quoting, inline comments, and formatting of unrelated entries are untouched.
func writeEnvFile(path string, values map[string]string, originalLines []string) error {
	var outputLines []string
	writtenKeys := make(map[string]bool)

	for _, line := range originalLines {
		trimmed := strings.TrimSpace(line)

		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			outputLines = append(outputLines, line)
			continue
		}

		idx := strings.Index(line, "=")
		if idx == -1 {
			outputLines = append(outputLines, line)
			continue
		}

		key := strings.TrimSpace(line[:idx])

		if newValue, ok := values[key]; ok {
			outputLines = append(outputLines, formatEnvLine(key, newValue))
			writtenKeys[key] = true
		} else {
			outputLines = append(outputLines, line)
		}
	}

	for _, key := range slices.Sorted(maps.Keys(values)) {
		if !writtenKeys[key] {
			outputLines = append(outputLines, formatEnvLine(key, values[key]))
		}
	}

	content := strings.Join(outputLines, "\n")
	if len(outputLines) > 0 {
		content += "\n"
	}

	return os.WriteFile(path, []byte(content), 0600)
}

func formatEnvLine(key, value string) string {
	needsQuotes := strings.ContainsAny(value, " \t\n\r\"'\\#") || strings.Contains(value, "\\n")

	if needsQuotes {
		escaped := strings.ReplaceAll(value, "\"", "\\\"")
		return fmt.Sprintf("%s=\"%s\"", key, escaped)
	}

	return fmt.Sprintf("%s=%s", key, value)
}

// Status returns the current registration state by checking the .env file.
func (s *LocalEnvFileStore) Status(ctx context.Context) (*InstallerStatus, error) {
	values, _, err := parseEnvFile(s.FilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return &InstallerStatus{}, nil
		}
		return nil, err
	}

	status := &InstallerStatus{
		AppSlug: values[EnvGitHubAppSlug],
		HTMLURL: values[EnvGitHubAppHTMLURL],
	}

	if idStr := strings.TrimSpace(values[EnvGitHubAppID]); idStr != "" {
		if id, err := strconv.ParseInt(idStr, 10, 64); err == nil {
			status.AppID = id
		}
	}

	status.Registered = hasAllValues(values,
		EnvGitHubAppID,
		EnvGitHubWebhookSecret,
		EnvGitHubClientID,
		EnvGitHubClientSecret,
		EnvGitHubAppPrivateKey,
	)

	status.InstallerDisabled = isFalseString(values[EnvGitHubAppInstallerEnabled])

	return status, nil
}

// DisableInstaller sets GITHUB_APP_INSTALLER_ENABLED=false in the .env file.
func (s *LocalEnvFileStore) DisableInstaller(ctx context.Context) error {
	dir := filepath.Dir(s.FilePath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	_, originalLines, err := parseEnvFile(s.FilePath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	updates := map[string]string{EnvGitHubAppInstallerEnabled: "false"}
	if err := writeEnvFile(s.FilePath, updates, originalLines); err != nil {
		return fmt.Errorf("failed to persist installer flag: %w", err)
	}

	return nil
}
