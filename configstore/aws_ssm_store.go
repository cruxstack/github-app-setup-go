// Copyright 2025 CruxStack
// SPDX-License-Identifier: MIT

package configstore

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/ssm/types"
)

// SSMClient defines the interface for AWS SSM operations.
type SSMClient interface {
	PutParameter(ctx context.Context, params *ssm.PutParameterInput,
		optFns ...func(*ssm.Options)) (*ssm.PutParameterOutput, error)
	GetParameter(ctx context.Context, params *ssm.GetParameterInput,
		optFns ...func(*ssm.Options)) (*ssm.GetParameterOutput, error)
}

// AWSSSMStore saves credentials to AWS SSM Parameter Store with encryption.
type AWSSSMStore struct {
	ParameterPrefix string
	KMSKeyID        string
	Tags            map[string]string
	ssmClient       SSMClient
}

// SSMStoreOption is a functional option for configuring AWSSSMStore.
type SSMStoreOption func(*AWSSSMStore)

// WithKMSKey sets a custom KMS key ID for parameter encryption.
func WithKMSKey(keyID string) SSMStoreOption {
	return func(s *AWSSSMStore) {
		s.KMSKeyID = keyID
	}
}

// WithTags adds AWS tags to all created parameters.
func WithTags(tags map[string]string) SSMStoreOption {
	return func(s *AWSSSMStore) {
		s.Tags = tags
	}
}

// WithSSMClient sets a custom SSM client.
func WithSSMClient(client SSMClient) SSMStoreOption {
	return func(s *AWSSSMStore) {
		s.ssmClient = client
	}
}

// NewAWSSSMStore creates a new AWS SSM Parameter Store backend.
// The prefix is normalized to always end with a slash.
func NewAWSSSMStore(prefix string, opts ...SSMStoreOption) (*AWSSSMStore, error) {
	if prefix == "" {
		return nil, fmt.Errorf("parameter prefix cannot be empty")
	}

	if !strings.HasSuffix(prefix, "/") {
		prefix = prefix + "/"
	}

	store := &AWSSSMStore{
		ParameterPrefix: prefix,
	}

	for _, opt := range opts {
		opt(store)
	}

	if store.ssmClient == nil {
		cfg, err := config.LoadDefaultConfig(context.Background())
		if err != nil {
			return nil, fmt.Errorf("failed to load AWS config: %w", err)
		}
		store.ssmClient = ssm.NewFromConfig(cfg)
	}

	return store, nil
}

// Save writes credentials to AWS SSM as encrypted SecureString parameters.
func (s *AWSSSMStore) Save(ctx context.Context, creds *AppCredentials) error {
	parameters := map[string]string{
		EnvGitHubAppID:         fmt.Sprintf("%d", creds.AppID),
		EnvGitHubWebhookSecret: creds.WebhookSecret,
		EnvGitHubClientID:      creds.ClientID,
		EnvGitHubClientSecret:  creds.ClientSecret,
		EnvGitHubAppPrivateKey: creds.PrivateKey,
	}

	if creds.AppSlug != "" {
		parameters[EnvGitHubAppSlug] = creds.AppSlug
	}
	if creds.HTMLURL != "" {
		parameters[EnvGitHubAppHTMLURL] = creds.HTMLURL
	}

	for key, value := range creds.CustomFields {
		if value != "" {
			parameters[key] = value
		}
	}

	for name, value := range parameters {
		if err := s.putParameter(ctx, name, value); err != nil {
			return fmt.Errorf("failed to save parameter %s: %w", name, err)
		}
	}

	return nil
}

// putParameter creates or updates a single SSM parameter.
func (s *AWSSSMStore) putParameter(ctx context.Context, name, value string) error {
	input := &ssm.PutParameterInput{
		Name:      aws.String(s.ParameterPrefix + name),
		Value:     aws.String(value),
		Type:      types.ParameterTypeSecureString,
		Overwrite: aws.Bool(true),
		DataType:  aws.String("text"),
	}

	if s.KMSKeyID != "" {
		input.KeyId = aws.String(s.KMSKeyID)
	}

	if len(s.Tags) > 0 {
		var tags []types.Tag
		for key, value := range s.Tags {
			tags = append(tags, types.Tag{
				Key:   aws.String(key),
				Value: aws.String(value),
			})
		}
		input.Tags = tags
	}

	_, err := s.ssmClient.PutParameter(ctx, input)
	if err != nil {
		return err
	}

	return nil
}

// Status returns the current registration state by checking required SSM parameters.
func (s *AWSSSMStore) Status(ctx context.Context) (*InstallerStatus, error) {
	status := &InstallerStatus{}
	required := []string{
		EnvGitHubAppID,
		EnvGitHubWebhookSecret,
		EnvGitHubClientID,
		EnvGitHubClientSecret,
		EnvGitHubAppPrivateKey,
	}

	values := make(map[string]string)
	for _, key := range required {
		value, err := s.getParameterValue(ctx, key)
		if err != nil {
			if isParameterNotFound(err) {
				return status, nil
			}
			return nil, err
		}
		values[key] = value
	}

	status.Registered = true
	if id, err := strconv.ParseInt(strings.TrimSpace(values[EnvGitHubAppID]), 10, 64); err == nil {
		status.AppID = id
	}

	if slug, err := s.getParameterValue(ctx, EnvGitHubAppSlug); err == nil {
		status.AppSlug = slug
	} else if !isParameterNotFound(err) {
		return nil, err
	}

	if html, err := s.getParameterValue(ctx, EnvGitHubAppHTMLURL); err == nil {
		status.HTMLURL = html
	} else if !isParameterNotFound(err) {
		return nil, err
	}

	if flag, err := s.getParameterValue(ctx, EnvGitHubAppInstallerEnabled); err == nil {
		status.InstallerDisabled = isFalseString(flag)
	} else if !isParameterNotFound(err) {
		return nil, err
	}

	return status, nil
}

// DisableInstaller sets a parameter to disable the installer.
func (s *AWSSSMStore) DisableInstaller(ctx context.Context) error {
	return s.putParameter(ctx, EnvGitHubAppInstallerEnabled, "false")
}

func (s *AWSSSMStore) getParameterValue(ctx context.Context, name string) (string, error) {
	output, err := s.ssmClient.GetParameter(ctx, &ssm.GetParameterInput{
		Name:           aws.String(s.ParameterPrefix + name),
		WithDecryption: aws.Bool(true),
	})
	if err != nil {
		return "", err
	}
	if output.Parameter == nil || output.Parameter.Value == nil {
		return "", fmt.Errorf("parameter %s missing value", name)
	}
	return aws.ToString(output.Parameter.Value), nil
}

func isParameterNotFound(err error) bool {
	var notFound *types.ParameterNotFound
	return errors.As(err, &notFound)
}
