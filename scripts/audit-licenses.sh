#!/usr/bin/env bash

set -euo pipefail

readonly GO_LICENSES_VERSION="v2.0.1"
readonly MODE="${1:-verify}"
readonly BUNDLE="third_party_licenses/go"
TEMPORARY="$(mktemp -d)"
readonly TEMPORARY
readonly REPORT="$TEMPORARY/go-licenses.csv"
readonly TOOL_DIR="$TEMPORARY/bin"
trap 'rm -rf "$TEMPORARY"' EXIT INT TERM

GOBIN="$TOOL_DIR" go install "github.com/google/go-licenses/v2@$GO_LICENSES_VERSION"
GOOS=linux GOARCH=amd64 CGO_ENABLED=1 "$TOOL_DIR/go-licenses" report ./... > "$REPORT"

unexpected="$(awk -F, 'NF >= 3 && $3 !~ /^(MIT|BSD-3-Clause|Apache-2.0|ISC)$/ { print }' "$REPORT")"
if [[ -n "$unexpected" ]]; then
    echo "Dependencies with unreviewed licenses:" >&2
    echo "$unexpected" >&2
    exit 1
fi

cat "$REPORT"

case "$MODE" in
    verify)
        generated="$TEMPORARY/licenses"
        GOOS=linux GOARCH=amd64 CGO_ENABLED=1 "$TOOL_DIR/go-licenses" save ./cmd/miniform \
            --ignore github.com/matteodante/miniform \
            --save_path "$generated"
        if ! diff -ru "$BUNDLE" "$generated"; then
            echo "Go dependency license bundle is stale; run 'make licenses'" >&2
            exit 1
        fi
        ;;
    update)
        GOOS=linux GOARCH=amd64 CGO_ENABLED=1 "$TOOL_DIR/go-licenses" save ./cmd/miniform \
            --force \
            --ignore github.com/matteodante/miniform \
            --save_path "$BUNDLE"
        ;;
    *)
        echo "usage: $0 [verify|update]" >&2
        exit 2
        ;;
esac
