# CLI reference

Miniform exposes the operator workflow through the `miniform` binary. The CLI uses the same domain functions and SQLite retry transactions as the Web UI.

## Run a command

From a source checkout or standalone binary:

```bash
miniform --json form list
```

Inside an OCI deployment, run the binary in the application container so it reads the mounted `/app/storage` volume and the container environment:

```bash
docker exec miniform miniform --json form list
```

Set `MINIFORM_ENV`, `MINIFORM_DATA_DIR`, and related variables exactly as the server process when you run the CLI outside a container. `config show` prints the resolved database path.

For data and administration resources, global flags may appear before or after the resource and action:

| Flag | Effect |
| --- | --- |
| `--json` | Emit one compact JSON object on stdout. |
| `--show-secrets` | Include tokens and stored credentials. The default output redacts them. |

Use the built-in command catalog instead of parsing help text:

```bash
miniform commands --json
miniform help form create
miniform --json help form
```

`commands --json` returns each command name, flags, mutation status, database requirement, examples, and safety notes.
Its `supports_json` field distinguishes deterministic resource commands from deployment commands such as `install`, `update`, and `reload`, which use human-oriented output.

## Output contract

A successful command with `--json` writes one object to stdout:

```json
{"ok":true,"command":"form.list","data":[]}
```

A failed command writes one object to stderr:

```json
{"ok":false,"command":"form.get","error":{"code":"not_found","message":"resource not found"}}
```

Exit codes remain stable for scripts and agents:

| Code | Meaning |
| ---: | --- |
| `0` | Success |
| `2` | Invalid command or flags |
| `3` | Validation or credential-verification failure |
| `4` | Resource not found |
| `5` | Conflict, duplicate, referenced profile, or existing destination |
| `10` | Internal or storage failure |

Without `--json`, commands print indented JSON data. File streaming with `submission file-copy --output -` writes raw bytes and rejects `--json`.

## Secret input

Password, token, and secret flags accept a file path or `-` for stdin. This keeps secrets out of shell history and process listings.

```bash
printf '%s' "$NEW_PASSWORD" | miniform account reset-password \
  --new-password-file -
```

One command can consume only one secret from stdin. Store additional secrets in permission-restricted files. Output redacts these fields unless you pass `--show-secrets`:

- form token, generated HTML, webhook secret, and webhook headers;
- SMTP password;
- captcha secret key;
- session secret.

Treat `--show-secrets` output as sensitive. Do not send it to logs, tickets, or model prompts that do not need the values.

## Account

```bash
miniform --json account show

printf '%s' "$CURRENT_PASSWORD" | miniform account set-email \
  --email admin@example.com \
  --current-password-file -

miniform account change-password \
  --current-password-file ./current-password \
  --new-password-file ./new-password

printf '%s' "$NEW_PASSWORD" | miniform account reset-password \
  --email admin@example.com \
  --new-password-file -
```

`account reset-password` bypasses current-password verification. Run it only from a trusted administrative shell with access to the Miniform database.

## Runtime configuration

Inspect the effective process configuration:

```bash
miniform --json config show
```

The command is read-only. Change `MINIFORM_*` variables in the process manager or container environment, then restart Miniform.

## Forms

List, inspect, create, update, rotate, and delete endpoints:

```bash
miniform --json form list
miniform --json form get --id 1
miniform --json --show-secrets form get --slug contact
miniform --json form code --slug contact
miniform --json --show-secrets form code --slug contact

miniform --json form create \
  --template contact \
  --allowed-origins 'example.com,*.example.org'

miniform form create \
  --name 'Contact API' \
  --slug contact-api \
  --allowed-origins '*' \
  --webhook-enabled \
  --webhook-url https://hooks.example.com/miniform \
  --webhook-secret-file ./webhook-secret \
  --webhook-headers-file ./webhook-headers.json

miniform form update \
  --id 1 \
  --name 'Customer contact' \
  --email-enabled=false \
  --use-sdk=true

miniform --show-secrets form rotate-token --id 1 --yes
miniform form delete --id 1 --yes
```

