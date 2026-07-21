const { test, expect } = require("./fixtures");

test("test installation is healthy and accepts the operator login", async ({ page, client }) => {
  const health = await page.goto("/_health");
  expect(health.ok()).toBeTruthy();

  await client.login();
  await expect(page).toHaveURL(/\/admin\/submissions/);
  await expect(page.getByRole("heading", { name: /Every response/i })).toBeVisible();
});
