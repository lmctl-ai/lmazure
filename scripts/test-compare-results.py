#!/usr/bin/env python3
"""Unit regressions for the pure comparison engine."""
import json
import tempfile
from pathlib import Path
import sys

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from compare.compare_results import compare, write_results

def side(name="item", tag="run", leftovers=0):
    return {
        "mode": "mock",
        "account_fingerprint": "safe-account",
        "resources_created": [
            {"type": "Microsoft.Storage/storageAccounts/blobServices/containers",
             "name": name, "tags": {"lmazure_test": tag}, "metadata_sha256": "abc"}
        ],
        "outputs": {"rows": [{"name": name, "secret": "never compare"}]},
        "wall_seconds": 1.0,
        "estimated_cost_usd": 0.0,
        "leftovers": leftovers,
    }

def report(**changes):
    a, b = side(), side()
    for key, value in changes.items():
        if key.startswith("a_"): a[key[2:]] = value
        elif key.startswith("b_"): b[key[2:]] = value
    return {"lmazure": a, "lmcloud": b}

def expect(condition, message):
    if not condition:
        raise AssertionError(message)

same = compare(report())
expect(same["ok"], "normalized Azure/Tofu container should match")
expect(same["offline"]["resources"]["status"] == "same", "resource status")
expect(same["live"]["status"] == "pending", "live must remain pending")
expect(same["offline"]["time"]["status"] == "same", "equal time")

different_tag = compare(report(b_resources_created=[{"type": "azurerm_storage_container", "name": "item", "tags": {"lmazure_test": "other"}}]))
expect(different_tag["ok"] is False, "ownership tag mismatch must fail")
duplicate = compare(report(a_resources_created=side()["resources_created"] * 2))
expect(duplicate["offline"]["resources"]["status"] == "differs", "duplicates must fail")
missing = compare(report(a_leftovers=None))
expect(missing["offline"]["cleanup"]["lmazure"]["status"] == "pending", "null leftovers must stay pending")
remaining = compare(report(b_leftovers=1))
expect(remaining["ok"] is False, "leftovers must fail")
time_cost = compare(report(b_wall_seconds=9.0, b_estimated_cost_usd=0.0001))
expect(time_cost["offline"]["time"]["status"] == "differs", "time difference is retained")
expect(time_cost["offline"]["cost"]["status"] == "differs", "cost difference is retained")
account = compare(report(b_account_fingerprint="other"))
expect(account["offline"]["account"]["status"] == "differs", "account mismatch")
unsafe = compare(report(a_resources_created=[{"type": "azurerm_storage_container", "name": "item", "tags": {}}]))
expect(unsafe["ok"] is False, "missing ownership metadata")
hash_diff = compare(report(b_resources_created=[{"type": "azurerm_storage_container", "name": "item", "tags": {"lmazure_test": "run"}, "metadata_sha256": "different"}]))
expect(hash_diff["offline"]["resources"]["status"] == "differs", "metadata fingerprint mismatch")
hash_unknown = compare(report(b_resources_created=[{"type": "azurerm_storage_container", "name": "item", "tags": {"lmazure_test": "run"}}]))
expect(hash_unknown["offline"]["resources"]["status"] == "pending", "single unknown metadata fingerprint")
no_outputs = compare({"lmazure": {**side(), "outputs": None}, "lmcloud": side()})
expect(no_outputs["offline"]["outputs"]["status"] == "pending" and no_outputs["ok"] is False, "null outputs")
same_wrong_account = compare({"lmazure": {**side(), "account_fingerprint": "a"}, "lmcloud": {**side(), "account_fingerprint": "b"}})
expect(same_wrong_account["ok"] is False, "wrong account must fail even when resources are empty or same")
fixture = compare({**report(), "fixture_inputs_identical": True})
expect(fixture["ok"] is False, "fixture-only sameness must not claim actual equivalence")

with tempfile.TemporaryDirectory() as directory:
    path = Path(directory) / "RESULTS.md"
    write_results([{"name": "sample"}], {"sample": same}, path)
    expect("pending" in path.read_text(encoding="utf-8"), "RESULTS must state live pending")

print("compare-results checks: 11 passed")
