#!/usr/bin/env bash

set -euo pipefail

readonly VENDOR_DIR="web/static/vendor"
readonly BIN_DIR="bin"
readonly TAILWIND_VERSION="v3.4.19"

mkdir -p "$VENDOR_DIR" "$BIN_DIR"

sha256() {
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$1" | awk '{print $1}'
        return
    fi
    shasum -a 256 "$1" | awk '{print $1}'
}

download_verified() {
    local url="$1"
    local destination="$2"
    local expected="$3"

    if [[ -f "$destination" ]] && [[ "$(sha256 "$destination")" == "$expected" ]]; then
        return
    fi

    local temporary
    temporary="$(mktemp "${TMPDIR:-/tmp}/miniform-vendor.XXXXXX")"
    trap 'rm -f "$temporary"' RETURN

    curl --fail --silent --show-error --location "$url" --output "$temporary"
    local actual
    actual="$(sha256 "$temporary")"
    if [[ "$actual" != "$expected" ]]; then
        echo "Checksum mismatch for $url" >&2
        echo "expected: $expected" >&2
        echo "actual:   $actual" >&2
        exit 1
    fi

    mv "$temporary" "$destination"
    trap - RETURN
}

download_verified \
    "https://unpkg.com/htmx.org@1.9.12/dist/htmx.min.js" \
    "$VENDOR_DIR/htmx.min.js" \
    "449317ade7881e949510db614991e195c3a099c4c791c24dacec55f9f4a2a452"

case "$(uname -s)-$(uname -m)" in
    Darwin-arm64)
        tailwind_asset="tailwindcss-macos-arm64"
        tailwind_sha="7fdeb00818b6214a337383063282b2361ecb08bbc08f8c8a7ba97ee1e2eaa4fe"
        ;;
    Darwin-x86_64)
        tailwind_asset="tailwindcss-macos-x64"
        tailwind_sha="a597f407e0f1f03535731f5b42f1576a8152cb5fffc2f38e754722bc0c280045"
        ;;
    Linux-aarch64|Linux-arm64)
        tailwind_asset="tailwindcss-linux-arm64"
        tailwind_sha="e5b2d27694daa80cc52ec29553ba2c6bd43d86bd51a9d633ed24058b9c05a676"
        ;;
    Linux-x86_64)
        tailwind_asset="tailwindcss-linux-x64"
        tailwind_sha="4af3198c015616ea7d6617974ec3d70d987ecc00c1ca8463b0a30fd65cc7c06e"
        ;;
    *)
        echo "Unsupported platform for Tailwind: $(uname -s) $(uname -m)" >&2
        exit 1
        ;;
esac

download_verified \
    "https://github.com/tailwindlabs/tailwindcss/releases/download/$TAILWIND_VERSION/$tailwind_asset" \
    "$BIN_DIR/tailwindcss" \
    "$tailwind_sha"
chmod +x "$BIN_DIR/tailwindcss"

echo "Vendored frontend assets are present and verified."
