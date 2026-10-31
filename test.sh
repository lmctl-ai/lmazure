#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
export LMAZURE_LIVE=0 LMAZURE_ALLOW_WRITES=0
timeout 90 python3 scripts/sync-sdk.py --ensure
timeout 30 python3 scripts/check-ci-safety.py --self-test
timeout 30 python3 scripts/check-ci-safety.py .github/workflows/ci.yml
timeout 30 python3 scripts/check-version.py
timeout 300 dotnet restore src/LmAzure/LmAzure.csproj --locked-mode
timeout 180 dotnet build src/LmAzure/LmAzure.csproj -c Release -p:PublishAot=false --no-restore -warnaserror
python3 scripts/check-host-flags.py sha256:05ab40974f2a9b748a77f1e7c57172ced7a7c2a64c61a43603c7c04bfda7b7ef dotnet src/LmAzure/bin/Release/net10.0/lmazure.dll
timeout 180 python3 scripts/test-cli.py
timeout 30 python3 scripts/test-examples.py

timeout 30 python3 scripts/test-compare-configs.py
timeout 30 python3 scripts/test-compare-results.py
timeout 30 python3 scripts/test-comparison-harness.py
