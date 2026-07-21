const { test, expect } = require("./fixtures");
const { uniqueName } = require("./test-client");

test.describe("email routes", () => {
  test("keeps entered values after validation fails", async ({ page, admin }) => {
    await admin.open("/admin/settings/mailers/new");
    const name = uniqueName("invalid-mailer");
    await page.getByLabel("Profile name").fill(name);
    await page.getByLabel("SMTP host").fill("smtp.draft.example");
    await page.getByLabel("Port").fill("70000");
    await page.getByLabel("From email").fill("draft@example.com");
    await page.getByRole("button", { name: "Create email route" }).click();

    await expect(page.getByText(/SMTP port must be between 1 and 65535/i)).toBeVisible();
    await expect(page.getByLabel("Profile name")).toHaveValue(name);
    await expect(page.getByLabel("SMTP host")).toHaveValue("smtp.draft.example");
    await expect(page.getByLabel("Port")).toHaveValue("70000");
    await expect(page.getByLabel("From email")).toHaveValue("draft@example.com");
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
