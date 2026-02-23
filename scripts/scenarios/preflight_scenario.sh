#!/usr/bin/env bash
set -euo pipefail

SCENARIO_SCRIPT="${1:-}"
APP_DIR="${2:-}"
REQUIRE_LIVE="${REQUIRE_LIVE:-1}"

if [ -z "$SCENARIO_SCRIPT" ] || [ -z "$APP_DIR" ]; then
  echo "usage: $0 <scenario_script> <app_dir>"
  exit 2
fi

if [ ! -x "$SCENARIO_SCRIPT" ]; then
  echo "scenario script must be executable: $SCENARIO_SCRIPT"
  exit 1
fi

echo "[preflight] selftest: $SCENARIO_SCRIPT $APP_DIR"
SCENARIO_MODE=selftest "$SCENARIO_SCRIPT" "$APP_DIR"

if [ "$REQUIRE_LIVE" = "1" ]; then
  echo "[preflight] live: $SCENARIO_SCRIPT $APP_DIR"
  SCENARIO_MODE=live "$SCENARIO_SCRIPT" "$APP_DIR"
else
  echo "[preflight] skipping live checks (REQUIRE_LIVE=0)"
fi

echo "[preflight] passed: $SCENARIO_SCRIPT"
