#!/bin/sh
# shellcheck disable=SC2016 # Assertions intentionally match literal Make/workflow variables.

set -eu

root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
validator="$root/scripts/validate-release-tag.sh"

expect_valid() {
	"$validator" "$1" >/dev/null
}

expect_invalid() {
	if "$validator" "$1" >/dev/null 2>&1; then
		echo "accepted invalid release tag: $1" >&2
		exit 1
	fi
}

expect_valid v0.0.0
expect_valid v1.2.3

expect_invalid 1.2.3
expect_invalid v01.2.3
expect_invalid v1.2.3-rc.1
expect_invalid v1.2.3-alpha-1+build.42
expect_invalid v1.2.3+001
expect_invalid v1.2.3-01
expect_invalid v1.2.3-alpha.
expect_invalid v1.2.3-alpha..1
expect_invalid v1.2.3-..
expect_invalid v1.2.3+build..1
expect_invalid v1.2.3_alpha

grep -Fq '$(TOOLS_DIR)/actionlint -shellcheck=$(TOOLS_DIR)/shellcheck' "$root/Makefile"
grep -Eq '^GO_IMAGE \?= golang:1\.26\.5-bookworm@sha256:[0-9a-f]{64}$' "$root/Makefile"
grep -Eq '^ALPINE_AMD64_IMAGE \?= alpine:3\.23@sha256:[0-9a-f]{64}$' "$root/Makefile"
grep -Eq '^ALPINE_ARM64_IMAGE \?= alpine:3\.23@sha256:[0-9a-f]{64}$' "$root/Makefile"
grep -Fq './scripts/test-release-binaries.sh dist/artifacts.json "$(ALPINE_AMD64_IMAGE)" "$(ALPINE_ARM64_IMAGE)"' "$root/Makefile"
grep -Fq -- '--volume "$(CURDIR):/src:ro"' "$root/Makefile"
grep -Fq -- '--volume "$(INSTALLER_BINARY_DIR):/out"' "$root/Makefile"
grep -Fq './scripts/validate-release-tag.sh "v$(v)"' "$root/Makefile"
grep -Fq 'git tag -s "v$(v)" -m "Release v$(v)"' "$root/Makefile"
grep -Fq 'container_name="miniform-test-$$(date +%s)-$$$$"' "$root/Makefile"
grep -Fq 'volume=$$(docker volume create)' "$root/Makefile"
grep -Fq 'docker rm --force "$$container_id"' "$root/Makefile"
grep -Fq 'docker volume rm --force "$$volume"' "$root/Makefile"
grep -Fq 'find "$storage_tree" -xdev -exec chown -h miniform:miniform {} \;' "$root/docker-entrypoint.sh"
grep -Fq '"$data_dir/.upload-staging"' "$root/docker-entrypoint.sh"
grep -Fq 'MINIFORM_DATABASE_FILENAME must be a filename' "$root/docker-entrypoint.sh"
grep -Fq '/app/storage/.upload-staging/restored/uploads/restored/attachment.txt' "$root/Makefile"
grep -Fq '"builder": "RAILPACK"' "$root/railway.json"
grep -Fq '"startCommand": "./out"' "$root/railway.json"
grep -Fxq 'Dockerfile' "$root/.railwayignore"
grep -Fq 'for database_file in "$database_path" "$database_path-wal" "$database_path-shm"; do' "$root/docker-entrypoint.sh"
grep -Eq 'registry:2@sha256:[0-9a-f]{64}' "$root/.github/workflows/ci.yml"
grep -Fq 'run: ./scripts/validate-release-tag.sh "$RELEASE_TAG"' "$root/.github/workflows/release.yml"
grep -Fq 'git merge-base --is-ancestor "$GITHUB_SHA" refs/remotes/origin/main' "$root/.github/workflows/release.yml"
grep -Fq 'flavor: latest=false' "$root/.github/workflows/release.yml"
grep -Fq "type=semver,pattern=v{{major}},enable=\${{ !startsWith(github.ref, 'refs/tags/v0.') }}" "$root/.github/workflows/release.yml"
grep -Fq 'type=raw,value=latest' "$root/.github/workflows/release.yml"
grep -Fq 'needs: [validate, installer]' "$root/.github/workflows/release.yml"
grep -Fq 'MINIFORM_RUN_INSTALLATION_TEST=1' "$root/.github/workflows/release.yml"
grep -Fq 'platforms: linux/amd64' "$root/.github/workflows/release.yml"
grep -Fq 'platforms: linux/arm64' "$root/.github/workflows/release.yml"
grep -Fq 'image-ref: miniform:scan-amd64' "$root/.github/workflows/release.yml"
grep -Fq 'image-ref: miniform:scan-arm64' "$root/.github/workflows/release.yml"
grep -Fq 'run: make test-release-binaries' "$root/.github/workflows/ci.yml"
grep -Fq 'run: make test-release-binaries' "$root/.github/workflows/release.yml"
grep -Fq 'libc6-dev-arm64-cross' "$root/.github/workflows/ci.yml"
grep -Fq 'libc6-dev-arm64-cross' "$root/.github/workflows/release.yml"
grep -Fq -- '- netgo' "$root/.goreleaser.yml"
grep -Fq -- '- osusergo' "$root/.goreleaser.yml"
grep -Fq -- '- -s -w -linkmode external -extldflags "-static"' "$root/.goreleaser.yml"
grep -Fq -- '- -X main.version={{ .Tag }}' "$root/.goreleaser.yml"
grep -Fq 'formats: [tar.gz, binary]' "$root/.goreleaser.yml"
grep -Fq 'subject-checksums: dist/checksums.txt' "$root/.github/workflows/release.yml"
grep -Fq 'artifacts: binary' "$root/.goreleaser.yml"
grep -Fq 'test -z "$$(git status --porcelain)"' "$root/Makefile"
grep -Fq 'rm -f $(BIN_DIR)/$(APP) $(INSTALLER_BINARY) $(E2E_BINARY)' "$root/Makefile"
grep -Fq 'rm -rf "$(CURDIR)/dist"' "$root/Makefile"

