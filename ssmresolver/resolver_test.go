// Copyright 2025 CruxStack
// SPDX-License-Identifier: MIT

package ssmresolver

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/ssm/types"
)

func TestIsSSMARN(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		// Valid ARNs
		{
			name:  "valid ARN with simple path",
			value: "arn:aws:ssm:us-east-1:123456789012:parameter/my-param",
			want:  true,
		},
		{
			name:  "valid ARN with nested path",
			value: "arn:aws:ssm:us-west-2:111122223333:parameter/octo-sts/prod/GITHUB_APP_ID",
			want:  true,
		},
		{
			name:  "valid ARN with leading slash in path",
			value: "arn:aws:ssm:eu-west-1:999888777666:parameter//app/secret",
			want:  true,
		},

		// Invalid ARNs
		{
			name:  "empty string",
			value: "",
			want:  false,
		},
		{
			name:  "plain value",
			value: "my-secret-value",
			want:  false,
		},
		{
			name:  "wrong service",
			value: "arn:aws:s3:us-east-1:123456789012:bucket/my-bucket",
			want:  false,
		},
		{
			name:  "missing parameter prefix",
			value: "arn:aws:ssm:us-east-1:123456789012:secret/my-secret",
			want:  false,
		},
		{
			name:  "incomplete ARN",
			value: "arn:aws:ssm:us-east-1:123456789012",
			want:  false,
		},
		{
			name:  "ARN-like but malformed",
			value: "arn:aws:ssm:parameter/test",
			want:  false,
		},
		{
			name:  "URL that looks like ARN",
			value: "https://arn:aws:ssm:us-east-1:123456789012:parameter/test",
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsSSMARN(tt.value)
			if got != tt.want {
				t.Errorf("IsSSMARN(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestExtractParameterName(t *testing.T) {
	tests := []struct {
		name      string
		arn       string
		wantName  string
		wantFound bool
	}{
		{
			name:      "simple parameter name",
			arn:       "arn:aws:ssm:us-east-1:123456789012:parameter/my-param",
			wantName:  "/my-param",
			wantFound: true,
		},
		{
			name:      "nested path without leading slash",
			arn:       "arn:aws:ssm:us-west-2:111122223333:parameter/octo-sts/prod/GITHUB_APP_ID",
			wantName:  "/octo-sts/prod/GITHUB_APP_ID",
			wantFound: true,
		},
		{
			name:      "path already has leading slash is normalized",
			arn:       "arn:aws:ssm:us-east-1:123456789012:parameter//app/secret",
			wantName:  "/app/secret", // leading slash in path becomes single slash after normalization
			wantFound: true,
		},
		{
			name:      "invalid ARN returns empty",
			arn:       "not-an-arn",
			wantName:  "",
			wantFound: false,
		},
		{
			name:      "empty string",
			arn:       "",
			wantName:  "",
			wantFound: false,
		},
		{
			name:      "wrong service ARN",
			arn:       "arn:aws:s3:us-east-1:123456789012:bucket/my-bucket",
			wantName:  "",
			wantFound: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotName, gotFound := ExtractParameterName(tt.arn)
			if gotName != tt.wantName {
				t.Errorf("ExtractParameterName(%q) name = %q, want %q", tt.arn, gotName, tt.wantName)
			}
			if gotFound != tt.wantFound {
				t.Errorf("ExtractParameterName(%q) found = %v, want %v", tt.arn, gotFound, tt.wantFound)
			}
		})
	}
}

// mockSSMClient implements the Client interface for testing
type mockSSMClient struct {
	getParameterFunc func(ctx context.Context, params *ssm.GetParameterInput, optFns ...func(*ssm.Options)) (*ssm.GetParameterOutput, error)
}

func (m *mockSSMClient) GetParameter(ctx context.Context, params *ssm.GetParameterInput, optFns ...func(*ssm.Options)) (*ssm.GetParameterOutput, error) {
	return m.getParameterFunc(ctx, params, optFns...)
}

func TestResolveValue_NonARN(t *testing.T) {
	resolver := NewWithClient(&mockSSMClient{
		getParameterFunc: func(ctx context.Context, params *ssm.GetParameterInput, optFns ...func(*ssm.Options)) (*ssm.GetParameterOutput, error) {
			t.Fatal("GetParameter should not be called for non-ARN values")
			return nil, nil
		},
	})

	tests := []struct {
		name  string
		value string
	}{
		{"plain string", "my-secret-value"},
		{"empty string", ""},
		{"number", "12345"},
		{"URL", "https://example.com"},
		{"JSON", `{"key": "value"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolver.ResolveValue(context.Background(), tt.value)
			if err != nil {
				t.Errorf("ResolveValue(%q) error = %v, want nil", tt.value, err)
			}
			if got != tt.value {
				t.Errorf("ResolveValue(%q) = %q, want unchanged value", tt.value, got)
			}
		})
	}
}

func TestResolveValue_ValidARN(t *testing.T) {
	expectedValue := "resolved-secret-value"
	var capturedParamName string

	resolver := NewWithClient(&mockSSMClient{
		getParameterFunc: func(ctx context.Context, params *ssm.GetParameterInput, optFns ...func(*ssm.Options)) (*ssm.GetParameterOutput, error) {
			capturedParamName = *params.Name
			return &ssm.GetParameterOutput{
				Parameter: &types.Parameter{
					Value: &expectedValue,
				},
			}, nil
		},
	})

	arn := "arn:aws:ssm:us-east-1:123456789012:parameter/my-app/secret"
	got, err := resolver.ResolveValue(context.Background(), arn)

	if err != nil {
		t.Errorf("ResolveValue() error = %v, want nil", err)
	}
	if got != expectedValue {
		t.Errorf("ResolveValue() = %q, want %q", got, expectedValue)
	}
	if capturedParamName != "/my-app/secret" {
		t.Errorf("GetParameter called with name = %q, want %q", capturedParamName, "/my-app/secret")
	}
}

func TestResolveValue_SSMError(t *testing.T) {
	expectedErr := errors.New("SSM access denied")

	resolver := NewWithClient(&mockSSMClient{
		getParameterFunc: func(ctx context.Context, params *ssm.GetParameterInput, optFns ...func(*ssm.Options)) (*ssm.GetParameterOutput, error) {
			return nil, expectedErr
		},
	})

	arn := "arn:aws:ssm:us-east-1:123456789012:parameter/my-secret"
	_, err := resolver.ResolveValue(context.Background(), arn)

	if err == nil {
		t.Error("ResolveValue() expected error, got nil")
	}
	if !errors.Is(err, expectedErr) {
		t.Errorf("ResolveValue() error should wrap %v", expectedErr)
	}
}

func TestResolveValue_NilParameter(t *testing.T) {
	resolver := NewWithClient(&mockSSMClient{
		getParameterFunc: func(ctx context.Context, params *ssm.GetParameterInput, optFns ...func(*ssm.Options)) (*ssm.GetParameterOutput, error) {
			return &ssm.GetParameterOutput{
				Parameter: nil,
			}, nil
		},
	})

	arn := "arn:aws:ssm:us-east-1:123456789012:parameter/my-secret"
	_, err := resolver.ResolveValue(context.Background(), arn)

	if err == nil {
		t.Error("ResolveValue() expected error for nil parameter, got nil")
	}
}

func TestResolveValue_NilValue(t *testing.T) {
	resolver := NewWithClient(&mockSSMClient{
		getParameterFunc: func(ctx context.Context, params *ssm.GetParameterInput, optFns ...func(*ssm.Options)) (*ssm.GetParameterOutput, error) {
			return &ssm.GetParameterOutput{
				Parameter: &types.Parameter{
					Value: nil,
				},
			}, nil
		},
	})

	arn := "arn:aws:ssm:us-east-1:123456789012:parameter/my-secret"
	_, err := resolver.ResolveValue(context.Background(), arn)

	if err == nil {
		t.Error("ResolveValue() expected error for nil value, got nil")
	}
}

func TestResolveValue_DecryptionEnabled(t *testing.T) {
	var capturedWithDecryption *bool

	resolver := NewWithClient(&mockSSMClient{
		getParameterFunc: func(ctx context.Context, params *ssm.GetParameterInput, optFns ...func(*ssm.Options)) (*ssm.GetParameterOutput, error) {
			capturedWithDecryption = params.WithDecryption
			value := "decrypted"
			return &ssm.GetParameterOutput{
				Parameter: &types.Parameter{Value: &value},
			}, nil
		},
	})

	arn := "arn:aws:ssm:us-east-1:123456789012:parameter/encrypted-secret"
	_, _ = resolver.ResolveValue(context.Background(), arn)

	if capturedWithDecryption == nil || !*capturedWithDecryption {
		t.Error("ResolveValue() should request decryption for SSM parameters")
	}
}

func TestNewRetryConfigFromEnv_Defaults(t *testing.T) {
	cfg := NewRetryConfigFromEnv()

	if cfg.MaxRetries != DefaultMaxRetries {
		t.Errorf("MaxRetries = %d, want %d", cfg.MaxRetries, DefaultMaxRetries)
	}
	if cfg.RetryInterval != DefaultRetryInterval {
		t.Errorf("RetryInterval = %v, want %v", cfg.RetryInterval, DefaultRetryInterval)
	}
}
