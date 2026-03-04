#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
docker compose -f docker-compose.factory-api.yml up -d --build
for i in $(seq 1 30); do
  if curl -fsS http://127.0.0.1:8080/health >/dev/null 2>&1; then
    echo "factory-api is healthy"
    exit 0
  fi
  sleep 1
done
echo "factory-api failed health check" >&2
exit 1
