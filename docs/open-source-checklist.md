# Public launch checklist

Complete this checklist immediately before making the repository public.

## Repository

- [ ] Confirm the repository URL and add `origin`
- [ ] Confirm `main` is the default branch and remove obsolete branches and tags
- [ ] Verify the full reachable Git history contains no secrets, private data, local paths, or obsolete product names
- [ ] Confirm `.env`, databases, logs, browser output, caches, and binaries are untracked
- [ ] Set repository description, website, topics, and social preview
- [ ] Enable Issues and Discussions with Q&A, Ideas, and Show and tell categories

## Security and controls

- [ ] Enable private vulnerability reporting
- [ ] Enable secret scanning and push protection
- [ ] Enable dependency graph, Dependabot alerts, and Dependabot security updates
- [ ] Require actions to be pinned to full commit SHAs
- [ ] Protect `main`: pull requests, one approval when collaborators exist, resolved conversations, linear history, blocked force pushes and deletions
- [ ] Require the CI, CodeQL, and security checks after their first successful run
- [ ] Protect `v*` tags and require signed release tags
- [ ] Review workflow token permissions and environment protection for releases

## Project health

- [ ] Verify GitHub recognizes LICENSE, README, CONTRIBUTING, CODE_OF_CONDUCT, SECURITY, and SUPPORT
- [ ] Confirm issue forms, pull request template, discussion guidance, governance, and roadmap render correctly
- [ ] Confirm the security reporting link works privately
- [ ] Decide whether to add a `FUNDING.yml`; keep it absent until a real funding destination exists

## Release readiness

- [ ] Run `make audit`, `make check`, `make test-e2e`, and `make container-test`
- [ ] Validate a clean clone on macOS and Linux
- [ ] Validate Apple Container build, health check, persistence, and stop
- [ ] Run the release workflow in snapshot mode and inspect binaries, checksums, SBOMs, and licenses
- [ ] Create and verify the first signed version tag
- [ ] Confirm GHCR visibility, immutable digest, provenance, and version labels
- [ ] Test installation and upgrade instructions exactly as written

No remote repository setting or public release is changed by this checklist automatically.
