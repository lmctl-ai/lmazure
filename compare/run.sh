#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
exec timeout 2400 python3 compare/run.py "$@"
