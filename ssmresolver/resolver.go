// Copyright 2025 CruxStack
// SPDX-License-Identifier: MIT

// Package ssmresolver provides utilities for resolving AWS SSM Parameter Store
// ARNs in environment variables.
package ssmresolver

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/chainguard-dev/clog"
)

const (
	EnvMaxRetries    = "CONFIG_WAIT_MAX_RETRIES"
	EnvRetryInterval = "CONFIG_WAIT_RETRY_INTERVAL"
)

const (
	DefaultMaxRetries    = 5
	DefaultRetryInterval = 1 * time.Second
)

var ssmARNPattern = regexp.MustCompile(`^arn:aws:ssm:[^:]+:[^:]+:parameter/(.+)$`)

// Client defines the interface for SSM operations.
type Client interface {
	GetParameter(ctx context.Context, params *ssm.GetParameterInput, optFns ...func(*ssm.Options)) (*ssm.GetParameterOutput, error)
}

// Resolver handles SSM parameter resolution.
type Resolver struct {
	client Client
}

// New creates a Resolver with the default AWS configuration.
func New(ctx context.Context) (*Resolver, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}
	return &Resolver{
		client: ssm.NewFromConfig(cfg),
	}, nil
}

// NewWithClient creates a Resolver with a custom SSM client.
func NewWithClient(client Client) *Resolver {
	return &Resolver{client: client}
}

// IsSSMARN checks if the given value is an SSM Parameter Store ARN.
func IsSSMARN(value string) bool {
	return ssmARNPattern.MatchString(value)
}

// ExtractParameterName extracts the parameter name from an SSM ARN.
func ExtractParameterName(arn string) (string, bool) {
	matches := ssmARNPattern.FindStringSubmatch(arn)
	if len(matches) != 2 {
		return "", false
	}
	paramName := matches[1]
	if !strings.HasPrefix(paramName, "/") {
		paramName = "/" + paramName
	}
	return paramName, true
}

// ResolveValue resolves an SSM ARN to its value, or returns it unchanged.
func (r *Resolver) ResolveValue(ctx context.Context, value string) (string, error) {
	if !IsSSMARN(value) {
		return value, nil
	}

	paramName, ok := ExtractParameterName(value)
	if !ok {
		return "", fmt.Errorf("invalid SSM ARN format: %s", value)
	}

	resp, err := r.client.GetParameter(ctx, &ssm.GetParameterInput{
		Name:           &paramName,
		WithDecryption: ptr(true),
	})
	if err != nil {
		return "", fmt.Errorf("failed to get SSM parameter %s: %w", paramName, err)
	}

	if resp.Parameter == nil || resp.Parameter.Value == nil {
		return "", fmt.Errorf("SSM parameter %s has no value", paramName)
	}

	return *resp.Parameter.Value, nil
}

// ResolveEnvironment resolves any SSM ARN values in environment variables.
func (r *Resolver) ResolveEnvironment(ctx context.Context) error {
	for _, env := range os.Environ() {
		parts := strings.SplitN(env, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key, value := parts[0], parts[1]

		if IsSSMARN(value) {
			resolved, err := r.ResolveValue(ctx, value)
			if err != nil {
				return fmt.Errorf("failed to resolve %s: %w", key, err)
			}
			if err := os.Setenv(key, resolved); err != nil {
				return fmt.Errorf("failed to set %s: %w", key, err)
			}
		}
	}
	return nil
}

// ResolveEnvironmentWithDefaults creates a resolver and resolves all env vars.
func ResolveEnvironmentWithDefaults(ctx context.Context) error {
	resolver, err := New(ctx)
	if err != nil {
		return err
	}
	return resolver.ResolveEnvironment(ctx)
}

// RetryConfig configures retry behavior for SSM resolution.
type RetryConfig struct {
	MaxRetries    int
	RetryInterval time.Duration
}

// NewRetryConfigFromEnv creates a RetryConfig from environment variables.
func NewRetryConfigFromEnv() RetryConfig {
	cfg := RetryConfig{
		MaxRetries:    DefaultMaxRetries,
		RetryInterval: DefaultRetryInterval,
	}

	if v := os.Getenv(EnvMaxRetries); v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil && n > 0 {
			cfg.MaxRetries = n
		}
	}

	if v := os.Getenv(EnvRetryInterval); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			cfg.RetryInterval = d
		}
	}

	return cfg
}

// ResolveEnvironmentWithRetry resolves all environment variables with retry logic.
func ResolveEnvironmentWithRetry(ctx context.Context, cfg RetryConfig) error {
	log := clog.FromContext(ctx)
	var lastErr error

	for attempt := 1; attempt <= cfg.MaxRetries; attempt++ {
		err := ResolveEnvironmentWithDefaults(ctx)
		if err == nil {
			if attempt > 1 {
				log.Infof("[ssmresolver] SSM parameters resolved successfully after %d attempts", attempt)
			}
			return nil
		}

		lastErr = err
		log.Warnf("[ssmresolver] attempt %d/%d failed: %v", attempt, cfg.MaxRetries, err)

		if attempt < cfg.MaxRetries {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(cfg.RetryInterval):
			}
		}
	}

	return lastErr
}

func ptr[T any](v T) *T {
	return &v
}
