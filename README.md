# Miniform

[![CI](https://github.com/matteodante/miniform/actions/workflows/ci.yml/badge.svg)](https://github.com/matteodante/miniform/actions/workflows/ci.yml)
[![CodeQL](https://github.com/matteodante/miniform/actions/workflows/codeql.yml/badge.svg)](https://github.com/matteodante/miniform/actions/workflows/codeql.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-111111.svg)](LICENSE)

**A quiet, self-hosted inbox for form submissions.**

Miniform accepts submissions and file uploads from any HTML form, stores them in SQLite, and forwards them to webhooks or SMTP. It ships as one Go binary and one OCI image, with no hosted account or external database.

> Miniform is preparing its first public release. Until a versioned release is published, build from source and treat `main` as development software.

## Why Miniform

- Private, searchable inbox for submissions and files
- Independent forms with tokens and allowed-origin policies
- Retried webhook and email delivery with visible history
- Honeypot, rate limiting, origin checks, and per-endpoint Turnstile
- Embedded web UI and SQLite persistence in a single process
- Standard HTML forms first; the small JavaScript helper is optional

## Quick start from source

Requirements: Go 1.26.5, a C compiler, Node.js 20 or newer, and `make`.

```bash
git clone https://github.com/matteodante/miniform.git
cd miniform
make bootstrap
make run
```

Open <http://127.0.0.1:8080>. On the first boot, Miniform prints a unique temporary admin password to the terminal. Change it after signing in.

For a disposable local instance with sample data and a working submission page:

```bash
make demo
```

Open <http://127.0.0.1:8080/_demo>. The isolated admin account is `admin@miniform.local` with password `miniform`, and its data stays under `tmp/demo`.

## Run in a container

Build the same OCI-compatible `Dockerfile` used by releases:

```bash
docker build --tag miniform:local .
docker volume create miniform-data
docker run --rm --name miniform \
  --publish 8080:8080 \
  --env MINIFORM_ENV=development \
  --volume miniform-data:/app/storage \
  miniform:local
```

On Apple silicon with macOS 26 and the official [`apple/container`](https://github.com/apple/container) CLI:

```bash
make apple-container-run
make apple-container-health
make apple-container-stop
```

Production mode requires HTTPS and a persistent `MINIFORM_SESSION_SECRET`. See the [installation guide](docs/installation.md) before exposing Miniform to the internet.

## Documentation

- [Installation and upgrades](docs/installation.md)
- [Configuration reference](docs/configuration.md)
- [CLI reference](docs/cli.md)
- [Architecture](docs/architecture.md)
- [Development guide](docs/development.md)
- [Troubleshooting](docs/troubleshooting.md)
- [Release process](docs/releasing.md)
- [Brand guidelines](docs/brand-guidelines.md)

## Project health

- Bugs and feature proposals: [GitHub Issues](https://github.com/matteodante/miniform/issues)
- Questions and ideas: [GitHub Discussions](https://github.com/matteodante/miniform/discussions)
- Vulnerabilities: follow the private process in [SECURITY.md](SECURITY.md)
- Planned work: [ROADMAP.md](ROADMAP.md)
- Release history: [CHANGELOG.md](CHANGELOG.md)

## Contributing

Contributions are welcome. Read [CONTRIBUTING.md](CONTRIBUTING.md), the [governance model](GOVERNANCE.md), and the [Code of Conduct](CODE_OF_CONDUCT.md) before opening a pull request.

## License

Miniform is available under the [MIT License](LICENSE). Runtime dependency and vendored asset notices are listed in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
