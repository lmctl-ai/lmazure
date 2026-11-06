#!/usr/bin/env python3
"""Run every recipes/sim/*.lms recipe against freshly started azuresim emulators.

Starts azuresim twice on free loopback ports (open mode, and --auth enforce with the
account-key/token policy file that auth_enforced_denial.lms authors), runs each recipe in
its own temporary working directory and asserts the PASS line.

Binaries: --azuresim/AZURESIM_BIN (else PATH), --lmazure/LMAZURE_BIN (released .NET host used
for `lmazure --simulator`, else PATH), --cloud-host/LMAZURE_CLOUD_BIN (Go lmazure build serving
the shared lmcloud schema; built from ./cmd/lmazure into build/sim/ when absent),
--lmscript/LMSCRIPT_BIN (else PATH). No Azure credentials are read and no network leaves the
loopback interface.
"""
from __future__ import annotations

import argparse
import json
import os
import shutil
import socket
import subprocess
import sys
import tempfile
import time
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
RECIPES = ROOT / "recipes" / "sim"
ENFORCE_RECIPES = {"auth_enforced_denial", "auth_enforced_cli"}


def free_port() -> int:
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


def which(explicit: str | None, env: str, name: str) -> str | None:
    for candidate in (explicit, os.environ.get(env)):
        if candidate:
            return str(Path(candidate).resolve())
    return shutil.which(name)


def start_sim(binary: str, port: int, log: Path, extra: list[str], env: dict[str, str]) -> subprocess.Popen:
    proc = subprocess.Popen([binary, "serve", "--port", str(port), *extra], stdout=log.open("w"), stderr=subprocess.STDOUT, env=env)
    url = f"http://127.0.0.1:{port}/devstoreaccount1?comp=list"
    for _ in range(600):
        if proc.poll() is not None:
            raise SystemExit(f"azuresim on port {port} exited early; see {log}")
        try:
            urllib.request.urlopen(url, timeout=1)
            return proc
        except urllib.error.HTTPError:
            return proc  # any HTTP answer (including 403 in enforce mode) means the listener is up
        except Exception:
            time.sleep(0.2)
    raise SystemExit(f"azuresim on port {port} did not become ready; see {log}")


def build_cloud_host() -> str:
    out = ROOT / "build" / "sim" / "lmazure-cloud"
    out.parent.mkdir(parents=True, exist_ok=True)
    env = dict(os.environ, CGO_ENABLED="0", GOTOOLCHAIN=os.environ.get("GOTOOLCHAIN", "auto"))
    subprocess.run(["go", "build", "-trimpath", "-o", str(out), "./cmd/lmazure"], cwd=ROOT, check=True, env=env)
    return str(out)


