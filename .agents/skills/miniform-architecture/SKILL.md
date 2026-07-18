---
name: miniform-architecture
description: Apply Miniform's domain-package architecture and ownership boundaries. Use when adding, moving, or reviewing domain models, business logic, HTTP handlers, routes, database access, shared packages, or cross-domain dependencies in this repository.
---

# Miniform Architecture

## Workflow

1. Read [AGENTS.md](../../../AGENTS.md) and the original [architecture guide](../../../.claude/skills/architecture.md) completely.
2. Inspect the current neighboring implementation before choosing a package or API. Treat current code and `AGENTS.md` as authoritative when an older example differs.
3. Put models and business rules in the owning `internal/<domain>` package.
4. Keep `internal/http` handlers thin: parse transport input, call domain functions, map the result to HTTP.
5. Register routes centrally in `internal/routes.go`; do not hide domain access in unrelated packages.
6. Add shared code under `internal/pkg` only after concrete reuse exists.
7. Preserve application ownership of its listener, logger, database manager, HTTP server, request cancellation, and background runner.

## Boundaries

- Keep accounts, forms, integrations, jobs, HTTP, auth, config, database, middleware, and server responsibilities separate.
- Avoid direct cross-domain database access when the owning package can expose the operation.
- Preload GORM associations explicitly when a use case needs them.
- Route every SQLite mutation through `$miniform-sqlite-writes`.
- Preserve the single-process deployment model unless the user explicitly requests an architectural change.
- Give every goroutine a cancellation path and an owner that waits for it.
- Keep upload staging, database commit, promotion, deletion quarantine, and startup recovery as one lifecycle owned by `internal/forms`.
- Stop replacement processes before starting the next SQLite owner; managed deployment failures require compensating rollback.

## Verification

Run focused package tests, then the broader Go suite. Add `make test-race` for lifecycle or concurrency changes. Confirm imports do not introduce cycles and the change remains in the smallest owning package.
