#!/usr/bin/env bash

set -euo pipefail

readonly VENDOR_DIR="web/static/vendor"
readonly BIN_DIR="bin"
readonly TAILWIND_VERSION="v3.4.17"

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

download_verified \
    "https://cdnjs.cloudflare.com/ajax/libs/highlight.js/11.9.0/highlight.min.js" \
    "$VENDOR_DIR/highlight.min.js" \
    "837a6fa5b0c736b52bbde2b2b6190f305da3fc9ed41681db5321507057b5c846"

download_verified \
    "https://cdnjs.cloudflare.com/ajax/libs/highlight.js/11.9.0/styles/github-dark.min.css" \
    "$VENDOR_DIR/highlight-github-dark.min.css" \
    "9f208d022102b1d0c7aebfecd8e42ca7997d5de636649d2b31ea63093d809019"

case "$(uname -s)-$(uname -m)" in
    Darwin-arm64)
        tailwind_asset="tailwindcss-macos-arm64"
        tailwind_sha="a1d0c7985759accca0bf12e51ac1dcbf0f6cf2fffb62e6e0f62d091c477a10a3"
        ;;
    Darwin-x86_64)
        tailwind_asset="tailwindcss-macos-x64"
        tailwind_sha="6cbdad74be776c087ffa5e9a057512c54898f9fe8828d3362212dfe32fc933a3"
        ;;
    Linux-aarch64|Linux-arm64)
        tailwind_asset="tailwindcss-linux-arm64"
        tailwind_sha="69b1378b8133192d7d2feb12a116fa12d035594f58db3eff215879e4ad8cf39b"
        ;;
    Linux-x86_64)
        tailwind_asset="tailwindcss-linux-x64"
        tailwind_sha="7d24f7fa191d2193b78cd5f5a42a6093e14409521908529f42d80b11fde1f1d4"
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
