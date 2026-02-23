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

cat >"$WORK/go.mod" <<'EOF'
module smoke

go 1.22
EOF
cat >"$WORK/main.go" <<'EOF'
package main

func main() {}
EOF
cat >"$WORK/main_test.go" <<'EOF'
package main

import "testing"

func TestSmoke(t *testing.T) {}
EOF

cat >"$PIPE" <<'EOF'
digraph G {
  start [shape=Mdiamond];
  generate [shape=box, "test.verification_plan_json"="{\"files\":[\"go.mod\",\"main.go\"],\"commands\":[\"GOCACHE=\\\"$PWD/.gocache\\\" go test ./...\"]}"];
  verify [shape=parallelogram, type=verification, "verification.allowed_commands"="go test"];
  exit [shape=Msquare];
  start -> generate;
  generate -> verify;
  verify -> exit [condition="outcome=success"];
}
EOF

ATTRACTION_BACKEND=fake /tmp/factory-smoke run --workdir "$WORK" --runsdir "$RUNS" --run-id smoke-verify-allowlist "$PIPE"

grep -q '"outcome": "success"' "$RUNS/smoke-verify-allowlist/verify/status.json"

echo "smoke verification allowlist ok"
