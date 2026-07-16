---
name: miniform-testing
description: Write and run Miniform Go and Playwright tests using repository conventions. Use when adding regression coverage, changing test helpers, testing domain logic, HTTP handlers, SQLite behavior, integrations, or end-to-end admin and submission flows.
---

# Miniform Testing

## Workflow

1. Read [AGENTS.md](../../../AGENTS.md) and the original [testing guide](../../../.claude/skills/testing.md) completely.
2. Inspect neighboring tests and reuse `internal/pkg/testsupport` or existing E2E helpers.
3. Add the smallest regression test that would fail without the behavior under test.
4. Run the focused test first, then the appropriate broader suite.

## Go Test Rules

- Organize scenarios under one top-level test with `t.Run`; do not create one top-level function per scenario.
- Use table-driven subtests only for many cases sharing setup and assertion structure.
- Use independent `t.Run` blocks when setup or behavior differs.
- Use `require` for prerequisites and `assert` for non-fatal comparisons.
- Prefer in-memory SQLite and existing helpers; keep production mutations behind domain APIs and `dbtxn.WithRetry`.
- Silence logs unless output is part of the assertion.

## E2E Rules

- Keep browser tests under `e2e/` and use existing login, database, and cleanup helpers.
- Target semantic roles, labels, stable form actions, or stable attributes rather than layout-specific selectors.
- Assert the final URL and visible outcome; do not allow an error branch to count as success.
- Use unique slugs or names for records created through the UI.

## Verification Commands

Use repository Make targets when available. Otherwise run focused `go test` commands before `go test ./...`, and run the relevant Playwright spec before the full E2E suite.
