// Copyright 2025 CruxStack
// SPDX-License-Identifier: MIT

package configstore

import (
	"context"
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/ssm/types"
)

// mockSSMClient implements SSMClient for testing
type mockSSMClient struct {
	parameters map[string]string
	putCalls   []ssm.PutParameterInput
	getCalls   []ssm.GetParameterInput
	putErr     error
	getErr     error
}

func newMockSSMClient() *mockSSMClient {
	return &mockSSMClient{
		parameters: make(map[string]string),
	}
}

func (m *mockSSMClient) PutParameter(ctx context.Context, params *ssm.PutParameterInput, optFns ...func(*ssm.Options)) (*ssm.PutParameterOutput, error) {
	m.putCalls = append(m.putCalls, *params)
	if m.putErr != nil {
		return nil, m.putErr
	}
	m.parameters[*params.Name] = *params.Value
	return &ssm.PutParameterOutput{}, nil
}

func (m *mockSSMClient) GetParameter(ctx context.Context, params *ssm.GetParameterInput, optFns ...func(*ssm.Options)) (*ssm.GetParameterOutput, error) {
	m.getCalls = append(m.getCalls, *params)
	if m.getErr != nil {
		return nil, m.getErr
	}
	value, ok := m.parameters[*params.Name]
	if !ok {
		return nil, &types.ParameterNotFound{}
	}
	return &ssm.GetParameterOutput{
		Parameter: &types.Parameter{
			Name:  params.Name,
			Value: aws.String(value),
		},
	}, nil
}

func TestNewAWSSSMStore(t *testing.T) {
	t.Run("empty prefix returns error", func(t *testing.T) {
		_, err := NewAWSSSMStore("")
		if err == nil {
			t.Error("NewAWSSSMStore(\"\") should return error")
		}
	})

	t.Run("prefix without trailing slash is normalized", func(t *testing.T) {
		mock := newMockSSMClient()
		store, err := NewAWSSSMStore("/my/prefix", WithSSMClient(mock))
		if err != nil {
			t.Fatalf("NewAWSSSMStore() error = %v", err)
		}
		if store.ParameterPrefix != "/my/prefix/" {
			t.Errorf("ParameterPrefix = %q, want %q", store.ParameterPrefix, "/my/prefix/")
		}
	})

	t.Run("prefix with trailing slash is preserved", func(t *testing.T) {
		mock := newMockSSMClient()
		store, err := NewAWSSSMStore("/my/prefix/", WithSSMClient(mock))
		if err != nil {
			t.Fatalf("NewAWSSSMStore() error = %v", err)
		}
		if store.ParameterPrefix != "/my/prefix/" {
			t.Errorf("ParameterPrefix = %q, want %q", store.ParameterPrefix, "/my/prefix/")
		}
	})
}

func TestAWSSSMStore_WithOptions(t *testing.T) {
	t.Run("WithKMSKey sets KMS key ID", func(t *testing.T) {
		mock := newMockSSMClient()
		store, err := NewAWSSSMStore("/prefix", WithSSMClient(mock), WithKMSKey("alias/my-key"))
		if err != nil {
			t.Fatalf("NewAWSSSMStore() error = %v", err)
		}
		if store.KMSKeyID != "alias/my-key" {
			t.Errorf("KMSKeyID = %q, want %q", store.KMSKeyID, "alias/my-key")
		}
	})

	t.Run("WithTags sets tags", func(t *testing.T) {
		mock := newMockSSMClient()
		tags := map[string]string{"env": "prod", "team": "platform"}
		store, err := NewAWSSSMStore("/prefix", WithSSMClient(mock), WithTags(tags))
		if err != nil {
			t.Fatalf("NewAWSSSMStore() error = %v", err)
		}
		if len(store.Tags) != 2 {
			t.Errorf("Tags count = %d, want 2", len(store.Tags))
		}
		if store.Tags["env"] != "prod" {
			t.Errorf("Tags[\"env\"] = %q, want %q", store.Tags["env"], "prod")
		}
	})
}

