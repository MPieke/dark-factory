#!/usr/bin/env bash
set -euo pipefail

APP_DIR="${1:-agent}"

test -f "$APP_DIR/go.mod"
test -f "$APP_DIR/main.go"

UNFORMATTED="$(gofmt -l "$APP_DIR")"
if [ -n "$UNFORMATTED" ]; then
  echo "Unformatted Go files:"
  echo "$UNFORMATTED"
  exit 1
fi

(
  cd "$APP_DIR"
  export GOCACHE="$PWD/.gocache"
  go test ./...
  go build .
)

echo "agent component checks passed"
