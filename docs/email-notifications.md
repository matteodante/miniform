# Email notifications

Each form owns zero or more independent email notifications. A notification defines its SMTP route, recipient source, optional `Reply-To`, subject, text body, and optional HTML body.

Multiple static recipients on one notification receive the same message in one SMTP transaction. Create two notifications when the recipients, headers, content, status, or retries must differ.

## Common two-message setup

For a booking form containing `name`, `email`, and `message` fields:

1. Internal notification
   - recipient source: `static`
   - recipient: `bookings@example.com`
   - Reply-To source: `field`
   - Reply-To value: `email`
2. Customer confirmation
   - recipient source: `field`
   - recipient value: `email`
   - Reply-To source: `static`
   - Reply-To value: `Support <support@example.com>`

The first message lets the team reply directly to the submitter. The second sends a separate acknowledgement to the validated address submitted in the `email` field.

Use **Add notification** on the endpoint page to configure each message. The first notification remains editable from the endpoint editor for compatibility with existing installations.

## Rendered preview

The notification editor can render its current unsaved subject, addresses, text body, and HTML body against one of the endpoint's 25 latest submissions. Select an inbox entry under **Preview with**, then choose **Preview email**.

The preview uses the same renderer as SMTP delivery, including contextual HTML escaping, subject validation, dynamic recipient resolution, and the plain-text fallback. HTML is displayed inside a sandboxed iframe. Preview responses are marked `no-store`; previewing does not save the notification, create a delivery event, connect to SMTP, or send an email.

An endpoint needs at least one submission before a data-backed preview is available. Use only test submissions when the template contains personal data that should not be displayed during configuration.

## Address sources

`recipient_source` accepts:

- `static`: an RFC 5322 comma-separated address list;
- `field`: the name of one submitted field containing one address.

`reply_to_source` accepts:

- `none`: omit the `Reply-To` header;
- `static`: one RFC 5322 address;
- `field`: the name of one submitted field containing one address.

Field-derived addresses are parsed as single RFC 5322 mailboxes. Missing, malformed, multiline, or null-byte values fail that notification before any SMTP connection is opened. Other notifications for the same submission retain their own status and continue independently.

A field-derived recipient lets a public submission choose one outbound address. Use it for confirmations only on endpoints protected with restrictive origins and, where appropriate, Turnstile; otherwise the endpoint can be abused to send unwanted mail.

## Template data

Subjects and text bodies use Go `text/template`; HTML bodies use Go `html/template`, which context-escapes submitted values. Available data:

| Expression | Value |
| --- | --- |
| `{{.FormName}}` | Endpoint name |
| `{{.SubmittedAt}}` | RFC 3339 UTC submission time |
| `{{.Fields.email}}` | A submitted field with an identifier-like name |
| `{{index .Fields "field-name"}}` | A submitted field with any name |
| `{{range .FieldList}}...{{end}}` | All fields, sorted by name; each item has `.Name` and `.Value` |

Example subject:

```gotemplate
New appointment request · {{.Fields.name}}
```

Example text template:

```gotemplate
Hello {{.Fields.name}},

we received your request on {{.SubmittedAt}}.
```

Example HTML template:

```html
<!doctype html>
<html lang="en">
  <body>
    <h1>Hello {{.Fields.name}}</h1>
    <p>We received your request on {{.SubmittedAt}}.</p>
  </body>
</html>
```

HTML notifications are sent as `multipart/alternative` and always include the configured text body. A rendered subject must be one non-empty line; newline and null-byte output is rejected before SMTP to prevent header injection.

## CLI

Create template files first:

```text
# confirmation.txt
Hello {{.Fields.name}},

we received your request.
```

```html
<!-- confirmation.html -->
<p>Hello <strong>{{.Fields.name}}</strong>,</p>
<p>we received your request.</p>
```

Then create the customer confirmation:

```bash
miniform --json email create \
  --form-id 1 \
  --name 'Customer confirmation' \
  --enabled \
  --mailer-profile-id 1 \
  --recipient-source field \
  --recipient email \
  --reply-to-source static \
  --reply-to 'Support <support@example.com>' \
  --subject-template 'We received your request, {{.Fields.name}}' \
  --format html \
  --text-template-file ./confirmation.txt \
  --html-template-file ./confirmation.html
```

Inspect and update notifications:

```bash
miniform --json email list --form-id 1
miniform --json email get --id 2
miniform --json email update --id 2 --reply-to-source field --reply-to email
```

`email get` includes both templates; `email list` returns compact configuration summaries. Deletion requires explicit confirmation:

```bash
miniform email delete --id 2 --yes
```

Deleting a notification preserves its historical events. A queued event whose notification no longer exists finishes as failed rather than being redirected to another configuration.

## Delivery lifecycle

Each accepted non-spam submission creates one durable event for every enabled notification. Workers claim, deliver, retry, and finish those events independently. SMTP I/O happens outside SQLite transactions. Configuration changes affect attempts that have not yet been delivered; a manual retry uses the current notification configuration.

Use a local capture server or an SMTP sandbox when testing delivery. Merely saving or previewing a notification does not send a message; an accepted, non-spam submission is what enqueues delivery for every enabled notification.

## Automated delivery tests

The repository includes two isolated SMTP suites:

```bash
go test -count=1 -run TestEmailDelivery -v ./internal/jobs
make test-stress
```

The focused suite covers two notifications from one submission, static and field-derived recipients, `Reply-To`, text and HTML rendering, escaping, malformed recipient isolation, retries, and cancellation. The stress suite runs the real HTTP server and background worker against temporary SQLite storage, captures concurrent SMTP traffic on loopback, injects transient SMTP failures, restarts with queued work, rejects every recipient outside `.invalid`, and checks delivery counts, duplicates, CPU, and RSS. Neither suite connects to an external SMTP service.
