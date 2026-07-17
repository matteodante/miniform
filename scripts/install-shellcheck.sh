#!/bin/sh

set -eu

version="${1:?usage: install-shellcheck.sh VERSION DESTINATION}"
destination="${2:?usage: install-shellcheck.sh VERSION DESTINATION}"
os="$(uname -s | tr '[:upper:]' '[:lower:]')"

case "$(uname -m)" in
    arm64 | aarch64) arch="aarch64" ;;
    x86_64 | amd64) arch="x86_64" ;;
    *) echo "unsupported ShellCheck architecture: $(uname -m)" >&2; exit 1 ;;
esac

asset="shellcheck-${version}.${os}.${arch}.tar.gz"
case "$asset" in
    shellcheck-v0.11.0.darwin.aarch64.tar.gz) checksum="339b930feb1ea764467013cc1f72d09cd6b869ebf1013296ba9055ab2ffbd26f" ;;
    shellcheck-v0.11.0.darwin.x86_64.tar.gz) checksum="c2c15e08df0e8fbc374c335b230a7ee958c313fa5714817a59aa59f1aa594f51" ;;
    shellcheck-v0.11.0.linux.aarch64.tar.gz) checksum="68a8133197a50beb8803f8d42f9908d1af1c5540d4bb05fdfca8c1fa47decefc" ;;
    shellcheck-v0.11.0.linux.x86_64.tar.gz) checksum="b7af85e41cc99489dcc21d66c6d5f3685138f06d34651e6d34b42ec6d54fe6f6" ;;
    *) echo "unsupported ShellCheck release: $asset" >&2; exit 1 ;;
esac

temporary="$(mktemp -d)"
trap 'rm -rf "$temporary"' EXIT INT TERM
archive="$temporary/$asset"
curl -fsSL "https://github.com/koalaman/shellcheck/releases/download/$version/$asset" -o "$archive"

if command -v sha256sum >/dev/null 2>&1; then
    actual="$(sha256sum "$archive" | awk '{print $1}')"
else
    actual="$(shasum -a 256 "$archive" | awk '{print $1}')"
fi
if [ "$actual" != "$checksum" ]; then
    echo "ShellCheck checksum mismatch for $asset" >&2
    exit 1
fi

tar -xzf "$archive" -C "$temporary"
install -d "$destination"
install -m 0755 "$temporary/shellcheck-${version}/shellcheck" "$destination/shellcheck"
