---
name: miniform-code-style
description: Apply Miniform's pragmatic Go coding style. Use when writing, refactoring, or reviewing Go code in this repository, especially naming, function shape, control flow, abstractions, logging, comments, and error context.
---

# Miniform Code Style

## Workflow

1. Read [AGENTS.md](../../../AGENTS.md) completely.
2. Inspect adjacent files and preserve established package vocabulary and APIs.
3. Implement the smallest clear solution, then remove dead branches and obsolete helpers created by the change.
4. Format modified Go files with `gofmt` and run focused tests.

## Rules

- Prefer intent-revealing names, early returns, shallow nesting, and one responsibility per function.
- Extract helpers only when they clarify a real repeated concept.
- Avoid boolean parameters, generic containers, speculative interfaces, and clever one-liners.
- Wrap errors with actionable operation context and `%w`.
- Use the logger already established by the package; do not introduce another logging stack.
- Comment why a non-obvious constraint exists, not what straightforward code does.
- Keep whitespace useful and public APIs intentionally small.

## Review Check

Confirm a reader can understand the happy path without tracing avoidable abstractions, and that every added type or helper earns its maintenance cost.
