# Architecture Pattern

## Overview

Miniform uses small domain packages inside one Go process.

## Directory Structure

```
internal/
├── forms/           # Forms domain (models + business logic)
├── accounts/        # User accounts domain
├── cli/             # Administrative CLI
├── integrations/    # Third-party integrations (SMTP, Turnstile)
├── jobs/            # Background job processors
├── http/            # HTTP handlers (cartridge-based)
├── config/          # Configuration
├── database/        # Connection lifecycle, migrations, and seed data
├── server/          # Logging, rendering, client IP, and error behavior
└── pkg/             # Shared utilities (dbtxn, sqliteerr, testsupport)
```

## Layer Responsibilities

- Models in `internal/{domain}/models.go` describe only persisted domain data.
- Domain functions validate inputs and own database reads and retry-wrapped writes.
- Handlers in `internal/http` parse transport input, call domain functions, and render responses.
- Routes in `internal/routes.go` connect handlers and middleware.
- `internal.App` owns the listener, logger, database manager, HTTP server, request cancellation, and background runner.
- `internal/forms` owns upload staging, promotion, deletion quarantine, and startup recovery.

## Key Principles

1. **Domain isolation**: Each domain (forms, accounts) owns its models and business logic
2. **Transaction handling**: Business layer handles transactions with retry logic
3. **Thin handlers**: HTTP handlers only parse input and call business functions
4. **Preloading**: Explicitly preload associations when needed
5. **Error types**: Each domain defines its own error types
6. **YAGNI**: Add shared abstractions only after a second concrete use case exists
7. **Lifecycle ownership**: Every goroutine and resource has cancellation, ordered shutdown, and an idempotent owner
8. **Single process**: Stop the previous database owner before starting a replacement
9. **Recoverable files**: Coordinate upload filesystem changes with committed database state
