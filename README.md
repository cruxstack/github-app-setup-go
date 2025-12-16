# github-app-setup-go

A Go library for creating and managing GitHub Apps using the
[GitHub App Manifest flow](https://docs.github.com/en/apps/sharing-github-apps/registering-a-github-app-from-a-manifest).
Provides a web-based installer, multiple credential storage backends, and
utilities for configuration management in containerized environments.

## Features

- **Unified runtime** - Single API for both HTTP servers and Lambda functions
- **Web-based installer** - User-friendly UI for creating GitHub Apps with
  pre-configured permissions
- **Multiple storage backends** - AWS SSM Parameter Store, `.env` files, or
  individual files
- **Hot reload support** - Reload configuration via SIGHUP or installer callback
- **SSM ARN resolution** - Resolve AWS SSM Parameter Store ARNs in environment
  variables (useful for Lambda)
- **Ready gate** - HTTP middleware that returns 503 until configuration is
  loaded

## Installation

```bash
go get github.com/cruxstack/github-app-setup-go
```

## Packages

| Package       | Description                                               |
|---------------|-----------------------------------------------------------|
| `ghappsetup`  | **Unified runtime** for HTTP servers and Lambda functions |
| `installer`   | HTTP handler implementing the GitHub App Manifest flow    |
| `configstore` | Storage backends for GitHub App credentials               |
| `configwait`  | Startup wait logic and ready gate middleware              |
| `ssmresolver` | Resolves SSM Parameter Store ARNs in environment vars     |

## Quick Start

The `ghappsetup.Runtime` provides unified lifecycle management for both HTTP
servers and Lambda functions:

```go
package main

import (
    "context"
    "fmt"
    "log"
    "net/http"
    "os"

    "github.com/cruxstack/github-app-setup-go/ghappsetup"
    "github.com/cruxstack/github-app-setup-go/installer"
)

func main() {
    ctx := context.Background()

    // Create runtime with unified lifecycle management
    runtime, err := ghappsetup.NewRuntime(ghappsetup.Config{
        LoadFunc:     loadConfig,
        AllowedPaths: []string{"/healthz", "/setup", "/callback", "/"},
    })
    if err != nil {
        log.Fatal(err)
    }

    // Set up routes
    mux := http.NewServeMux()
    mux.HandleFunc("/healthz", runtime.HealthHandler())
    mux.HandleFunc("/webhook", webhookHandler)

    // Create installer using convenience method (auto-wires Store and reload callback)
    installerHandler, err := runtime.InstallerHandler(installer.Config{
        Manifest: installer.Manifest{
            URL:    "https://example.com",
            Public: false,
            DefaultPerms: map[string]string{
                "contents":      "read",
                "pull_requests": "write",
            },
            DefaultEvents: []string{"pull_request", "push"},
        },
        AppDisplayName: "My GitHub App",
    })
    if err != nil {
        log.Fatal(err)
    }

    mux.Handle("/setup", installerHandler)
    mux.Handle("/callback", installerHandler)

    // Start HTTP server with ReadyGate middleware
    srv := &http.Server{
        Addr:    ":8080",
        Handler: runtime.Handler(mux),
    }
    go srv.ListenAndServe()

    // Block until config loads, then listen for SIGHUP reloads
    if err := runtime.Start(ctx); err != nil {
        log.Fatal(err)
    }
    log.Println("Configuration loaded, service is ready")
    runtime.ListenForReloads(ctx)
}

func loadConfig(ctx context.Context) error {
    // Validate required environment variables are present
    if os.Getenv("GITHUB_APP_ID") == "" {
        return fmt.Errorf("GITHUB_APP_ID not set")
    }
    return nil
}

func webhookHandler(w http.ResponseWriter, r *http.Request) {
    // Handle GitHub webhooks
    w.WriteHeader(http.StatusOK)
}
```

## Configuration

### Environment Variables

#### Installer

| Variable                       | Description                                 | Default              |
|--------------------------------|---------------------------------------------|----------------------|
| `GITHUB_URL`                   | GitHub base URL (for GHE Server)            | `https://github.com` |
| `GITHUB_ORG`                   | Organization (empty = personal account)     | -                    |
| `GITHUB_APP_INSTALLER_ENABLED` | Enable the installer UI (`true`, `1`, `yes`)| -                    |

#### Storage

| Variable                  | Description                                  | Default     |
|---------------------------|----------------------------------------------|-------------|
| `STORAGE_MODE`            | Backend: `envfile`, `files`, or `aws-ssm`    | `envfile`   |
| `STORAGE_DIR`             | Directory/path for local storage backends    | `./.env`    |
| `AWS_SSM_PARAMETER_PREFIX`| SSM parameter path prefix (for `aws-ssm`)    | -           |
| `AWS_SSM_KMS_KEY_ID`      | Custom KMS key for SSM encryption            | AWS managed |
| `AWS_SSM_TAGS`            | JSON object of tags for SSM parameters       | -           |

#### Config Wait

| Variable                    | Description                          | Default |
|-----------------------------|--------------------------------------|---------|
| `CONFIG_WAIT_MAX_RETRIES`   | Maximum retry attempts               | `30`    |
| `CONFIG_WAIT_RETRY_INTERVAL`| Duration between retries (e.g., `2s`)| `2s`    |

## Storage Backends

### AWS SSM Parameter Store

Stores credentials as encrypted SecureString parameters:

```go
store, err := configstore.NewAWSSSMStore("/my-app/prod/",
    configstore.WithKMSKey("alias/my-key"),
    configstore.WithTags(map[string]string{
        "Environment": "production",
    }),
)
```

Parameters are stored at paths like `/my-app/prod/GITHUB_APP_ID`,
`/my-app/prod/GITHUB_APP_PRIVATE_KEY`, etc.

### Local .env File

Saves credentials to a `.env` file, preserving existing content:

```go
store := configstore.NewLocalEnvFileStore("./.env")
```

### Local Files

Saves each credential as a separate file:

```go
store := configstore.NewLocalFileStore("./secrets/")
// Creates: ./secrets/app-id, ./secrets/private-key.pem, etc.
```

## Hot Reload

The Runtime supports hot-reloading configuration via SIGHUP signals. When the
installer saves new credentials, it automatically triggers a reload via the
callback:

```go
// ListenForReloads handles both SIGHUP signals and installer callbacks
runtime.ListenForReloads(ctx)
```

For manual reload triggering:

```go
// Trigger a reload programmatically
runtime.Reload()
```

## Lambda Usage

For AWS Lambda functions, use `EnsureLoaded()` for lazy initialization:

```go
var runtime *ghappsetup.Runtime

func init() {
    runtime, _ = ghappsetup.NewRuntime(ghappsetup.Config{
        LoadFunc: func(ctx context.Context) error {
            // Resolve SSM parameters passed as ARNs
            if err := ssmresolver.ResolveEnvironmentWithDefaults(ctx); err != nil {
                return err
            }
            return validateConfig()
        },
    })
}

func handler(ctx context.Context, req events.APIGatewayV2HTTPRequest) (Response, error) {
    // Lazy-load config with retries (idempotent after first success)
    if err := runtime.EnsureLoaded(ctx); err != nil {
        return Response{StatusCode: 503, Body: "Service unavailable"}, nil
    }
    return handleRequest(ctx, req)
}
```

The Runtime auto-detects Lambda environments and adjusts retry settings:
- **HTTP**: 30 retries, 2-second intervals (suitable for startup)
- **Lambda**: 5 retries, 1-second intervals (suitable for cold starts)

## SSM ARN Resolution

For Lambda deployments where secrets are passed as SSM ARNs:

```go
// Resolve all environment variables that contain SSM ARNs
err := ssmresolver.ResolveEnvironmentWithRetry(ctx, ssmresolver.NewRetryConfigFromEnv())

// Or resolve a single value
resolver, _ := ssmresolver.New(ctx)
value, err := resolver.ResolveValue(ctx, os.Getenv("MY_SECRET"))
```

## Stored Credentials

After a GitHub App is created, the following credentials are stored:

| Key                       | Description                           |
|---------------------------|---------------------------------------|
| `GITHUB_APP_ID`           | The numeric App ID                    |
| `GITHUB_APP_SLUG`         | The app's URL slug                    |
| `GITHUB_APP_HTML_URL`     | URL to the app's GitHub settings page |
| `GITHUB_WEBHOOK_SECRET`   | Webhook signature secret              |
| `GITHUB_CLIENT_ID`        | OAuth client ID                       |
| `GITHUB_CLIENT_SECRET`    | OAuth client secret                   |
| `GITHUB_APP_PRIVATE_KEY`  | Private key (PEM format)              |

## License

MIT License - Copyright 2025 CruxStack
