# Contributing to Miniform

Thank you for helping improve Miniform. Small, focused changes with clear evidence are easiest to review and maintain.

## Before opening a change

- Search existing issues and discussions.
- Use a discussion for support or an early idea.
- Open an issue before substantial features or behavior changes.
- Report vulnerabilities privately according to [SECURITY.md](SECURITY.md).

Documentation fixes and narrowly scoped bug fixes may go directly to a pull request.

## Development setup

Follow [docs/development.md](docs/development.md). The short version is:

```bash
make bootstrap
make check
```

Do not commit local environment files, databases, logs, browser artifacts, binaries, or credentials.

## Architecture and code

- Keep business rules and persistence in the owning `internal/<domain>` package.
- Keep HTTP handlers thin and register routes in `internal/routes.go`.
- Wrap every SQLite mutation with `dbtxn.WithRetry`.
- Prefer explicit Go code, contextual errors, and the existing logging stack.
- Organize test scenarios with `t.Run` and reuse existing test helpers.

The complete repository conventions are in [AGENTS.md](AGENTS.md) and [docs/architecture.md](docs/architecture.md).

## Pull requests

1. Branch from an up-to-date `main`.
2. Keep unrelated refactors out of the change.
3. Add or update tests for behavior changes.
4. Run `make check`; add `make test-e2e` for user-facing flows, `make test-race` for lifecycle or concurrency work, and `make audit` for dependency, release, or security changes.
5. Update user-facing documentation and `CHANGELOG.md` when needed.
6. Complete the pull request template and link the relevant issue.

Maintainers may ask for a change to be split, revised, or closed when it conflicts with project scope. Submission does not guarantee acceptance.

## Commit and certificate of origin

Use a short imperative subject and explain the reason for non-obvious changes in the body. By contributing, you certify that you have the right to submit the work under the project's MIT license, consistent with the [Developer Certificate of Origin 1.1](https://developercertificate.org/).

Add a sign-off with:

```bash
git commit --signoff
```

## Review expectations

Reviews consider correctness, security, compatibility, maintainability, tests, and documentation. Maintainers make the final merge decision under [GOVERNANCE.md](GOVERNANCE.md).
