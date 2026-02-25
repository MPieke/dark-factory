#!/usr/bin/env bash
set -euo pipefail

APP_DIR="${1:-agent}"
SCENARIO_MODE="${SCENARIO_MODE:-live}"
REQUIRE_LIVE="${REQUIRE_LIVE:-1}"
ROOT_DIR="$(cd "$APP_DIR/.." && pwd)"
OPENAI_LIVE_MODEL="${OPENAI_LIVE_MODEL:-gpt-4.1-mini}"
ANTHROPIC_LIVE_MODEL="${ANTHROPIC_LIVE_MODEL:-}"

load_env_if_present() {
  if [ -f "$ROOT_DIR/.env" ]; then
    set -a
    # shellcheck disable=SC1090
    source "$ROOT_DIR/.env"
    set +a
  fi
}

run_json() {
  (
    cd "$APP_DIR"
    export GOCACHE="$PWD/.gocache"
    go run . ask "$@"
  )
}

assert_contains() {
  local haystack="$1"
  local needle="$2"
  if [[ "$haystack" != *"$needle"* ]]; then
    echo "Expected output to contain: $needle"
    echo "Actual: $haystack"
    exit 1
  fi
}

assert_not_contains() {
  local haystack="$1"
  local needle="$2"
  if [[ "$haystack" == *"$needle"* ]]; then
    echo "Expected output NOT to contain: $needle"
    echo "Actual: $haystack"
    exit 1
  fi
}

load_env_if_present

resolve_anthropic_model() {
  if [ -n "$ANTHROPIC_LIVE_MODEL" ]; then
    echo "$ANTHROPIC_LIVE_MODEL"
    return 0
  fi

  local models_body
  models_body="$(mktemp)"
  local status
  status="$(curl -sS -o "$models_body" -w "%{http_code}" \
    -X GET "https://api.anthropic.com/v1/models" \
    -H "x-api-key: $ANTHROPIC_API_KEY" \
    -H "anthropic-version: 2023-06-01")"

  if [ "$status" -lt 200 ] || [ "$status" -ge 300 ]; then
    echo "Anthropic models list failed ($status): $(cat "$models_body")"
    rm -f "$models_body"
    return 1
  fi

  local ids
  ids="$(grep -Eo '"id"[[:space:]]*:[[:space:]]*"[^"]+"' "$models_body" | sed -E 's/^"id"[[:space:]]*:[[:space:]]*"([^"]+)"$/\1/')"
  rm -f "$models_body"
  if [ -z "$ids" ]; then
    echo "Anthropic models list returned no model ids"
    return 1
  fi

  local preferred
  for preferred in \
    "claude-3-5-haiku-latest" \
    "claude-3-5-haiku-20241022" \
    "claude-3-7-sonnet-20250219"
  do
    if echo "$ids" | grep -Fxq "$preferred"; then
      echo "$preferred"
      return 0
    fi
  done

  echo "$ids" | head -n 1
}

run_selftest() {
  # openai default cheap model
  OUT1="$(run_json --provider openai --prompt hi --mock)"
  assert_contains "$OUT1" "\"provider\":\"openai\""
  assert_contains "$OUT1" "\"model\":\"gpt-4.1-mini\""
  assert_contains "$OUT1" "\"prompt\":\"hi\""
  assert_contains "$OUT1" "\"response\":\"mock:openai:gpt-4.1-mini:hi\""

  # anthropic explicit cheap model
  OUT2="$(run_json --provider anthropic --model claude-3-5-haiku-latest --prompt hello --mock)"
  assert_contains "$OUT2" "\"provider\":\"anthropic\""
  assert_contains "$OUT2" "\"model\":\"claude-3-5-haiku-latest\""
  assert_contains "$OUT2" "\"response\":\"mock:anthropic:claude-3-5-haiku-latest:hello\""

  # unsupported provider must fail
  local bad_out
  local bad_err
  bad_out="$(mktemp)"
  bad_err="$(mktemp)"
  set +e
  (
    cd "$APP_DIR"
    export GOCACHE="$PWD/.gocache"
    go run . ask --provider bad --prompt x --mock >"$bad_out" 2>"$bad_err"
  )
  RC=$?
  set -e
  rm -f "$bad_out" "$bad_err"
  if [ "$RC" -eq 0 ]; then
    echo "Expected unsupported provider to fail"
    exit 1
  fi
}

run_live() {
  if [ -z "${OPENAI_API_KEY:-}" ]; then
    echo "OPENAI_API_KEY is not set for live scenario checks"
    exit 1
  fi
  if [ -z "${ANTHROPIC_API_KEY:-}" ]; then
    echo "ANTHROPIC_API_KEY is not set for live scenario checks"
    exit 1
  fi
  ANTHROPIC_LIVE_MODEL="$(resolve_anthropic_model)" || exit 1

  LIVE1="$(run_json --provider openai --model "$OPENAI_LIVE_MODEL" --prompt live-openai-check)"
  assert_contains "$LIVE1" "\"provider\":\"openai\""
  assert_contains "$LIVE1" "\"model\":\"$OPENAI_LIVE_MODEL\""
  assert_contains "$LIVE1" "\"prompt\":\"live-openai-check\""
  assert_not_contains "$LIVE1" "\"response\":\"\""
  assert_not_contains "$LIVE1" "\"response\":\"mock:"

  LIVE2="$(run_json --provider anthropic --model "$ANTHROPIC_LIVE_MODEL" --prompt live-anthropic-check)"
  assert_contains "$LIVE2" "\"provider\":\"anthropic\""
  assert_contains "$LIVE2" "\"model\":\"$ANTHROPIC_LIVE_MODEL\""
  assert_contains "$LIVE2" "\"prompt\":\"live-anthropic-check\""
  assert_not_contains "$LIVE2" "\"response\":\"\""
  assert_not_contains "$LIVE2" "\"response\":\"mock:"
}

case "$SCENARIO_MODE" in
  selftest)
    run_selftest
    ;;
  live)
    if [ "$REQUIRE_LIVE" != "1" ]; then
      echo "Skipping live checks (REQUIRE_LIVE=0)"
      exit 0
    fi
    run_live
    ;;
  *)
    echo "unsupported SCENARIO_MODE: $SCENARIO_MODE (expected selftest or live)"
    exit 2
    ;;
esac

echo "agent user scenarios passed ($SCENARIO_MODE)"
