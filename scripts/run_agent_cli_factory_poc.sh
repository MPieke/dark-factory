#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

go build -o /tmp/factory-poc ./cmd/factory

TMP="$(mktemp -d)"
WORK="$TMP/work"
RUNS="$TMP/runs"
mkdir -p "$WORK/agent" "$WORK/examples/specs" "$WORK/scripts/scenarios" "$RUNS"

cp examples/specs/agent_cli_poc_spec.md "$WORK/examples/specs/agent_cli_poc_spec.md"
cp scripts/scenarios/agent_cli_component_checks.sh "$WORK/scripts/scenarios/agent_cli_component_checks.sh"
cp scripts/scenarios/agent_cli_user_scenarios.sh "$WORK/scripts/scenarios/agent_cli_user_scenarios.sh"
cp scripts/scenarios/lint_scenarios.sh "$WORK/scripts/scenarios/lint_scenarios.sh"
cp scripts/scenarios/preflight_scenario.sh "$WORK/scripts/scenarios/preflight_scenario.sh"
cp scripts/scenarios/preflight_provider_live.sh "$WORK/scripts/scenarios/preflight_provider_live.sh"
chmod +x "$WORK/scripts/scenarios/agent_cli_component_checks.sh" "$WORK/scripts/scenarios/agent_cli_user_scenarios.sh" "$WORK/scripts/scenarios/lint_scenarios.sh" "$WORK/scripts/scenarios/preflight_scenario.sh" "$WORK/scripts/scenarios/preflight_provider_live.sh"
if [ -f "$ROOT/.env" ]; then
  cp "$ROOT/.env" "$WORK/.env"
  echo "Loaded API keys from $ROOT/.env into POC workspace"
else
  echo "No .env found at $ROOT/.env; live provider validation may fail"
fi

cat > "$WORK/agent/go.mod" <<'EOF'
module agentcli

go 1.22
EOF

cat > "$WORK/agent/main.go" <<'EOF'
package main

import "fmt"

func main() {
	fmt.Println("TODO")
}
EOF

echo "POC workspace: $TMP"
echo "Running layer (1)+(2): dark-factory + codex backend"
ATTRACTOR_AGENT_BACKEND=codex \
ATTRACTOR_CODEX_SANDBOX=workspace-write \
ATTRACTOR_CODEX_APPROVAL=never \
ATTRACTOR_CODEX_SKIP_GIT_REPO_CHECK=true \
/tmp/factory-poc run --workdir "$WORK" --runsdir "$RUNS" --run-id agent-cli-poc examples/agent_cli_factory_poc.dot

APP="$RUNS/agent-cli-poc/workspace/agent"
echo "Running layer (3) produced CLI manually:"
echo "- openai default model"
(cd "$APP" && GOCACHE="$APP/.gocache" go run . ask --provider openai --prompt hi --mock)
echo "- anthropic explicit cheap model"
(cd "$APP" && GOCACHE="$APP/.gocache" go run . ask --provider anthropic --model claude-3-5-haiku-latest --prompt hello --mock)

echo "Artifacts:"
echo "  $RUNS/agent-cli-poc"
echo "Trace tail:"
tail -n 25 "$RUNS/agent-cli-poc/trace.jsonl" || true
