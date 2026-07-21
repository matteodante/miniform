const { test, expect } = require("./fixtures");
const { ADMIN_EMAIL, ADMIN_PASSWORD, uniqueName } = require("./test-client");

test.describe("operator settings", () => {
  test("shows workspace identity and security controls", async ({ page, admin }) => {
    await admin.open("/admin/settings");
    await expect(page.getByRole("heading", { name: "Operator access." })).toBeVisible();
    await expect(page.getByRole("heading", { name: "Operator email" })).toBeVisible();
    await expect(page.getByRole("heading", { name: "Workspace password" })).toBeVisible();
  });

  test("changes the login email", async ({ page, admin }) => {
    const temporaryEmail = `${uniqueName("operator")}@example.com`;
    await admin.open("/admin/settings");
    await page.getByLabel("Email").fill(temporaryEmail);
    await page.getByLabel("Current password").first().fill(ADMIN_PASSWORD);
    await page.getByRole("button", { name: "Update email" }).click();
    await expect(page.getByRole("status")).toContainText("Email updated successfully");

    await admin.logout();
    await admin.login(temporaryEmail, ADMIN_PASSWORD);
    await expect(page).toHaveURL(/\/admin\/submissions/);
  });

  test("revalidates password confirmation after repeated HTMX navigation", async ({ page, admin }) => {
    await admin.open("/admin/submissions");

    for (let visit = 0; visit < 2; visit += 1) {
      const settingsResponse = page.waitForResponse((response) => (
        new URL(response.url()).pathname === "/admin/settings"
      ));
      await page.locator('a[data-nav][href="/admin/settings"]').click();
      const response = await settingsResponse;
      expect(response.request().headers()["hx-request"]).toBe("true");
      await expect(page).toHaveURL(/\/admin\/settings$/);
      await expect(page.locator("#new_password")).toBeVisible();

      if (visit === 0) {
        await page.locator('a[data-nav][href="/admin/submissions"]').click();
        await expect(page).toHaveURL(/\/admin\/submissions$/);
      }
    }

    const newPassword = page.locator("#new_password");
    const confirmation = page.locator("#confirm_password");

    await newPassword.fill("replacement-one");
    await confirmation.fill("replacement-two");
    expect(await confirmation.evaluate((input) => input.checkValidity())).toBe(false);

    await newPassword.fill("replacement-two");
    expect(await confirmation.evaluate((input) => input.checkValidity())).toBe(true);
  });

  test("changes the password", async ({ page, admin }) => {
    const temporaryPassword = `Tmp-${uniqueName("password")}`;
    await admin.open("/admin/settings");
    await page.locator("#current_password").fill(ADMIN_PASSWORD);
    await page.locator("#new_password").fill(temporaryPassword);
    await page.locator("#confirm_password").fill(temporaryPassword);
    await page.getByRole("button", { name: "Update password" }).click();
    await expect(page.getByRole("status")).toContainText("Password updated successfully");

    await admin.logout();
    await admin.login(ADMIN_EMAIL, temporaryPassword);
    await expect(page).toHaveURL(/\/admin\/submissions/);
  });
});
