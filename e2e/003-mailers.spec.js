const { test, expect } = require("./fixtures");
const { uniqueName } = require("./test-client");

test.describe("email routes", () => {
	test("keeps entered values after validation fails", async ({ page, admin }) => {
		await admin.open("/admin/settings/mailers/new");
		const name = uniqueName("invalid-mailer");
		await page.getByLabel("Profile name").fill(name);
		await page.getByLabel("SMTP host").fill("smtp.draft.example");
		await page.getByLabel(/Provider defaults/).fill("[]");
		await page.getByRole("button", { name: "Create email route" }).click();

		await expect(page.getByText(/defaults must be a JSON object/i)).toBeVisible();
		await expect(page.getByLabel("Profile name")).toHaveValue(name);
		await expect(page.getByLabel("SMTP host")).toHaveValue("smtp.draft.example");
		await expect(page.getByLabel(/Provider defaults/)).toHaveValue("[]");
	});

  test("creates an SMTP connection from the UI", async ({ page, admin }) => {
    await admin.open("/admin/settings/mailers/new");
    const name = uniqueName("smtp");
    await page.getByLabel("Profile name").fill(name);
    await page.getByLabel("SMTP host").fill("smtp.example.com");
    await page.getByLabel("Username").fill("smtp-user");
    await page.getByLabel("Password").fill("smtp-pass");
    await page.getByLabel("From email").fill("noreply@example.com");
    await Promise.all([
      page.waitForURL("**/admin/settings/mailers"),
      page.getByRole("button", { name: "Create email route" }).click(),
    ]);
    await expect(page.locator("body")).toContainText(name);
  });

  test("creates a complete Mailgun connection from the UI", async ({ page, admin }) => {
    await admin.open("/admin/settings/mailers/new");
    await expect(page.getByLabel("API key")).toBeHidden();
    await page.locator("#provider").selectOption("mailgun");
    await expect(page.getByLabel("API key")).toBeVisible();
    await expect(page.getByLabel("SMTP host")).toBeHidden();

    const name = uniqueName("mailgun");
    const defaults = JSON.stringify({ tags: ["miniform-e2e"] });
    await page.getByLabel("Profile name").fill(name);
    await page.getByLabel("API key").fill("key-test-mailgun");
    await page.getByLabel("Sending domain").fill("mg.example.com");
    await page.getByLabel("From name").fill("Miniform E2E");
    await page.getByLabel("From email").fill("mailgun@example.com");
    await page.getByLabel(/Provider defaults/).fill(defaults);
    await Promise.all([
      page.waitForURL("**/admin/settings/mailers"),
      page.getByRole("button", { name: "Create email route" }).click(),
    ]);
    await expect(page.locator("body")).toContainText(name);

    expect(await admin.row(
      `SELECT provider, api_key, domain, default_from_name, default_from_email, defaults_json
       FROM mailer_profiles WHERE name = ?`,
      [name],
    )).toMatchObject({
      provider: "mailgun",
      api_key: "key-test-mailgun",
      domain: "mg.example.com",
      default_from_name: "Miniform E2E",
      default_from_email: "mailgun@example.com",
      defaults_json: defaults,
    });
  });

  test("updates an existing connection", async ({ page, admin }) => {
    const original = uniqueName("mailer");
    const id = await admin.createMailer(original);
    await admin.open(`/admin/settings/mailers/${id}/edit`);
    const renamed = uniqueName("renamed-mailer");
    await page.getByLabel("Profile name").fill(renamed);
    await Promise.all([
      page.waitForURL(`**/admin/settings/mailers/${id}`),
      page.getByRole("button", { name: "Save changes" }).click(),
    ]);
    await expect(page.locator("body")).toContainText(renamed);
    await expect(page.locator("body")).not.toContainText(original);
  });
});
