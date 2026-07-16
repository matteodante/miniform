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

## Boundaries

- Keep accounts, forms, integrations, jobs, HTTP, auth, config, database, middleware, and server responsibilities separate.
- Avoid direct cross-domain database access when the owning package can expose the operation.
- Preload GORM associations explicitly when a use case needs them.
- Route every SQLite mutation through `$miniform-sqlite-writes`.
- Preserve the single-process deployment model unless the user explicitly requests an architectural change.

## Verification

Run focused package tests, then the broader Go suite. Confirm imports do not introduce cycles and the change remains in the smallest owning package.
