#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
export LMAZURE_LIVE=0 LMAZURE_ALLOW_WRITES=0
timeout 90 python3 scripts/sync-sdk.py --ensure
timeout 30 python3 scripts/check-version.py
timeout 300 dotnet restore src/LmAzure/LmAzure.csproj --locked-mode
timeout 180 dotnet build src/LmAzure/LmAzure.csproj -c Release -p:PublishAot=false --no-restore -warnaserror
timeout 180 python3 scripts/test-cli.py
timeout 30 python3 scripts/test-examples.py
