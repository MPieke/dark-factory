#!/usr/bin/env bash
set -euo pipefail

TARGET="${1:-scripts/scenarios}"

if [ -d "$TARGET" ]; then
  FILES=()
  while IFS= read -r line; do
    FILES+=("$line")
  done < <(find "$TARGET" -maxdepth 1 -type f -name "*.sh" | sort)
else
  FILES=("$TARGET")
fi

if [ "${#FILES[@]}" -eq 0 ]; then
  echo "no scenario scripts found in: $TARGET"
  exit 1
fi

fail=0
for f in "${FILES[@]}"; do
  if [ ! -f "$f" ]; then
    echo "missing file: $f"
    fail=1
    continue
  fi

  if [[ "$f" == *"_user_scenarios.sh" ]]; then
    if ! grep -q 'SCENARIO_MODE=' "$f"; then
      echo "$f: missing SCENARIO_MODE contract"
      fail=1
    fi

    if grep -nE 'ANTHROPIC_LIVE_MODEL=.*\:-[^}"]' "$f" >/dev/null; then
      echo "$f: hardcoded ANTHROPIC_LIVE_MODEL default detected; use empty default + dynamic resolver"
      fail=1
    fi
  fi

  if grep -nE '/tmp/[A-Za-z0-9._-]+' "$f" >/dev/null; then
    echo "$f: fixed /tmp path detected; use mktemp"
    fail=1
  fi
done

if [ "$fail" -ne 0 ]; then
  exit 1
fi

echo "scenario lint passed (${#FILES[@]} files)"
