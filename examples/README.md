# Plain HTML example

`simple-form.html` is a framework-free form that posts directly to Miniform. Copy it into a site and replace these placeholders in its `action` URL:

- `YOUR_FORM_SLUG` with the endpoint slug;
- `YOUR_FORM_TOKEN` with its public submission token;
- `http://localhost:8080` with the Miniform origin.

The example also demonstrates:

- `_success_url` and `_error_url` browser redirects;
- the `__mf_hp` honeypot field;
- regular named fields that Miniform stores as JSON.

The redirect controls and honeypot are consumed by Miniform and are not stored in the entry payload. Relative redirect paths must exist on the website serving the form; absolute redirects must match the endpoint’s allowed origins.

Open `simple-form.html` through a local web server instead of `file://`, then allow that server’s host in the endpoint settings. A legitimate submission needs at least one regular field or uploaded file.

JavaScript is optional: native forms use redirects, while a custom `fetch` integration can handle the JSON response in place. See [Submitting forms to Miniform](../docs/submitting-forms.md) for native HTML, `fetch`, JSON, file upload, and `curl` examples.
