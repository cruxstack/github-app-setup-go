// Copyright 2025 CruxStack
// SPDX-License-Identifier: MIT

//go:build integration

package integration

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// RequestRecord captures details of an HTTP request made to the mock server.
type RequestRecord struct {
	Timestamp time.Time
	Method    string
	Path      string
	Query     string
	Headers   http.Header
	Body      string
}

// MockResponse defines a canned HTTP response for matching requests.
type MockResponse struct {
	Method     string            `yaml:"method"`
	Path       string            `yaml:"path"`
	StatusCode int               `yaml:"status"`
	Headers    map[string]string `yaml:"headers,omitempty"`
	Body       string            `yaml:"body"`
}

// MockGitHubServer simulates the GitHub API for integration testing.
type MockGitHubServer struct {
	mu        sync.Mutex
	requests  []RequestRecord
	responses map[string]MockResponse
	verbose   bool
}

// NewMockGitHubServer creates a new mock GitHub API server.
func NewMockGitHubServer(responses []MockResponse, verbose bool) *MockGitHubServer {
	respMap := make(map[string]MockResponse)
	for _, r := range responses {
		key := fmt.Sprintf("%s:%s", r.Method, r.Path)
		respMap[key] = r
	}
	return &MockGitHubServer{
		requests:  make([]RequestRecord, 0),
		responses: respMap,
		verbose:   verbose,
	}
}

// ServeHTTP implements http.Handler.
func (m *MockGitHubServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	r.Body.Close()

	rec := RequestRecord{
		Timestamp: time.Now(),
		Method:    r.Method,
		Path:      r.URL.Path,
		Query:     r.URL.RawQuery,
		Headers:   r.Header.Clone(),
		Body:      string(body),
	}

	m.mu.Lock()
	m.requests = append(m.requests, rec)
	m.mu.Unlock()

	if m.verbose {
		fmt.Printf("  [mock-github] %s %s\n", r.Method, r.URL.Path)
	}

	// Try exact match first
	key := fmt.Sprintf("%s:%s", r.Method, r.URL.Path)
	if resp, ok := m.responses[key]; ok {
		m.writeResponse(w, resp)
		return
	}

	// Try wildcard matching
	for respKey, resp := range m.responses {
		parts := strings.SplitN(respKey, ":", 2)
		if len(parts) == 2 {
			method, pattern := parts[0], parts[1]
			if method == r.Method && matchPath(r.URL.Path, pattern) {
				m.writeResponse(w, resp)
				return
			}
		}
	}

	// No match found
	if m.verbose {
		fmt.Printf("  [mock-github] no mock response for: %s %s\n", r.Method, r.URL.Path)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	w.Write([]byte(`{"message":"Not Found"}`))
}

func (m *MockGitHubServer) writeResponse(w http.ResponseWriter, resp MockResponse) {
	for k, v := range resp.Headers {
		w.Header().Set(k, v)
	}
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(resp.StatusCode)
	w.Write([]byte(resp.Body))
}

// GetRequests returns all captured requests.
func (m *MockGitHubServer) GetRequests() []RequestRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	reqs := make([]RequestRecord, len(m.requests))
	copy(reqs, m.requests)
	return reqs
}

// Reset clears all recorded requests.
func (m *MockGitHubServer) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests = make([]RequestRecord, 0)
}

// matchPath checks if a path matches a pattern with wildcard support.
func matchPath(path, pattern string) bool {
	pathParts := strings.Split(strings.Trim(path, "/"), "/")
	patternParts := strings.Split(strings.Trim(pattern, "/"), "/")

	if len(pathParts) != len(patternParts) {
		return false
	}

	for i, patternPart := range patternParts {
		if patternPart == "*" {
			continue
		}
		if pathParts[i] != patternPart {
			return false
		}
	}

	return true
}