e2e_setup=$(make -s -n -C "$root" test-e2e-setup)
printf '%s\n' "$e2e_setup" | grep -Fq "PLAYWRIGHT_BROWSERS_PATH=\"$root/tmp/ms-playwright\" npx playwright install chromium"

installer_run_line=$(grep -nF '"$binary" install' "$root/install.sh" | cut -d: -f1)
installer_replace_line=$(grep -nF 'install --mode 0755 "$binary" "$INSTALL_DIR/miniform"' "$root/install.sh" | cut -d: -f1)
if [ -z "$installer_run_line" ] || [ -z "$installer_replace_line" ]; then
	echo "installer candidate lifecycle is missing" >&2
	exit 1
fi
if [ "$installer_run_line" -ge "$installer_replace_line" ]; then
	echo "installer replaces the current manager before installation succeeds" >&2
	exit 1
fi

if grep -Fq 'raw.githubusercontent.com/golangci/golangci-lint' "$root/Makefile"; then
	echo "golangci-lint installer script is executed from a mutable Git tag" >&2
	exit 1
fi

if grep -Eq 'container=miniform-test;|volume=miniform-test-storage|127\.0\.0\.1:18080' "$root/Makefile"; then
	echo "container-test uses shared Docker resources" >&2
	exit 1
fi

if grep -Fq "contains(github.ref_name, '-')" "$root/.github/workflows/release.yml"; then
	echo "release stability is inferred from build metadata" >&2
	exit 1
fi

if grep -Fq 'CronUpdates: true' "$root/cmd/miniform/main.go"; then
	echo "deployment enables unattended updates" >&2
	exit 1
fi

if grep -Fq 'Compress: true' "$root/internal/assets.go"; then
	echo "development server generates non-reproducible Fiber sidecars" >&2
	exit 1
fi

echo "CI policy checks passed"
