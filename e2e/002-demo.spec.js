const { test, expect } = require("@playwright/test");
const { TestHelpers } = require("./test-helpers");

test.describe("Local test page", () => {
  let helpers;

  test.beforeEach(async ({ page }) => {
    helpers = new TestHelpers(page);
  });

  test.afterEach(async () => {
    if (helpers) {
      await helpers.cleanup();
    }
  });

  test("submits fields and an attachment to a selected endpoint", async ({ page }) => {
    const slug = `demo-page-${Date.now()}`;
    const { formId } = await helpers.createFormData("Demo page endpoint", slug, {
      allowedOrigins: "*",
    });

    await page.goto(`/_demo?slug=${slug}`);
    await expect(page.getByRole("heading", { name: "Send one. Trace everything." })).toBeVisible();

    await page.getByLabel("Name").fill("Browser Tester");
    await page.getByLabel("Email", { exact: true }).fill("browser@example.com");
    await page.getByLabel("Topic").selectOption("feedback");
    await page.getByLabel("Message").fill("Testing the local demo end to end.");
    await page.getByLabel("Webhook delivery").check();
    await page.getByLabel("Attachment").setInputFiles({
      name: "demo-note.txt",
      mimeType: "text/plain",
      buffer: Buffer.from("Miniform demo attachment"),
    });

    await page.getByRole("button", { name: "Send test submission" }).click();

    await expect(page.getByRole("status")).toContainText("Submission accepted · HTTP 200");
    await expect(page.locator("#result-payload")).toContainText('"ok": true');

    const submission = await helpers.getSQL(
      "SELECT id, data_json FROM submissions WHERE form_id = ? ORDER BY id DESC LIMIT 1",
      [formId]
    );
    expect(submission).toBeTruthy();
    expect(JSON.parse(submission.data_json)).toMatchObject({
      name: "Browser Tester",
      email: "browser@example.com",
      topic: "feedback",
      message: "Testing the local demo end to end.",
      channels: ["email", "webhook"],
    });
    await expect(page.getByRole("link", { name: `Open submission #${submission.id}` })).toHaveAttribute(
      "href",
      `/admin/submissions/${submission.id}`
    );

    const attachment = await helpers.getSQL(
      "SELECT filename FROM submission_files WHERE submission_id = ?",
      [submission.id]
    );
    expect(attachment.filename).toBe("demo-note.txt");
  });
});
