# Development

## Requirements

- Go 1.26.5
- A working C compiler for `go-sqlite3`
- Node.js 20 or newer and npm
- `make`
- Chromium dependencies for Playwright E2E tests

Optional tools: `watchexec`, `gotestsum`, Docker, Apple Container, `golangci-lint`, and `actionlint`.

On macOS, ensure command-line builds use the installed Xcode toolchain:

```bash
xcode-select --print-path
cc --version
```

## Bootstrap

```bash
cp .env.example .env
make bootstrap
```

`make bootstrap` downloads pinned frontend tooling, builds CSS, and installs Playwright dependencies. `.env`, caches, binaries, databases, and browser output are ignored.

## Common commands

```bash
make help
make run
make dev
make build
make test-unit
make test-e2e
make check
make clean
```

Run `make check` before opening a pull request. It verifies formatting, modules, generated assets, static analysis, and Go tests. User-facing flows also require `make test-e2e`.

## Test conventions

- Group scenarios under one top-level test using `t.Run`.
- Use table-driven tests for repeated input/output cases.
- Reuse `internal/pkg/testsupport` and in-memory SQLite.
- Use semantic Playwright locators and assert the final visible outcome.
- Keep E2E tests sequential unless their shared setup is redesigned.

## Frontend assets

Vendored JavaScript and Tailwind are pinned in `scripts/vendor.sh`. Run:

```bash
make css
```

Commit the generated `web/static/app.built.css` so normal Go and container builds do not download tools. CI verifies that regeneration produces no diff.

## Architecture

Read [architecture.md](architecture.md) before moving code across packages. Every SQLite mutation uses the retry transaction pattern. Keep handlers thin and domain ownership explicit.

## Repository skills

The `.agents/skills` directory mirrors project guidance for compatible coding agents. Update the source guidance and its corresponding skill together when conventions change.
