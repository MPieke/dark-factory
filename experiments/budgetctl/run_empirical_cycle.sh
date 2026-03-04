#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

RUN_ID="${RUN_ID:-budgetctl-hypothesis-$(date +%Y%m%d-%H%M%S)}"
WORK="${WORK:-$ROOT}"
RUNS="${RUNS:-$ROOT/.runs}"
BACKEND="${ATTRACTOR_AGENT_BACKEND:-codex}"
MAX_FACTORY_RETRIES="${MAX_FACTORY_RETRIES:-3}"
FACTORY_API_URL="${FACTORY_API_URL:-}"
FACTORY_API_WORKDIR="${FACTORY_API_WORKDIR:-$WORK}"
FACTORY_API_RUNSDIR="${FACTORY_API_RUNSDIR:-$RUNS}"

mkdir -p "$RUNS"

# Ensure local codex wrapper exists in source workdir before workspace copy.
mkdir -p "$WORK/.factory/bin"
cp scripts/codex-wrapper.sh "$WORK/.factory/bin/codex"
chmod +x "$WORK/.factory/bin/codex"

factory_rc=1
factory_classification="unknown"
for attempt in $(seq 1 "$MAX_FACTORY_RETRIES"); do
  echo "factory_attempt=$attempt/$MAX_FACTORY_RETRIES"
  run_log="$(mktemp)"
  set +e
  if [ -n "$FACTORY_API_URL" ]; then
    payload=$(cat <<JSON
{"pipeline_path":"examples/budgetctl_hypothesis_loop.dot","workdir":"$FACTORY_API_WORKDIR","runsdir":"$FACTORY_API_RUNSDIR","run_id":"$RUN_ID","resume":false}
JSON
)
    curl -fsS -X POST "$FACTORY_API_URL/runs" -H 'content-type: application/json' -d "$payload" >"$run_log"
    post_rc=$?
    if [ "$post_rc" -ne 0 ]; then
      factory_rc=$post_rc
      set -e
      factory_classification="infra_api_unreachable"
      break
    fi
    for _ in $(seq 1 1200); do
      status_json="$(curl -fsS "$FACTORY_API_URL/runs/$RUN_ID" 2>/dev/null || true)"
      echo "$status_json" >>"$run_log"
      status="$(echo "$status_json" | sed -n 's/.*"status"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')"
      if [ "$status" = "success" ]; then
        factory_rc=0
        break
      fi
      if [ "$status" = "failed" ]; then
        factory_rc=1
        break
      fi
      sleep 1
    done
  else
    FACTORY_LOG_CODEX_STREAM="${FACTORY_LOG_CODEX_STREAM:-1}" \
    ATTRACTOR_AGENT_BACKEND="$BACKEND" \
    ATTRACTOR_CODEX_PATH=".factory/bin/codex" \
    ATTRACTOR_CODEX_DISABLE_MCP=true \
    ATTRACTOR_CODEX_SANDBOX=workspace-write \
    ATTRACTOR_CODEX_APPROVAL=never \
    ATTRACTOR_CODEX_SKIP_GIT_REPO_CHECK=true \
    GOCACHE="${GOCACHE:-$ROOT/.gocache}" \
    go run cmd/factory/main.go run \
      --workdir "$WORK" \
      --runsdir "$RUNS" \
      --run-id "$RUN_ID" \
      examples/budgetctl_hypothesis_loop.dot 2>&1 | tee "$run_log"
    factory_rc=$?
  fi
  set -e

  if [ "$factory_rc" -eq 0 ]; then
    factory_classification="success"
    break
  fi

  if grep -Eiq "stream disconnected|failed to queue rollout items|error sending request" "$run_log"; then
    factory_classification="infra_backend_transport"
  elif grep -Eiq "connection refused|failed to connect|timed out|service unavailable" "$run_log"; then
    factory_classification="infra_api_unreachable"
  else
    factory_classification="non_transport_failure"
  fi

  if [[ "$factory_classification" == infra_* ]] && [ "$attempt" -lt "$MAX_FACTORY_RETRIES" ]; then
    echo "retrying after infrastructure failure..."
    sleep 2
    continue
  fi
  break
done

RUN_DIR="$RUNS/$RUN_ID"
REPORT="$RUN_DIR/experiment.report.md"

visible="unknown"
hidden="unknown"
probe="unknown"
if [ -f "$RUN_DIR/validate_visible/status.json" ]; then
  grep -q '"outcome": "success"' "$RUN_DIR/validate_visible/status.json" && visible="pass" || visible="fail"
fi
if [ -f "$RUN_DIR/validate_hidden/status.json" ]; then
  grep -q '"outcome": "success"' "$RUN_DIR/validate_hidden/status.json" && hidden="pass" || hidden="fail"
fi
if [ -f "$RUN_DIR/independent_probe/status.json" ]; then
  grep -q '"outcome": "success"' "$RUN_DIR/independent_probe/status.json" && probe="pass" || probe="fail"
fi

mkdir -p "$RUN_DIR"
cat > "$REPORT" <<RPT
# Budgetctl Empirical Report

- run_id: $RUN_ID
- backend: $BACKEND
- pipeline: examples/budgetctl_hypothesis_loop.dot
- factory_exit_code: $factory_rc
- factory_classification: $factory_classification

## Hypothesis outcomes
- H1_spec_priority / H2_spec_fallback (visible): $visible
- H3_transfer_holdout (hidden): $hidden
- H4_robust_input (independent probe): $probe
- H5_non_gameable (policy): enforced by strict read scope + scenario path blocking

## Failure classification guidance
- visible fail -> likely spec_gap or implementation_bug
- hidden fail with visible pass -> likely test_gap or transfer failure
- probe fail with visible+hidden pass -> likely robustness gap / Goodhart risk

## Key artifacts
- run_dir: $RUN_DIR
- trace: $RUN_DIR/trace.jsonl
- implement status: $RUN_DIR/implement/status.json
- verify status: $RUN_DIR/verify_plan/status.json
- visible status: $RUN_DIR/validate_visible/status.json
- hidden status: $RUN_DIR/validate_hidden/status.json
- probe status: $RUN_DIR/independent_probe/status.json
RPT

echo "empirical cycle complete"
echo "report: $REPORT"
if [ "$factory_rc" -ne 0 ]; then
  echo "factory failed before completing empirical cycle (classification=$factory_classification)" >&2
  exit "$factory_rc"
fi
