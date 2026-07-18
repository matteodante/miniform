# Architecture

Miniform is a single Go process with embedded web assets and SQLite storage. It favors explicit domain boundaries over services or a plugin framework.

```text
HTML form -> public HTTP route -> validation -> upload staging -> SQLite submission
                                                                   |
                                                                   +-> webhook queue
                                                                   +-> SMTP queue

Operator browser -> authenticated admin routes -> inbox and configuration
```

## Domain ownership

- `internal/accounts`: operator identity and authentication rules
- `internal/forms`: form definitions, origins, files, and submissions
- `internal/integrations`: SMTP mailer profiles and Turnstile credentials
- `internal/jobs`: asynchronous webhook and email delivery
- `internal/http`: transport parsing, authentication handlers, and response rendering
- `internal/config`: application configuration
- `internal/database`: connection setup, migrations, and development seed data
- `internal/server`: server setup, middleware, rendering, and error behavior
- `internal/pkg`: shared code with demonstrated cross-domain reuse

Handlers parse transport input, call the owning domain package, and map results to HTTP. A domain must not reach into another domain's tables when an explicit domain API can own the operation.

## Application lifecycle

`internal.NewApp` owns the pre-bound listener, structured logger, SQLite manager, HTTP server, session manager, request-cancellation root, and background runner. Startup fails before serving if the port, storage, logger, database, server, migrations, or initial account cannot be prepared.

Shutdown is ordered: cancel jobs and request contexts, stop accepting HTTP work, wait within the environment-specific deadline, checkpoint the WAL, close the database, close the logger, and restore the previous process logger. `App.Close` and the database manager are idempotent so partial startup and repeated cleanup do not leak or double-close resources. New goroutines must accept cancellation and have an owner that waits for completion.

## Request context

Handlers receive `cartridge.Context`, which exposes the logger, configuration, database manager, session manager, and Fiber request. They use context fields and `ctx.DB()` rather than storing application dependencies in arbitrary Fiber locals. The application attaches a cancellable Go context to every request so database work and synchronous Turnstile verification stop during shutdown. The background runner separately propagates its application context into webhook and SMTP delivery.

## SQLite write model

SQLite permits one writer at a time. `internal/database.Manager` owns one bounded connection pool configured with WAL, foreign keys, busy timeout, normal synchronization, and immediate write transactions. Every mutation must use `internal/pkg/dbtxn.WithRetry`, which combines immediate transactions, context cancellation, bounded exponential backoff, and jitter. Raw `Create`, `Save`, `Update`, or `Delete` calls outside the retry transaction are not accepted in production code.

Migrations are idempotent compatibility operations. They preserve supported legacy records and relationships before recording completion. Startup recovers interrupted upload operations only after migrations succeed, then checkpoints the WAL before serving.

## Time handling

Miniform stores and exchanges instants canonically in UTC. SQLite compares the canonical values directly and uses timestamp indexes for inbox and delivery-queue queries. API and webhook timestamps use RFC 3339 UTC. The operator UI converts those instants to the browser's IANA timezone and exposes the active timezone in the header and timestamp tooltip. Durations use Go's monotonic clock and are not converted. A future feature based on civil time or recurrence must store the originating IANA timezone separately; current domains only model absolute instants.

## Background processing

Webhook and email dispatchers run in process under the application context. Database rows are the durable queue and delivery history. `next_attempt_at` is both the queue eligibility field and, while delivering, an expiring lease. A worker claims one row transactionally, performs network I/O outside the transaction, and updates only the lease it still owns. Expired work is reclaimable; terminal events set `next_attempt_at` to `NULL`.

Webhook requests carry a stable idempotency key. SMTP and webhook calls honor cancellation, use bounded attempts, compact stored errors, and never keep a database transaction open during network I/O. A manual retry resets the durable event; delivery still requires a running server process.

## Upload lifecycle

Incoming files are streamed into a randomly named `.upload-staging` operation beneath the configured data root. Database rows are committed before files are promoted to their final `uploads` paths. Submission and form deletion first quarantine files under `.upload-deletions`, then commit database removal, then remove unreferenced files. A failed transaction restores quarantined files.

All paths are resolved beneath an `os.Root`; absolute paths, traversal, and symlink escape are rejected. Startup reconciles both staging roots against database references, which makes interrupted create and delete operations recoverable without guessing whether the database committed.

## HTTP and browser lifecycle

Public submissions require a valid endpoint token and allowed origin, enforce scalar and file limits, and optionally require Turnstile. Honeypot-triggered requests are persisted as spam without uploads or delivery jobs; legitimate submissions require at least one field or file. Public CORS permits only `POST`, `OPTIONS`, and `Content-Type`. Production rate limits are process-local and keyed from the direct peer except in a Matcha-managed deployment, where Miniform accepts only the last address appended by that trusted proxy.

Native HTML forms are the canonical public client and Miniform ships no browser submission library. Integrations that need inline behavior use the documented HTTP contract and own their pending, redirect, and error states. They must send one request and must not retry an ambiguous network failure automatically. HTMX acceleration is limited to the authenticated operator UI and does not cache authenticated pages; legacy history is cleared at bootstrap and after swaps. Browser errors either redirect to a working admin page or render a visible error document.

## Managed deployment lifecycle

The management binary serializes deployment commands with a host lock. Update pulls before downtime, stops the active container, snapshots the final stopped database, and starts exactly one replacement. Restore validates and stages a snapshot before stopping, saves the previous database when valid, replaces atomically, and either starts the restored deployment or performs a compensating rollback. Uploaded files are outside the database snapshot and require a stopped full-storage copy for point-in-time disaster recovery.

## Assets and packaging

Templates and an explicit allowlist of static assets are embedded with `go:embed`; development-only Fiber compression sidecars are neither generated nor packaged. The OCI image uses a multi-stage build and the same application binary. Runtime state belongs under the configured data directory; the application image is replaceable.

## Design constraints

- One process is the default deployment unit.
- SQLite is the authoritative store.
- Database rows decide whether interrupted upload staging is promoted, restored, or removed.
- Standard HTML form submission remains functional without JavaScript.
- Public submissions have one documented HTML and HTTP contract; no first-party client wrapper duplicates it.
- No submission requires a Miniform-operated service.
- Unattended image or manager updates are not enabled.
- New shared abstractions need concrete reuse, not anticipated reuse.

Repository-specific implementation rules are also captured in [AGENTS.md](../AGENTS.md) and `.agents/skills`.
