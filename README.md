# Miniform

[![GitHub stars](https://img.shields.io/github/stars/matteodante/miniform?style=flat-square)](https://github.com/matteodante/miniform/stargazers)
[![License](https://img.shields.io/github/license/matteodante/miniform?style=flat-square)](LICENSE)

Form backend you actually own. Collect submissions on your server. One OCI container or binary. No SaaS bills.

**[Repository](https://github.com/matteodante/miniform)** · **[Releases](https://github.com/matteodante/miniform/releases)** · **[OCI images](https://github.com/matteodante/miniform/pkgs/container/miniform)**

## Overview

Miniform enables developers running static or serverless sites to handle form submissions with a single-binary deployment. Store submissions in SQLite, review them in a lightweight admin UI, and route data asynchronously to webhooks or email via Mailgun.

## Features

- **Single-binary deployment** — One executable with SQLite storage, no external dependencies
- **Admin dashboard** — View and manage forms, submissions, and delivery status
- **Asynchronous delivery** — Queue webhook and email notifications with retry logic
- **Spam protection** — Built-in honeypot field and rate limiting
- **API-first design** — Dashboard consumes the same REST endpoints available for integrations
- **Your server, your data** — We don't run servers. We can't see your submissions. That's the point.

## JavaScript SDK (Optional)

For improved reliability, include the optional SDK that adds automatic retry with exponential backoff:

```html
<script src="https://your-miniform.com/assets/miniform.js"></script>

<form action="https://your-miniform.com/forms/contact/submit?token=YOUR_TOKEN" method="post">
  <input name="email" type="email" required>
  <button type="submit">Send</button>
</form>
```

The SDK auto-detects Miniform forms and enhances them with:
- **Retry logic** — 3 attempts with exponential backoff on 503/network errors
- **Graceful degradation** — Falls back to normal form POST if JS fails
- **Loading states** — Disables form and shows "Sending..." during submission

Forms work without the SDK via standard HTML POST. The SDK is purely an enhancement.

## Installation

### One-Line Install (Recommended for VPS/Servers)

Install Miniform with Docker, Caddy reverse proxy, and automatic SSL certificates:

```bash
curl -fsSL https://raw.githubusercontent.com/matteodante/miniform/main/install.sh | sudo bash
```

This interactive installer will:
- Check system requirements (Docker, ports 80/443)
- Prompt for your domain name
- Set up Caddy as a reverse proxy with automatic HTTPS
- Configure automatic daily backups
- Start the Miniform container

After installation, access your dashboard at `https://your-domain.com`

**Management commands:**
```bash
miniform update              # Update to latest version
miniform reload              # Reload containers
miniform restore-db          # Restore from backup
miniform change-admin-password  # Reset admin password
```

---

## Quick Start (Development/Local)

### Using Apple Container

Requires Apple Silicon, macOS 26, and the official [`apple/container`](https://github.com/apple/container) CLI.

```bash
make apple-container-run
make apple-container-health
```

The target starts the Apple container service when needed, builds `miniform:local` from the OCI-compatible `Dockerfile`, publishes port `8080`, and persists SQLite data in `./storage`. It prints the container URL after startup.

If `http://127.0.0.1:8080` resets the connection, enable **System Settings → Privacy & Security → Local Network → container-runtime-linux**, then restart the Apple container service. Direct container-IP access and `make apple-container-health` work independently of that forwarding permission.

```bash
make apple-container-stop
```

### Using Docker

**First, generate and save your session secret:**

```bash
# Generate once and save this value securely
export MINIFORM_SESSION_SECRET=$(openssl rand -hex 32)
echo "Save this secret: $MINIFORM_SESSION_SECRET"
```

**Then run the container with your saved secret:**

```bash
docker run -d \
  -p 8080:8080 \
  -e MINIFORM_SESSION_SECRET="your-saved-secret-here" \
  -v $(pwd)/storage:/app/storage \
  ghcr.io/matteodante/miniform:latest
```

**Important:** Use the same `MINIFORM_SESSION_SECRET` value across restarts to prevent logging out all users.

**HTTPS is required.** The Docker image runs in production mode, which marks the session cookie `Secure`. Without TLS in front (Caddy, Nginx, Traefik, etc.) browsers silently drop the cookie and login appears to fail. The bundled `install.sh` sets up Caddy with automatic certificates; if you roll your own with docker-compose, put a TLS terminator in front of `:8080`.

Access the admin dashboard at `http://localhost:8080` with default credentials:
- Email: `admin@miniform.local`
- Password: `miniform` (change it immediately after first login)

### Running the Binary

1. Download the latest release from the [Releases page](https://github.com/matteodante/miniform/releases)
2. Generate and save your session secret:
   ```bash
   # Generate once and save this value
   export MINIFORM_SESSION_SECRET=$(openssl rand -hex 32)
   echo "Save this secret: $MINIFORM_SESSION_SECRET"
   
   export MINIFORM_DATA_DIR=./storage
   ```
3. Run the binary:
   ```bash
   ./miniform
   ```

## Configuration

Miniform uses [Viper](https://github.com/spf13/viper) for flexible configuration. You can configure via:
- Environment variables (prefix: `MINIFORM_`)
- `.env` file for easier local development
- Environment variables always override `.env` file values

**Required Environment Variable (Production Only):**
- `MINIFORM_SESSION_SECRET` - HMAC secret for signing session cookies (fixed default in dev/test)

**Optional Environment Variables:**
- `MINIFORM_ENV` - Environment mode: `development`, `production` (default: `development` for binary / `go run`; the Docker image sets `production`)
- `MINIFORM_PORT` - HTTP port (default: `8080`)
- `MINIFORM_LOG_LEVEL` - Log level: `debug`, `info`, `warn`, `error` (default: `error`)
- `MINIFORM_DATA_DIR` - Data directory path (default: `./storage`)

> **Note:** In development/test, a fixed default secret is used if not set, allowing sessions to persist across restarts.

> **HTTPS required when setting `MINIFORM_ENV=production`.** Production mode marks the session cookie `Secure`, so browsers will not send it back over plain HTTP and login will appear to silently fail. Terminate TLS in front of Miniform (the bundled `install.sh` does this with Caddy automatically). The default is `development`, which is safe for plain-HTTP local testing.

**Or use a .env file** (`.env`):
```bash
MINIFORM_ENV=production
MINIFORM_PORT=8080
MINIFORM_SESSION_SECRET=your-secret-here
MINIFORM_LOG_LEVEL=info
MINIFORM_DATA_DIR=./storage
```

### Building from Source

1. Clone the repository
2. Build:
   ```bash
   make build
   ```
3. Generate and save your session secret, then run:
   ```bash
   # Generate once and save this value
   export MINIFORM_SESSION_SECRET=$(openssl rand -hex 32)
   echo "Save this secret: $MINIFORM_SESSION_SECRET"
   
   ./bin/miniform
   ```

## Releases

Miniform uses semantic versioning. OCI images are published to GitHub Container Registry when version tags are pushed.

### Docker Images

```bash
# Latest stable release
docker pull ghcr.io/matteodante/miniform:latest

# Specific version
docker pull ghcr.io/matteodante/miniform:v1.0.0

# Major version (receives minor + patch updates)
docker pull ghcr.io/matteodante/miniform:v1
```

**Note:** Docker images are published automatically via GitHub Actions when a version tag (e.g., `v1.0.0`) is pushed to the repository.

### Building from Source

If you prefer to run a native binary instead of Docker:

```bash
git clone https://github.com/matteodante/miniform.git
cd miniform
make build
export MINIFORM_SESSION_SECRET=$(openssl rand -hex 32)
./bin/miniform
```

**Supported platforms for building from source:**
- Linux (amd64, arm64)
- macOS (amd64, arm64)

## Architecture

Miniform follows a **Phoenix Context Architecture**, organizing code into bounded contexts with clear separation of concerns:

```
[Static Site] --> POST /forms/:slug/submit
                        |
                  [Miniform]
                   /    |    \
            [SQLite] [Jobs] [Admin UI]
                      |
               [Webhook/Email Dispatchers]
                      |
            [External Services/Mailgun]
```

### Key Components

- **HTTP Server** — Fiber-based cartridge wrapper handling public submissions and admin dashboard
- **Database Layer** — GORM + SQLite with WAL mode
- **Custom Write Retry Logic** — `dbtxn.WithRetry` ensures writes eventually succeed despite SQLite's single-writer constraint
- **Job System** — In-process dispatchers for asynchronous webhook and email delivery
- **Cartridge Context** — Request-scoped dependency injection providing type-safe access to logger, config, and database

### SQLite Write Handling

Due to SQLite's single-writer limitation, all write operations use a custom retry mechanism (`internal/pkg/dbtxn/retry.go`) that:
- Detects busy/locked database errors
- Retries with exponential backoff (up to 10 attempts)
- Adds jitter to prevent thundering herd issues
- Works alongside WAL mode, busy_timeout pragmas, and immediate transaction locks

This ensures writes eventually succeed even under concurrent load.

## Development

Build and run locally:
```bash
make build
make dev
```

Run tests:
```bash
make test
```

## Contributing

Contributions are welcome! Please open an issue first to discuss proposed changes, or submit a pull request for bug fixes and improvements.

## License

[MIT](LICENSE)
