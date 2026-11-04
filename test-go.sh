#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Go host verification and azuresim conformance runner for lmazure (no make).
set -euo pipefail

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
cd "$ROOT"

LMSCRIPT_BIN="${LMSCRIPT_BIN:-/home/ubuntu/lmctl-mono/lmscript/bin/lmscript}"
LMCLOUD_DIR="${LMCLOUD_DIR:-/home/ubuntu/cloud/lmcloud}"
SIM_SH="${SIM_SH:-${LMCLOUD_DIR}/scripts/sim.sh}"
export LMCLOUD_SIM_DIR="${LMCLOUD_SIM_DIR:-${LMCLOUD_DIR}/.cache/sims}"
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
grep -q "06cd9ae991d90fea9ad38f3b071da646c76832f4" sdk.lock
grep -q "06cd9ae991d9" go.mod
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
'
"$LMSCRIPT_BIN" --dry-run -e '
s = load_schema(schema="schema/lmcloud.dsl.json");
p = _lmcode_ s;
  let prov = cloud.provider();
  let c = cloud.bucket_create("lmazure-smoke-bucket");
  let ok_code = cloud.last_error().code() == ErrorCode.OK;
  let ex = cloud.bucket_exists("lmazure-smoke-bucket");
  let d = cloud.bucket_delete("lmazure-smoke-bucket");
  cloud.set_abort_on_error(false);
  cloud.object_get("lmazure-smoke-missing", "k");
  let nf_code = cloud.last_error().code() == ErrorCode.NOT_FOUND;
  lmcode_return_put("prov", prov);
  lmcode_return_put("created", c);
  lmcode_return_put("ok_code", ok_code);
  lmcode_return_put("nf_code", nf_code);
  lmcode_return_put("exists", ex);
  lmcode_return_put("deleted", d);
end _lmcode_
r_dry = run_lmcode(p, {command:["./bin/lmazure", "cloud", "--lmcode", "$DSLFILE"]});
assert(r_dry.ok && r_dry.dry && r_dry.result.prov == "azure" && r_dry.result.created && r_dry.result.ok_code && r_dry.result.nf_code && r_dry.result.exists && r_dry.result.deleted, "dry smoke failed");
'

echo "==> [8/9] Running go vet ./..., go test -race -count=3 ./..., and HTTP dry-run spy"
go vet ./...
go test -race -count=3 ./...
python3 "$ROOT/scripts/test-dry-spy.py" "$LMSCRIPT_BIN" "$BIN" "$LMCLOUD_DIR/schema/lmcloud.dsl.json"

if [[ -x "$SIM_SH" && -x "$LMCLOUD_DIR/tests/run-conformance.sh" ]]; then
  echo "==> [9/9] Running lmcloud conformance (open) against azuresim"
  cleanup_sim() {
    "$SIM_SH" stop azure --name "$SIM_NAME" >/dev/null 2>&1 || true
  }
  trap cleanup_sim EXIT
  ENDPOINT=$("$SIM_SH" start azure --name "$SIM_NAME" --free-port)
  LMSCRIPT_BIN="$LMSCRIPT_BIN" "$LMCLOUD_DIR/tests/run-conformance.sh" "$BIN" "$ENDPOINT" all
  cleanup_sim
  trap - EXIT

  SIM_BIN="$LMCLOUD_SIM_DIR/azuresim"
  if [[ -x "$SIM_BIN" ]]; then
    echo "==> [9b/9] Running full lmcloud conformance under azuresim --auth enforce"
    ENF_TMP=$(mktemp -d)
    cat > "$ENF_TMP/auth.json" <<'EOF'
{
  "accounts": {
    "devstoreaccount1": "Eby8vdM02xNOcqFlqUwJPLlmEtlCDXJ1OUzFT50uSRZ6IFsuFq2UVErCz4I6tq/K1SZFPTOtr/KBHBeksoGMGw=="
  },
  "tokens": {
    "local-demo-token": "demo-user"
  },
  "Statement": [
    {
      "Effect": "Allow",
      "ActionGlob": "Microsoft.Storage/*",
      "ResourceGlob": "*",
      "Principal": ["devstoreaccount1"]
    }
  ]
}
EOF
    ENFORCE_PORT=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()')
    "$SIM_BIN" serve --auth enforce --auth-config "$ENF_TMP/auth.json" --port "$ENFORCE_PORT" >/dev/null 2>&1 &
    ENFORCE_PID=$!
    cleanup_enforce() {
      kill -TERM "$ENFORCE_PID" >/dev/null 2>&1 || true
      wait "$ENFORCE_PID" >/dev/null 2>&1 || true
      rm -rf "$ENF_TMP"
    }
    trap cleanup_enforce EXIT
    for _ in $(seq 1 30); do
      if curl -s "http://127.0.0.1:$ENFORCE_PORT/devstoreaccount1?comp=list" >/dev/null 2>&1 || curl -s "http://127.0.0.1:$ENFORCE_PORT" >/dev/null 2>&1; then
        break
      fi
      sleep 0.1
    done
    LMCLOUD_ENFORCE_DENIED_AZURE_TOKEN="local-demo-token" \
    LMSCRIPT_BIN="$LMSCRIPT_BIN" "$LMCLOUD_DIR/tests/run-conformance.sh" "$BIN" "http://127.0.0.1:$ENFORCE_PORT" queue
    LMCLOUD_ENFORCE_DENIED_AZURE_TOKEN="local-demo-token" \
    LMSCRIPT_BIN="$LMSCRIPT_BIN" "$LMCLOUD_DIR/tests/run-conformance.sh" "$BIN" "http://127.0.0.1:$ENFORCE_PORT" cost-perf
    LMCLOUD_ENFORCE_DENIED_AZURE_TOKEN="local-demo-token" \
    "$LMSCRIPT_BIN" "$LMCLOUD_DIR/tests/conformance/object-storage/12-auth-enforced-denial.lms" "$BIN" "http://127.0.0.1:$ENFORCE_PORT" "enf-blob"
    cleanup_enforce
    trap - EXIT
    echo "Auth enforce conformance (queue 13/13, cost-perf 10/10, object-storage/12-auth-enforced-denial) passed cleanly"
  fi
else
  echo "==> [9/9] Skipping azuresim conformance (sim.sh or lmcloud repo not found)"
fi

echo "ALL GO HOST CHECKS PASSED"
