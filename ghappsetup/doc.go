// Copyright 2025 CruxStack
// SPDX-License-Identifier: MIT

// Package ghappsetup provides a unified runtime for GitHub App lifecycle
// management. It coordinates configuration loading, readiness gating, and
// hot reloading for both HTTP servers and AWS Lambda functions.
//
// The Runtime type provides a consistent interface across both environments:
//
//   - HTTP servers use Start() to block until config loads, Handler() to wrap
//     requests with a ReadyGate, and ListenForReloads() for SIGHUP handling.
//
//   - Lambda functions use EnsureLoaded() for lazy initialization with retry
//     logic on each invocation.
//
// # HTTP Server Usage
//
// For HTTP servers, create a Runtime and use its methods to manage the
// application lifecycle:
//
//	runtime, err := ghappsetup.NewRuntime(ghappsetup.Config{
//	    LoadFunc:     loadConfig,
//	    AllowedPaths: []string{"/healthz", "/setup", "/callback", "/"},
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	mux := http.NewServeMux()
//	mux.HandleFunc("/healthz", runtime.HealthHandler())
//	mux.HandleFunc("/webhook", webhookHandler)
//
//	// Option A: Manual installer wiring
//	installerHandler, _ := installer.New(installer.Config{
//	    Store:          runtime.Store(),
//	    Manifest:       manifest,
//	    OnReloadNeeded: runtime.ReloadCallback(),
//	})
//
//	// Option B: Convenience method (recommended)
//	installerHandler, _ := runtime.InstallerHandler(installer.Config{
//	    Manifest: manifest,
//	})
//
//	mux.Handle("/setup", installerHandler)
//
//	srv := &http.Server{Handler: runtime.Handler(mux)}
//	go srv.ListenAndServe()
//
//	// Block until config loads, then listen for SIGHUP reloads
//	runtime.Start(ctx)
//	runtime.ListenForReloads(ctx)
//
// # Lambda Usage
//
// For Lambda functions, use EnsureLoaded() at the start of each handler
// invocation:
//
//	var runtime *ghappsetup.Runtime
//
//	func init() {
//	    runtime, _ = ghappsetup.NewRuntime(ghappsetup.Config{
//	        LoadFunc: func(ctx context.Context) error {
//	            // Resolve SSM parameters if needed
//	            if err := ssmresolver.ResolveEnvironmentWithDefaults(ctx); err != nil {
//	                return err
//	            }
//	            return initHandler()
//	        },
//	    })
//	}
//
//	func handler(ctx context.Context, req events.APIGatewayV2HTTPRequest) (Response, error) {
//	    if err := runtime.EnsureLoaded(ctx); err != nil {
//	        return serviceUnavailableResponse(), nil
//	    }
//	    return handleRequest(ctx, req)
//	}
//
// # Environment Detection
//
// The Runtime automatically detects whether it's running in an HTTP server
// or Lambda environment by checking for the AWS_LAMBDA_FUNCTION_NAME
// environment variable. This affects default retry settings:
//
//   - HTTP: 30 retries with 2-second intervals (suitable for startup)
//   - Lambda: 5 retries with 1-second intervals (suitable for cold starts)
//
// # Installer Integration
//
// The Runtime integrates with the installer package for GitHub App creation.
// When credentials are saved via the installer, the Runtime's reload callback
// is invoked to trigger configuration reloading. This eliminates the need for
// global state and enables proper dependency injection.
package ghappsetup
