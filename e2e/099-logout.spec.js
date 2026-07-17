const { test, expect } = require("./fixtures");

test("logout invalidates access to admin pages", async ({ page, admin }) => {
  await admin.logout();
  await expect(page).toHaveURL(/\/admin\/login/);

  await page.goto("/admin/forms");
  await expect(page).toHaveURL(/\/admin\/login/);
});
