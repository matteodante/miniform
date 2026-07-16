// e2e/test-constants.js - Shared test constants
// This file exists to avoid Playwright errors about test files importing from each other

// Admin credentials (created automatically by the app on first run)
const TEST_EMAIL = "admin@miniform.local";
const TEST_PASSWORD = "miniform";

module.exports = {
  TEST_EMAIL,
  TEST_PASSWORD,
};
