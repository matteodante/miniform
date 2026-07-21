# Configuration

Miniform loads an optional `.env` file from its working directory, then reads `MINIFORM_*` variables. Values already present in the process environment take precedence. Copy [`.env.example`](../.env.example) for local use; `.env` is ignored by Git.

Development defaults also work without a configuration file. The built-in Matcha installer supplies its generated `PRIVATE_KEY` directly as the session secret when `MINIFORM_SESSION_SECRET` is absent; this is an internal deployment contract, not an additional user setting.

## Core settings

| Variable | Default | Production guidance |
| --- | --- | --- |
| `MINIFORM_ENV` | `development` | Set to `production`; requires HTTPS |
| `MINIFORM_PORT` | `8080` | Bind behind a reverse proxy |
| `MINIFORM_SESSION_SECRET` | Random value per development process | Required; stable random 32-byte value |
| `MINIFORM_LOG_LEVEL` | `info` outside production; `error` in production | `debug`, `info`, `warn`/`warning`, or `error` |
| `MINIFORM_DATA_DIR` | `./storage` | Mount persistent, access-controlled storage |
| `MINIFORM_DATABASE_FILENAME` | `miniform.db` | Filename only; use `MINIFORM_DATABASE_PATH` for a path |
| `MINIFORM_DATABASE_PATH` | Derived from data directory, environment, and filename | Use only when an explicit SQLite path is required |
| `MINIFORM_LOGS_DIR` | Under the data directory | Keep on persistent storage if logs are retained |
| `MINIFORM_SESSION_TIMEOUT_SECONDS` | `604800`; OCI image: `1800` | Set an explicit organizational policy if needed |
| `MINIFORM_MAX_INPUT_FIELDS` | `200` | Maximum scalar fields accepted in one submission |
| `MINIFORM_MAX_PAYLOAD_BYTES` | `65536` | Maximum combined scalar data; request bodies are capped separately at 6 MiB |
| `MINIFORM_MAX_UPLOAD_STORAGE_BYTES` | `1073741824` | Global stored-upload quota; rejected atomically before database commit |
| `MINIFORM_REQUEST_CONCURRENCY` | `32` | Maximum requests processed concurrently by the HTTP server |
| `MINIFORM_PUBLIC_GLOBAL_RATE_LIMIT` | `120` | Maximum public submissions per minute across all clients and forms |
| `MINIFORM_WEBHOOK_SIGNATURE_HEADER` | `X-Miniform-Signature` | Header used for outbound webhook signatures |
| `MINIFORM_WEBHOOK_RETRY_LIMIT` | `3` | Maximum configured webhook and SMTP delivery attempts |
| `MINIFORM_WEBHOOK_BACKOFF_SCHEDULE` | `1,5,15,60` | Webhook and SMTP retry delays in seconds |

Miniform rejects unsupported log levels, invalid ports, non-positive limits, and malformed retry schedules at startup. It does not silently replace an invalid configured value with a default.

Generate secrets with:

```bash
openssl rand -hex 32
```

Production rejects session secrets shorter than 32 characters. The cookie uses the `__Host-` prefix, `Secure`, `HttpOnly`, and `SameSite=Lax`. Only SHA-256 hashes of active tokens are retained server-side; logout removes the current token and changing an account email or password removes every active session. Changing the session secret also signs out every user.

## Email notifications

Mailer profiles contain the SMTP connection and sender identity. Each form owns one or more independent notifications. Every notification selects a mailer profile, static recipients or one recipient field, an optional static or field-derived `Reply-To`, a subject, and either plain text or HTML output. HTML uses Go's context-aware escaping and always includes the configured plain-text alternative.

Recipient lists accept comma-separated addresses in the admin UI or CLI. The admin UI also accepts one address per line. Miniform validates and deduplicates the list before storing it, then sends one SMTP `RCPT TO` command per recipient in a single message transaction.

Each enabled notification creates its own durable event. Notifications use the same expiring lease, bounded retry limit, and backoff schedule as webhooks, but one notification failing does not overwrite another notification's status. Changing a notification affects queued attempts that have not yet been sent. Disabling it prevents new events and causes an already queued event to finish as failed rather than silently disappear.

Submitted values may enter the body through escaped templates and may supply a recipient or `Reply-To` only through an explicitly selected field. An enabled field-derived recipient requires Turnstile on that endpoint; startup disables an unsafe legacy notification instead of retaining an open mail relay. Dynamic addresses are parsed as single RFC 5322 mailboxes, and rendered subjects containing newline or null bytes are rejected before SMTP. See [Email notifications](email-notifications.md) for the complete data contract and CLI examples.

The authenticated notification editor can preview unsaved settings against a recent submission. It uses the production renderer in a sandboxed iframe, marks the response `no-store`, and performs no SMTP or database write.

## Network boundaries

Production applies process-local limits of 30 public submissions per minute per resolved client plus 120 submissions per minute globally. Sign-in permits 5 attempts per minute per client plus 30 globally. The counters reset when the process restarts and are disabled in development and test environments. The HTTP server also caps each body at 6 MiB and concurrent request processing at 32 by default.

Direct and unmanaged deployments ignore forwarding headers and use the network peer address. A Matcha-managed production deployment trusts only the last `X-Forwarded-For` address appended by its proxy. Railway deployments use Railway's `X-Real-IP` contract. Keep the trusted proxy hop private and do not expose the application port directly.

Production responses enable HSTS. Every environment emits a nonce-based Content Security Policy, clickjacking, MIME-sniffing, referrer, and permissions defenses. Administrative responses are marked `no-store`.

## Example production environment

For a local environment file:

```bash
cp .env.example .env
```

Edit `.env`, or provide the same values through the process manager:

```bash
export MINIFORM_ENV=production
export MINIFORM_PORT=8080
export MINIFORM_SESSION_SECRET='replace-with-a-secret-manager-value'
export MINIFORM_LOG_LEVEL=info
export MINIFORM_DATA_DIR=/app/storage
export MINIFORM_DATABASE_FILENAME=miniform.db
```

Do not store production secrets in a repository, container image, shell history, support ticket, or public log.

## Stored application data

SMTP profiles, Turnstile credentials, forms, delivery destinations, submissions, and events are stored in SQLite. Uploaded files live beneath the data directory and are referenced by database rows. Manage records through the operator UI or the [CLI](cli.md); a complete point-in-time backup requires the stopped database and upload tree together.

Use `miniform config show` to inspect the effective runtime values. Change configuration in the process manager or container environment, then restart Miniform.
