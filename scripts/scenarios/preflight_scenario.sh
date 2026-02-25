#!/usr/bin/env bash
set -euo pipefail

SCENARIO_SCRIPT="${1:-}"
APP_DIR="${2:-}"
REQUIRE_LIVE="${REQUIRE_LIVE:-1}"

if [ -z "$SCENARIO_SCRIPT" ] || [ -z "$APP_DIR" ]; then
  echo "usage: $0 <scenario_script> <app_dir>"
  exit 2
fi

if [ ! -f "$SCENARIO_SCRIPT" ]; then
  echo "scenario script not found: $SCENARIO_SCRIPT"
  exit 1
fi

echo "[preflight] selftest: $SCENARIO_SCRIPT $APP_DIR"
SCENARIO_MODE=selftest bash "$SCENARIO_SCRIPT" "$APP_DIR"

if [ "$REQUIRE_LIVE" = "1" ]; then
  echo "[preflight] live: $SCENARIO_SCRIPT $APP_DIR"
  live_out="$(mktemp)"
  live_err="$(mktemp)"
  set +e
  SCENARIO_MODE=live bash "$SCENARIO_SCRIPT" "$APP_DIR" >"$live_out" 2>"$live_err"
  rc=$?
  set -e
  cat "$live_out"
  if [ "$rc" -ne 0 ]; then
    cat "$live_err" 1>&2
    combined="$(cat "$live_out" "$live_err" 2>/dev/null || true)"
    if echo "$combined" | grep -Eiq "API_KEY|not set for live|status 401|status 403|status 404|not_found_error|quota|rate limit|connection|timeout|models list failed"; then
      echo "failure_class=infra" 1>&2
      rm -f "$live_out" "$live_err"
      exit 86
    fi
    echo "failure_class=product" 1>&2
    rm -f "$live_out" "$live_err"
    exit "$rc"
  fi
  rm -f "$live_out" "$live_err"
else
  echo "[preflight] skipping live checks (REQUIRE_LIVE=0)"
fi

echo "[preflight] passed: $SCENARIO_SCRIPT"
