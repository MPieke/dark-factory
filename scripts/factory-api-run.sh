#!/usr/bin/env bash
set -euo pipefail

API_URL="${FACTORY_API_URL:-http://127.0.0.1:8080}"
PIPELINE_PATH="${1:-}"
WORKDIR="${2:-}"
RUNSDIR="${3:-}"
RUN_ID="${4:-}"
RESUME="${5:-false}"

if [ -z "$PIPELINE_PATH" ] || [ -z "$WORKDIR" ] || [ -z "$RUNSDIR" ]; then
  echo "usage: $0 <pipeline_path> <workdir> <runsdir> [run_id] [resume]" >&2
  exit 2
fi

payload=$(cat <<JSON
{"pipeline_path":"$PIPELINE_PATH","workdir":"$WORKDIR","runsdir":"$RUNSDIR","run_id":"$RUN_ID","resume":$RESUME}
JSON
)

resp="$(curl -fsS -X POST "$API_URL/runs" -H 'content-type: application/json' -d "$payload")"
echo "$resp"
run_id="$(echo "$resp" | sed -n 's/.*"id"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')"
if [ -z "$run_id" ]; then
  echo "failed to parse run id from response" >&2
  exit 1
fi

for _ in $(seq 1 600); do
  status_json="$(curl -fsS "$API_URL/runs/$run_id")"
  status="$(echo "$status_json" | sed -n 's/.*"status"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')"
  if [ "$status" = "success" ]; then
    echo "$status_json"
    exit 0
  fi
  if [ "$status" = "failed" ]; then
    echo "$status_json" >&2
    exit 1
  fi
  sleep 1
done

echo "timed out waiting for run $run_id" >&2
exit 1
