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
    await expect(page.getByLabel("Primary email notification")).not.toBeChecked();
    await Promise.all([
      page.waitForURL(/\/admin\/forms\/\d+$/),
      page.getByRole("button", { name: "Create endpoint" }).click(),
    ]);
    await expect(page.getByRole("heading", { name: "Contact Form" })).toBeVisible();
    await expect(page.locator("body")).toContainText(slug);
  });

  test("shows the token and previews native form HTML", async ({ page, admin }) => {
    const endpoint = await admin.createForm("Native sample", uniqueName("native"));
    await admin.open("/admin/forms");
    await Promise.all([
      page.waitForURL(`/admin/forms/${endpoint.id}`),
      page.getByRole("link", { name: "Native sample", exact: true }).click(),
    ]);
    await expect(page.locator("code").first()).toContainText(endpoint.token);
    await expect(page.locator("#form-preview")).toHaveAttribute("title", "Starter HTML preview");

    const source = page.locator("#form-code");
    await expect(source).toContainText(endpoint.token);
    await expect(source).toContainText(`/forms/${endpoint.slug}/submit`);
    await expect(source).toHaveAttribute("data-form-code-ready", "true");

    const sourceTab = page.getByRole("tab", { name: "Source" });
    const previewTab = page.getByRole("tab", { name: "Preview" });
    await sourceTab.press("ArrowRight");
    await expect(previewTab).toBeFocused();
    await expect(previewTab).toHaveAttribute("aria-selected", "true");
    await expect(page.getByRole("tabpanel", { name: "Preview" })).toBeVisible();
  });

  test("reports whether copying source succeeds or fails", async ({ page, admin }) => {
    const endpoint = await admin.createForm("Copy sample", uniqueName("copy"));
    await admin.open(`/admin/forms/${endpoint.id}`);

    await page.evaluate(() => {
      Object.defineProperty(navigator, "clipboard", {
        configurable: true,
        value: { writeText: async (text) => { window.__copiedSource = text; } },
      });
    });
    await page.getByRole("button", { name: "Copy source" }).click();
    await expect(page.getByRole("status")).toHaveText("Source copied.");
    expect(await page.evaluate(() => window.__copiedSource)).toContain(endpoint.token);

    await page.evaluate(() => {
      Object.defineProperty(navigator, "clipboard", { configurable: true, value: undefined });
    });
    await page.getByRole("button", { name: "Copy source" }).click();
    await expect(page.getByRole("status")).toHaveText("Copy failed. Select the source and copy it manually.");

    await admin.open("/admin/forms/new?template=contact");
    await page.evaluate(() => {
      Object.defineProperty(navigator, "clipboard", {
        configurable: true,
        value: { writeText: async () => { throw new DOMException("Denied", "NotAllowedError"); } },
      });
    });
    await page.getByRole("tab", { name: "Source" }).click();
    await page.getByRole("button", { name: "Copy source" }).click();
    await expect(page.getByRole("status")).toHaveText("Copy failed. Select the source and copy it manually.");
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

    await page.locator("#captcha_profile_id").selectOption(String(captchaProfileID));
    await page.getByLabel("Webhook").check();
    await page.getByLabel("Destination URL").fill("https://example.com/webhook");
    await page.getByLabel("Primary email notification").check();
    await page.getByLabel("Email route").selectOption(String(mailerProfileID));
    await page.getByLabel("Recipients").fill("bugs@example.com\narchive@example.com");
    await page.getByLabel("Message format").selectOption("html");
    await Promise.all([
      page.waitForURL(`/admin/forms/${endpoint.id}`),
      page.getByRole("button", { name: "Save changes" }).click(),
    ]);
    await expect(page.locator("body")).toContainText("https://example.com/webhook");
    await expect(page.locator("body")).toContainText("bugs@example.com");
    await expect(page.locator("body")).toContainText("archive@example.com");
    await expect(page.locator("body")).toContainText("HTML + text");
    expect(await admin.row("SELECT captcha_profile_id FROM forms WHERE id = ?", [endpoint.id]))
      .toMatchObject({ captcha_profile_id: captchaProfileID });
    expect(await admin.row("SELECT recipient, format FROM email_deliveries WHERE form_id = ?", [endpoint.id]))
      .toMatchObject({ recipient: "bugs@example.com, archive@example.com", format: "html" });
  });

  test("adds a second email notification with dynamic recipient and Reply-To", async ({ page, admin }) => {
    const mailerProfileID = await admin.createMailer(uniqueName("multi-email-mailer"));
    const captchaProfileID = await admin.createCaptcha(uniqueName("multi-email-captcha"));
    const endpoint = await admin.createForm("Two messages", uniqueName("two-messages"), { captchaProfileID });
    await admin.open(`/admin/forms/${endpoint.id}`);

    await page.getByRole("link", { name: "Add notification" }).click();
    await page.getByLabel("Notification name").fill("Customer confirmation");
    await page.getByLabel("Enabled").check();
    await page.getByLabel("Email route").selectOption(String(mailerProfileID));
    await page.getByLabel("Recipient source").selectOption("field");
    await page.getByLabel("Recipient value").fill("email");
    await page.getByLabel("Reply-To source").selectOption("static");
    await page.getByLabel("Reply-To value").fill("Support <support@example.com>");
    await page.getByLabel("Subject template").fill("Thanks {{.Fields.name}}");
    await page.getByLabel("Message format").selectOption("html");
    await Promise.all([
      page.waitForURL(`/admin/forms/${endpoint.id}`),
      page.getByRole("button", { name: "Add notification" }).click(),
    ]);

    const notification = page.locator("article", { hasText: "Customer confirmation" });
    await expect(notification).toContainText("Field: email");
    await expect(notification).toContainText("support@example.com");
    await expect(notification).toContainText("HTML + text");
    expect(await admin.row("SELECT COUNT(*) AS count FROM email_deliveries WHERE form_id = ?", [endpoint.id]))
      .toMatchObject({ count: 2 });
  });

  test("previews an unsaved email with real submission data without scheduling delivery", async ({ page, admin }) => {
    const mailerProfileID = await admin.createMailer(uniqueName("preview-email-mailer"));
    const endpoint = await admin.createForm("Email preview", uniqueName("email-preview"));
    const submissionResponse = await admin.submit(endpoint.slug, endpoint.token, {
      name: "<Ada>",
      email: "ada@recipient.invalid",
    });
    expect(submissionResponse.ok()).toBe(true);
    const submission = await admin.row(
      "SELECT id FROM submissions WHERE form_id = ? ORDER BY id DESC LIMIT 1",
      [endpoint.id],
    );
    const eventsBefore = await admin.row(
      "SELECT COUNT(*) AS count FROM email_events WHERE submission_id = ?",
      [submission.id],
    );

    await admin.open(`/admin/forms/${endpoint.id}`);
    await page.getByRole("link", { name: "Add notification" }).click();
    await expect(page.getByRole("heading", { name: "Add notification" })).toBeVisible();
    await page.getByLabel("Email route").selectOption(String(mailerProfileID));
    await page.getByLabel("Recipient source").selectOption("field");
    await page.getByLabel("Recipient value").fill("email");
    await page.getByLabel("Reply-To source").selectOption("field");
    await page.getByLabel("Reply-To value").fill("email");
    await page.getByLabel("Subject template").fill("Preview · {{.Fields.name}}");
    await page.getByLabel("Message format").selectOption("html");
    await page.getByLabel("Text template").fill("Hello {{.Fields.name}}");
    await page.getByLabel("HTML template").fill("<h1>Hello {{.Fields.name}}</h1>");
    await page.getByRole("button", { name: "Preview email" }).click();

    await expect(page.getByRole("status")).toHaveText(
      `Rendered from entry #${submission.id}. No email was sent.`,
    );
    await expect(page.locator("#emailPreviewTo")).toHaveText("ada@recipient.invalid");
    await expect(page.locator("#emailPreviewReplyTo")).toHaveText("ada@recipient.invalid");
    await expect(page.locator("#emailPreviewSubject")).toHaveText("Preview · <Ada>");
    await expect(page.frameLocator("#emailPreviewFrame").getByRole("heading", { name: "Hello <Ada>" })).toBeVisible();

    const eventsAfter = await admin.row(
      "SELECT COUNT(*) AS count FROM email_events WHERE submission_id = ?",
      [submission.id],
    );
    expect(eventsAfter.count).toBe(eventsBefore.count);
  });

  test("preserves non-secret values after a validation error", async ({ page, admin }) => {
    const mailerProfileID = await admin.createMailer(uniqueName("draft-mailer"));
    const captchaProfileID = await admin.createCaptcha(uniqueName("draft-captcha"));
    const endpoint = await admin.createForm("Original name", uniqueName("draft"));
    await admin.open(`/admin/forms/${endpoint.id}/edit`);

    await page.getByLabel("Endpoint name").fill("Unsaved endpoint");
    await page.getByLabel("Allowed origins").fill("forms.example.com");
    await page.locator("#captcha_profile_id").selectOption(String(captchaProfileID));
    await page.getByLabel("Webhook").check();
    await page.getByLabel("Destination URL").fill("https://hooks.example.com/submissions");
    await page.getByLabel("HMAC secret").fill("draft-secret");
    await page.getByLabel(/Custom headers/).fill("{");
    await page.getByLabel("Primary email notification").check();
    await page.getByLabel("Email route").selectOption(String(mailerProfileID));
    await page.getByLabel("Recipients").fill("draft@example.com\narchive@example.com");
    await page.getByLabel("Message format").selectOption("html");
    await page.getByRole("button", { name: "Save changes" }).click();

    await expect(page.getByRole("alert")).toContainText("Webhook headers");
    await expect(page.getByLabel("Endpoint name")).toHaveValue("Unsaved endpoint");
    await expect(page.getByLabel("Allowed origins")).toHaveValue("forms.example.com");
    await expect(page.locator("#captcha_profile_id")).toHaveValue(String(captchaProfileID));
    await expect(page.getByLabel("Destination URL")).toHaveValue("https://hooks.example.com/submissions");
    await expect(page.getByLabel("HMAC secret")).toHaveValue("");
    await expect(page.getByLabel(/Custom headers/)).toHaveValue("");
    await expect(page.locator("body")).not.toContainText("draft-secret");
    await expect(page.getByLabel("Email route")).toHaveValue(String(mailerProfileID));
    await expect(page.getByLabel("Recipients")).toHaveValue("draft@example.com\narchive@example.com");
    await expect(page.getByLabel("Message format")).toHaveValue("html");
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

  test("shows HTMX server-error responses instead of leaving a stale page", async ({ page, admin }) => {
    await admin.open("/admin/forms");
    await page.route("**/admin/frontend-lifecycle-failure", (route) => route.fulfill({
      status: 500,
      contentType: "text/html",
      body: "<!doctype html><html><body><main><h1>Recovered server error</h1></main></body></html>",
    }));

    await page.evaluate(() => window.htmx.ajax("GET", "/admin/frontend-lifecycle-failure", {
      target: document.body,
    }));

    await expect(page.getByRole("heading", { name: "Recovered server error" })).toBeVisible();
  });
});
