const { test, expect } = require("./fixtures");
const { uniqueName } = require("./test-client");

test.describe("safeguards", () => {
	test("keeps entered values after validation fails", async ({ page, admin }) => {
		await admin.open("/admin/settings/captcha/new");
		const name = uniqueName("invalid-captcha");
		const siteKeys = JSON.stringify([{ host_pattern: "*.draft.example", site_key: "draft-key" }]);
		await page.getByLabel("Profile name").fill(name);
		await page.getByLabel("Secret key").fill("draft-secret");
		await page.getByLabel(/Site keys/).fill(siteKeys);
		await page.getByLabel(/Default policy/).fill(JSON.stringify({ theme: "sepia" }));
		await page.getByRole("button", { name: "Create safeguard" }).click();

		await expect(page.getByText(/theme must be auto, light, or dark/i)).toBeVisible();
		await expect(page.getByLabel("Profile name")).toHaveValue(name);
		await expect(page.getByLabel("Secret key")).toHaveValue("draft-secret");
		await expect(page.getByLabel(/Site keys/)).toHaveValue(siteKeys);
	});

  test("creates a Turnstile profile from the UI", async ({ page, admin }) => {
    await admin.open("/admin/settings/captcha/new");
    const name = uniqueName("turnstile");
    await page.getByLabel("Profile name").fill(name);
    await page.getByLabel("Secret key").fill("1x0000000000000000000000000000000AA");
    await page.getByLabel(/Site keys/).fill(JSON.stringify([{ host_pattern: "*", site_key: "test-site-key" }]));
    await page.getByLabel(/Default policy/).fill(JSON.stringify({ required: true, action: "submit" }));
    await Promise.all([
      page.waitForURL("**/admin/settings/captcha"),
      page.getByRole("button", { name: "Create safeguard" }).click(),
    ]);
    await expect(page.locator("body")).toContainText(name);
  });

  test("updates an existing Turnstile profile", async ({ page, admin }) => {
    const id = await admin.createCaptcha(uniqueName("captcha"));
    await admin.open(`/admin/settings/captcha/${id}/edit`);
    const renamed = uniqueName("renamed-captcha");
    await page.getByLabel("Profile name").fill(renamed);
    await Promise.all([
      page.waitForURL(`**/admin/settings/captcha/${id}`),
      page.getByRole("button", { name: "Save changes" }).click(),
    ]);
    await expect(page.locator("body")).toContainText(renamed);
  });

  test("rejects a protected submission without a Turnstile response", async ({ request, admin }) => {
    const captchaProfileID = await admin.createCaptcha(uniqueName("required-captcha"), { required: true });
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

  test("accepts a missing Turnstile response when the policy is optional", async ({ admin }) => {
    const captchaProfileID = await admin.createCaptcha(uniqueName("optional-captcha"), { required: false });
    const endpoint = await admin.createForm("Optional safeguard", uniqueName("optional"), { captchaProfileID });

    const response = await admin.submit(endpoint.slug, endpoint.token, { name: "Ada" });

    expect(response.ok()).toBe(true);
  });
});
