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
├── database/        # Migrations and seed data
├── server/          # Server setup and error handling
└── pkg/             # Shared utilities (dbtxn, testsupport)
```

## Layer Responsibilities

- Models in `internal/{domain}/models.go` describe only persisted domain data.
- Domain functions validate inputs and own database reads and retry-wrapped writes.
- Handlers in `internal/http` parse transport input, call domain functions, and render responses.
- Routes in `internal/routes.go` connect handlers and middleware.

## Key Principles

1. **Domain isolation**: Each domain (forms, accounts) owns its models and business logic
2. **Transaction handling**: Business layer handles transactions with retry logic
3. **Thin handlers**: HTTP handlers only parse input and call business functions
4. **Preloading**: Explicitly preload associations when needed
5. **Error types**: Each domain defines its own error types
6. **YAGNI**: Add shared abstractions only after a second concrete use case exists
