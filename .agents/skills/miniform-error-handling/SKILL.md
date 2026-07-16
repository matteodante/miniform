---
name: miniform-error-handling
description: Design consistent Miniform domain and HTTP error flows. Use when adding sentinel or typed errors, wrapping failures, mapping errors to Fiber responses, changing the global error handler, logging failures, or reviewing information exposure.
---

# Miniform Error Handling

## Workflow

1. Read [AGENTS.md](../../../AGENTS.md) and the original [error-handling guide](../../../.claude/skills/error-handling.md) completely.
2. Inspect current domain errors and `internal/server` handling before defining a new error.
3. Return errors from the owning domain with enough operation context for diagnosis.
4. Map known domain outcomes at the HTTP boundary; leave unexpected failures to the global handler.
5. Test both the error identity and the user-visible status or payload.

## Rules

- Use sentinel errors for stable categories and typed errors when structured context matters.
- Preserve identity through `fmt.Errorf("operation: %w", err)` and check with `errors.Is` or `errors.As`.
- Use Fiber errors for standard HTTP outcomes when no richer response is required.
- Log unexpected failures once with request or entity context; avoid duplicate logging at every layer.
- Never log passwords, tokens, secrets, raw authorization headers, or sensitive submission payloads.
- Return stable public messages; do not expose internal database or stack details to clients.

## Verification

Add focused subtests for known mappings, wrapped-error matching, JSON versus HTML behavior where applicable, and unexpected failures.
