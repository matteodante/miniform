# Browser acceptance tests

This directory verifies Miniform’s user-facing flows with Playwright and Chromium. The runner starts an isolated `MINIFORM_ENV=test` server on port `41817` by default. Each run creates a uniquely owned temporary data directory, uses one SQLite database inside it, and removes only that owned directory during global teardown.

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

The test-only account is `admin@miniform.local` / `miniform`. Tests use isolated browser contexts and a small database fixture; the single worker preserves deterministic UI state and SQLite ordering.

Set `PLAYWRIGHT_TEST_PORT` to change the local server port. When overriding the full client and health-check URL with `PLAYWRIGHT_BASE_URL`, set its port to the same value as `PLAYWRIGHT_TEST_PORT`; changing the base URL alone does not change the port passed to Miniform. Set `MINIFORM_E2E_DATA_DIR` to use and preserve a specific data directory for debugging. The teardown requires an ownership marker before removing an automatically created directory and never removes an explicitly supplied one.

`make test-e2e` first runs the Node teardown unit tests, builds the E2E server binary, then executes the complete Playwright suite.

Artifacts for failed tests are written to `e2e/test-results/`.
