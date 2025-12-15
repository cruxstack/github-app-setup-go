.PHONY: all build test test-unit test-integration lint fmt vet clean tidy help

GO ?= go
GOFLAGS ?=
PACKAGES := $(shell $(GO) list ./... | grep -v '/examples/' | grep -v '/docs/' | grep -v '/integration')
INTEGRATION_PKG := ./integration/...

all: fmt vet lint test-unit ## Run all checks and unit tests

build: ## Build all packages
	$(GO) build $(GOFLAGS) ./...

test: test-unit ## Alias for test-unit

test-unit: ## Run unit tests only
	$(GO) test $(GOFLAGS) $(PACKAGES)

test-unit-v: ## Run unit tests with verbose output
	$(GO) test $(GOFLAGS) -v $(PACKAGES)

test-integration: ## Run integration tests
	$(GO) test $(GOFLAGS) -tags=integration $(INTEGRATION_PKG)

test-integration-v: ## Run integration tests with verbose output
	VERBOSE=1 $(GO) test $(GOFLAGS) -v -tags=integration $(INTEGRATION_PKG)

test-all: test-unit test-integration ## Run all tests (unit + integration)

test-v: ## Run unit tests with verbose output (alias for test-unit-v)
	$(GO) test $(GOFLAGS) -v $(PACKAGES)

test-race: ## Run tests with race detector
	$(GO) test $(GOFLAGS) -race $(PACKAGES)

test-cover: ## Run tests with coverage
	$(GO) test $(GOFLAGS) -cover $(PACKAGES)

test-cover-html: ## Run tests and generate HTML coverage report
	$(GO) test $(GOFLAGS) -coverprofile=coverage.out $(PACKAGES)
	$(GO) tool cover -html=coverage.out -o coverage.html

lint: ## Run linter (requires golangci-lint)
	@which golangci-lint > /dev/null || (echo "golangci-lint not installed, skipping" && exit 0)
	golangci-lint run ./...

fmt: ## Format code
	$(GO) fmt ./...

vet: ## Run go vet
	$(GO) vet ./...

tidy: ## Tidy go.mod
	$(GO) mod tidy

clean: ## Clean build artifacts
	rm -f coverage.out coverage.html

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-15s\033[0m %s\n", $$1, $$2}'
