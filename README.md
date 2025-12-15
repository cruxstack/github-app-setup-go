# github-app-setup-go

A Go library for creating and managing GitHub Apps using the
[GitHub App Manifest flow](https://docs.github.com/en/apps/sharing-github-apps/registering-a-github-app-from-a-manifest).
Provides a web-based installer, multiple credential storage backends, and
utilities for configuration management in containerized environments.

## Features

- **Web-based installer** - User-friendly UI for creating GitHub Apps with
  pre-configured permissions
- **Multiple storage backends** - AWS SSM Parameter Store, `.env` files, or
  individual files
- **Hot reload support** - Reload configuration via SIGHUP or programmatic
  triggers
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
| `installer`   | HTTP handler implementing the GitHub App Manifest flow    |
| `configstore` | Storage backends for GitHub App credentials               |
| `configwait`  | Startup wait logic, ready gate middleware, and reload     |
| `ssmresolver` | Resolves SSM Parameter Store ARNs in environment vars     |

## Quick Start

```go
package main

import (
    "context"
    "log"
    "net/http"

    "github.com/cruxstack/github-app-setup-go/configstore"
    "github.com/cruxstack/github-app-setup-go/configwait"
    "github.com/cruxstack/github-app-setup-go/installer"
)

func main() {
    ctx := context.Background()

    // Create a storage backend (uses STORAGE_MODE env var, defaults to .env file)
    store, err := configstore.NewFromEnv()
    if err != nil {
        log.Fatal(err)
    }

    // Define the GitHub App manifest with required permissions
    manifest := installer.Manifest{
        URL:    "https://example.com",
        Public: false,
        DefaultPerms: map[string]string{
            "contents":      "read",
            "pull_requests": "write",
        },
        DefaultEvents: []string{"pull_request", "push"},
    }

    // Create the installer handler
    installerHandler, err := installer.New(installer.Config{
        Store:          store,
        Manifest:       manifest,
        AppDisplayName: "My GitHub App",
    })
    if err != nil {
        log.Fatal(err)
    }

    // Set up routes
    mux := http.NewServeMux()
    mux.Handle("/setup", installerHandler)
    mux.Handle("/callback", installerHandler)

    // Create a ready gate that allows /setup through before app is configured
    gate := configwait.NewReadyGate(mux, []string{"/setup", "/callback", "/healthz"})

    // Start the server
    log.Println("Starting server on :8080")
    log.Fatal(http.ListenAndServe(":8080", gate))
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

The library supports hot-reloading configuration via SIGHUP signals or
programmatic triggers:

```go
// Create a reloader that calls your reload function
reloader := configwait.NewReloader(ctx, gate, func(ctx context.Context) error {
    // Reload your configuration here
    newHandler := buildHandler()
    gate.SetHandler(newHandler)
    gate.SetReady()
    return nil
})

// Set as global reloader (allows installer to trigger reload after saving)
configwait.SetGlobalReloader(reloader)

// Start listening for SIGHUP
reloader.Start()
```

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