`form update` preserves omitted flags. Pass explicit boolean values to disable features. Clear nullable or secret values with the corresponding `--clear-*` flag.

`form code` produces the final copyable HTML, including captcha markup and the optional SDK. It follows the stored `use_sdk` setting unless `--include-sdk=true|false` is passed. By default the action uses `YOUR_FORM_TOKEN`; pass `--show-secrets` only when deployable HTML with the live token is required.

Email forwarding requires `--email-enabled`, `--mailer-profile-id`, and `--email-recipient`. Webhook forwarding requires `--webhook-enabled` and `--webhook-url`.

Complex form values come from files:

- `--generated-html-file`: HTML stored for the embed view;
- `--webhook-headers-file`: JSON object whose values are strings.

Assigning `--captcha-profile-id` makes Turnstile mandatory for every public submission to that form. Use `--clear-captcha-profile` to disable it.

Built-in templates are discoverable without SQLite:

```bash
miniform --json form template-list
miniform --json form template-get --template contact
miniform --json form template-get \
  --template contact \
  --action 'https://forms.example.com/forms/contact/submit?token=TOKEN'
```

Creating with `--template` fills the template name and slug and stores HTML with the live form action. The blank template still requires `--slug`.

## Mailers

```bash
miniform --json mailer list
miniform --json mailer get --id 1

miniform mailer create \
  --name primary-smtp \
  --smtp-host smtp.example.com \
  --smtp-port 587 \
  --smtp-username miniform \
  --smtp-password-file ./smtp-password \
  --smtp-encryption starttls \
  --default-from-email forms@example.com

miniform mailer update --id 1 --smtp-host smtp2.example.com
miniform mailer delete --id 1 --yes
```

Updates preserve omitted passwords. Use `--clear-smtp-password` to remove one. Miniform rejects deletion while a form references the profile.

## Captcha profiles

```bash
miniform --json captcha list
miniform --json captcha get --id 1

miniform captcha create \
  --name production-turnstile \
  --site-key PUBLIC_SITE_KEY \
  --secret-key-file ./turnstile-secret

miniform captcha update --id 1 --site-key NEW_PUBLIC_SITE_KEY
miniform captcha delete --id 1 --yes
```

A profile is exactly a name, one Turnstile site key, and one secret key. Updates preserve omitted fields. The fixed Turnstile action is `submit`; assigning the profile to a form enables mandatory server-side validation.

## Submissions and files

List and search the inbox:

```bash
miniform --json submission list \
  --form-id 1 \
  --range 30d \
  --query customer@example.com \
  --spam=false \
  --page 1 \
  --per-page 50

miniform --json submission get --id 42
```

Create a submission from a JSON object. Repeat `--file` for uploads:

```bash
miniform --json submission create \
  --slug contact \
  --data-file ./payload.json \
  --file attachment=./brief.pdf \
  --file screenshot=./screen.png
```

The command applies the same file-count, size, and extension limits as public HTTP submissions. It creates webhook and email events from the form delivery policy.

List and copy uploaded files:

```bash
miniform --json submission file-list --id 42
miniform submission file-copy --id 42 --file-id 7 --output ./brief.pdf
miniform submission file-copy --id 42 --file-id 7 --output - > brief.pdf
```

File copy refuses to overwrite a destination unless you pass `--force`.

Delete a submission and its upload directory:

```bash
miniform submission delete --id 42 --yes
```

## Delivery events

```bash
miniform --json event list --type webhook --form-id 1 --status failed --limit 100
miniform --json event list --type email --status retrying

miniform event retry --type webhook --id 12 --yes
miniform event retry --type email --id 18 --yes
```

Retry resets the attempt counter and schedules the selected event for the background dispatcher. The server process must run for delivery to occur.

## Destructive operations

The following commands require `--yes` and never prompt:

```text
form delete
form rotate-token
mailer delete
captcha delete
submission delete
event retry
```

This rule keeps automated runs deterministic and makes destructive intent visible in command logs.
