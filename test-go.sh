#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Go host verification and azuresim conformance runner for lmazure (no make).
set -euo pipefail

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
cd "$ROOT"

LMSCRIPT_BIN="${LMSCRIPT_BIN:-/home/ubuntu/lmctl-mono/lmscript/bin/lmscript}"
LMCLOUD_DIR="${LMCLOUD_DIR:-/home/ubuntu/cloud/lmcloud}"
SIM_SH="${SIM_SH:-/home/ubuntu/cloud/lmcloud-sims/scripts/sim.sh}"
export LMCLOUD_SIM_DIR="${LMCLOUD_SIM_DIR:-/home/ubuntu/cloud/lmcloud-sims/.cache/sims}"
SIM_NAME="${LMAZURE_SIM_NAME:-lmazure-test}"

echo "==> [1/9] Building Go host bin/lmazure (CGO_ENABLED=0 -trimpath)"
mkdir -p bin
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o bin/lmazure ./cmd/lmazure
BIN="$ROOT/bin/lmazure"

if [[ "$(uname -s)" == "Linux" ]]; then
  echo "==> [2/9] Checking Linux binary linkage (static or glibc allowlist)"
  if command -v readelf >/dev/null 2>&1; then
    needed="$(readelf -d "$BIN" 2>/dev/null | grep '(NEEDED)' || true)"
    if [[ -n "$needed" ]]; then
      bad="$(echo "$needed" | grep -vE '\[(libc\.so\.6|libm\.so\.6|libpthread\.so\.0|libdl\.so\.2|librt\.so\.1|ld-linux[^]]*)\]' || true)"
      [[ -z "$bad" ]] || { echo "disallowed NEEDED libraries on Linux: $bad" >&2; exit 1; }
    fi
  elif command -v ldd >/dev/null 2>&1; then
    ldd_out="$(ldd "$BIN" 2>&1 || true)"
    case "$ldd_out" in
      *"not a dynamic executable"*|*"statically linked"*) ;;
      *)
        bad="$(echo "$ldd_out" | awk '/=>/ {print $1}' | grep -vE '^(libc\.so\.6|libm\.so\.6|libpthread\.so\.0|libdl\.so\.2|librt\.so\.1|ld-linux.*|linux-vdso\.so\..*)$' || true)"
        [[ -z "$bad" ]] || { echo "disallowed dynamic libs on Linux: $bad" >&2; exit 1; }
        ;;
    esac
  fi
fi

echo "==> [3/9] Checking --version, --help, --notices, sdk.lock, and lmcloud.lock"
VER=$("$BIN" --version)
[[ "$VER" == "lmazure 0.1.5" ]] || { echo "unexpected --version: $VER" >&2; exit 1; }
"$BIN" --help | grep -q "lmazure 0.1.5"
"$BIN" --notices | grep -q "github.com/Azure/azure-sdk-for-go"
grep -q "77932bf7134af4c330fa1eee16c7e10c1fb1a1d7" sdk.lock
grep -q "77932bf7134a" go.mod
./scripts/sync-lmcloud-schema.sh --verify

echo "==> [4/9] Checking --lmcode-schema [--json] byte parity (top-level and 'cloud' subcommand)"
cmp -s <("$BIN" --lmcode-schema) "$ROOT/schema/lmcloud.dsl.json"
cmp -s <("$BIN" --lmcode-schema --json) "$ROOT/schema/lmcloud.dsl.json"
cmp -s <("$BIN" cloud --lmcode-schema --json) "$ROOT/schema/lmcloud.dsl.json"
if [[ -f "$LMCLOUD_DIR/schema/lmcloud.dsl.json" ]]; then
  cmp -s <("$BIN" --lmcode-schema --json) "$LMCLOUD_DIR/schema/lmcloud.dsl.json"
fi

echo "==> [5/9] Checking --lmcode-signatures parity against lmscript"
diff -u <("$LMSCRIPT_BIN" --lmcode-signatures "$ROOT/schema/lmcloud.dsl.json") <("$BIN" --lmcode-signatures)
diff -u <("$LMSCRIPT_BIN" --lmcode-signatures "$ROOT/schema/lmcloud.dsl.json" --without-std) <("$BIN" --lmcode-signatures --without-std)
diff -u <("$LMSCRIPT_BIN" --lmcode-signatures "$ROOT/schema/lmcloud.dsl.json" --without-std) <("$BIN" cloud --lmcode-signatures --without-std)

echo "==> [6/9] Checking CLI failure frames (unknown flag, corrupt bytecode, invalid LMAZURE_MAX_MEMORY_MB)"
set +e
"$BIN" --unknown-flag >/dev/null 2>&1
RC_FLAG=$?
TMP_BAD=$(mktemp)
printf 'CORRUPT_BYTECODE' > "$TMP_BAD"
"$BIN" --lmcode "$TMP_BAD" >/dev/null 2>&1
RC_BAD=$?
LMAZURE_MAX_MEMORY_MB=not-a-number "$BIN" --lmcode "$TMP_BAD" >/dev/null 2>&1
RC_MEM=$?
rm -f "$TMP_BAD"
set -e
[[ "$RC_FLAG" -eq 2 && "$RC_BAD" -eq 3 && "$RC_MEM" -eq 2 ]] || {
  echo "expected exit codes flag=2 bad=3 mem=2; got flag=$RC_FLAG bad=$RC_BAD mem=$RC_MEM" >&2
  exit 1
}

