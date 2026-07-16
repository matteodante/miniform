#!/usr/bin/env bash

set -euo pipefail

readonly GO_LICENSES_VERSION="v2.0.1"
readonly REPORT="${TMPDIR:-/tmp}/miniform-go-licenses.csv"

go run "github.com/google/go-licenses/v2@$GO_LICENSES_VERSION" report ./... > "$REPORT"

unexpected="$(awk -F, 'NF >= 3 && $3 !~ /^(MIT|BSD-3-Clause|Apache-2.0|ISC)$/ { print }' "$REPORT")"
if [[ -n "$unexpected" ]]; then
    echo "Dependencies with unreviewed licenses:" >&2
    echo "$unexpected" >&2
    exit 1
fi

cat "$REPORT"
