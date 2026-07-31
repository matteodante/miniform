# Installation and upgrades

This guide covers supported installation paths. For local development, use [development.md](development.md).

## Requirements

Miniform supports Linux `amd64` and `arm64` for production containers and binaries. Building from source also supports macOS `amd64` and `arm64`.

Production deployments require:

- HTTPS at a reverse proxy
- Persistent storage for `/app/storage`
- A stable, randomly generated `MINIFORM_SESSION_SECRET`
- A backup process that copies SQLite safely and includes uploads when point-in-time recovery is required

## Docker Compose

The repository includes a production-oriented [`compose.yaml`](../compose.yaml) with one Miniform service and one persistent named volume. Generate and save the session secret once:

```bash
umask 077
printf 'MINIFORM_SESSION_SECRET=%s\n' "$(openssl rand -hex 32)" > .env.compose
docker compose --env-file .env.compose up --detach
docker compose --env-file .env.compose ps
docker compose --env-file .env.compose logs miniform
```

The default image is the current stable release, `ghcr.io/matteodante/miniform:v0.2.3`. To pin a different release, add `MINIFORM_IMAGE=ghcr.io/matteodante/miniform:vX.Y.Z` to `.env.compose`. The service binds `127.0.0.1:8080` by default; set `MINIFORM_PORT` in the same file if the reverse proxy needs another local port.

Terminate TLS with Caddy, Nginx, Traefik, or another reverse proxy. Miniform's production cookie is `Secure`, so authentication fails over plain HTTP. Keep `.env.compose` private, back up the `miniform-data` volume, and never run more than one application replica against the SQLite database.

To stop the service without deleting its data:

```bash
docker compose --env-file .env.compose down
```

## OCI container

Versioned images are published publicly to `ghcr.io/matteodante/miniform`. Pin an exact version in production; do not deploy `latest` unattended.

```bash
docker pull ghcr.io/matteodante/miniform:v0.2.3
docker volume create miniform-data
docker run --detach --name miniform \
  --restart unless-stopped \
  --publish 127.0.0.1:8080:8080 \
  --env MINIFORM_ENV=production \
  --env MINIFORM_SESSION_SECRET="$(openssl rand -hex 32)" \
  --volume miniform-data:/app/storage \
  ghcr.io/matteodante/miniform:v0.2.3
```

Generate the session secret once, store it in your secret manager, and reuse it across restarts. The inline command above is illustrative; shell history and process inspection may expose values on shared systems.

Terminate TLS with Caddy, Nginx, Traefik, or another reverse proxy. Miniform's production cookie is `Secure`; authentication will fail over plain HTTP.

## Build from source

Install Go 1.26.5, a C compiler, Node.js 24 or newer, and `make`:

```bash
git clone https://github.com/matteodante/miniform.git
cd miniform
cp .env.example .env
make bootstrap
make build
./bin/miniform
```

Edit `.env` before a production start, set `MINIFORM_ENV=production`, and provide a stable `MINIFORM_SESSION_SECRET`. Variables supplied by the process manager override `.env`.

The SQLite driver requires CGO. Do not build release binaries with `CGO_ENABLED=0`.

## Railway

The repository includes `railway.json`, which builds the Go application with Railpack, starts the generated `./out` binary, checks `/_health`, and runs one replica. `.railwayignore` excludes the root `Dockerfile` only from Railway uploads so automatic Dockerfile detection does not override Railpack; the Dockerfile remains available for OCI builds and releases. Attach a persistent volume at `/app/storage`, generate a public domain, and set at least:

```text
MINIFORM_ENV=production
MINIFORM_SESSION_SECRET=<stable-random-secret-at-least-32-characters>
```

The container already defaults to port `8080` and stores both the SQLite database and uploads below `/app/storage`. Miniform detects Railway through `RAILWAY_ENVIRONMENT_ID` and uses Railway's `X-Real-IP` header for per-client limits. Do not deploy without the volume: a replacement would lose submissions and integration configuration. Keep SMTP credentials in Miniform's access-controlled persistent storage, not in the static site that submits the form. Railway does not provide an application WAF by default; keep Miniform's limits enabled and add an edge WAF when the threat model requires one.