func TestAWSSSMStore_Save(t *testing.T) {
	mock := newMockSSMClient()
	store, err := NewAWSSSMStore("/app/github/", WithSSMClient(mock))
	if err != nil {
		t.Fatalf("NewAWSSSMStore() error = %v", err)
	}

	creds := &AppCredentials{
		AppID:         12345,
		AppSlug:       "my-app",
		ClientID:      "Iv1.abc123",
		ClientSecret:  "secret123",
		WebhookSecret: "whsec_123",
		PrivateKey:    "-----BEGIN RSA PRIVATE KEY-----\nkey\n-----END RSA PRIVATE KEY-----",
		HTMLURL:       "https://github.com/apps/my-app",
		CustomFields: map[string]string{
			"STS_DOMAIN": "sts.example.com",
		},
	}

	err = store.Save(context.Background(), creds)
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Verify all expected parameters were saved
	expectedParams := map[string]string{
		"/app/github/GITHUB_APP_ID":          "12345",
		"/app/github/GITHUB_APP_SLUG":        "my-app",
		"/app/github/GITHUB_CLIENT_ID":       "Iv1.abc123",
		"/app/github/GITHUB_CLIENT_SECRET":   "secret123",
		"/app/github/GITHUB_WEBHOOK_SECRET":  "whsec_123",
		"/app/github/GITHUB_APP_PRIVATE_KEY": "-----BEGIN RSA PRIVATE KEY-----\nkey\n-----END RSA PRIVATE KEY-----",
		"/app/github/GITHUB_APP_HTML_URL":    "https://github.com/apps/my-app",
		"/app/github/STS_DOMAIN":             "sts.example.com",
	}

	for name, wantValue := range expectedParams {
		gotValue, ok := mock.parameters[name]
		if !ok {
			t.Errorf("Parameter %q was not saved", name)
			continue
		}
		if gotValue != wantValue {
			t.Errorf("Parameter %q = %q, want %q", name, gotValue, wantValue)
		}
	}

	// Verify all parameters were saved as SecureString
	for _, call := range mock.putCalls {
		if call.Type != types.ParameterTypeSecureString {
			t.Errorf("Parameter %q type = %v, want SecureString", *call.Name, call.Type)
		}
		if call.Overwrite == nil || !*call.Overwrite {
			t.Errorf("Parameter %q Overwrite should be true", *call.Name)
		}
	}
}

func TestAWSSSMStore_Save_WithKMSKey(t *testing.T) {
	mock := newMockSSMClient()
	store, err := NewAWSSSMStore("/prefix/", WithSSMClient(mock), WithKMSKey("alias/custom-key"))
	if err != nil {
		t.Fatalf("NewAWSSSMStore() error = %v", err)
	}

	creds := &AppCredentials{
		AppID:         1,
		ClientID:      "client",
		ClientSecret:  "secret",
		WebhookSecret: "webhook",
		PrivateKey:    "key",
	}

	err = store.Save(context.Background(), creds)
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Verify KMS key was used
	for _, call := range mock.putCalls {
		if call.KeyId == nil || *call.KeyId != "alias/custom-key" {
			t.Errorf("Parameter %q KeyId = %v, want alias/custom-key", *call.Name, call.KeyId)
		}
	}
}

func TestAWSSSMStore_Save_WithTags(t *testing.T) {
	mock := newMockSSMClient()
	tags := map[string]string{"env": "test", "app": "myapp"}
	store, err := NewAWSSSMStore("/prefix/", WithSSMClient(mock), WithTags(tags))
	if err != nil {
		t.Fatalf("NewAWSSSMStore() error = %v", err)
	}

	creds := &AppCredentials{
		AppID:         1,
		ClientID:      "client",
		ClientSecret:  "secret",
		WebhookSecret: "webhook",
		PrivateKey:    "key",
	}

	err = store.Save(context.Background(), creds)
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Verify tags were applied to all parameters
	for _, call := range mock.putCalls {
		if len(call.Tags) != 2 {
			t.Errorf("Parameter %q has %d tags, want 2", *call.Name, len(call.Tags))
		}
	}
}

func TestAWSSSMStore_Save_OmitsEmptyOptionalFields(t *testing.T) {
	mock := newMockSSMClient()
	store, err := NewAWSSSMStore("/prefix/", WithSSMClient(mock))
	if err != nil {
		t.Fatalf("NewAWSSSMStore() error = %v", err)
	}

	creds := &AppCredentials{
		AppID:         1,
		ClientID:      "client",
		ClientSecret:  "secret",
		WebhookSecret: "webhook",
		PrivateKey:    "key",
		// AppSlug and HTMLURL are empty
	}

	err = store.Save(context.Background(), creds)
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Optional parameters should not be saved
	for _, name := range []string{"/prefix/GITHUB_APP_SLUG", "/prefix/GITHUB_APP_HTML_URL"} {
		if _, ok := mock.parameters[name]; ok {
			t.Errorf("Empty optional parameter %q should not be saved", name)
		}
	}
}

func TestAWSSSMStore_Save_Error(t *testing.T) {
	mock := newMockSSMClient()
	mock.putErr = fmt.Errorf("access denied")

	store, err := NewAWSSSMStore("/prefix/", WithSSMClient(mock))
	if err != nil {
		t.Fatalf("NewAWSSSMStore() error = %v", err)
	}

	creds := &AppCredentials{
		AppID:         1,
		ClientID:      "client",
		ClientSecret:  "secret",
		WebhookSecret: "webhook",
		PrivateKey:    "key",
	}

	err = store.Save(context.Background(), creds)
	if err == nil {
		t.Error("Save() should return error when PutParameter fails")
	}
}

