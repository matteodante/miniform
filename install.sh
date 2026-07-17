#!/usr/bin/env bash

set -Eeuo pipefail

readonly GITHUB_REPO="matteodante/miniform"
readonly INSTALL_DIR="/usr/local/bin"

fail() {
    echo "Error: $*" >&2
    exit 1
}

if [[ "$(id -u)" -ne 0 ]]; then
    fail "this installer requires root privileges; run 'sudo bash install.sh'"
fi

if [[ ! -t 0 ]]; then
    fail "interactive input is required; download and review install.sh before running it"
fi

case "$(uname -m)" in
    x86_64) arch="amd64" ;;
    aarch64|arm64) arch="arm64" ;;
    *) fail "unsupported architecture: $(uname -m)" ;;
esac

for command in curl jq sha256sum tar; do
    if command -v "$command" >/dev/null 2>&1; then
        continue
    fi
    command -v apt-get >/dev/null 2>&1 || fail "missing $command and no supported package manager was found"
    apt-get update -qq
    apt-get install --yes --quiet ca-certificates coreutils curl jq tar
    break
done

temporary_dir="$(mktemp -d /tmp/miniform-install.XXXXXX)"
trap 'rm -rf "$temporary_dir"' EXIT

release_json="$temporary_dir/release.json"
curl --fail --silent --show-error --location \
    "https://api.github.com/repos/$GITHUB_REPO/releases/latest" \
    --output "$release_json"

release_tag="$(jq -er '.tag_name' "$release_json")" || fail "no published release was found"
asset_name="miniform-linux-$arch.tar.gz"

asset_url="$(jq -er --arg name "$asset_name" '.assets[] | select(.name == $name) | .browser_download_url' "$release_json")" \
    || fail "release $release_tag does not contain $asset_name"
checksums_url="$(jq -er '.assets[] | select(.name == "checksums.txt") | .browser_download_url' "$release_json")" \
    || fail "release $release_tag does not contain checksums.txt"

archive="$temporary_dir/$asset_name"
checksums="$temporary_dir/checksums.txt"

echo "Downloading Miniform $release_tag for linux/$arch..."
curl --fail --silent --show-error --location "$asset_url" --output "$archive"
curl --fail --silent --show-error --location "$checksums_url" --output "$checksums"

expected_hash="$(awk -v asset="$asset_name" '$2 == asset { print $1 }' "$checksums")"
[[ -n "$expected_hash" ]] || fail "no checksum was published for $asset_name"
echo "$expected_hash  $archive" | sha256sum --check --status \
    || fail "SHA-256 verification failed for $asset_name"
echo "Checksum verified."

extract_dir="$temporary_dir/extracted"
mkdir -p "$extract_dir"
tar --extract --gzip --file "$archive" --directory "$extract_dir"

binary="$(find "$extract_dir" -type f -name miniform -perm -u+x -print -quit)"
[[ -n "$binary" ]] || fail "the release archive does not contain the Miniform executable"

echo "Starting the interactive Miniform system installer..."
"$binary" install

install --mode 0755 "$binary" "$INSTALL_DIR/miniform"
echo "Installed $INSTALL_DIR/miniform"
