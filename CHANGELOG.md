# Changelog

All notable changes to Miniform are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and releases follow [Semantic Versioning](https://semver.org/).

## [Unreleased]

## [0.2.0] - 2026-07-21

### Added

- Data-backed email previews for unsaved notification templates, addresses, subjects, HTML, and text fallbacks without SMTP delivery

### Changed

- Reused one domain renderer for both preview and background SMTP delivery

### Security

- Isolated rendered email HTML in a sandboxed iframe and prevented preview responses from being cached

## [0.1.0] - 2026-07-21

### Added

- Open-source governance, contribution, support, security, and release policies
- Reproducible quality, security, and release automation
- Apple Container development workflow
- Repository-scoped skills for architecture, testing, and maintenance conventions
- Deterministic administrative CLI for accounts, forms, submissions, files, delivery events, mailers, captcha profiles, and effective configuration
- Managed database backup, update, reload, and interactive restore workflows with serialized deployment operations and compensating rollback
- Explicit application, database, logger, request, and background-job lifecycle ownership with graceful shutdown and WAL checkpoints
- Crash-recoverable upload staging and deletion quarantine, including startup reconciliation against committed database state
- Browser acceptance coverage for onboarding, form management, native submissions, settings, error recovery, and logout history
- A complete submission guide for native HTML, `fetch`, JSON, file uploads, and `curl`
- An opt-in stress suite for HTTP ingestion, SQLite contention, uploads, webhook and SMTP delivery, transient email retries, graceful restart, and idle/load resource budgets
- A Railway deployment manifest and persistent-volume installation guidance
- Per-form plain-text or escaped HTML email notifications with text fallback and multiple SMTP recipients
- Multiple independent email notifications per form with static or field-derived recipients and Reply-To, configurable subjects and text/HTML templates, and per-notification delivery events

### Changed

- Rebranded the application and source tree as Miniform
- Removed commercial licensing gates while preserving self-hosted functionality
- Standardized transactional email delivery on SMTP
- Simplified configuration, email recipients, and Turnstile profiles while preserving supported legacy schemas and stored values through compatibility migrations
- Required replacement of the generated first-run password before the rest of the operator workspace becomes available
- Removed the first-party browser SDK and its stored preference, CLI flags, admin controls, asset route, and duplicate lifecycle; native HTML and direct HTTP are the only public submission contract
- Made the Apple Container development lifecycle use a writable persistent volume, migrate legacy project storage on first use, and fail health checks when the container is absent or stopped
- Removed the CLI form-list N+1 query by reusing the delivery records loaded by the domain list operation
- Removed client-side syntax highlighting, excluded Tailwind source from production binaries, and dropped unused container timezone data
- Restricted releases to stable `vMAJOR.MINOR.PATCH` tags whose commits belong to `main`; unattended deployment updates are disabled

### Fixed

- Preserved legacy users, forms, settings, mailers, captcha profiles, delivery references, origins, and canonical timestamps during migration
- Prevented multiple application processes, background workers, or deployment replacements from sharing one SQLite database during lifecycle transitions
- Added delivery claims with expiring leases and compare-and-set completion so webhook and SMTP work can be retried without stale workers overwriting state
- Made upload creation and deletion atomic with database outcomes and recoverable after interruption
- Rejected empty submissions while retaining honeypot-only spam handling and file-only submissions
- Re-rendered actionable admin validation and reference conflicts instead of returning plaintext or leaving dormant error branches
- Prevented HTMX from retaining authenticated pages after logout and made browser-visible `4xx` and `5xx` recovery deterministic
- Restored attachment downloads, repeated settings navigation, and Turnstile verification
- Hardened installer, backup, restore, release binary, SBOM, and image-tag lifecycles against partial or ambiguous completion
- Preserved SMTP cancellation as `context.Canceled` when connection teardown races with deadline or protocol I/O
- Made public rate-limit failures follow the same `{ok:false,error}` JSON contract as submission errors

### Security

- Prepared private vulnerability reporting, dependency scanning, secret scanning, and least-privilege workflows
- Enforced token, origin, captcha, honeypot, input-count, body-size, file, rate-limit, and trusted-proxy boundaries on public submissions and login
- Stopped secret form values and legacy HTMX history from being reflected or retained in browser state
- Rejected symlinked or non-normalized container storage paths before privileged ownership changes and restored safe ownership without following links
- Required an operator-installed, running Docker Engine instead of executing a mutable remote installer as root
- Added signed local release tags, release-source ancestry checks, checksums, SBOMs, provenance, and architecture-specific binary execution tests

[Unreleased]: https://github.com/matteodante/miniform/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/matteodante/miniform/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/matteodante/miniform/releases/tag/v0.1.0
