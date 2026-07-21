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
	golangci-lint-2.12.2-darwin-amd64.tar.gz) checksum="f6f06d94b6241521c53d15450c5209b028270bf966f842afb11c030c79f5bc16" ;;
	golangci-lint-2.12.2-darwin-arm64.tar.gz) checksum="a9c54498731b3128f79e090be6110f3e5fffccc617b08142ed244d4126c73f29" ;;
	golangci-lint-2.12.2-linux-amd64.tar.gz) checksum="8df580d2670fed8fa984aac0507099af8df275e665215f5c7a2ae3943893a553" ;;
	golangci-lint-2.12.2-linux-arm64.tar.gz) checksum="44cd40a8c76c86755375adfeea52cfd3533cb43d7bd647771e0ae065e166df3a" ;;
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
