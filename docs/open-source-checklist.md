# Public launch verification

Verified for the initial `v0.1.0` publication on 2026-07-21. Repeat the relevant checks before future releases or repository-policy changes.

## Repository

- [x] Confirm the repository URL and `origin`
- [x] Confirm `main` is the default branch and remove obsolete branches and tags
- [x] Verify the full reachable Git history contains no secrets, private data, local paths, or obsolete product names
- [x] Confirm databases, logs, browser output, caches, binaries, and local secret files are untracked
- [x] Set the repository description and topics
- [x] Enable Issues and Discussions with Q&A, Ideas, and Show and tell categories

## Security and controls

- [x] Enable private vulnerability reporting
- [x] Enable secret scanning and push protection
- [x] Enable dependency graph, Dependabot alerts, and Dependabot security updates
- [x] Require actions to be pinned to full commit SHAs
- [x] Protect `main`: pull requests, resolved conversations, linear history, blocked force pushes and deletions
- [x] Require CI and CodeQL checks after their first successful run
- [x] Protect `v*` tags and require signed release tags
- [x] Review workflow token permissions and release-job permissions

## Project health

- [x] Verify GitHub recognizes LICENSE, README, CONTRIBUTING, CODE_OF_CONDUCT, SECURITY, and SUPPORT
- [x] Confirm issue forms, pull request template, discussion guidance, governance, and roadmap render correctly
- [x] Confirm the security reporting link works privately
- [x] Keep `FUNDING.yml` absent until a real funding destination exists

## Release readiness

- [x] Run `make audit`, `make check`, `make test-race`, `make test-e2e`, `make release-check`, and the OCI lifecycle tests
- [x] Validate the clean root tree on macOS and Linux
- [x] Validate Apple Container build, health check, persistence, and stop
- [x] Run the release workflow and inspect binaries, checksums, SBOMs, and licenses
- [x] Create and verify the first signed version tag
- [x] Confirm GHCR public visibility, immutable digest, provenance, and version labels
- [x] Test installation and upgrade instructions through the release integration suite
- [x] Confirm all user, operator, contributor, security, and release documentation matches the tagged behavior

Repository settings remain enforceable on GitHub; this file records the expected public state rather than configuring it.
