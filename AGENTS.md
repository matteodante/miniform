# Codex AI Assistant Instructions for Miniform

## Project Overview

Miniform is a self-hosted form inbox written in Go. It uses small domain packages with clear ownership and dependency boundaries.

## Project Skills

Project-specific skills live in `.agents/skills`. Load every skill that matches the task before making changes:

| Task | Required skill |
| --- | --- |
| Domain packages, ownership boundaries, routes, cross-domain dependencies, application lifecycle, goroutines, uploads, or managed deployment | `miniform-architecture` |
| Background jobs, retries, leases, or delivery state | `miniform-background-jobs` |
| Operating a Miniform instance through its CLI | `miniform-cli` |
| Writing, refactoring, or reviewing Go code | `miniform-code-style` |
| Domain errors, HTTP error mapping, or error logging | `miniform-error-handling` |
| Fiber/Cartridge handlers, routes, middleware, or CORS | `miniform-http-routing` |
| Any GORM or SQLite mutation | `miniform-sqlite-writes` |
| Go or Playwright tests and test helpers | `miniform-testing` |

Use all applicable skills when a task crosses multiple areas. Treat `.agents/skills` as the canonical source; compatibility directories must link to it rather than duplicate its contents.

## Architecture Patterns

### Domain package architecture

Code is organized into contexts (bounded domains):
- **accounts** — User management
- **forms** — Form definitions and submissions
- **integrations** — External services (SMTP, Turnstile)
- **jobs** — Background processing
- **http** — HTTP handlers

Each context owns its domain logic and data access. Avoid cross-context direct database access.

### Cartridge Context Pattern

Handlers receive `cartridge.Context`, which embeds the Fiber context and exposes the logger, configuration, database manager, and session manager.

**Important:** Access dependencies via fields, not `fiber.Ctx.Locals()`. Use `ctx.DB()` for database access.

### Application Lifecycle

`internal.App` owns the listener, logger, database manager, HTTP server, request cancellation, and background runner. Preserve the shutdown order: cancel jobs and requests, stop HTTP, wait for workers, checkpoint WAL, then close database and logger resources.

- Cleanup methods must be idempotent.
- Every goroutine must have cancellation and an owner that waits for it.
- Do not open a second application database or start a replacement process before the previous owner stops.
- Propagate request or job contexts into database and network operations.

### SQLite Write Handling

SQLite only allows **one writer** at a time. Always wrap write operations with `dbtxn.WithRetry`:

```go
import "github.com/matteodante/miniform/internal/pkg/dbtxn"

err := dbtxn.WithRetry(logger, db, func(tx *gorm.DB) error {
    return tx.Create(&record).Error
})
```

This handles:
- Busy/locked database errors
- Automatic retries with exponential backoff
- Jittered delays to prevent thundering herd

**Never** use raw `db.Create()` or `db.Save()` for writes without the retry wrapper.

## Code Style

- Use structured logging with `slog.Logger`
- Return errors, don't panic
- Prefer explicit over clever
- Comment only when clarification is needed
- Use Go formatting conventions (`gofmt`)

## Database Patterns

- GORM for ORM layer
- SQLite with WAL mode
- Store timestamps canonically in UTC and compare SQLite time columns directly
- Emit RFC 3339 UTC timestamps and localize only at the browser boundary
- Transactions with immediate locks (`_txlock=immediate`)
- All writes via `dbtxn.WithRetry`
- Migrations in `internal/database/migrate.go`
- Connection lifecycle and WAL checkpoints in `internal/database/manager.go`

## Upload Patterns

- Stream new files through `.upload-staging`; promote them only after the database commit.
- Quarantine files under `.upload-deletions` before deleting their rows; restore them if the transaction fails.
- Resolve stored paths beneath `os.Root`. Reject absolute paths, traversal, and symlink escape.
- Keep startup recovery in `forms.RecoverUploadDeletions` aligned with create and delete choreography.

## Background Job Patterns

