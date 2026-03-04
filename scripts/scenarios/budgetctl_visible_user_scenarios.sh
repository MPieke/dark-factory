#!/usr/bin/env bash
set -euo pipefail

APP_DIR="${1:-budgetctl}"
MODE="${SCENARIO_MODE:-live}"
ROOT="$(cd "${APP_DIR}/.." && pwd)"
FIX="$ROOT/scripts/scenarios/fixtures/budgetctl/visible"

if [ "$MODE" = "selftest" ]; then
  [ -f "$FIX/transactions.csv" ]
  [ -f "$FIX/rules.csv" ]
  echo "budgetctl visible selftest passed"
  exit 0
fi

OUT="$(mktemp)"
(
  cd "$APP_DIR"
  export GOCACHE="$PWD/.gocache"
  go run . run --tx "$FIX/transactions.csv" --rules "$FIX/rules.csv" --out "$OUT"
)

# H1/H2 visible assertions
# - Food category appears
# - Uncategorized appears for unmatched txn
# - Summary includes uncategorized_count 1
if ! grep -q '"category":"Food"' "$OUT"; then
  echo "visible_fail: missing Food category" >&2
  exit 1
fi
if ! grep -q '"category":"Uncategorized"' "$OUT"; then
  echo "visible_fail: missing Uncategorized fallback" >&2
  exit 1
fi
if ! grep -Eq '"uncategorized_count"[[:space:]]*:[[:space:]]*1' "$OUT"; then
  echo "visible_fail: unexpected uncategorized_count" >&2
  exit 1
fi

echo "budgetctl visible scenarios passed"
