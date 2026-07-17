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
expect_valid v1.2.3-rc.1
expect_valid v1.2.3-alpha-1+build.42
expect_valid v1.2.3-x-y-z.--
expect_valid v1.2.3+001

expect_invalid 1.2.3
expect_invalid v01.2.3
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
grep -Eq 'registry:2@sha256:[0-9a-f]{64}' "$root/.github/workflows/ci.yml"
grep -Fq 'run: ./scripts/validate-release-tag.sh "$RELEASE_TAG"' "$root/.github/workflows/release.yml"
grep -Fq 'version_without_build="${RELEASE_TAG%%+*}"' "$root/.github/workflows/release.yml"
grep -Fq 'if [[ "$version_without_build" == *-* ]]; then' "$root/.github/workflows/release.yml"
grep -Fq 'echo "stable=false" >> "$GITHUB_OUTPUT"' "$root/.github/workflows/release.yml"
grep -Fq 'echo "stable=true" >> "$GITHUB_OUTPUT"' "$root/.github/workflows/release.yml"
grep -Fq 'stable: ${{ steps.release.outputs.stable }}' "$root/.github/workflows/release.yml"
grep -Fq 'type=raw,value=latest,enable=${{ steps.release.outputs.stable }}' "$root/.github/workflows/release.yml"
grep -Fq 'STABLE_RELEASE: ${{ needs.image.outputs.stable }}' "$root/.github/workflows/release.yml"
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
grep -Fq 'test -z "$$(git status --porcelain)"' "$root/Makefile"
grep -Fq 'rm -f $(BIN_DIR)/$(APP) $(INSTALLER_BINARY) $(E2E_BINARY)' "$root/Makefile"
grep -Fq 'rm -rf "$(CURDIR)/dist"' "$root/Makefile"

if grep -Fq 'raw.githubusercontent.com/golangci/golangci-lint' "$root/Makefile"; then
	echo "golangci-lint installer script is executed from a mutable Git tag" >&2
	exit 1
fi

if grep -Fq "contains(github.ref_name, '-')" "$root/.github/workflows/release.yml"; then
	echo "release stability is inferred from build metadata" >&2
	exit 1
fi

echo "CI policy checks passed"
