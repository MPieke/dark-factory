#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

go build -o /tmp/factory-smoke ./cmd/factory

TMP="$(mktemp -d)"
WORK="$TMP/work"
RUNS="$TMP/runs"
PIPE="$TMP/pipeline.dot"
mkdir -p "$WORK" "$RUNS"

cat > "$PIPE" <<'DOT'
digraph G {
  start [shape=Mdiamond];
  a [shape=box];
  exit [shape=Msquare];
  start -> a;
  a -> exit;
}
DOT

ATTRACTION_BACKEND=fake /tmp/factory-smoke run --workdir "$WORK" --runsdir "$RUNS" --run-id smoke "$PIPE"
test -f "$RUNS/smoke/a/status.json"

echo "smoke ok"
