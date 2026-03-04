#!/usr/bin/env bash
set -euo pipefail

APP_DIR="${1:-budgetctl}"
MODE="${SCENARIO_MODE:-live}"
ROOT="$(cd "${APP_DIR}/.." && pwd)"
FIX="$ROOT/scripts/scenarios/fixtures/budgetctl/hidden"

if [ "$MODE" = "selftest" ]; then
  [ -f "$FIX/transactions.csv" ]
  [ -f "$FIX/rules.csv" ]
  echo "budgetctl hidden selftest passed"
  exit 0
fi

OUT="$(mktemp)"
(
  cd "$APP_DIR"
  export GOCACHE="$PWD/.gocache"
  go run . run --tx "$FIX/transactions.csv" --rules "$FIX/rules.csv" --out "$OUT"
)

# H3 holdout transfer assertions
if ! grep -q '"category":"Income"' "$OUT"; then
  echo "hidden_fail: income categorization missing" >&2
  exit 1
fi
if ! grep -Eq '"Food"[[:space:]]*:[[:space:]]*-50(\.0+)?' "$OUT"; then
  echo "hidden_fail: expected Food total around -50.0" >&2
  exit 1
fi

echo "budgetctl hidden scenarios passed"
