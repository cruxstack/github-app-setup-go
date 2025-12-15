// Copyright 2025 CruxStack
// SPDX-License-Identifier: MIT

// Package configstore provides storage backends for GitHub App credentials.
// It supports multiple storage backends including AWS SSM Parameter Store,
// local .env files, and individual files.
package configstore

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

const (
	EnvGitHubAppID         = "GITHUB_APP_ID"
	EnvGitHubAppSlug       = "GITHUB_APP_SLUG"
	EnvGitHubAppHTMLURL    = "GITHUB_APP_HTML_URL"
	EnvGitHubAppPrivateKey = "GITHUB_APP_PRIVATE_KEY"
	EnvGitHubWebhookSecret = "GITHUB_WEBHOOK_SECRET"
	EnvGitHubClientID      = "GITHUB_CLIENT_ID"
	EnvGitHubClientSecret  = "GITHUB_CLIENT_SECRET"
)

const (
	EnvGitHubAppInstallerEnabled = "GITHUB_APP_INSTALLER_ENABLED"
	EnvStorageMode               = "STORAGE_MODE"
	EnvStorageDir                = "STORAGE_DIR"
	EnvAWSSSMParameterPfx        = "AWS_SSM_PARAMETER_PREFIX"
	EnvAWSSSMKMSKeyID            = "AWS_SSM_KMS_KEY_ID"
	EnvAWSSSMTags                = "AWS_SSM_TAGS"
)

// Storage mode constants for STORAGE_MODE environment variable.
const (
	// StorageModeEnvFile saves credentials to a .env file (default mode).
	StorageModeEnvFile = "envfile"
	// StorageModeFiles saves credentials as individual files in a directory.
	StorageModeFiles = "files"
	// StorageModeAWSSSM saves credentials to AWS SSM Parameter Store.
	StorageModeAWSSSM = "aws-ssm"
)

// HookConfig contains webhook configuration returned from GitHub.
type HookConfig struct {
	URL string `json:"url"`
}

// AppCredentials holds credentials returned from GitHub App manifest creation.
type AppCredentials struct {
	AppID         int64      `json:"id"`
	AppSlug       string     `json:"slug"`
	ClientID      string     `json:"client_id"`
	ClientSecret  string     `json:"client_secret"`
	WebhookSecret string     `json:"webhook_secret"`
	PrivateKey    string     `json:"pem"`
	HTMLURL       string     `json:"html_url"`
	HookConfig    HookConfig `json:"hook_config"`

	// CustomFields stores additional app-specific values alongside credentials.
	CustomFields map[string]string `json:"-"`
}

// InstallerStatus describes the current GitHub App registration state.
type InstallerStatus struct {
	Registered        bool
	InstallerDisabled bool
	AppID             int64
	AppSlug           string
	HTMLURL           string
}

// Store saves app credentials to various backends (local disk, AWS SSM, etc).
type Store interface {
	Save(ctx context.Context, creds *AppCredentials) error
	Status(ctx context.Context) (*InstallerStatus, error)
	DisableInstaller(ctx context.Context) error
}

// NewFromEnv creates a Store based on environment variable configuration.
// It reads STORAGE_MODE to determine the backend type:
//   - "envfile" (default): saves to a .env file at STORAGE_DIR (default: ./.env)
//   - "files": saves to individual files in STORAGE_DIR directory
//   - "aws-ssm": saves to AWS SSM Parameter Store with AWS_SSM_PARAMETER_PREFIX
//
// Returns an error if configuration is invalid or store creation fails.
func NewFromEnv() (Store, error) {
	mode := GetEnvDefault(EnvStorageMode, StorageModeEnvFile)

	switch mode {
	case StorageModeFiles:
		dir := GetEnvDefault(EnvStorageDir, "./.env")
		return NewLocalFileStore(dir), nil

	case StorageModeEnvFile:
		path := GetEnvDefault(EnvStorageDir, "./.env")
		return NewLocalEnvFileStore(path), nil

	case StorageModeAWSSSM:
		prefix := os.Getenv(EnvAWSSSMParameterPfx)
		if prefix == "" {
			return nil, fmt.Errorf("%s is required when using %s storage mode", EnvAWSSSMParameterPfx, StorageModeAWSSSM)
		}

		var opts []SSMStoreOption

		if kmsKeyID := os.Getenv(EnvAWSSSMKMSKeyID); kmsKeyID != "" {
			opts = append(opts, WithKMSKey(kmsKeyID))
		}

		if tagsJSON := os.Getenv(EnvAWSSSMTags); tagsJSON != "" {
			var tags map[string]string
			if err := json.Unmarshal([]byte(tagsJSON), &tags); err != nil {
				return nil, fmt.Errorf("failed to parse %s as JSON: %w", EnvAWSSSMTags, err)
			}
			opts = append(opts, WithTags(tags))
		}

		return NewAWSSSMStore(prefix, opts...)

	default:
		return nil, fmt.Errorf("unknown %s: %s (expected '%s', '%s', or '%s')",
			EnvStorageMode, mode, StorageModeEnvFile, StorageModeFiles, StorageModeAWSSSM)
	}
}

// InstallerEnabled returns true if the installer is enabled via environment variable.
func InstallerEnabled() bool {
	v := strings.ToLower(os.Getenv(EnvGitHubAppInstallerEnabled))
	return v == "true" || v == "1" || v == "yes"
}

// GetEnvDefault returns an env var value, or defaultValue if not set or empty.
func GetEnvDefault(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}

func hasAllValues(values map[string]string, keys ...string) bool {
	if len(values) == 0 {
		return false
	}
	for _, key := range keys {
		if strings.TrimSpace(values[key]) == "" {
			return false
		}
	}
	return true
}

func isFalseString(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "false", "0", "no", "off":
		return true
	default:
		return false
	}
}
