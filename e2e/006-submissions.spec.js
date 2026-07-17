const { test, expect } = require("./fixtures");
const { TestClient, uniqueName } = require("./test-client");

test.describe("inbox entries", () => {
  let endpoint;

  test.beforeEach(async ({ admin }) => {
    endpoint = await admin.createForm("Contact inbox", uniqueName("inbox"));
  });

  test("stores a public submission and shows it on the endpoint", async ({ page, admin }) => {
    const response = await admin.submit(endpoint.slug, endpoint.token, {
      name: "Alice Smith",
      email: "alice@example.com",
      message: "Browser acceptance entry",
    });
    expect(response.status()).toBe(200);

    await admin.open(`/admin/forms/${endpoint.id}`);
    await expect(page.locator("body")).toContainText("alice@example.com");

    await page.getByRole("link", { name: "Open", exact: true }).first().click();
    await expect(page.getByRole("heading", { name: /Entry #/ })).toBeVisible();
    await expect(page.locator("body")).toContainText("Alice Smith");
  });

  test("renders canonical UTC timestamps in the browser timezone", async ({ page, admin }) => {
    expect((await admin.submit(endpoint.slug, endpoint.token, { email: "time@example.com" })).status()).toBe(200);
    await admin.open("/admin/submissions");

    const received = page.locator('time[data-local-time][data-date-style="medium"]').first();
    const instant = await received.getAttribute("datetime");
    expect(instant).toMatch(/Z$/);
    const expected = await page.evaluate(
      (value) => new Intl.DateTimeFormat(undefined, { dateStyle: "medium" }).format(new Date(value)),
      instant,
    );
    await expect(received).toHaveText(expected);
    const browserTimezone = await page.evaluate(() => Intl.DateTimeFormat().resolvedOptions().timeZone || "local time");
    await expect(page.locator("[data-timezone-label]")).toHaveText(browserTimezone);
  });

  test("presents one instant differently across timezones", async ({ browser, page, admin }) => {
    expect((await admin.submit(endpoint.slug, endpoint.token, { email: "zones@example.com" })).status()).toBe(200);
    const baseURL = new URL(page.url()).origin;
    const labels = [];
    let canonicalInstant;

    for (const timezoneId of ["Europe/Rome", "America/New_York"]) {
      const context = await browser.newContext({ baseURL, locale: "en-US", timezoneId });
      const zonedPage = await context.newPage();
      const zonedClient = new TestClient(zonedPage);
      await zonedClient.login();
      await zonedClient.open("/admin/submissions");
      const time = zonedPage.locator('time[data-local-time][data-time-style="short"]').first();
      const instant = await time.getAttribute("datetime");
      canonicalInstant ||= instant;
      expect(instant).toBe(canonicalInstant);
      labels.push(await time.textContent());
      await expect(zonedPage.locator("[data-timezone-label]")).toHaveText(timezoneId);
      await context.close();
    }
    expect(labels[0]).not.toBe(labels[1]);
  });

  test("rejects an invalid token", async ({ request }) => {
    const response = await request.post(`/forms/${endpoint.slug}/submit?token=wrong`, {
      form: { message: "not accepted" },
      headers: { Origin: "http://localhost:3000" },
    });
    expect(response.status()).toBe(401);
  });
});
