const { test, expect } = require("./fixtures");
const { uniqueName } = require("./test-client");

test.describe("endpoint management", () => {
  test("creates an endpoint from a starter template", async ({ page, admin }) => {
    await admin.open("/admin/forms");
    await page.getByRole("link", { name: "New endpoint" }).click();
    await expect(page.getByRole("heading", { name: "Choose a starting point" })).toBeVisible();
    await page.getByText("Contact Form", { exact: true }).click();
    await expect(page.getByLabel("Endpoint name")).toHaveValue("Contact Form");
    await expect(page.getByLabel(/Slug/)).toHaveValue("contact");

    const previewTab = page.getByRole("tab", { name: "Preview" });
    const sourceTab = page.getByRole("tab", { name: "Source" });
    await expect(previewTab).toHaveAttribute("aria-selected", "true");
    await expect(page.locator("#formEmbedPreviewFrame")).toHaveAttribute("title", "Starter HTML preview");
    await previewTab.press("ArrowRight");
    await expect(sourceTab).toBeFocused();
    await expect(sourceTab).toHaveAttribute("aria-selected", "true");
    await expect(page.getByRole("tabpanel", { name: "Source" })).toBeVisible();

    const slug = uniqueName("contact");
    await page.getByLabel(/Slug/).fill(slug);
    await page.getByLabel("Allowed origins").fill("*");
    await page.getByLabel("Email forwarding").uncheck();
    await Promise.all([
      page.waitForURL(/\/admin\/forms\/\d+$/),
      page.getByRole("button", { name: "Create endpoint" }).click(),
    ]);
    await expect(page.getByRole("heading", { name: "Contact Form" })).toBeVisible();
    await expect(page.locator("body")).toContainText(slug);
  });

  test("shows the token and toggles the optional SDK", async ({ page, admin }) => {
    const endpoint = await admin.createForm("SDK sample", uniqueName("sdk"));
    await admin.open("/admin/forms");
    await Promise.all([
      page.waitForURL(`/admin/forms/${endpoint.id}`),
      page.getByRole("link", { name: "SDK sample", exact: true }).click(),
    ]);
    await expect(page.locator("code").first()).toContainText(endpoint.token);
    await expect(page.locator("#form-preview")).toHaveAttribute("title", "Starter HTML preview");

    const source = page.locator("#form-code");
    await expect(source).not.toContainText("miniform.js");
    await page.locator("#include-sdk").check();
    await expect(source).toContainText("Miniform SDK");
    await expect(source).toContainText("miniform.js");
    await page.locator("#include-sdk").uncheck();
    await expect(source).not.toContainText("miniform.js");

    const sourceTab = page.getByRole("tab", { name: "Source" });
    const previewTab = page.getByRole("tab", { name: "Preview" });
    await sourceTab.press("ArrowRight");
    await expect(previewTab).toBeFocused();
    await expect(previewTab).toHaveAttribute("aria-selected", "true");
    await expect(page.getByRole("tabpanel", { name: "Preview" })).toBeVisible();
  });

  test("shows a local Turnstile placeholder in the sandboxed preview", async ({ page, admin }) => {
    const captchaProfileID = await admin.createCaptcha(uniqueName("preview-captcha"));
    const endpoint = await admin.createForm("Protected preview", uniqueName("preview"), { captchaProfileID });
    await admin.open(`/admin/forms/${endpoint.id}`);
    await page.getByRole("tab", { name: "Preview" }).click();
    await expect(page.frameLocator("#form-preview").getByText("Turnstile captcha preview")).toBeVisible();
  });

  test("persists email, webhook, and safeguard settings", async ({ page, admin }) => {
    const mailerProfileID = await admin.createMailer(uniqueName("delivery-mailer"));
    const captchaProfileID = await admin.createCaptcha(uniqueName("delivery-captcha"));
    const endpoint = await admin.createForm("Delivery sample", uniqueName("delivery"));
    await admin.open(`/admin/forms/${endpoint.id}/edit`);

    await page.getByLabel("Safeguard").selectOption(String(captchaProfileID));
    await page.getByLabel("Webhook").check();
    await page.getByLabel("Destination URL").fill("https://example.com/webhook");
    await page.getByLabel("Email forwarding").check();
    await page.getByLabel("Email route").selectOption(String(mailerProfileID));
    await page.getByLabel("Recipient").fill("bugs@example.com");
    await Promise.all([
      page.waitForURL(`/admin/forms/${endpoint.id}`),
      page.getByRole("button", { name: "Save changes" }).click(),
    ]);
    await expect(page.locator("body")).toContainText("https://example.com/webhook");
    await expect(page.locator("body")).toContainText("bugs@example.com");
    expect(await admin.row("SELECT captcha_profile_id FROM forms WHERE id = ?", [endpoint.id]))
      .toMatchObject({ captcha_profile_id: captchaProfileID });
  });

  test("preserves submitted values after a validation error", async ({ page, admin }) => {
    const mailerProfileID = await admin.createMailer(uniqueName("draft-mailer"));
    const captchaProfileID = await admin.createCaptcha(uniqueName("draft-captcha"));
    const endpoint = await admin.createForm("Original name", uniqueName("draft"));
    await admin.open(`/admin/forms/${endpoint.id}/edit`);

    await page.getByLabel("Endpoint name").fill("Unsaved endpoint");
    await page.getByLabel("Allowed origins").fill("forms.example.com");
    await page.getByLabel("Include JavaScript SDK").check();
    await page.getByLabel("Safeguard").selectOption(String(captchaProfileID));
    await page.getByLabel("Webhook").check();
    await page.getByLabel("Destination URL").fill("https://hooks.example.com/submissions");
    await page.getByLabel("HMAC secret").fill("draft-secret");
    await page.getByLabel(/Custom headers/).fill("{");
    await page.getByLabel("Email forwarding").check();
    await page.getByLabel("Email route").selectOption(String(mailerProfileID));
    await page.getByLabel("Recipient").fill("draft@example.com");
    await page.getByRole("button", { name: "Save changes" }).click();

    await expect(page.getByRole("alert")).toContainText("Webhook headers");
    await expect(page.getByLabel("Endpoint name")).toHaveValue("Unsaved endpoint");
    await expect(page.getByLabel("Allowed origins")).toHaveValue("forms.example.com");
    await expect(page.getByLabel("Include JavaScript SDK")).toBeChecked();
    await expect(page.getByLabel("Safeguard")).toHaveValue(String(captchaProfileID));
    await expect(page.getByLabel("Destination URL")).toHaveValue("https://hooks.example.com/submissions");
    await expect(page.getByLabel("HMAC secret")).toHaveValue("draft-secret");
    await expect(page.getByLabel(/Custom headers/)).toHaveValue("{");
    await expect(page.getByLabel("Email route")).toHaveValue(String(mailerProfileID));
    await expect(page.getByLabel("Recipient")).toHaveValue("draft@example.com");
  });

  test("binds the HTMX lifecycle listener only once", async ({ page, admin }) => {
    await page.waitForLoadState("domcontentloaded");
    await page.waitForFunction(() => Boolean(window.htmx));
    await page.evaluate(() => {
      window.__miniformAfterSwapBindings = 0;
      const addEventListener = EventTarget.prototype.addEventListener;
      EventTarget.prototype.addEventListener = function instrumented(type, ...args) {
        if (this === document.body && type === "htmx:afterSwap") window.__miniformAfterSwapBindings += 1;
        return addEventListener.call(this, type, ...args);
      };
    });

    const destinations = ["/admin/forms", "/admin/submissions", "/admin/settings/captcha"];
    for (const pathname of destinations) {
      await page.evaluate((path) => window.htmx.ajax("GET", path, { target: document.body }), pathname);
    }

    expect(await page.evaluate(() => window.__miniformAfterSwapBindings)).toBe(0);
  });
});
