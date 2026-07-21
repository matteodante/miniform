# Automation

All third-party actions are pinned to full commit SHAs. Dependabot tracks their upstream releases.

- `ci.yml`: formatting, generated assets, lint, Go tests, browser tests, secret and license audit, vulnerability scans, dependency review, and OCI health checks
- `codeql.yml`: CodeQL analysis on changes to `main`, pull requests, and a weekly schedule
- `release.yml`: stable semantic tags whose commits belong to `main`, draft GoReleaser raw binaries and archives, SBOMs, provenance, blocking per-architecture image scans, multi-architecture GHCR publication, then final release publication

Workflow tokens default to read-only and receive write permissions only in the publishing jobs that need them. Repository rules and security settings that cannot be stored in Git are listed in `docs/open-source-checklist.md`.
