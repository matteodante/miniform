# Architecture

Miniform is a single Go process with embedded web assets and SQLite storage. It favors explicit domain boundaries over services or a plugin framework.

```text
HTML form -> public HTTP route -> validation -> SQLite submission
                                                |
                                                +-> webhook jobs
                                                +-> SMTP/Mailgun jobs

Operator browser -> authenticated admin routes -> inbox and configuration
```

## Domain ownership

- `internal/accounts`: users, authentication rules, and account settings
- `internal/forms`: form definitions, origins, files, and submissions
- `internal/integrations`: mailer and captcha configuration
- `internal/jobs`: asynchronous webhook and email delivery
- `internal/http`: transport parsing and response rendering
- `internal/auth`: session helpers
- `internal/config`: application configuration
- `internal/database`: connection setup, migrations, and development seed data
- `internal/middleware`: cross-cutting HTTP controls
- `internal/server`: server-specific rendering and error behavior
- `internal/pkg`: shared code with demonstrated cross-domain reuse

Handlers parse transport input, call the owning domain package, and map results to HTTP. A domain must not reach into another domain's tables when an explicit domain API can own the operation.

## Request context

`internal/pkg/cartridge.Context`-style dependencies are provided through the Cartridge request context. Handlers use context fields and `ctx.DB()` rather than storing application dependencies in arbitrary Fiber locals.

## SQLite write model

SQLite permits one writer at a time. Every mutation must use `internal/pkg/dbtxn.WithRetry`, which combines immediate transactions, bounded exponential backoff, and jitter. The database runs in WAL mode with busy timeouts. Raw `Create`, `Save`, `Update`, or `Delete` calls outside the retry transaction are not accepted in production code.

## Time handling

Miniform stores and exchanges instants canonically in UTC. SQLite compares the canonical values directly and uses timestamp indexes for inbox and delivery-queue queries. API and webhook timestamps use RFC 3339 UTC. The operator UI converts those instants to the browser's IANA timezone and exposes the active timezone in the header and timestamp tooltip. Durations use Go's monotonic clock and are not converted. A future feature based on civil time or recurrence must store the originating IANA timezone separately; current domains only model absolute instants.

## Background processing

Webhook and email dispatchers run in process. Database rows are the durable queue and delivery history. `next_attempt_at` is the queue eligibility field: scheduled work has a UTC instant, while terminal events set it to `NULL`. Jobs must be idempotent where possible, use bounded retries, preserve error context, and never hold a database transaction open during network I/O.

## Assets and packaging

Templates and static assets are embedded with `go:embed`. The OCI image uses a multi-stage build and the same application binary. Runtime state belongs under the configured data directory; the application image is replaceable.

## Design constraints

- One process is the default deployment unit.
- SQLite is the authoritative store.
- Standard HTML form submission remains functional without JavaScript.
- No submission requires a Miniform-operated service.
- New shared abstractions need concrete reuse, not anticipated reuse.

Repository-specific implementation rules are also captured in [AGENTS.md](../AGENTS.md) and `.agents/skills`.
