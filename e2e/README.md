# Browser acceptance tests

This directory verifies Miniform’s user-facing flows with Playwright and Chromium. The runner starts an isolated `MINIFORM_ENV=test` server on port `41817` and recreates `storage/miniform.test.db` for each run.

Install once:

```bash
make test-e2e-setup
```

Run the suite:

```bash
make test-e2e
```

For an interactive run:

```bash
cd e2e
npm run test:ui
```

The test-only account is `admin@miniform.local` / `miniform`. Tests use isolated browser contexts and a small database fixture; the single worker prevents concurrent writes to the shared SQLite test database.

Artifacts for failed tests are written to `e2e/test-results/`.