echo "==> [7/9] Checking real and dry invocation through lmscript run_lmcode"
"$LMSCRIPT_BIN" -e '
s = load_schema(schema="schema/lmcloud.dsl.json");
p = _lmcode_ s;
  let prov = cloud.provider();
  let c = cloud.bucket_create("lmazure-smoke-bucket");
  let ex = cloud.bucket_exists("lmazure-smoke-bucket");
  let d = cloud.bucket_delete("lmazure-smoke-bucket");
  lmcode_return_put("prov", prov);
  lmcode_return_put("created", c);
  lmcode_return_put("exists", ex);
  lmcode_return_put("deleted", d);
end _lmcode_
r_real = run_lmcode(p, {command:["./bin/lmazure", "--lmcode", "$DSLFILE"]});
assert(r_real.ok && r_real.result.prov == "azure" && r_real.result.created && r_real.result.exists && r_real.result.deleted, "real smoke failed");
r_dry = run_lmcode(p, {command:["./bin/lmazure", "cloud", "--dry", "--lmcode", "$DSLFILE"]});
assert(r_dry.ok && r_dry.result.prov == "azure" && !r_dry.result.exists, "dry smoke failed");
'

echo "==> [8/9] Running go vet ./... and go test ./..."
go vet ./...
go test ./...

if [[ -x "$SIM_SH" && -x "$LMCLOUD_DIR/tests/run-conformance.sh" ]]; then
  echo "==> [9/9] Running lmcloud object-storage + queue conformance against azuresim"
  cleanup_sim() {
    "$SIM_SH" stop azure --name "$SIM_NAME" >/dev/null 2>&1 || true
  }
  trap cleanup_sim EXIT
  ENDPOINT=$("$SIM_SH" start azure --name "$SIM_NAME" --free-port)
  LMSCRIPT_BIN="$LMSCRIPT_BIN" "$LMCLOUD_DIR/tests/run-conformance.sh" "$BIN" "$ENDPOINT" all
  cleanup_sim
  trap - EXIT

  AUTH_CONFIG="/home/ubuntu/cloud/azuresim/examples/auth.json"
  SIM_BIN="$LMCLOUD_SIM_DIR/azuresim"
  if [[ -f "$AUTH_CONFIG" && -x "$SIM_BIN" ]]; then
    echo "==> [9b] Running scenarios 12 and 13 under azuresim --auth enforce"
    ENFORCE_PORT=4595
    "$SIM_BIN" serve --auth enforce --auth-config "$AUTH_CONFIG" --port "$ENFORCE_PORT" >/dev/null 2>&1 &
    ENFORCE_PID=$!
    cleanup_enforce() {
      kill -TERM "$ENFORCE_PID" >/dev/null 2>&1 || true
      wait "$ENFORCE_PID" >/dev/null 2>&1 || true
    }
    trap cleanup_enforce EXIT
    for _ in $(seq 1 30); do
      if curl -s "http://127.0.0.1:$ENFORCE_PORT/devstoreaccount1?comp=list" >/dev/null 2>&1 || curl -s "http://127.0.0.1:$ENFORCE_PORT" >/dev/null 2>&1; then
        break
      fi
      sleep 0.1
    done
    LMCLOUD_ENFORCE_DENIED_AZURE_TOKEN="local-demo-token" \
    LMCLOUD_ENFORCE_DENIED_AZURE_KEY="YWp1c3QtbG9jYWwtZGV2ZWxvcG1lbnQta2V5IQ==" \
    "$LMSCRIPT_BIN" "$LMCLOUD_DIR/tests/conformance/object-storage/12-auth-enforced-denial.lms" "$BIN" "http://127.0.0.1:$ENFORCE_PORT" "enf-blob"
    LMCLOUD_ENFORCE_DENIED_AZURE_TOKEN="local-demo-token" \
    LMCLOUD_ENFORCE_DENIED_AZURE_KEY="YWp1c3QtbG9jYWwtZGV2ZWxvcG1lbnQta2V5IQ==" \
    "$LMSCRIPT_BIN" "$LMCLOUD_DIR/tests/conformance/queue/13-queue-auth-enforced-denial.lms" "$BIN" "http://127.0.0.1:$ENFORCE_PORT" "enf-queue"
    cleanup_enforce
    trap - EXIT
    echo "Auth enforce scenarios 12 & 13 passed cleanly"
  fi
else
  echo "==> [9/9] Skipping azuresim conformance (sim.sh or lmcloud repo not found)"
fi

echo "ALL GO HOST CHECKS PASSED"
