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

## Apple Container networking

Requires Apple silicon, macOS 26, and the official `container` CLI.

```bash
container system status
container list --all
make apple-container-health
```

If host port forwarding resets while direct container-IP health checks work, allow `container-runtime-linux` under **System Settings → Privacy & Security → Local Network**, then restart the container system.

## E2E browser installation failures

Run `make test-e2e-setup`. In CI or Linux, Playwright may need system packages and therefore uses `npx playwright install --with-deps chromium`.

## Asking for help

Follow [SUPPORT.md](../SUPPORT.md) and redact secrets, submission contents, and personal data from logs.