- Database event rows are the durable queue.
- Claim work with an expiring lease before network I/O and update only the lease still owned by that worker.
- Never hold a SQLite transaction during SMTP or HTTP calls.
- Honor context cancellation, use bounded retries, and make webhook delivery externally idempotent.

## Testing

- **Always use `t.Run()` for test scenarios** — Never use separate top-level test functions per scenario
- Use `internal/pkg/testsupport` helpers
- In-memory SQLite for unit tests
- E2E tests in `e2e/` directory

### Test Patterns

**Table-driven tests** — Use when you have many similar cases with the same structure:

```go
func TestExtractDomain(t *testing.T) {
    tests := []struct {
        name     string
        input    string
        expected string
    }{
        {"https URL", "https://example.com", "example.com"},
        {"with port", "https://example.com:8080", "example.com"},
        {"with path", "https://example.com/path", "example.com"},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            assert.Equal(t, tt.expected, extractDomain(tt.input))
        })
    }
}
```

**Independent `t.Run()` blocks** — Use when cases have different setup, logic, or assertions:

```go
func TestUserService(t *testing.T) {
    t.Run("creates user with valid data", func(t *testing.T) {
        db := setupTestDB(t)
        svc := NewUserService(db)

        user, err := svc.Create("test@example.com")
        require.NoError(t, err)
        assert.NotEmpty(t, user.ID)
    })

    t.Run("rejects duplicate email", func(t *testing.T) {
        db := setupTestDB(t)
        svc := NewUserService(db)
        svc.Create("test@example.com")

        _, err := svc.Create("test@example.com")
        assert.ErrorIs(t, err, ErrDuplicateEmail)
    })
}
```

**Do NOT** use separate top-level functions per scenario:

```go
// ❌ Wrong
func TestUserService_CreatesUser(t *testing.T) { ... }
func TestUserService_RejectsDuplicate(t *testing.T) { ... }

// ✅ Correct - use t.Run() inside one function
func TestUserService(t *testing.T) {
    t.Run("creates user", func(t *testing.T) { ... })
    t.Run("rejects duplicate", func(t *testing.T) { ... })
}
```

## Common Tasks

### Adding a New Context

1. Create package under `internal/`
2. Define models (if needed)
3. Implement business logic with public API
4. Add tests
5. Update routing in `internal/routes.go`

### Adding a Database Write

Always use retry wrapper:

```go
err := dbtxn.WithRetry(ctx.Logger, db, func(tx *gorm.DB) error {
    // Your write operations here
    return tx.Create(&model).Error
})
```

### Adding a New Handler

```go
func HandleSomething(ctx *cartridge.Context) error {
    result, err := forms.List(ctx.DB())
    if err != nil {
        return err
    }

    return ctx.JSON(result)
}
```

Register in `internal/routes.go`.

## Project Structure

```
internal/
├── accounts/        # User context
├── cli/             # Administrative CLI
├── config/         # Configuration
├── database/       # Connection lifecycle, migrations, and seed data
├── forms/          # Forms context
├── http/           # HTTP handlers
├── integrations/   # External services
├── jobs/           # Background jobs
├── server/          # Logging, rendering, middleware, and error behavior
└── pkg/
    ├── dbtxn/      # Transaction helpers
    ├── sqliteerr/  # SQLite error classification
    └── testsupport/ # Shared test setup
```

## Key Files

- `internal/app.go` — Application bootstrap
- `internal/routes.go` — Route definitions
- `internal/database/manager.go` — SQLite connection ownership and checkpoints
- `internal/database/migrate.go` — Database schema
- `internal/forms/files.go` — Upload staging and promotion
- `internal/forms/upload_deletions.go` — Upload quarantine and recovery
- `internal/jobs/runner.go` — Cancellable background runner
- `internal/jobs/retry.go` — Delivery claims, leases, and retry state
- `internal/pkg/dbtxn/retry.go` — Write retry logic
- `internal/server/logger.go` — Structured log lifecycle
- `internal/server/server.go` — Rendering, client IP, and error behavior

## License

MIT — This is an open-source project.
