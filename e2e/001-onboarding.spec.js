// e2e/001-onboarding.spec.js - MUST RUN FIRST to verify admin account
const { test, expect } = require("@playwright/test");
const { TestHelpers } = require("./test-helpers");
const { TEST_EMAIL, TEST_PASSWORD } = require("./test-constants");

test.describe("Onboarding Flow - MUST RUN FIRST", () => {
  let helpers;

  test.beforeEach(async ({ page }) => {
    helpers = new TestHelpers(page);
    helpers.log("=== ONBOARDING SETUP - Verifying Admin Account ===");
  });

  test.afterEach(async ({ page }) => {
    if (helpers) {
      await helpers.cleanup();
    }
  });

  test("1. Verify admin account and login", async ({ page }) => {
    helpers.log("=== PHASE 1: ONBOARDING VERIFICATION ===");

        // Wait for server to be ready (server creates admin user automatically via migrations)
    await helpers.waitForServer();
    
    // Give the server extra time to complete migrations and user creation
    await page.waitForTimeout(3000);

    // Clear any existing session
    await page.context().clearCookies();
    await page.context().clearPermissions();

    // Note: Admin user (admin@miniform.local / miniform) is created automatically by server migrations

    // Step 1: Verify health endpoint
    const healthResponse = await page.goto("/_health");
    expect(healthResponse.ok()).toBeTruthy();
    helpers.log("✅ Server health check passed");

    // Step 2: Login with admin credentials
    helpers.log(`Logging in with admin credentials: ${TEST_EMAIL}`);
    await helpers.login(TEST_EMAIL, TEST_PASSWORD, {
      expectSuccess: true,
    });
    helpers.log("✅ Logged in successfully");

    // Step 3: Verify we're in the inbox
    const url = page.url();
    expect(url).not.toContain("/admin/login");
    expect(url).toContain("/admin");
    helpers.log("✅ Successfully authenticated and redirected from login");

    // Step 4: Verify inbox elements are present
    await helpers.waitForElement("h1", { timeout: 10000 });
    const pageContent = await page.textContent("body");
    expect(pageContent).toContain("Inbox");
    helpers.log("✅ Inbox loaded with expected content");

    helpers.log(`🎯 ADMIN ACCOUNT VERIFIED: ${TEST_EMAIL} / ${TEST_PASSWORD}`);
    helpers.log("✅ Onboarding completed - all other tests can now use this account");
  });
});
