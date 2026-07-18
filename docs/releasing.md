# Releasing

Miniform follows Semantic Versioning. Before 1.0, minor versions may include documented breaking changes; patch versions must remain backward compatible.

Release tags use stable `vMAJOR.MINOR.PATCH` versions only. Prerelease and build-metadata tags remain unsupported until the installed manager can compare them correctly.

## Prerequisites

- The release commit is on protected `main` with all required checks passing.
- `CHANGELOG.md` contains the release notes and migration guidance.
- The version has been tested from a clean clone.
- The OCI image has passed its health check on Docker and Apple Container.
- Dependency, vulnerability, license, and secret audits are clean or have documented accepted findings.

## Create a release

1. Move entries from `Unreleased` to a dated version heading in `CHANGELOG.md`.
2. Merge the release preparation pull request.
3. Create a signed annotated tag from the exact `main` commit:

   ```bash
   git tag --sign v0.1.0 -m "Miniform v0.1.0"
   git push origin v0.1.0
   ```

4. The release workflow verifies that the tagged commit belongs to `main`, reruns checks, builds CGO-enabled Linux artifacts and multi-architecture OCI images, generates checksums and SBOMs, and publishes the GitHub release and GHCR image.
5. Verify release checksums, provenance, image digest, first boot, database persistence, and upgrade behavior.

Do not move or reuse a published tag. If a release is defective, publish a new patch version.

## Release artifacts

- Linux `amd64` and `arm64` raw binaries and archives with SQLite support
- SHA-256 checksums
- SPDX JSON SBOMs
- MIT license and third-party notices
- Multi-architecture OCI image for Linux `amd64` and `arm64`
- GitHub artifact provenance

macOS users can build natively or run the OCI image with Apple Container. Native macOS release binaries may be added once they can be built and verified reproducibly with the required CGO toolchain.

## Verification

Consumers can verify a downloaded attestation with GitHub CLI:

```bash
gh attestation verify miniform-linux-arm64 --repo matteodante/miniform
```

Compare the file hash with `checksums.txt` before installation.

Before `1.0`, image tags include the exact version and `vMAJOR.MINOR`; the moving `vMAJOR` tag is intentionally omitted for `v0` releases because minor versions may contain documented breaking changes. `latest` is published only by the stable-tag workflow.
