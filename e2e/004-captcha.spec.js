const { test, expect } = require("./fixtures");
const { uniqueName } = require("./test-client");

test.describe("safeguards", () => {
  test("keeps non-secret values after validation fails", async ({ page, admin }) => {
    await admin.open("/admin/settings/captcha/new");
    const name = uniqueName("invalid-captcha");
    await page.getByLabel("Profile name").fill(name);
    await page.getByLabel("Site key").fill(" ");
    await page.getByLabel("Secret key").fill("draft-secret");
    await page.getByRole("button", { name: "Create safeguard" }).click();

    await expect(page.getByText(/site key is required/i)).toBeVisible();
    await expect(page.getByLabel("Profile name")).toHaveValue(name);
    await expect(page.getByLabel("Secret key")).toHaveValue("");
  });

  test("creates a Turnstile profile from the UI", async ({ page, admin }) => {
    await admin.open("/admin/settings/captcha/new");
    const name = uniqueName("turnstile");
    await page.getByLabel("Profile name").fill(name);
    await page.getByLabel("Site key").fill("1x00000000000000000000AA");
    await page.getByLabel("Secret key").fill("1x0000000000000000000000000000000AA");
    await Promise.all([
      page.waitForURL("**/admin/settings/captcha"),
      page.getByRole("button", { name: "Create safeguard" }).click(),
    ]);
    await expect(page.locator("body")).toContainText(name);
  });

  test("updates an existing Turnstile profile without exposing its secret", async ({ page, admin }) => {
    const id = await admin.createCaptcha(uniqueName("captcha"));
    await admin.open(`/admin/settings/captcha/${id}/edit`);
    await expect(page.getByLabel("Secret key")).toHaveValue("");
    const renamed = uniqueName("renamed-captcha");
    await page.getByLabel("Profile name").fill(renamed);
    await page.getByLabel("Site key").fill("rotated-site-key");
    await Promise.all([
      page.waitForURL(`**/admin/settings/captcha/${id}`),
      page.getByRole("button", { name: "Save changes" }).click(),
    ]);
    await expect(page.locator("body")).toContainText(renamed);
    await expect(page.locator("body")).toContainText("rotated-site-key");
    await expect(page.locator("body")).not.toContainText("test-secret");
    const stored = await admin.row("SELECT secret_key FROM captcha_profiles WHERE id = ?", [id]);
    expect(stored.secret_key).toBe("test-secret");
  });

  test("rejects a protected submission without a Turnstile response", async ({ request, admin }) => {
    const captchaProfileID = await admin.createCaptcha(uniqueName("required-captcha"));
    const endpoint = await admin.createForm("Protected form", uniqueName("protected"), { captchaProfileID });
    const response = await request.post(`/forms/${endpoint.slug}/submit?token=${endpoint.token}`, {
      data: { name: "Robot" },
      headers: { Origin: "http://localhost:3000" },
    });
    expect(response.status()).toBe(400);
    const body = await response.json();
    expect(body).toMatchObject({ ok: false });
    expect(body.error).toMatch(/captcha/i);
  });
});
