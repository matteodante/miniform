const http = require("node:http");
const { test, expect } = require("./fixtures");

test.describe("browser SDK", () => {
  let consumerServer;
  let consumerURL;

  test.beforeAll(async () => {
    consumerServer = http.createServer((_request, response) => {
      response.writeHead(200, { "Content-Type": "text/html; charset=utf-8" });
      response.end("<!doctype html><title>SDK consumer</title>");
    });
    await new Promise((resolve, reject) => {
      consumerServer.once("error", reject);
      consumerServer.listen(0, "127.0.0.1", resolve);
    });
    consumerURL = `http://127.0.0.1:${consumerServer.address().port}`;
  });

  test.afterAll(async () => {
    await new Promise((resolve, reject) => consumerServer.close((error) => (error ? reject(error) : resolve())));
  });

  test("submits once, keeps redirect fields client-side, and redirects after JSON success", async ({ page }) => {
    let requestCount = 0;
    let submittedBody = "";
    await page.route("**/forms/sdk-success/submit**", async (route) => {
      requestCount += 1;
      submittedBody = route.request().postData() || "";
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        headers: { "Access-Control-Allow-Origin": consumerURL },
        body: JSON.stringify({ ok: true }),
      });
    });

    await page.goto("/_health");
    const miniformOrigin = new URL(page.url()).origin;
    await page.goto(consumerURL);
    await page.setContent(`
      <form data-miniform action="${miniformOrigin}/forms/sdk-success/submit?token=test" method="post">
        <input name="message" value="hello">
        <input name="_success_url" value="/sdk-complete">
        <input name="_error_url" value="/sdk-error">
        <button type="submit">Send</button>
      </form>
    `);
    expect(miniformOrigin).not.toBe(consumerURL);
    await page.addScriptTag({ url: `${miniformOrigin}/assets/miniform.js` });
    await page.addScriptTag({ url: `${miniformOrigin}/assets/miniform.js` });

    await Promise.all([
      page.waitForURL(`${consumerURL}/sdk-complete`),
      page.getByRole("button", { name: "Send" }).click(),
    ]);

    expect(requestCount).toBe(1);
    expect(submittedBody).toContain('name="message"');
    expect(submittedBody).not.toContain("_success_url");
    expect(submittedBody).not.toContain("_error_url");
  });

  test("redirects once to the local error URL after a non-JSON HTTP failure", async ({ page }) => {
    let requestCount = 0;
    await page.route("**/forms/sdk-error/submit**", async (route) => {
      requestCount += 1;
      await route.fulfill({
        status: 422,
        contentType: "text/html",
        headers: { "Access-Control-Allow-Origin": consumerURL },
        body: "<h1>Upstream failure</h1>",
      });
    });

    await page.goto("/_health");
    const miniformOrigin = new URL(page.url()).origin;
    await page.goto(consumerURL);
    await page.setContent(`
      <form data-miniform action="${miniformOrigin}/forms/sdk-error/submit?token=test" method="post">
        <input name="message" value="hello">
        <input name="_error_url" value="/sdk-error">
        <button type="submit">Send</button>
      </form>
    `);
    await page.addScriptTag({ url: `${miniformOrigin}/assets/miniform.js` });

    await Promise.all([
      page.waitForURL(`${consumerURL}/sdk-error`),
      page.getByRole("button", { name: "Send" }).click(),
    ]);

    expect(requestCount).toBe(1);
  });

  test("does not retry an ambiguous failed submission", async ({ page }) => {
    let requestCount = 0;
    await page.route("**/forms/sdk-failure/submit**", async (route) => {
      requestCount += 1;
      await route.fulfill({
        status: 503,
        contentType: "application/json",
        headers: { "Access-Control-Allow-Origin": consumerURL },
        body: JSON.stringify({ ok: false, error: "Service unavailable" }),
      });
    });

    await page.goto("/_health");
    const miniformOrigin = new URL(page.url()).origin;
    await page.goto(consumerURL);
    await page.setContent(`
      <form data-miniform action="${miniformOrigin}/forms/sdk-failure/submit?token=test" method="post">
        <input name="message" value="hello">
        <button type="submit">Send</button>
      </form>
    `);
    await page.addScriptTag({ url: `${miniformOrigin}/assets/miniform.js` });
    await page.getByRole("button", { name: "Send" }).click();
    await expect(page.getByRole("alert")).toHaveText("Service unavailable");
    await page.waitForTimeout(1_200);

    expect(requestCount).toBe(1);
  });

  test("intercepts forms inserted after the SDK has loaded", async ({ page }) => {
    let requestCount = 0;
    await page.route("**/forms/sdk-dynamic/submit**", async (route) => {
      requestCount += 1;
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        headers: { "Access-Control-Allow-Origin": consumerURL },
        body: JSON.stringify({ ok: true }),
      });
    });

    await page.goto("/_health");
    const miniformOrigin = new URL(page.url()).origin;
    await page.goto(consumerURL);
    await page.addScriptTag({ url: `${miniformOrigin}/assets/miniform.js` });
    await page.evaluate(({ exactAction, trailingSlashAction }) => {
      document.body.insertAdjacentHTML("beforeend", `
        <form id="exact-route" action="${exactAction}" method="post">
          <input name="message" value="exact route">
          <button type="submit">Send exact</button>
        </form>
        <form id="trailing-slash-route" action="${trailingSlashAction}" method="post">
          <input name="message" value="trailing slash route">
          <button type="submit">Send trailing</button>
        </form>
      `);
    }, {
      exactAction: `${miniformOrigin}/forms/sdk-dynamic/submit?token=test`,
      trailingSlashAction: `${miniformOrigin}/forms/sdk-dynamic/submit/?token=test`,
    });

    const exactForm = page.locator("#exact-route");
    const trailingSlashForm = page.locator("#trailing-slash-route");
    await exactForm.getByRole("button", { name: "Send exact" }).click();
    await expect(exactForm.getByRole("status")).toHaveText("Form submitted successfully!");
    await trailingSlashForm.getByRole("button", { name: "Send trailing" }).click();
    await expect(trailingSlashForm.getByRole("status")).toHaveText("Form submitted successfully!");

    expect(requestCount).toBe(2);
  });

  test("does not intercept lookalike submit URLs", async ({ page }) => {
    await page.goto("/_health");
    const miniformOrigin = new URL(page.url()).origin;
    await page.goto(consumerURL);
    await page.setContent(`
      <form id="suffix-lookalike" action="${miniformOrigin}/forms/not-miniform/submit-preview" method="post">
        <button type="submit">Suffix lookalike</button>
      </form>
      <form id="query-lookalike" action="${miniformOrigin}/other?next=/forms/not-miniform/submit" method="post">
        <button type="submit">Query lookalike</button>
      </form>
    `);
    await page.addScriptTag({ url: `${miniformOrigin}/assets/miniform.js` });

    const submissionsWereNotCanceled = await page.locator("form").evaluateAll((forms) => forms.map((form) => (
      form.dispatchEvent(new SubmitEvent("submit", {
        bubbles: true,
        cancelable: true,
        submitter: form.querySelector("button"),
      }))
    )));

    expect(submissionsWereNotCanceled).toEqual([true, true]);
  });

  test("includes and disables the clicked submitter with the FormData fallback", async ({ page }) => {
    let submittedBody = "";
    await page.route("**/forms/sdk-submitter/submit**", async (route) => {
      submittedBody = route.request().postData() || "";
      await new Promise((resolve) => setTimeout(resolve, 300));
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        headers: { "Access-Control-Allow-Origin": consumerURL },
        body: JSON.stringify({ ok: true }),
      });
    });

    await page.goto("/_health");
    const miniformOrigin = new URL(page.url()).origin;
    await page.goto(consumerURL);
    await page.setContent(`
      <form data-miniform action="${miniformOrigin}/forms/sdk-submitter/submit?token=test" method="post">
        <input name="message" value="hello">
        <button type="submit" name="intent" value="draft">Save draft</button>
        <button type="submit" name="intent" value="publish">Publish</button>
      </form>
    `);
    await page.evaluate(() => {
      const NativeFormData = window.FormData;
      window.FormData = function (form) {
        return form ? new NativeFormData(form) : new NativeFormData();
      };
      window.FormData.prototype = NativeFormData.prototype;
    });
    await page.addScriptTag({ url: `${miniformOrigin}/assets/miniform.js` });

    const draft = page.getByRole("button", { name: "Save draft" });
    const publish = page.getByRole("button", { name: "Publish" });
    await publish.click();
    await expect(publish).toBeDisabled();
    await expect(draft).toBeEnabled();
    await expect(page.getByRole("status")).toHaveText("Form submitted successfully!");
    await expect(publish).toBeEnabled();

    expect(submittedBody).toContain('name="intent"');
    expect(submittedBody).toContain("publish");
    expect(submittedBody).not.toContain("draft");
  });

  test("shows unknown status and resets Turnstile once after a network failure", async ({ page }) => {
    let requestCount = 0;
    await page.route("**/forms/sdk-network-failure/submit**", async (route) => {
      requestCount += 1;
      await route.abort("failed");
    });

    await page.goto("/_health");
    const miniformOrigin = new URL(page.url()).origin;
    await page.goto(consumerURL);
    await page.setContent(`
      <form data-miniform action="${miniformOrigin}/forms/sdk-network-failure/submit?token=test" method="post">
        <input name="message" value="hello">
        <input name="cf-turnstile-response" value="single-use-token">
        <div class="cf-turnstile"></div>
        <button type="submit">Send</button>
      </form>
    `);
    await page.evaluate(() => {
      window.turnstileResets = [];
      window.turnstile = {
        reset(selector) {
          if (!document.querySelector(selector)) throw new Error("unknown widget");
          window.turnstileResets.push(selector);
        },
      };
    });
    await page.addScriptTag({ url: `${miniformOrigin}/assets/miniform.js` });

    await page.getByRole("button", { name: "Send" }).click();

    await expect(page.getByRole("alert")).toHaveText(
      "Submission status unknown. Check before trying again.",
    );
    await expect.poll(() => page.evaluate(() => window.turnstileResets.length)).toBe(1);
    await page.waitForTimeout(1_200);
    expect(requestCount).toBe(1);
  });

  test("blocks concurrent submits without altering control state or button markup", async ({ page }) => {
    let requestCount = 0;
    await page.route("**/forms/sdk-pending/submit**", async (route) => {
      requestCount += 1;
      await new Promise((resolve) => setTimeout(resolve, 300));
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        headers: { "Access-Control-Allow-Origin": consumerURL },
        body: JSON.stringify({ ok: true }),
      });
    });

    await page.goto("/_health");
    const miniformOrigin = new URL(page.url()).origin;
    await page.goto(consumerURL);
    await page.setContent(`
      <form data-miniform action="${miniformOrigin}/forms/sdk-pending/submit?token=test" method="post">
        <input name="message" value="hello">
        <input name="locked" value="keep-disabled" disabled>
        <button type="submit"><span>Send</span><svg aria-hidden="true"></svg></button>
      </form>
    `);
    await page.addScriptTag({ url: `${miniformOrigin}/assets/miniform.js` });

    const form = page.locator("form");
    const message = page.getByRole("textbox").first();
    const locked = page.locator('[name="locked"]');
    const submit = page.getByRole("button", { name: "Send" });
    const originalMarkup = await submit.evaluate((button) => button.innerHTML);

    await submit.click();
    await expect(submit).toBeDisabled();
    await expect(message).toBeEnabled();
    await expect(locked).toBeDisabled();
    expect(await submit.evaluate((button) => button.innerHTML)).toBe(originalMarkup);
    await form.evaluate((element) => element.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true })));

    await expect(page.getByRole("status")).toHaveText("Form submitted successfully!");
    await expect(submit).toBeEnabled();
    await expect(message).toBeEnabled();
    await expect(locked).toBeDisabled();
    expect(await submit.evaluate((button) => button.innerHTML)).toBe(originalMarkup);
    expect(requestCount).toBe(1);
  });
});
