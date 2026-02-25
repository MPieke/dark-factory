#!/usr/bin/env bash
set -euo pipefail

APP_DIR="${1:-app}"
SCENARIO_MODE="${SCENARIO_MODE:-live}"

run() {
  (
    cd "$APP_DIR"
    export GOCACHE="$PWD/.gocache"
    go run . "$@"
  )
}

run_selftest() {
  OUT1="$(run)"
  test "$OUT1" = "Hello, world!"

  OUT2="$(run --name Nate)"
  test "$OUT2" = "Hello, Nate!"

  OUT3="$(run --name nate --caps)"
  test "$OUT3" = "HELLO, NATE!"
}

case "$SCENARIO_MODE" in
  selftest)
    run_selftest
    ;;
  live)
    run_selftest
    ;;
  *)
    echo "unsupported SCENARIO_MODE: $SCENARIO_MODE (expected selftest or live)"
    exit 2
    ;;
esac

echo "hello user scenarios passed ($SCENARIO_MODE)"
