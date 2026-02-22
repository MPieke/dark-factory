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
cat > "$WORK/main.go" <<'EOF'
package main
func main() {}
EOF

cat > "$PIPE" <<'DOT'
digraph G {
  start [shape=Mdiamond];
  gen [shape=box, "test.verification_plan_json"="{\"files\":[\"main.go\"],\"commands\":[\"test -f main.go\"]}"];
  verify [shape=parallelogram, type=verification, "verification.allowed_commands"="test -f"];
  exit [shape=Msquare];
  start -> gen;
  gen -> verify;
  verify -> exit [condition="outcome=success"];
}
DOT

ATTRACTION_BACKEND=fake /tmp/factory-smoke run --workdir "$WORK" --runsdir "$RUNS" --run-id smoke-verify "$PIPE"

test -f "$RUNS/smoke-verify/verify/verification.plan.json"
test -f "$RUNS/smoke-verify/verify/verification.results.json"
grep -q '"outcome": "success"' "$RUNS/smoke-verify/verify/status.json"

echo "smoke verification ok"
