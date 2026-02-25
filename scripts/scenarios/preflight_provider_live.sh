#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(pwd)"
OPENAI_MODEL="${OPENAI_LIVE_MODEL:-gpt-4.1-mini}"
OPENAI_MAX_OUTPUT_TOKENS="${OPENAI_MAX_OUTPUT_TOKENS:-16}"
ANTHROPIC_MAX_TOKENS="${ANTHROPIC_MAX_TOKENS:-16}"

if [ -f "$ROOT_DIR/.env" ]; then
  set -a
  # shellcheck disable=SC1090
  source "$ROOT_DIR/.env"
  set +a
fi

if [ -z "${OPENAI_API_KEY:-}" ]; then
  echo "OPENAI_API_KEY is required for live preflight"
  exit 1
fi
if [ -z "${ANTHROPIC_API_KEY:-}" ]; then
  echo "ANTHROPIC_API_KEY is required for live preflight"
  exit 1
fi

resolve_anthropic_model() {
  if [ -n "${ANTHROPIC_LIVE_MODEL:-}" ]; then
    echo "$ANTHROPIC_LIVE_MODEL"
    return 0
  fi

  local models_body
  models_body="$(mktemp)"
  local models_status
  models_status="$(curl -sS -o "$models_body" -w "%{http_code}" \
    -X GET "https://api.anthropic.com/v1/models" \
    -H "x-api-key: $ANTHROPIC_API_KEY" \
    -H "anthropic-version: 2023-06-01")"

  if [ "$models_status" -lt 200 ] || [ "$models_status" -ge 300 ]; then
    echo "Anthropic models list failed ($models_status): $(cat "$models_body")"
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
    "claude-3-7-sonnet-20250219" \
    "claude-3-5-haiku-20241022" \
    "claude-3-5-haiku-latest"
  do
    if echo "$ids" | grep -Fxq "$preferred"; then
      echo "$preferred"
      return 0
    fi
  done

  echo "$ids" | head -n 1
}

ANTHROPIC_MODEL="$(resolve_anthropic_model)" || exit 1
echo "Using Anthropic model for preflight: $ANTHROPIC_MODEL"

OPENAI_BODY="$(mktemp)"
ANTHROPIC_BODY="$(mktemp)"
trap 'rm -f "$OPENAI_BODY" "$ANTHROPIC_BODY"' EXIT

OPENAI_STATUS="$(curl -sS -o "$OPENAI_BODY" -w "%{http_code}" \
  -X POST "https://api.openai.com/v1/responses" \
  -H "Authorization: Bearer $OPENAI_API_KEY" \
  -H "Content-Type: application/json" \
  -d "{\"model\":\"$OPENAI_MODEL\",\"input\":\"preflight-openai\",\"max_output_tokens\":$OPENAI_MAX_OUTPUT_TOKENS}")"

if [ "$OPENAI_STATUS" -lt 200 ] || [ "$OPENAI_STATUS" -ge 300 ]; then
  echo "OpenAI preflight failed ($OPENAI_STATUS): $(cat "$OPENAI_BODY")"
  exit 1
fi

ANTHROPIC_STATUS="$(curl -sS -o "$ANTHROPIC_BODY" -w "%{http_code}" \
  -X POST "https://api.anthropic.com/v1/messages" \
  -H "x-api-key: $ANTHROPIC_API_KEY" \
  -H "anthropic-version: 2023-06-01" \
  -H "content-type: application/json" \
  -d "{\"model\":\"$ANTHROPIC_MODEL\",\"max_tokens\":$ANTHROPIC_MAX_TOKENS,\"messages\":[{\"role\":\"user\",\"content\":\"preflight-anthropic\"}]}")"

if [ "$ANTHROPIC_STATUS" -lt 200 ] || [ "$ANTHROPIC_STATUS" -ge 300 ]; then
  echo "Anthropic preflight failed ($ANTHROPIC_STATUS): $(cat "$ANTHROPIC_BODY")"
  exit 1
fi

echo "provider live preflight passed"
