const { test, expect } = require("./fixtures");
const { uniqueName } = require("./test-client");

test("logout invalidates live and HTMX-cached access to admin pages", async ({ page, admin }) => {
  const endpoint = await admin.createForm("Logout history", uniqueName("logout-history"));
  await admin.open(`/admin/forms/${endpoint.id}`);
  await expect(page.getByText("Authenticated URL", { exact: true })).toBeVisible();
  await page.evaluate((token) => {
    localStorage.setItem("htmx-history-cache", JSON.stringify([{
      url: window.location.pathname,
      content: `<main><p>${token}</p></main>`,
      title: "Legacy cached endpoint",
      scroll: 0,
    }]));
  }, endpoint.token);
  expect(await page.evaluate(() => localStorage.getItem("htmx-history-cache"))).not.toBeNull();

  await admin.logout();
  await expect(page).toHaveURL(/\/admin\/login/);
  await expect(page.getByRole("heading", { name: "Open the signal desk" })).toBeVisible();
  expect(await page.evaluate(() => localStorage.getItem("htmx-history-cache"))).toBeNull();

  await page.goBack();
  await expect(page).toHaveURL(/\/admin\/login/);
  await expect(page.getByRole("heading", { name: "Open the signal desk" })).toBeVisible();
  await expect(page.locator("body")).not.toContainText(endpoint.token);

  await page.goto("/admin/forms");
  await expect(page).toHaveURL(/\/admin\/login/);
});
