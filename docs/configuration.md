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
| `MINIFORM_SESSION_TIMEOUT_SECONDS` | `604800` | Set an explicit organizational policy if needed |
| `MINIFORM_MAX_INPUT_FIELDS` | `200` | Maximum scalar fields accepted in one submission |
| `MINIFORM_WEBHOOK_SIGNATURE_HEADER` | `X-Miniform-Signature` | Header used for outbound webhook signatures |
| `MINIFORM_WEBHOOK_RETRY_LIMIT` | `3` | Maximum configured webhook delivery attempts |
| `MINIFORM_WEBHOOK_BACKOFF_SCHEDULE` | `1,5,15,60` | Retry delays in seconds |

Miniform rejects unsupported log levels, invalid ports, non-positive limits, and malformed retry schedules at startup. It does not silently replace an invalid configured value with a default.

Generate secrets with:

```bash
openssl rand -hex 32
```

Changing the session secret signs out every user.

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

SMTP profiles, Turnstile credentials, forms, delivery destinations, submissions, and events are stored in SQLite. Manage them through the operator UI or the [CLI](cli.md). Protect backups accordingly.

Use `miniform config show` to inspect the effective runtime values. Change configuration in the process manager or container environment, then restart Miniform.