## Installer

Install and start Docker Engine first using the official instructions for your operating system. Miniform refuses installation when Docker is unavailable; it never pipes a remote Docker installer into a privileged shell.

The installer downloads a versioned Linux release, verifies its SHA-256 checksum, and launches that verified candidate. It replaces the installed manager only after installation succeeds. Review the script before running it as root:

```bash
curl -fsSLO https://raw.githubusercontent.com/matteodante/miniform/v0.2.3/install.sh
less install.sh
sudo bash install.sh
```

Avoid piping remote scripts directly into a privileged shell.

The managed installer currently resolves the `latest` application image only during an explicit install or `sudo miniform update`; it does not create an unattended update schedule. Use the direct OCI path with an exact version tag when every rollout must be digest- or tag-pinned.

## First boot

On an empty database, Miniform creates `admin@miniform.local` with a unique temporary password and prints it once to standard output. Retrieve container logs if necessary:

```bash
# Docker Compose installation
docker compose --env-file .env.compose logs miniform

# Direct or managed OCI installation
app_container="$(docker ps \
  --filter 'name=^miniform$' \
  --filter 'name=^miniform-next$' \
  --format '{{.Names}}' | head -n 1)"
test -n "$app_container"
docker logs "$app_container"
```

Managed updates alternate between the `miniform` and `miniform-next` container names, so resolve the running container instead of assuming one name.

On the first sign-in, only the password settings remain available until you replace the temporary password. Then update the email address. The temporary password is never stored in plaintext.

## Upgrades

For an installation managed by the system installer:

```bash
sudo miniform backup
sudo miniform update
```

`update` updates the manager and pulls images while the current process is still serving. It then stops the old process, creates a verified snapshot of the final stopped SQLite state, and starts the replacement. This intentionally trades a short maintenance window for single-process SQLite ownership. A pull failure leaves the process untouched; a post-stop backup or start failure attempts to restore and restart the previous deployment before returning an error.

For other OCI or process-manager deployments:

1. Read [CHANGELOG.md](../CHANGELOG.md) and the release notes.
2. Back up the database and uploaded files.
3. Pull or download the exact target version.
4. Stop the old process before starting the replacement while preserving storage and environment configuration.
5. Confirm `/_health`, sign-in, a test submission, and configured deliveries.

For Docker Compose, set `MINIFORM_IMAGE` in `.env.compose` to the exact target version, then run `docker compose --env-file .env.compose pull` followed by `docker compose --env-file .env.compose up --detach`. Compose replaces the single application container while preserving the named volume.

Database migrations run automatically at startup and preserve the legacy schemas covered by the compatibility tests. Do not skip versions when release notes include an explicit staged migration, and keep a restorable backup before every upgrade; hand-edited schemas are not supported.

The security-hardening migration signs out sessions created by older releases, leaves uploads disabled on existing endpoints until explicitly enabled, and disables enabled field-derived email recipients that do not have Turnstile. After the first start, sign in again and review endpoint upload and notification settings before accepting traffic.

## Backups

`sudo miniform backup` creates and verifies a consistent SQLite snapshot under `/var/matcha/miniform/backups` and keeps the three newest snapshots. It does not copy uploaded files. For a point-in-time disaster-recovery copy, stop Miniform and copy the full `/var/matcha/miniform/storage` directory; a separate live file copy is not coordinated with the database snapshot.

Restore a database interactively with:

```bash
sudo miniform restore-db
```

The command validates and stages the selected snapshot, stops Miniform, backs up the final stopped database state, atomically replaces the database, and starts one application process. If backup or startup fails, it attempts to return to the previous deployment. If the previous database was missing or invalid, rollback is impossible and Miniform remains stopped with an explicit error. A useful backup is one that has been restored and verified. Keep backups encrypted and access-controlled because submissions and integration credentials may contain personal or sensitive data.

## Uninstall

Stop and remove the process or container, then remove application files. Persistent storage is intentionally not removed automatically. Delete it only after confirming retention and backup requirements.
