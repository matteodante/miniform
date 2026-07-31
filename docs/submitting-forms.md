# Submitting forms to Miniform

Miniform accepts native HTML form posts and direct HTTP requests. It does not require a client library.

## Endpoint

Every public submission uses this route:

```text
POST https://forms.example.com/forms/FORM_SLUG/submit?token=FORM_TOKEN
```

Copy the slug and token from the endpoint page or generate ready-to-use markup with:

```bash
miniform --show-secrets form code \
  --slug contact \
  --base-url https://forms.example.com
```

The token authorizes public submissions and is expected to appear in the page containing the form. Rotate it from the admin or with `miniform form rotate-token` if it is abused. Do not reuse an operator session or another administrative secret in public code.

The caller hostname conveyed by `Origin` or `Referer` must also match the endpoint's allowed-origin policy. Browsers send these headers automatically. A server-side client must send a matching `Origin` header unless the endpoint allows `*`.

## Native HTML

Native submission is the simplest integration and remains functional without JavaScript:

Starter HTML copied from the Miniform admin includes the `__mf_hp` honeypot automatically. When supplying custom Starter HTML, Miniform injects it unless a field with that name already exists.

```html
<style>
  .honeypot {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip-path: inset(50%);
  }
</style>

<form
  action="https://forms.example.com/forms/contact/submit?token=FORM_TOKEN"
  method="post"
>
  <label>
    Email
    <input type="email" name="email" required>
  </label>

  <label>
    Message
    <textarea name="message" required></textarea>
  </label>

  <label class="honeypot" aria-hidden="true">
    Leave this field empty
    <input type="text" name="__mf_hp" tabindex="-1" autocomplete="off">
  </label>
  <input type="hidden" name="_success_url" value="/thanks">
  <input type="hidden" name="_error_url" value="/contact-error">

  <button type="submit">Send</button>
</form>
```

Relative redirect paths resolve against the website that hosts the form. Absolute redirect URLs require an explicit allowed hostname; `*` never authorizes an absolute redirect. If no redirect field is present, Miniform returns JSON.

First enable uploads on the Miniform endpoint. Add `enctype="multipart/form-data"` only when the form contains a file input:

```html
<form
  action="https://forms.example.com/forms/apply/submit?token=FORM_TOKEN"
  method="post"
  enctype="multipart/form-data"
>
  <input type="email" name="email" required>
  <input type="file" name="resume" accept=".pdf" required>
  <button type="submit">Apply</button>
</form>
```

## Browser `fetch`

Use `FormData` to preserve repeated fields and files. Do not set `Content-Type`: the browser must add the multipart boundary. Send once and leave an ambiguous network failure to the user; automatic retries can create duplicate submissions.

```js
const form = document.querySelector("#contact-form");

form.addEventListener("submit", async (event) => {
  event.preventDefault();

  const button = form.querySelector('[type="submit"]');
  const data = new FormData(form);
  data.delete("_success_url");
  data.delete("_error_url");
  button.disabled = true;

  try {
    const response = await fetch(form.action, {
      method: "POST",
      headers: { Accept: "application/json" },
      body: data,
    });
    const result = await response.json();
    if (!response.ok) throw new Error(result.error || "Submission failed");

    console.log("Stored submission", result.submission_id);
    form.reset();
  } catch (error) {
    console.error(error);
  } finally {
    button.disabled = false;
  }
});
```

Deleting the redirect controls keeps the response as JSON. If they remain in `FormData`, `fetch` follows Miniform's redirect instead.

For JSON-only integrations:

```js
const response = await fetch(
  "https://forms.example.com/forms/contact/submit?token=FORM_TOKEN",
  {
    method: "POST",
    headers: {
      Accept: "application/json",
      "Content-Type": "application/json",
    },
    body: JSON.stringify({ email: "ada@example.com", message: "Hello" }),
  },
);

const result = await response.json();
if (!response.ok) throw new Error(result.error || "Submission failed");
```

JSON requests cannot upload files.

## `curl` and server-side clients

URL-encoded fields:

```bash
curl --fail-with-body \
  -H 'Accept: application/json' \
  -H 'Origin: https://www.example.com' \
  --data-urlencode 'email=ada@example.com' \
  --data-urlencode 'message=Hello from curl' \
  'https://forms.example.com/forms/contact/submit?token=FORM_TOKEN'
```

JSON:

```bash
curl --fail-with-body \
  -H 'Accept: application/json' \
  -H 'Content-Type: application/json' \
  -H 'Origin: https://www.example.com' \
  --data '{"email":"ada@example.com","topics":["support","billing"]}' \
  'https://forms.example.com/forms/contact/submit?token=FORM_TOKEN'
```

Multipart with a file:

```bash
curl --fail-with-body \
  -H 'Accept: application/json' \
  -H 'Origin: https://www.example.com' \
  -F 'email=ada@example.com' \
  -F 'resume=@./resume.pdf' \
  'https://forms.example.com/forms/apply/submit?token=FORM_TOKEN'
```

Replace the `Origin` value with a hostname permitted by that endpoint. This origin policy constrains callers but does not identify a trusted backend; use the submission token as the endpoint capability and authenticate sensitive workflows in the receiving application.

Submitted field names are also available to configured email notifications. For example, a notification can use the `email` field as its validated recipient or `Reply-To`, and templates can render `{{.Fields.email}}`. See [Email notifications](email-notifications.md) for the complete template and address-source contract.

## Responses

A successful request without `_success_url` returns HTTP `200`:

```json
{
  "ok": true,
  "submission_id": 42,
  "received_at": "2026-07-18T12:34:56Z"
}
```

Submission route and middleware errors return a `4xx` or `5xx` status with:

```json
{
  "ok": false,
  "error": "origin not allowed"
}
```

Always branch on the HTTP status or `response.ok`; do not treat a parsed JSON body alone as success. An HTTP server or reverse proxy can reject an oversized or malformed request before it reaches the submission route; treat any non-success status as failure even when that transport-level response is not JSON.

## Reserved fields and limits

- `_success_url` and `_error_url` control browser redirects and are not stored.
- `__mf_hp` is the honeypot. Leave it empty and hide it from people, not from bots. A filled honeypot is recorded as spam without uploads or delivery jobs.
- `cf-turnstile-response` is consumed when the endpoint has a Turnstile profile. Markup generated by Miniform includes the required widget and Cloudflare script.
- Repeated URL-encoded or multipart fields are stored as arrays.
- A legitimate request needs at least one stored field or file after reserved fields are consumed.
- The default scalar-field limit is `200`; combined scalar data is limited to 64 KiB. Configure them with `MINIFORM_MAX_INPUT_FIELDS` and `MINIFORM_MAX_PAYLOAD_BYTES`.
- The complete request body is limited to 6 MiB.
- Uploads are disabled per endpoint by default. An enabled endpoint accepts one file up to 5 MiB with `.jpg`, `.jpeg`, `.png`, `.gif`, `.webp`, `.pdf`, `.txt`, or `.csv`.
- Miniform verifies detected content against the extension, ignores the client MIME claim, generates a random storage name, and enforces the global `MINIFORM_MAX_UPLOAD_STORAGE_BYTES` quota.

See the complete framework-free example in [`examples/simple-form.html`](../examples/simple-form.html).
