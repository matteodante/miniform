---
name: miniform-sqlite-writes
description: Protect Miniform SQLite mutations with the repository retry transaction pattern. Use whenever creating, updating, deleting, upserting, or transactionally modifying records with GORM, or when reviewing database writes for lock safety.
---

# Miniform SQLite Writes

## Required Pattern

1. Read [AGENTS.md](../../../AGENTS.md) and the original [SQLite retry guide](../../../.claude/skills/sqlite-write-retries.md) completely.
2. Inspect `internal/pkg/dbtxn/retry.go` before changing retry behavior.
3. Wrap every application write in one `dbtxn.WithRetry(logger, db, func(tx *gorm.DB) error { ... })` call.
4. Use only the provided `tx` for all operations that must be atomic.

```go
err := dbtxn.WithRetry(logger, db, func(tx *gorm.DB) error {
    return tx.Create(&record).Error
})
```

## Rules

- Never call application-level `db.Create`, `db.Save`, `db.Updates`, or `db.Delete` directly.
- Group related writes in the same retry closure instead of nesting retry calls.
- Return the original database error from the closure so lock detection remains effective.
- Do not add sleeps or a second retry mechanism around `WithRetry`.
- Keep network calls and other irreversible side effects outside retried transactions.
- Read-only queries and the dedicated migration path do not need this wrapper.

## Verification

Search the changed code for direct GORM mutations, add contention or rollback coverage when relevant, and run the owning package tests.
