const { test, expect } = require("./fixtures");
const { uniqueName } = require("./test-client");

test("local demo submits structured fields and a file", async ({ page, client }) => {
  const endpoint = await client.createForm("Demo endpoint", uniqueName("demo"));

  await page.goto(`/_demo?slug=${endpoint.slug}`);
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

  const submission = await client.row(
    "SELECT id, data_json FROM submissions WHERE form_id = ? ORDER BY id DESC LIMIT 1",
    [endpoint.id],
  );
  expect(JSON.parse(submission.data_json)).toMatchObject({
    name: "Browser Tester",
    email: "browser@example.com",
    topic: "feedback",
    channels: ["email", "webhook"],
  });
  await expect(page.getByRole("link", { name: `Open submission #${submission.id}` })).toHaveAttribute(
    "href",
    `/admin/submissions/${submission.id}`,
  );
  expect(await client.row("SELECT filename FROM submission_files WHERE submission_id = ?", [submission.id]))
    .toMatchObject({ filename: "demo-note.txt" });
});
