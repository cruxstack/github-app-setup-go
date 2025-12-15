// Copyright 2025 CruxStack
// SPDX-License-Identifier: MIT

//go:build integration

package integration

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIntegrationScenarios(t *testing.T) {
	// Find scenarios file relative to this test file
	scenariosPath := filepath.Join("testdata", "scenarios.yaml")
	if _, err := os.Stat(scenariosPath); os.IsNotExist(err) {
		t.Fatalf("scenarios file not found: %s", scenariosPath)
	}

	scenarios, err := LoadScenarios(scenariosPath)
	if err != nil {
		t.Fatalf("load scenarios: %v", err)
	}

	if len(scenarios) == 0 {
		t.Fatal("no scenarios found in scenarios.yaml")
	}

	verbose := os.Getenv("VERBOSE") == "1" || os.Getenv("VERBOSE") == "true"
	runner := NewScenarioRunner(t, verbose)

	for _, scenario := range scenarios {
		runner.Run(scenario)
	}
}

// TestMatchPath validates the path matching logic used by the mock server.
func TestMatchPath(t *testing.T) {
	tests := []struct {
		path    string
		pattern string
		want    bool
	}{
		{"/app-manifests/abc123/conversions", "/app-manifests/*/conversions", true},
		{"/app-manifests/xyz/conversions", "/app-manifests/*/conversions", true},
		{"/repos/owner/repo/pulls/123", "/repos/*/*/pulls/*", true},
		{"/repos/owner/repo/pulls", "/repos/*/*/pulls/*", false}, // different segment count
		{"/exact/match", "/exact/match", true},
		{"/exact/mismatch", "/exact/match", false},
		{"/api/v3/app-manifests/code/conversions", "/api/v3/app-manifests/*/conversions", true},
	}

	for _, tt := range tests {
		t.Run(tt.path+"_"+tt.pattern, func(t *testing.T) {
			got := matchPath(tt.path, tt.pattern)
			if got != tt.want {
				t.Errorf("matchPath(%q, %q) = %v, want %v", tt.path, tt.pattern, got, tt.want)
			}
		})
	}
}
