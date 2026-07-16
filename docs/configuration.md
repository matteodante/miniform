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
| `MINIFORM_LOGS_DIR` | Under the data directory | Keep on persistent storage if logs are retained |
| `MINIFORM_SESSION_TIMEOUT_SECONDS` | Framework default | Set an explicit organizational policy if needed |

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

## Dashboard-managed settings

Mailer profiles, Turnstile profiles, allowed origins, rate limits, webhook credentials, and per-form delivery policies are stored in SQLite and managed through the operator UI. Protect backups accordingly.
