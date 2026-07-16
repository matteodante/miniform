# Automation

All third-party actions are pinned to full commit SHAs. Dependabot tracks their upstream releases.

- `ci.yml`: formatting, generated assets, lint, Go tests, browser tests, secret and license audit, vulnerability scans, dependency review, and OCI health checks
- `codeql.yml`: CodeQL analysis on changes to `main`, pull requests, and a weekly schedule
- `release.yml`: validated semantic tags, draft GoReleaser artifacts, SBOMs, provenance, blocking image scan, multi-architecture GHCR publication, then final release publication

Workflow tokens default to read-only and receive write permissions only in the publishing jobs that need them. Repository rules and security settings that cannot be stored in Git are listed in `docs/open-source-checklist.md`.
