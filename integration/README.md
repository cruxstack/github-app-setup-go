# Integration Tests

End-to-end integration tests that validate the GitHub App installer flow using
local HTTPS mock servers. Tests run the actual installer code against mock
GitHub API servers with self-signed TLS certificates.

## How It Works

1. **Mock GitHub API** starts on localhost with a self-signed TLS certificate
2. **Installer handler** is configured to use the mock server URL
3. **Test scenarios** execute HTTP requests against the installer
4. **Requests to GitHub API** are captured and matched against expected calls
5. **Store state** is verified after each scenario completes
6. **Reload triggers** are tracked to verify the installer triggers reloads

Key advantage: Tests run against production code paths with real HTTP
handling - no mocking of internal packages required.

## Running Tests

```bash
# Run integration tests only
make test-integration

# Run integration tests with verbose output
make test-integration-v

# Run all tests (unit + integration)
make test-all

# Run a specific scenario by name
go test -tags=integration ./integration/... -run "successful_manifest"
```

## Test Scenarios

Scenarios are defined in `testdata/scenarios.yaml`. Each scenario specifies:

- **config**: Installer configuration overrides
- **mock_responses**: Canned GitHub API responses
- **preset_credentials**: Optional pre-seeded credentials
- **steps**: HTTP requests to execute
- **expected_store**: Expected store state after test
- **expected_calls**: Expected HTTP calls to mock GitHub
- **expect_reload**: Whether a reload should be triggered

### Example Scenario

```yaml
- name: "successful_manifest_exchange"
  description: "Complete GitHub App manifest flow with valid code"
  config:
    app_display_name: "Test App"
  mock_responses:
    - method: POST
      path: /api/v3/app-manifests/*/conversions
      status: 201
      body: |
        {
          "id": 12345,
          "slug": "test-app",
          "client_id": "Iv1.abc123",
          "pem": "-----BEGIN RSA PRIVATE KEY-----\n...\n-----END RSA PRIVATE KEY-----"
        }
  steps:
    - action: request
      method: GET
      path: /callback?code=valid-code
      expect_status: 200
      expect_body_contains:
        - "test-app"
        - "12345"
  expected_store:
    registered: true
    app_id: 12345
    app_slug: "test-app"
  expected_calls:
    - method: POST
      path: /api/v3/app-manifests/*/conversions
  expect_reload: true
```

### Path Matching

Mock responses and expected calls support wildcard matching:
- `*` matches any single path segment
- Example: `/api/v3/app-manifests/*/conversions` matches
  `/api/v3/app-manifests/abc123/conversions`

## Adding Tests

1. Add a new scenario to `testdata/scenarios.yaml`
2. Define the mock responses needed
3. Specify the steps to execute
4. Define expected outcomes (store state, API calls, reload)
5. Run with `make test-integration`

## Architecture

```
+-------------------+
|   Test Scenario   |
+---------+---------+
          |
          v
+---------+---------+
| installer.Handler |
|   .ServeHTTP()    |
+---------+---------+
          |
          v
+---------+---------+       +-------------------------+
|   exchangeCode()  +------>| Mock GitHub HTTPS Server|
+---------+---------+       | (localhost)             |
          |                 +------------+------------+
          |                              |
          |                              v
          |                 +------------+------------+
          |                 |    Record Request       |
          |                 +------------+------------+
          |                              |
          |                              v
          |                 +------------+------------+
          |                 |  Return Mock Response   |
          |                 +-------------------------+
          |
          v
+---------+-------------------+
| configstore.LocalEnvFileStore|
|         .Save()             |
+---------+-------------------+
          |
          v
+---------+---------+
| configwait        |
|  .TriggerReload() |
+---------+---------+
          |
          v
+---------+-------------------+
| Verify:                     |
|  - Store State              |
|  - Expected Calls           |
|  - Reload Counter           |
+-----------------------------+
```

## Notes

- Each scenario runs with a fresh temp directory and store
- Self-signed TLS certificates are generated per test run
- The installer uses `/api/v3/app-manifests/*/conversions` for non-github.com
  URLs
- Tests require the `integration` build tag: `-tags=integration`
