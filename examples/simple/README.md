# Simple GitHub App Example

A minimal example demonstrating a GitHub App with webhook handling using Docker
and Docker Compose.

## Features

- Web-based GitHub App installer at `/setup`
- Webhook endpoint at `/webhook` that logs received events
- Configurable logging format (text or JSON via slog)
- Credentials stored in `.env` file (envfile storage backend)
- Health check endpoint at `/healthz`

## Prerequisites

- Docker Engine 24.0+ and Docker Compose v2.20+
- [ngrok](https://ngrok.com/) or similar tunnel for webhook delivery
- GitHub account with permission to create GitHub Apps

## Quick Start

### 1. Start the Application

```bash
cd examples/simple
docker compose up --build
```

The app starts with the installer enabled by default.

### 2. Expose the Application

GitHub needs to reach your webhook endpoint. Use ngrok or a similar tunnel:

```bash
ngrok http 8080
```

Copy the HTTPS forwarding URL (e.g., `https://abc123.ngrok-free.app`).

### 3. Create the GitHub App

1. Open your ngrok URL with `/setup` path in a browser:
   ```
   https://abc123.ngrok-free.app/setup
   ```

2. Enter your desired app name

3. Update the webhook URL to your ngrok URL + `/webhook`:
   ```
   https://abc123.ngrok-free.app/webhook
   ```

4. Click "Create GitHub App" and authorize the app creation

5. The installer saves credentials automatically and the app reloads

### 4. Install the App

After creation, click "Install App" to grant the app access to your
repositories. Select which repositories should send webhook events.

### 5. Test Webhooks

Push a commit or create a pull request in an installed repository. You should
see log output like:

```
level=INFO msg="received webhook" event=push action="" delivery_id=abc123 repository=owner/repo sender=username payload_size=1234
```

## Configuration

Copy `.env.example` to `.env` to customize settings:

```bash
cp .env.example .env
```

### Environment Variables

| Variable                       | Description                  | Default             |
|--------------------------------|------------------------------|---------------------|
| `LOG_FORMAT`                   | Log format: `text` or `json` | `text`              |
| `PORT`                         | HTTP port                    | `8080`              |
| `GITHUB_APP_INSTALLER_ENABLED` | Enable installer UI          | `true`              |
| `GITHUB_URL`                   | GitHub base URL              | `https://github.com`|
| `GITHUB_ORG`                   | Organization for app         | -                   |

### Disabling the Installer

After setup, disable the installer for security:

1. Click "Disable Setup & Continue" in the success page, or
2. Set `GITHUB_APP_INSTALLER_ENABLED=false` in `.env` and restart

## Endpoints

| Path       | Description                    |
|------------|--------------------------------|
| `/setup`   | GitHub App installer (enabled) |
| `/webhook` | Webhook receiver               |
| `/healthz` | Health check                   |

## Logs

The application uses Go's `slog` package with configurable output format.

**Text format** (default):
```
level=INFO msg="received webhook" event=push repository=owner/repo
```

**JSON format** (`LOG_FORMAT=json`):
```json
{"level":"INFO","msg":"received webhook","event":"push","repository":"owner/repo"}
```

## Architecture

```
                      +-------------------+
                      |       ngrok       |
                      |  (public HTTPS)   |
                      +---------+---------+
                                |
                                v
+-------------------------------+-------------------------------+
|                         App Container                         |
|                                                               |
|   /setup    --> Installer (creates GitHub App)                |
|   /webhook  --> Webhook Handler (logs events)                 |
|   /healthz  --> Health Check                                  |
|                                                               |
|   Storage: /data/.env (persisted via Docker volume)           |
+---------------------------------------------------------------+
```

## Troubleshooting

### App not receiving webhooks

1. Verify ngrok is running and the URL hasn't changed
2. Check webhook URL in GitHub App settings matches your ngrok URL + `/webhook`
3. View webhook deliveries in GitHub App settings for error details

### Configuration not loading

Check logs for retry messages:
```bash
docker compose logs -f
```

The app retries configuration loading for 60 seconds by default.

### Resetting the app

To start fresh, remove the Docker volume:
```bash
docker compose down -v
docker compose up --build
```
