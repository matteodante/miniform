# Troubleshooting

## `runtime/cgo` or compiler errors on macOS

Miniform's SQLite driver requires CGO. Confirm Xcode Command Line Tools are selected:

```bash
xcode-select --print-path
cc --version
go env CC CGO_ENABLED
```

If a language manager shadows `cc`, configure it to use the Xcode toolchain globally or invoke the build with `CC=/usr/bin/clang`. The normal repository commands should not require a custom downloaded Swift toolchain.

## Login loops in production

Production cookies are `Secure`. If Miniform is served over plain HTTP, the browser discards the cookie and login appears to loop. Terminate HTTPS at the reverse proxy and forward requests to Miniform over the private interface.

Also confirm that `MINIFORM_SESSION_SECRET` remains identical across restarts.

## Lost first-run password

The unique temporary password appears once in application output. For a container:

```bash
app_container="$(docker ps \
  --filter 'name=^miniform$' \
  --filter 'name=^miniform-next$' \
  --format '{{.Names}}' | head -n 1)"
test -n "$app_container"
docker logs "$app_container"
```

If it is unavailable, reset it inside the container without deleting the database:

```bash
app_container="$(docker ps \
  --filter 'name=^miniform$' \
  --filter 'name=^miniform-next$' \
  --format '{{.Names}}' | head -n 1)"
test -n "$app_container"
printf '%s' "$NEW_PASSWORD" | docker exec -i "$app_container" \
  miniform account reset-password --new-password-file -
```

## SQLite busy or locked errors

Do not place the database on an unreliable network filesystem. Confirm that the process can write to the full data directory and that only the intended Miniform process uses the database. Application writes already retry bounded busy errors; persistent lock failures usually indicate storage or process misuse.

## Container cannot write `/app/storage`

Prefer a named volume. For bind mounts, ensure the container's runtime user can write the host directory. Do not make submission storage world-writable.

The entrypoint rejects non-normalized paths and any existing symlink component in the configured data, log, or database path before changing ownership. Remove the symlink or mount the intended directory directly. Restored database, WAL, log, upload-staging, and upload-deletion files must become readable and writable by UID `10001`; startup fails explicitly when that cannot be established.

## Apple Container networking

Requires Apple silicon, macOS 26, and the official `container` CLI.

`make apple-container-run` uses the persistent `miniform-data` volume. When the volume does not exist yet, the target copies a non-empty legacy project `storage/` directory into it before startup and leaves the source directory untouched.

```bash
container system status
container list --all
make apple-container-health
```

If host port forwarding resets while direct container-IP health checks work, allow `container-runtime-linux` under **System Settings → Privacy & Security → Local Network**, then restart the container system.

## E2E browser installation failures

Run `make test-e2e-setup`. In CI or Linux, Playwright may need system packages and therefore uses `npx playwright install --with-deps chromium`.

Each E2E run creates an owned temporary data directory and removes only that directory during global teardown. Set `MINIFORM_E2E_DATA_DIR` to preserve and inspect a specific directory after the run; the teardown never removes an explicitly supplied directory.

## Email accepted but classified as spam

A `delivered` notification event means the SMTP server accepted the message. It does not report its destination folder, spam verdict, or later forwarding. A successful test at one mail provider does not establish delivery at another.

### Check the sending route and authentication

Confirm the notification's mailer profile, SMTP host, port, TLS mode, authenticated account, and configured From address. Use the SMTP provider's documented settings and a sender that the account is authorized to use. Keep the submitter's address in `Reply-To` rather than replacing the configured From address.

Download the original message from the receiving mailbox and inspect its headers:

| Check | What to verify |
| --- | --- |
| `Message-ID` | Miniform 0.3.4 and later generate this header with the sender's domain. Its presence does not prove authentication or explain a spam verdict. |
| SPF | The receiving server checks the connecting IP against the SMTP envelope sender's domain, normally visible in `Return-Path`. |
| DKIM | Check the receiver's verification result and the signature's `d=` signing domain and `s=` selector. A provider's valid signature can still be unaligned with the From domain. |
| DMARC | At least one passing SPF or DKIM identity must align with the visible From domain according to the published alignment policy. Both need not align for DMARC to pass. |

See [DMARC identifier alignment](https://www.rfc-editor.org/rfc/rfc9989.html#section-4.4). Passing authentication does not guarantee inbox placement. Missing SPF or DMARC results in the original headers do not establish a failed check; seek the receiving provider's logs or support. Do not add `Authentication-Results` headers to templates: the receiving system supplies its own results.

Configure SPF and DMARC in the sender domain's DNS. Configure DKIM signing at the SMTP provider and publish its required DNS records. A published public key alone does not prove that outgoing messages use that domain; verify a newly received signature. Miniform does not manage these DNS records or apply DKIM signatures.

For Aruba, follow the [DKIM setup guide](https://guide.aruba.it/hosting-e-domini/email/gestione-email/sicurezza-e-storico-accessi/usare-dkim) for the actual email service before publishing its key. Aruba's [antispam guide](https://guide.aruba.it/hosting-e-domini/email/webmail/servizi-e-strumenti-aggiuntivi/antivirus-antispam) describes a mode that wraps suspicious messages as attachments. If that occurs, preserve both the wrapper and the original `.eml`; this is different from the text/HTML alternatives Miniform sends.

### Test the complete receiving path

1. Start with a local capture server or SMTP sandbox. For an authorized live test, use synthetic submission data, a unique subject marker, and an isolated endpoint with static recipients limited to the intended test mailboxes. Use the production SMTP profile and notification template being investigated.
2. Submit once and inspect the notification's delivery event. Saving or previewing a notification does not send it. Every enabled notification on the chosen endpoint may send, so check all recipients first.
3. In each target mailbox, confirm the message is directly readable in the inbox, inspect text and HTML where configured, and check for any spam wrapper or unexpected attachment. Save the original headers, receiver verdict, timestamp, and Message-ID.
4. If mail is forwarded, verify the forwarding rule and the final mailbox separately. Forwarding can affect SPF; a preserved, valid DKIM signature aligned with From can still satisfy DMARC. Recheck authentication and folder placement after forwarding.
5. Disable temporary notifications after the test. Record which receiving paths passed and which remain untested; an SMTP success or an unrelated mailbox's inbox result cannot close the investigation.

Keep antispam protection enabled while diagnosing. If filtering persists, provide the receiving provider with the original message and ask for the specific filtering reason. Fixing a missing header without a receiver-side retest does not establish the cause or resolution. Redact credentials and personal submission data before sharing evidence publicly.

## Asking for help

Follow [SUPPORT.md](../SUPPORT.md) and redact secrets, submission contents, and personal data from logs.
