# Configuration

Miniform reads environment variables prefixed with `MINIFORM_` and an optional local `.env` file. Environment variables take precedence. Copy `.env.example` for development; never commit `.env`.

## Core settings

| Variable | Default | Production guidance |
| --- | --- | --- |
| `MINIFORM_ENV` | `development` | Set to `production`; requires HTTPS |
| `MINIFORM_PORT` | `8080` | Bind behind a reverse proxy |
| `MINIFORM_SESSION_SECRET` | Development-only fallback | Required; stable random 32-byte value |
| `MINIFORM_ANON_SALT` | Runtime-generated fallback | Set a stable random 32-byte value for consistent IP hashing |
| `MINIFORM_LOG_LEVEL` | `error` | Use `info` unless diagnosing a problem |
| `MINIFORM_DATA_DIR` | `./storage` | Mount persistent, access-controlled storage |
| `MINIFORM_DATABASE_FILENAME` | `miniform.db` | Change only before first deployment |
| `MINIFORM_DATABASE_PATH` | Derived from data directory, environment, and filename | Use only when an explicit SQLite path is required |
| `MINIFORM_LOGS_DIR` | Under the data directory | Keep on persistent storage if logs are retained |
| `MINIFORM_SESSION_TIMEOUT_SECONDS` | Framework default | Set an explicit organizational policy if needed |
| `MINIFORM_MAX_INPUT_FIELDS` | `200` | Maximum scalar fields accepted in one submission |
| `MINIFORM_WEBHOOK_SIGNATURE_HEADER` | `X-Miniform-Signature` | Header used for outbound webhook signatures |
| `MINIFORM_WEBHOOK_RETRY_LIMIT` | `3` | Maximum configured webhook delivery attempts |
| `MINIFORM_WEBHOOK_BACKOFF_SCHEDULE` | `1,5,15,60` | Retry delays in seconds |

Generate secrets with:

```bash
openssl rand -hex 32
```

Changing the session secret signs out every user. Changing the anonymization salt changes future IP hashes and prevents comparison with older values.

## Example production environment

```dotenv
MINIFORM_ENV=production
MINIFORM_PORT=8080
MINIFORM_SESSION_SECRET=replace-with-a-secret-manager-value
MINIFORM_ANON_SALT=replace-with-a-different-secret-manager-value
MINIFORM_LOG_LEVEL=info
MINIFORM_DATA_DIR=/app/storage
MINIFORM_DATABASE_FILENAME=miniform.db
```

Do not store production secrets in a repository, container image, shell history, support ticket, or public log.

## Dashboard and CLI-managed settings

Mailer profiles, Turnstile profiles, allowed origins, webhook credentials, submissions, and per-form delivery policies are stored in SQLite. Manage them through the operator UI or the [CLI](cli.md). Protect backups accordingly.

Use `miniform config show` to inspect the resolved paths and runtime values. `miniform config set` and `config unset` update a local dotenv file; restart the server afterward. In container deployments, change the container environment instead of an ephemeral file inside the image.
