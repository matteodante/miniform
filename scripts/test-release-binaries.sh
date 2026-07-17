#!/bin/sh

set -eu

manifest="${1:-dist/artifacts.json}"
amd64_image="${2:?usage: test-release-binaries.sh MANIFEST AMD64_IMAGE ARM64_IMAGE}"
arm64_image="${3:?usage: test-release-binaries.sh MANIFEST AMD64_IMAGE ARM64_IMAGE}"
temporary="$(mktemp -d)"
trap 'rm -rf "$temporary"' EXIT INT TERM

test_binary() {
	arch="$1"
	image="$2"
	binary="$(jq -er --arg arch "$arch" '
		[.[] | select(.type == "Binary" and .goos == "linux" and .goarch == $arch) | .path]
		| unique
		| if length == 1 then .[0] else error("expected exactly one release binary") end
	' "$manifest")"
	if [ ! -x "$binary" ]; then
		echo "missing executable release binary for linux/$arch: $binary" >&2
		exit 1
	fi
	cp "$binary" "$temporary/miniform-$arch"
	docker run --rm --platform "linux/$arch" \
		--volume "$temporary:/release:ro" \
		"$image" "/release/miniform-$arch" version
}

test_binary amd64 "$amd64_image"
test_binary arm64 "$arm64_image"
