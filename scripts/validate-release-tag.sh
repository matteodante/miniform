#!/bin/sh

set -eu

tag="${1:-}"
pattern='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'

if printf '%s\n' "$tag" | LC_ALL=C grep -Eq "$pattern"; then
	exit 0
fi

echo "invalid stable semantic release tag: $tag" >&2
exit 1
