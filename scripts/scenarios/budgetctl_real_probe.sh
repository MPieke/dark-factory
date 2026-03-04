#!/usr/bin/env bash
set -euo pipefail

APP_DIR="${1:-budgetctl}"
ROOT="$(cd "${APP_DIR}/.." && pwd)"
FIX="$ROOT/scripts/scenarios/fixtures/budgetctl/probe"

OUT="$(mktemp)"
(
  cd "$APP_DIR"
  export GOCACHE="$PWD/.gocache"
  go run . run --tx "$FIX/transactions.csv" --rules "$FIX/rules.csv" --out "$OUT"
)

# H4 robustness probe: deterministic duplicate handling + malformed input failure
if ! grep -Eq '"Food"[[:space:]]*:[[:space:]]*-17(\.0+)?' "$OUT"; then
  echo "probe_fail: expected Food total -17.0" >&2
  exit 1
fi

BAD_TX="$(mktemp)"
printf 'id,date,description,amount,currency,merchant,account\nX,2026-02-21,bad,NOT_A_NUMBER,USD,m,b\n' > "$BAD_TX"
set +e
(
  cd "$APP_DIR"
  export GOCACHE="$PWD/.gocache"
  go run . run --tx "$BAD_TX" --rules "$FIX/rules.csv" --out "$(mktemp)"
)
RC=$?
set -e
if [ "$RC" -eq 0 ]; then
  echo "probe_fail: malformed input should fail" >&2
  exit 1
fi

echo "budgetctl independent probe passed"
