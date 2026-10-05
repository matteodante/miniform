#!/bin/sh

set -eu

version="${1:?usage: install-golangci-lint.sh VERSION DESTINATION}"
destination="${2:?usage: install-golangci-lint.sh VERSION DESTINATION}"
plain_version="${version#v}"
os="$(uname -s | tr '[:upper:]' '[:lower:]')"

case "$(uname -m)" in
	arm64 | aarch64) arch="arm64" ;;
	x86_64 | amd64) arch="amd64" ;;
	*) echo "unsupported golangci-lint architecture: $(uname -m)" >&2; exit 1 ;;
esac

asset="golangci-lint-${plain_version}-${os}-${arch}.tar.gz"
case "$asset" in
	golangci-lint-2.14.0-darwin-amd64.tar.gz) checksum="a5667c1c3536be1740133213e1e822bfb8f0d98ea12903174d6d5f635e4ed68d" ;;
	golangci-lint-2.14.0-darwin-arm64.tar.gz) checksum="5ef5f36a7147e91dc58ef9ef4d11bb7bad5ead0c76eb6c01327a73c641d1dcc3" ;;
	golangci-lint-2.14.0-linux-amd64.tar.gz) checksum="ab90aeb7b066f92a33415b638a50fe5344bbb75a0d32ad30cc248d88f81032ab" ;;
	golangci-lint-2.14.0-linux-arm64.tar.gz) checksum="ee7ec5f3453d15ddf106fae5a4d6c71737712348a979d1fe9cd52ec7ea299bae" ;;
	*) echo "unsupported golangci-lint release: $asset" >&2; exit 1 ;;
esac

temporary="$(mktemp -d)"
trap 'rm -rf "$temporary"' EXIT INT TERM
archive="$temporary/$asset"
curl -fsSL "https://github.com/golangci/golangci-lint/releases/download/$version/$asset" -o "$archive"

if command -v sha256sum >/dev/null 2>&1; then
	actual="$(sha256sum "$archive" | awk '{print $1}')"
else
	actual="$(shasum -a 256 "$archive" | awk '{print $1}')"
fi
if [ "$actual" != "$checksum" ]; then
	echo "golangci-lint checksum mismatch for $asset" >&2
	exit 1
fi

tar -xzf "$archive" -C "$temporary"
install -d "$destination"
install -m 0755 "$temporary/golangci-lint-${plain_version}-${os}-${arch}/golangci-lint" "$destination/golangci-lint"
