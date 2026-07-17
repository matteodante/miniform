#!/bin/sh

set -eu

manifest="${1:-dist/artifacts.json}"
image="${2:?usage: test-release-binaries.sh MANIFEST ALPINE_IMAGE}"
temporary="$(mktemp -d)"
trap 'rm -rf "$temporary"' EXIT INT TERM

for arch in amd64 arm64; do
	binary="$(jq -er --arg arch "$arch" '.[] | select(.type == "Binary" and .goos == "linux" and .goarch == $arch) | .path' "$manifest")"
	if [ ! -x "$binary" ]; then
		echo "missing executable release binary for linux/$arch: $binary" >&2
		exit 1
	fi
	cp "$binary" "$temporary/miniform-$arch"
	docker run --rm --platform "linux/$arch" \
		--volume "$temporary:/release:ro" \
		"$image" "/release/miniform-$arch" version
done
