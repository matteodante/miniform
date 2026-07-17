# Installation and upgrades

This guide covers supported installation paths. For local development, use [development.md](development.md).

## Requirements

Miniform supports Linux `amd64` and `arm64` for production containers and binaries. Building from source also supports macOS `amd64` and `arm64`.

Production deployments require:

- HTTPS at a reverse proxy
- Persistent storage for `/app/storage`
- A stable, randomly generated `MINIFORM_SESSION_SECRET`
- A backup process that copies the SQLite database safely

## OCI container

Versioned images will be published to `ghcr.io/matteodante/miniform` beginning with the first public release. Pin an exact version in production; do not deploy `latest` unattended.

```bash
docker volume create miniform-data
docker run --detach --name miniform \
  --restart unless-stopped \
  --publish 127.0.0.1:8080:8080 \
  --env MINIFORM_ENV=production \
  --env MINIFORM_SESSION_SECRET="$(openssl rand -hex 32)" \
  --volume miniform-data:/app/storage \
  ghcr.io/matteodante/miniform:v0.1.0
```

Generate the session secret once, store it in your secret manager, and reuse it across restarts. The inline command above is illustrative; shell history and process inspection may expose values on shared systems.

Terminate TLS with Caddy, Nginx, Traefik, or another reverse proxy. Miniform's production cookie is `Secure`; authentication will fail over plain HTTP.

## Build from source

Install Go 1.26.5, a C compiler, Node.js 20 or newer, and `make`:

```bash
git clone https://github.com/matteodante/miniform.git
cd miniform
make bootstrap
make build
MINIFORM_ENV=production \
MINIFORM_SESSION_SECRET='replace-with-a-secret-manager-value' \
./bin/miniform
```

The SQLite driver requires CGO. Do not build release binaries with `CGO_ENABLED=0`.

## Installer

The installer downloads a versioned Linux release, verifies its SHA-256 checksum, and launches the interactive system installer. Review scripts before running them as root:

```bash
curl -fsSLO https://raw.githubusercontent.com/matteodante/miniform/main/install.sh
less install.sh
sudo bash install.sh
```

Avoid piping remote scripts directly into a privileged shell.

## First boot

On an empty database, Miniform creates `admin@miniform.local` with a unique temporary password and prints it once to standard output. Retrieve container logs if necessary:

```bash
docker logs miniform
```

Sign in immediately and change both the email address and password. The temporary password is never stored in plaintext.

## Upgrades

1. Read [CHANGELOG.md](../CHANGELOG.md) and the release notes.
2. Back up the database and uploaded files.
3. Pull or download the exact target version.
4. Replace the process or container while preserving storage and its environment configuration.
5. Confirm `/_health`, sign-in, a test submission, and configured deliveries.

Database migrations run automatically at startup. Do not skip versions when release notes include an explicit staged migration.

Before the first public release, development schemas are not preserved: recreate development storage when `main` changes its schema.

## Backups

Back up the full storage directory or use the management command installed by the system installer. A useful backup is one that has been restored and verified. Keep backups encrypted and access-controlled because submissions and integration credentials may contain personal or sensitive data.

## Uninstall

Stop and remove the process or container, then remove application files. Persistent storage is intentionally not removed automatically. Delete it only after confirming retention and backup requirements.
