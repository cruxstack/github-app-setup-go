// Copyright 2025 CruxStack
// SPDX-License-Identifier: MIT

package configstore

import (
	"os"
	"testing"
)

func TestInstallerEnabled(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{"true lowercase", "true", true},
		{"TRUE uppercase", "TRUE", true},
		{"True mixed", "True", true},
		{"1", "1", true},
		{"yes", "yes", true},
		{"YES uppercase", "YES", true},
		{"false", "false", false},
		{"FALSE", "FALSE", false},
		{"0", "0", false},
		{"no", "no", false},
		{"empty string", "", false},
		{"random string", "enabled", false},
		{"on (not supported)", "on", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Setenv(EnvGitHubAppInstallerEnabled, tt.value)
			defer os.Unsetenv(EnvGitHubAppInstallerEnabled)

			got := InstallerEnabled()
			if got != tt.want {
				t.Errorf("InstallerEnabled() with %q = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestHasAllValues(t *testing.T) {
	tests := []struct {
		name   string
		values map[string]string
		keys   []string
		want   bool
	}{
		{
			name:   "all keys present with values",
			values: map[string]string{"a": "1", "b": "2", "c": "3"},
			keys:   []string{"a", "b"},
			want:   true,
		},
		{
			name:   "missing key",
			values: map[string]string{"a": "1"},
			keys:   []string{"a", "b"},
			want:   false,
		},
		{
			name:   "empty value fails",
			values: map[string]string{"a": "1", "b": ""},
			keys:   []string{"a", "b"},
			want:   false,
		},
		{
			name:   "whitespace-only value fails",
			values: map[string]string{"a": "1", "b": "   "},
			keys:   []string{"a", "b"},
			want:   false,
		},
		{
			name:   "nil map",
			values: nil,
			keys:   []string{"a"},
			want:   false,
		},
		{
			name:   "empty map",
			values: map[string]string{},
			keys:   []string{"a"},
			want:   false,
		},
		{
			name:   "no keys required",
			values: map[string]string{"a": "1"},
			keys:   []string{},
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasAllValues(tt.values, tt.keys...)
			if got != tt.want {
				t.Errorf("hasAllValues() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsFalseString(t *testing.T) {
	tests := []struct {
		value string
		want  bool
	}{
		{"false", true},
		{"FALSE", true},
		{"False", true},
		{"0", true},
		{"no", true},
		{"NO", true},
		{"off", true},
		{"OFF", true},
		{"  false  ", true}, // with whitespace
		{"true", false},
		{"1", false},
		{"yes", false},
		{"on", false},
		{"", false},
		{"random", false},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			got := isFalseString(tt.value)
			if got != tt.want {
				t.Errorf("isFalseString(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestGetEnvDefault(t *testing.T) {
	tests := []struct {
		name         string
		envKey       string
		envValue     string
		setEnv       bool
		defaultValue string
		want         string
	}{
		{
			name:         "env set returns env value",
			envKey:       "TEST_VAR",
			envValue:     "custom_value",
			setEnv:       true,
			defaultValue: "default",
			want:         "custom_value",
		},
		{
			name:         "env not set returns default",
			envKey:       "TEST_VAR_UNSET",
			envValue:     "",
			setEnv:       false,
			defaultValue: "default_value",
			want:         "default_value",
		},
		{
			name:         "empty env returns default",
			envKey:       "TEST_VAR_EMPTY",
			envValue:     "",
			setEnv:       true,
			defaultValue: "default",
			want:         "default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Unsetenv(tt.envKey)
			if tt.setEnv {
				os.Setenv(tt.envKey, tt.envValue)
				defer os.Unsetenv(tt.envKey)
			}

			got := GetEnvDefault(tt.envKey, tt.defaultValue)
			if got != tt.want {
				t.Errorf("GetEnvDefault(%q, %q) = %q, want %q", tt.envKey, tt.defaultValue, got, tt.want)
			}
		})
	}
}

func TestNewFromEnv_StorageModes(t *testing.T) {
	t.Run("default mode creates LocalEnvFileStore", func(t *testing.T) {
		os.Unsetenv(EnvStorageMode)
		defer os.Unsetenv(EnvStorageMode)

		store, err := NewFromEnv()
		if err != nil {
			t.Fatalf("NewFromEnv() error = %v", err)
		}

		if _, ok := store.(*LocalEnvFileStore); !ok {
			t.Errorf("NewFromEnv() returned %T, want *LocalEnvFileStore", store)
		}
	})

	t.Run("envfile mode creates LocalEnvFileStore", func(t *testing.T) {
		os.Setenv(EnvStorageMode, StorageModeEnvFile)
		defer os.Unsetenv(EnvStorageMode)

		store, err := NewFromEnv()
		if err != nil {
			t.Fatalf("NewFromEnv() error = %v", err)
		}

		if _, ok := store.(*LocalEnvFileStore); !ok {
			t.Errorf("NewFromEnv() returned %T, want *LocalEnvFileStore", store)
		}
	})

	t.Run("files mode creates LocalFileStore", func(t *testing.T) {
		os.Setenv(EnvStorageMode, StorageModeFiles)
		defer os.Unsetenv(EnvStorageMode)

		store, err := NewFromEnv()
		if err != nil {
			t.Fatalf("NewFromEnv() error = %v", err)
		}

		if _, ok := store.(*LocalFileStore); !ok {
			t.Errorf("NewFromEnv() returned %T, want *LocalFileStore", store)
		}
	})

	t.Run("aws-ssm mode requires prefix", func(t *testing.T) {
		os.Setenv(EnvStorageMode, StorageModeAWSSSM)
		os.Unsetenv(EnvAWSSSMParameterPfx)
		defer os.Unsetenv(EnvStorageMode)

		_, err := NewFromEnv()
		if err == nil {
			t.Error("NewFromEnv() with aws-ssm and no prefix should return error")
		}
	})

	t.Run("unknown mode returns error", func(t *testing.T) {
		os.Setenv(EnvStorageMode, "invalid-mode")
		defer os.Unsetenv(EnvStorageMode)

		_, err := NewFromEnv()
		if err == nil {
			t.Error("NewFromEnv() with unknown mode should return error")
		}
	})
}

func TestNewFromEnv_CustomStorageDir(t *testing.T) {
	t.Run("envfile mode uses STORAGE_DIR", func(t *testing.T) {
		os.Setenv(EnvStorageMode, StorageModeEnvFile)
		os.Setenv(EnvStorageDir, "/custom/path/.env")
		defer os.Unsetenv(EnvStorageMode)
		defer os.Unsetenv(EnvStorageDir)

		store, err := NewFromEnv()
		if err != nil {
			t.Fatalf("NewFromEnv() error = %v", err)
		}

		envStore, ok := store.(*LocalEnvFileStore)
		if !ok {
			t.Fatalf("NewFromEnv() returned %T, want *LocalEnvFileStore", store)
		}

		if envStore.FilePath != "/custom/path/.env" {
			t.Errorf("FilePath = %q, want %q", envStore.FilePath, "/custom/path/.env")
		}
	})

	t.Run("files mode uses STORAGE_DIR", func(t *testing.T) {
		os.Setenv(EnvStorageMode, StorageModeFiles)
		os.Setenv(EnvStorageDir, "/custom/dir")
		defer os.Unsetenv(EnvStorageMode)
		defer os.Unsetenv(EnvStorageDir)

		store, err := NewFromEnv()
		if err != nil {
			t.Fatalf("NewFromEnv() error = %v", err)
		}

		fileStore, ok := store.(*LocalFileStore)
		if !ok {
			t.Fatalf("NewFromEnv() returned %T, want *LocalFileStore", store)
		}

		if fileStore.Dir != "/custom/dir" {
			t.Errorf("Dir = %q, want %q", fileStore.Dir, "/custom/dir")
		}
	})
}

func TestNewFromEnv_AWSSSMTags(t *testing.T) {
	t.Run("invalid JSON tags returns error", func(t *testing.T) {
		os.Setenv(EnvStorageMode, StorageModeAWSSSM)
		os.Setenv(EnvAWSSSMParameterPfx, "/test/prefix")
		os.Setenv(EnvAWSSSMTags, "not valid json")
		defer os.Unsetenv(EnvStorageMode)
		defer os.Unsetenv(EnvAWSSSMParameterPfx)
		defer os.Unsetenv(EnvAWSSSMTags)

		_, err := NewFromEnv()
		if err == nil {
			t.Error("NewFromEnv() with invalid JSON tags should return error")
		}
	})
}