def run_persistence(sim: str, lmscript: str, hostargs: list[str], env: dict[str, str], work: Path, timeout: int) -> bool:
    """persistence_restart.lms across a real emulator restart: write with --data-dir, stop, start again, verify."""
    data_dir = work / "persist-data"
    data_dir.mkdir()
    out_dir = work / "persist-out"
    out_dir.mkdir()
    cwd = work / "persistence_restart_restart"
    cwd.mkdir()
    port = free_port()
    recipe = str(RECIPES / "persistence_restart.lms")

    def phase(name: str) -> subprocess.CompletedProcess[str]:
        cmd = [lmscript, recipe, "--phase", name, "--endpoint", f"http://127.0.0.1:{port}", "--out", str(out_dir), "--run", "restart1", *hostargs]
        return subprocess.run(cmd, cwd=cwd, env=env, text=True, capture_output=True, timeout=timeout)

    proc = start_sim(sim, port, work / "azuresim-persist-1.log", ["--data-dir", str(data_dir)], env)
    try:
        wrote = phase("write")
    finally:
        proc.terminate()
        proc.wait(timeout=30)
    if wrote.returncode != 0:
        (cwd / "stdout.txt").write_text(wrote.stdout + wrote.stderr)
        return False
    proc = start_sim(sim, port, work / "azuresim-persist-2.log", ["--data-dir", str(data_dir)], env)
    try:
        verified = phase("verify")
    finally:
        proc.terminate()
        proc.wait(timeout=30)
    ok = verified.returncode == 0 and "PASS persistence_restart" in verified.stdout
    if not ok:
        (cwd / "stdout.txt").write_text(verified.stdout + verified.stderr)
    return ok


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--azuresim")
    ap.add_argument("--lmazure", help="released lmazure executable (.NET host)")
    ap.add_argument("--cloud-host", help="Go lmazure build serving the lmcloud schema")
    ap.add_argument("--lmscript")
    ap.add_argument("--only", action="append", default=[], help="recipe stem to run (repeatable)")
    ap.add_argument("--timeout", type=int, default=420)
    args = ap.parse_args()

    azuresim = which(args.azuresim, "AZURESIM_BIN", "azuresim")
    lmazure = which(args.lmazure, "LMAZURE_BIN", "lmazure")
    lmscript = which(args.lmscript, "LMSCRIPT_BIN", "lmscript")
    cloud = which(args.cloud_host, "LMAZURE_CLOUD_BIN", "lmazure-cloud") or build_cloud_host()
    missing = [n for n, v in (("azuresim", azuresim), ("lmazure", lmazure), ("lmscript", lmscript)) if not v]
    if missing:
        print("missing executables: " + ", ".join(missing) + " (pass --<name> or set <NAME>_BIN; azuresim releases: https://github.com/lmctl-ai/azuresim/releases)", file=sys.stderr)
        return 2

    recipes = sorted(p for p in RECIPES.glob("*.lms") if not args.only or p.stem in args.only)
    if not recipes:
        print("no recipes selected", file=sys.stderr)
        return 2

    work = Path(tempfile.mkdtemp(prefix="lmazure-sim-recipes-"))
    procs: list[subprocess.Popen] = []
    base_env = {k: v for k, v in os.environ.items() if not k.startswith(("LMAZURE_", "LMCLOUD_", "AZURE_"))}
    failures: list[str] = []
    try:
        open_port, enforce_port = free_port(), free_port()
        procs.append(start_sim(azuresim, open_port, work / "azuresim-open.log", [], base_env))
        print(f"azuresim open mode on {open_port}")
        prep = work / "prepare"
        prep.mkdir()
        subprocess.run([lmscript, str(RECIPES / "auth_enforced_denial.lms"), "--prepare", "--out", str(prep)], cwd=prep, env=base_env, check=True, capture_output=True, text=True)
        auth_config = prep / "azuresim-auth.json"
        procs.append(start_sim(azuresim, enforce_port, work / "azuresim-enforce.log", ["--auth", "enforce", "--auth-config", str(auth_config)], base_env))
        print(f"azuresim enforce mode on {enforce_port} with {auth_config.name}")

        for recipe in recipes:
            port = enforce_port if recipe.stem in ENFORCE_RECIPES else open_port
            cwd = work / recipe.stem
            cwd.mkdir()
            cmd = [lmscript, str(recipe), "--endpoint", f"http://127.0.0.1:{port}", "--host", lmazure, "--cloud-host", cloud]
            started = time.monotonic()
            result = None
            try:
                result = subprocess.run(cmd, cwd=cwd, env=base_env, text=True, capture_output=True, timeout=args.timeout)
                ok = result.returncode == 0 and f"PASS {recipe.stem}" in result.stdout
                detail = "" if ok else ((result.stdout + result.stderr).strip().splitlines() or ["(no output)"])[-1]
            except subprocess.TimeoutExpired:
                ok, detail = False, "timeout"
            print(f"{'ok' if ok else 'FAIL':4} {recipe.stem:26} {time.monotonic() - started:6.1f}s" + ("" if ok else f"  {detail[:200]}"))
            if not ok:
                failures.append(recipe.stem)
                (cwd / "stdout.txt").write_text(result.stdout if result else "")

        if not args.only or "persistence_restart" in args.only:
            started = time.monotonic()
            ok = run_persistence(azuresim, lmscript, ["--host", lmazure, "--cloud-host", cloud], base_env, work, args.timeout)
            print(f"{'ok' if ok else 'FAIL':4} {'persistence_restart*':26} {time.monotonic() - started:6.1f}s" + ("" if ok else "  (write, emulator restart with --data-dir, verify)"))
            if not ok:
                failures.append("persistence_restart(restart)")
    finally:
        for proc in procs:
            proc.terminate()
        for proc in procs:
            try:
                proc.wait(timeout=20)
            except subprocess.TimeoutExpired:
                proc.kill()
    print(json.dumps({"recipes": len(recipes), "failed": failures, "work": str(work)}))
    if failures:
        print(f"FAIL {len(failures)}/{len(recipes)} recipes; logs under {work}", file=sys.stderr)
        return 1
    shutil.rmtree(work, ignore_errors=True)
    print(f"PASS {len(recipes)} sim recipes")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
