#!/usr/bin/env bash
set -euo pipefail
exec codex -c "mcp_servers.memory_ops.enabled=false" "$@"
