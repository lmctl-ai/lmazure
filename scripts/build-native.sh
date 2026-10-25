#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
python3 scripts/sync-sdk.py --ensure
case "$(uname -s):$(uname -m)" in
  Linux:x86_64) rid=linux-x64 ;;
  Linux:aarch64) rid=linux-arm64 ;;
  Darwin:arm64) rid=osx-arm64 ;;
  Darwin:x86_64) rid=osx-x64 ;;
  MINGW*:*|MSYS*:*|CYGWIN*:*) rid=win-x64 ;;
  *) echo 'unsupported native build target' >&2; exit 2 ;;
esac
mkdir -p artifacts/native
options=(-p:IlcTreatWarningsAsErrors=true)
if [[ "$rid" == linux-* ]]; then
  # Prefer the baseline CMake installed with the Linux build prerequisites.
  mkdir -p build/native-tools
  ln -sf /usr/bin/cmake build/native-tools/cmake
  export PATH="$PWD/build/native-tools:$PATH"
  options+=(-p:StaticOpenSslLinking=true)
  export LMAZURE_STATIC_ZLIB
  LMAZURE_STATIC_ZLIB="$(gcc -print-file-name=libz.a)"
  [[ -f "$LMAZURE_STATIC_ZLIB" ]] || { echo 'static zlib development archive required' >&2; exit 2; }
fi
timeout 1200 dotnet publish src/LmAzure/LmAzure.csproj -r "$rid" -c Release -p:PublishAot=true "${options[@]}" -o artifacts/native