func TestAWSSSMStore_Status_Registered(t *testing.T) {
	mock := newMockSSMClient()
	mock.parameters = map[string]string{
		"/prefix/GITHUB_APP_ID":          "12345",
		"/prefix/GITHUB_APP_SLUG":        "test-app",
		"/prefix/GITHUB_APP_HTML_URL":    "https://github.com/apps/test-app",
		"/prefix/GITHUB_CLIENT_ID":       "client123",
		"/prefix/GITHUB_CLIENT_SECRET":   "secret123",
		"/prefix/GITHUB_WEBHOOK_SECRET":  "webhook123",
		"/prefix/GITHUB_APP_PRIVATE_KEY": "-----BEGIN RSA-----\nkey\n-----END RSA-----",
	}

	store, err := NewAWSSSMStore("/prefix/", WithSSMClient(mock))
	if err != nil {
		t.Fatalf("NewAWSSSMStore() error = %v", err)
	}

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

func TestAWSSSMStore_Status_NotRegistered(t *testing.T) {
	mock := newMockSSMClient()
	// No parameters exist

	store, err := NewAWSSSMStore("/prefix/", WithSSMClient(mock))
	if err != nil {
		t.Fatalf("NewAWSSSMStore() error = %v", err)
	}

	status, err := store.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}

	if status.Registered {
		t.Error("Status.Registered = true, want false (no parameters)")
	}
}

func TestAWSSSMStore_Status_InstallerDisabled(t *testing.T) {
	mock := newMockSSMClient()
	mock.parameters = map[string]string{
		"/prefix/GITHUB_APP_ID":                "12345",
		"/prefix/GITHUB_CLIENT_ID":             "client",
		"/prefix/GITHUB_CLIENT_SECRET":         "secret",
		"/prefix/GITHUB_WEBHOOK_SECRET":        "webhook",
		"/prefix/GITHUB_APP_PRIVATE_KEY":       "key",
		"/prefix/GITHUB_APP_INSTALLER_ENABLED": "false",
	}

	store, err := NewAWSSSMStore("/prefix/", WithSSMClient(mock))
	if err != nil {
		t.Fatalf("NewAWSSSMStore() error = %v", err)
	}

	status, err := store.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}

	if !status.InstallerDisabled {
		t.Error("Status.InstallerDisabled = false, want true")
	}
}

func TestAWSSSMStore_Status_Error(t *testing.T) {
	mock := newMockSSMClient()
	mock.getErr = fmt.Errorf("access denied")

	store, err := NewAWSSSMStore("/prefix/", WithSSMClient(mock))
	if err != nil {
		t.Fatalf("NewAWSSSMStore() error = %v", err)
	}

	_, err = store.Status(context.Background())
	if err == nil {
		t.Error("Status() should return error when GetParameter fails")
	}
}

func TestAWSSSMStore_DisableInstaller(t *testing.T) {
	mock := newMockSSMClient()
	store, err := NewAWSSSMStore("/prefix/", WithSSMClient(mock))
	if err != nil {
		t.Fatalf("NewAWSSSMStore() error = %v", err)
	}

	err = store.DisableInstaller(context.Background())
	if err != nil {
		t.Fatalf("DisableInstaller() error = %v", err)
	}

	// Verify the parameter was set
	value, ok := mock.parameters["/prefix/GITHUB_APP_INSTALLER_ENABLED"]
	if !ok {
		t.Error("DisableInstaller() did not create parameter")
	}
	if value != "false" {
		t.Errorf("Parameter value = %q, want %q", value, "false")
	}
}

func TestAWSSSMStore_DisableInstaller_Error(t *testing.T) {
	mock := newMockSSMClient()
	mock.putErr = fmt.Errorf("access denied")

	store, err := NewAWSSSMStore("/prefix/", WithSSMClient(mock))
	if err != nil {
		t.Fatalf("NewAWSSSMStore() error = %v", err)
	}

	err = store.DisableInstaller(context.Background())
	if err == nil {
		t.Error("DisableInstaller() should return error when PutParameter fails")
	}
}

func TestIsParameterNotFound(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "ParameterNotFound error",
			err:  &types.ParameterNotFound{},
			want: true,
		},
		{
			name: "other error",
			err:  fmt.Errorf("some other error"),
			want: false,
		},
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isParameterNotFound(tt.err)
			if got != tt.want {
				t.Errorf("isParameterNotFound() = %v, want %v", got, tt.want)
			}
		})
	}
}
