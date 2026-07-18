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
make bootstrap
```

`make bootstrap` downloads pinned frontend tooling, builds CSS, and installs Playwright dependencies. Caches, binaries, databases, and browser output are ignored. Set `MINIFORM_*` environment variables in the process that starts Miniform only when overriding the development defaults.

## Common commands

```bash
make help
make run
make dev
make build
make test-unit
make test-race
make test-stress
make test-e2e
make check
make audit
make clean
```

Run `make check` before opening a pull request. It verifies formatting, modules, generated assets, static analysis, shell scripts, workflows, dead code, and Go tests. User-facing flows also require `make test-e2e`; concurrency or lifecycle changes require `make test-race`; dependency, release, or security changes require `make audit`.

`make test-stress` builds and starts a separate Miniform process against temporary file-backed SQLite storage. It mixes native, JSON, multipart, invalid-token, and webhook submissions; interrupts one delivery during shutdown; restarts the same storage; and verifies database, upload, lease, idempotency, WAL, CPU, and RSS outcomes. The default profile is 500 requests at concurrency 16:

```bash
make test-stress
STRESS_REQUESTS=2000 STRESS_CONCURRENCY=32 make test-stress
```

Resource budgets default to 128 MiB idle RSS, 256 MiB peak RSS, 10% idle CPU, and 64 MiB post-load RSS growth. Calibrate them without changing the test by setting `MINIFORM_STRESS_MAX_IDLE_RSS_MB`, `MINIFORM_STRESS_MAX_PEAK_RSS_MB`, `MINIFORM_STRESS_MAX_IDLE_CPU_PERCENT`, `MINIFORM_STRESS_MAX_POST_RSS_GROWTH_MB`, or `MINIFORM_STRESS_IDLE_SECONDS`.

## Test conventions

- Group scenarios under one top-level test using `t.Run`.
- Use table-driven tests for repeated input/output cases.
- Reuse `internal/pkg/testsupport` and in-memory SQLite.
- Use semantic Playwright locators and assert the final visible outcome.
- Keep E2E tests sequential unless their shared setup is redesigned.
- Let Playwright create and own its temporary data directory. Set `MINIFORM_E2E_DATA_DIR` only when the directory must survive teardown.

## Frontend assets

Vendored JavaScript and Tailwind are pinned in `scripts/vendor.sh`. Run:

```bash
make css
```

Commit the generated `web/static/app.built.css` so normal Go and container builds do not download tools. CI verifies that regeneration produces no diff.

## Architecture

Read [architecture.md](architecture.md) before moving code across packages. Every SQLite mutation uses the retry transaction pattern. Keep handlers thin and domain ownership explicit. Preserve application shutdown order, delivery leases, upload staging/quarantine, and compensating deployment rollback when changing lifecycle code.

## Repository skills

The `.agents/skills` directory mirrors project guidance for compatible coding agents. Update the source guidance and its corresponding skill together when conventions change.
