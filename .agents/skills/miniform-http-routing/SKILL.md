---
name: miniform-http-routing
description: Add and review Miniform HTTP routes and Cartridge/Fiber handlers. Use when changing internal/routes.go, admin or public endpoints, middleware, CORS, rate limits, redirects, parameters, rendering, authentication, or write-concurrency settings.
---

# Miniform HTTP Routing

## Workflow

1. Read [AGENTS.md](../../../AGENTS.md) and the original [HTTP routing guide](../../../.claude/skills/http-routing.md) completely.
2. Consult the current official Fiber and Cartridge documentation for APIs affected by the change.
3. Inspect `internal/routes.go`, the neighboring handler, and its domain function.
4. Choose the existing authenticated, public, or specialized route configuration; do not duplicate middleware stacks.
5. Implement a thin `*cartridge.Context` handler using context fields and methods instead of Fiber locals.
6. Register the route centrally and add focused route or handler coverage.

## Rules

- Apply session and password-change middleware to protected admin routes.
- Restrict public form routes with the established CORS, security, rate-limit, and write-concurrency policy.
- Parse and validate path, query, and form input before calling domain logic.
- Use stable status codes, redirects, JSON shapes, and `ContentView` rendering conventions.
- Enable write concurrency controls on routes that mutate SQLite.
- Keep compatibility paths unless the user explicitly approves a breaking API change.

## Verification

Test authentication, validation, success, failure mapping, and relevant method or middleware behavior. Run the focused HTTP tests and broader internal suite.
