---
name: miniform-background-jobs
description: Implement and review Miniform background processing for email and webhook delivery. Use when changing dispatchers, processors, event states, batch selection, retry schedules, idempotency, delivery history, or job-related database updates under internal/jobs.
---

# Miniform Background Jobs

## Workflow

1. Read [AGENTS.md](../../../AGENTS.md) and the original [background-jobs guide](../../../.claude/skills/background-jobs.md) completely.
2. Inspect `internal/jobs` and the owning forms or integrations models before editing; follow the current state constants and context types.
3. Define explicit state transitions and make repeated processing safe.
4. Select bounded batches in deterministic order and honor retry eligibility timestamps.
5. Keep external delivery attempts observable through status, attempt count, error, and next-attempt fields.
6. Perform every state mutation with `dbtxn.WithRetry`; use `$miniform-sqlite-writes` for detailed write rules.

## Reliability Rules

- Preserve `pending → delivering → delivered`, with retry and terminal failure paths where supported.
- Use the configured backoff and retry limit; do not add an independent retry loop.
- Use UTC timestamps and propagate cancellation through current job contexts.
- Avoid holding a database transaction open during network I/O.
- Make updates conditional when needed to prevent two workers from claiming the same event.
- Log delivery failures with event identity and attempt context without exposing secrets.

## Verification

Add subtests for success, retry, terminal failure, idempotent reprocessing, and invalid state where relevant. Run focused job tests and the full internal Go suite.
