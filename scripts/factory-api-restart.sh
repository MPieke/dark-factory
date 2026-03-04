#!/usr/bin/env bash
set -euo pipefail
"$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/factory-api-down.sh"
"$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/factory-api-up.sh"
