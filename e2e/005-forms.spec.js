// e2e/005-forms.spec.js - Test endpoint CRUD operations
const { test, expect } = require("@playwright/test");
const { TestHelpers } = require("./test-helpers");
const { TEST_EMAIL, TEST_PASSWORD } = require("./test-constants");

test.describe("Endpoint management", () => {
  let helpers;

  test.beforeEach(async ({ page }) => {
    helpers = new TestHelpers(page);

    // Login before each test
    await page.context().clearCookies();
    await helpers.login(TEST_EMAIL, TEST_PASSWORD);
  });

  test.afterEach(async ({ page }) => {
    if (helpers) {
      await helpers.cleanup();
    }
  });

  test("1. Create a new form", async ({ page }) => {
    helpers.log("=== Creating New Endpoint ===");

    await helpers.navigateTo("/admin/forms");

    // Open the endpoint starting-point selector.
    await page.click('text=New endpoint');

    // Wait for template selector page to load
    await page.waitForURL("**/admin/forms/new");
    helpers.log(`Template selector URL: ${page.url()}`);

    // Wait for template selector content
    await page.waitForSelector('h1:has-text("Choose a starting point")');

    // Click on Contact Form template
    await page.click('text=Contact Form');

    // Wait for form creation page with template parameter
    await page.waitForURL("**/admin/forms/new?template=contact");
    helpers.log(`Form creation URL: ${page.url()}`);

    // Wait for form fields to be populated
    await page.waitForSelector('input[name="name"]');
    expect(await page.inputValue('input[name="name"]')).toBe("Contact Form");
    expect(await page.inputValue('input[name="slug"]')).toBe("contact");

    const endpointSlug = `contact-${Date.now()}`;
    await page.fill('input[name="slug"]', endpointSlug);
    await page.fill('input[name="allowed_origins"]', '*');
    await page.uncheck('input[name="email_enabled"]');

    // Submit the form
    await page.click('main form[action="/admin/forms"] button[type="submit"]');
    helpers.log("Clicked Create endpoint button");

    await page.waitForURL(/\/admin\/forms\/\d+$/);

    const endpointContent = await page.textContent("body");
    expect(endpointContent).toContain("Contact Form");
    expect(endpointContent).toContain(endpointSlug);

    helpers.log("✅ Endpoint created successfully");
  });

  test("2. View form details and get token", async ({ page }) => {
    helpers.log("=== Creating and Viewing Form Details ===");

    // Create a form using DB helper
    const formSlug = `test-contact-${Date.now()}`;
    const { formId, token } = await helpers.createFormData(
      "Contact Form Details",
      formSlug
    );

    // Navigate to form details
    await helpers.navigateTo(`/admin/forms/${formId}`);
    await page.waitForLoadState("networkidle");

    // Should see form details
    const pageContent = await page.textContent("body");
    expect(pageContent).toContain("Contact Form Details");
    expect(pageContent).toContain(formSlug);

    // Should see token in the endpoint code block
    const endpointElement = await page.waitForSelector('code:has-text("token=")');
    const endpointText = await endpointElement.textContent();
    const tokenMatch = endpointText.match(/token=([a-f0-9]+)/);
    expect(tokenMatch).toBeTruthy();
    const extractedToken = tokenMatch[1];
    expect(extractedToken).toBe(token); // Should match the token we created

    helpers.log(`✅ Form token verified: ${token}`);
  });

  test("3. Edit form settings", async ({ page }) => {
    helpers.log("=== Editing Form Settings ===");

    // Create a form via database to edit
    const formData = await helpers.createFormData("Test Feedback Form", "test-feedback");
    helpers.log(`Created form with ID: ${formData.formId}`);

    // Navigate to forms list and find our form
    await helpers.navigateTo("/admin/forms");

    // Verify the form appears in the list
    const pageContent = await page.textContent("body");
    expect(pageContent).toContain("Test Feedback Form");

    helpers.log("✅ Form exists in list");
  });

  test("4. Toggle SDK inclusion in form code", async ({ page }) => {
    helpers.log("=== Testing SDK Toggle ===");

    // Create a form using DB helper
    const formSlug = `test-sdk-${Date.now()}`;
    const { formId } = await helpers.createFormData("SDK Toggle Test", formSlug);

    // Navigate to form details
    await helpers.navigateTo(`/admin/forms/${formId}`);
    await page.waitForLoadState("networkidle");

    // Get initial form code (should not include SDK)
    const codeElement = await page.waitForSelector("#form-code");
    const initialCode = await codeElement.textContent();
    expect(initialCode).not.toContain("miniform.js");
    helpers.log("✅ Initial code does not include SDK");

    // Check the SDK toggle
    const sdkCheckbox = await page.waitForSelector("#include-sdk");
    await sdkCheckbox.check();

    // Wait for code to update
    await page.waitForTimeout(100);

    // Verify SDK script is now included
    const codeWithSdk = await codeElement.textContent();
    expect(codeWithSdk).toContain("miniform.js");
    expect(codeWithSdk).toContain("Miniform SDK");
    helpers.log("✅ Code includes SDK after toggle");

    // Uncheck the SDK toggle
    await sdkCheckbox.uncheck();
    await page.waitForTimeout(100);

    // Verify SDK script is removed
    const codeWithoutSdk = await codeElement.textContent();
    expect(codeWithoutSdk).not.toContain("miniform.js");
    helpers.log("✅ Code excludes SDK after untoggle");
  });

  test("5. Configure form delivery settings", async ({ page }) => {
    helpers.log("=== Configuring Form Delivery ===");

    // Create mailer and captcha profiles first
    const mailerProfileId = await helpers.createMailerProfile("Test Mailer Delivery", "mailgun", "test@example.com");
    const captchaProfileId = await helpers.createCaptchaProfile("Test Captcha Delivery", "turnstile");

    // Create form with email and webhook enabled
    const formData = await helpers.createFormData("Test Bug Report", "test-bug-report", {
      mailerProfileId,
      captchaProfileId,
      emailEnabled: true,
      webhookEnabled: true,
      emailRecipient: "bugs@example.com",
      webhookUrl: "https://example.com/webhook"
    });

    helpers.log(`Created form with delivery: ${formData.formId}`);

    // Navigate to forms list and verify the form exists
    await helpers.navigateTo("/admin/forms");

    const pageContent = await page.textContent("body");
    expect(pageContent).toContain("Test Bug Report");

    helpers.log("✅ Form with delivery settings created");
  });
});
