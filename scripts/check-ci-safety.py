#!/usr/bin/env python3
"""Reject credentials and live Azure enablement in public CI configuration."""
from __future__ import annotations

import argparse
import re
import sys
from pathlib import Path

BAD_ACTIONS = re.compile(r"(?i)(?:azure/(?:login|cli|powershell|webapps-deploy|docker-login)|azure/login@)")
BAD_PERMISSION = re.compile(r"(?i)(?:[\"']?id-token[\"']?\s*:\s*[\"']?write\b|permissions\s*:\s*[\"']?write-all\b)")
BAD_SECRET = re.compile(r"\$\{\{\s*secrets\.(?!GITHUB_TOKEN\b)[A-Za-z0-9_]+\s*\}\}")
BAD_CREDENTIAL_NAME = re.compile(r"(?i)(?<![A-Z0-9_])(?:AZURE|ARM)_(?:CLIENT_ID|TENANT_ID|SUBSCRIPTION_ID|CLIENT_SECRET|FEDERATED_TOKEN_FILE|AUTHORITY_HOST|ACCESS_TOKEN|PASSWORD|CREDENTIALS)(?![A-Z0-9_])")
LIVE_ENABLED = re.compile(r"(?i)(?<![A-Z0-9_])(?:LMAZURE_LIVE|LMAZURE_ALLOW_WRITES)\s*(?::|=)\s*['\"]?(?:1|true|yes)\b")
BAD_SECRET_REFERENCE = re.compile(r"(?i)\b(?:azure|arm)[_\-]?(?:token|password|secret|credentials?)\b|secrets\s*\[\s*['\"][^'\"]+['\"]\s*\]")


def violations(text: str) -> list[str]:
    found: list[str] = []
    if BAD_PERMISSION.search(text): found.append("id-token: write is forbidden")
    if BAD_ACTIONS.search(text): found.append("Azure login/provider action is forbidden")
    if BAD_SECRET.search(text): found.append("non-GitHub secret reference is forbidden")
    if BAD_CREDENTIAL_NAME.search(text): found.append("Azure credential variable name is forbidden")
    if BAD_SECRET_REFERENCE.search(text): found.append("Azure credential/token/password reference is forbidden")
    if LIVE_ENABLED.search(text): found.append("live Azure or write enablement is forbidden")
    return found


def check(path: Path) -> int:
    problems = violations(path.read_text(encoding="utf-8"))
    if problems:
        for problem in problems: print(f"CI safety violation: {problem}", file=sys.stderr)
        return 1
    print(f"CI safety guard passed: {path}")
    return 0


def self_test() -> int:
    good = """permissions:\n  contents: read\nenv:\n  LMAZURE_LIVE: '0'\n  LMAZURE_ALLOW_WRITES: '0'\nenv2: ${{ secrets.GITHUB_TOKEN }}\n"""
    bad_cases = [
        "permissions:\n  id-token: write\n",
        "permissions: {id-token: write}\n",
        "permissions: write-all\n",
        "permissions:\n  id-token: 'write'\n",
        "uses: azure/login@v2\n",
        "env: AZURE_CLIENT_ID: ${{ secrets.AZURE_CLIENT_ID }}\n",
        "env: AZURE_CLIENT_ID=hardcoded\n",
        "env: AZURE_FEDERATED_TOKEN_FILE=/tmp/token\n",
        "env: AZURE_AUTHORITY_HOST=https://login.microsoftonline.com\n",
        "env: TOKEN=${{ secrets['AZURE_TOKEN'] }}\n",
        "env:\n  LMAZURE_LIVE: '1'\n",
        "env:\n  LMAZURE_ALLOW_WRITES: true\n",
        "run: export LMAZURE_LIVE=1\n",
        "env: {LMAZURE_LIVE: 1}\n",
        "run: LMAZURE_ALLOW_WRITES=1 program\n",
        "env: password=${{ secrets.SERVICE_PASSWORD }}\n",
    ]
    if violations(good): raise SystemExit("guard self-test rejected valid fake-only fixture")
    for case in bad_cases:
        if not violations(case): raise SystemExit("guard self-test accepted unsafe fixture")
    print(f"CI safety guard self-tests passed: {len(bad_cases) + 1}")
    return 0


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("path", nargs="?", type=Path)
    parser.add_argument("--self-test", action="store_true")
    args = parser.parse_args()
    if args.self_test: return self_test()
    paths = [args.path] if args.path else sorted(Path(".github/workflows").glob("*.y*ml"))
    if not paths: raise SystemExit("no CI workflows found")
    return max(check(path) for path in paths)


if __name__ == "__main__":
    raise SystemExit(main())
