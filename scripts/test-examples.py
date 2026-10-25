#!/usr/bin/env python3
"""Validate the catalog-driven lmazure gallery and run every example in isolation."""
from pathlib import Path
import json
import os
import re
import shutil
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[1]
catalog = json.loads((ROOT / "examples/catalog.json").read_text(encoding="utf-8"))
if not isinstance(catalog, list) or len(catalog) < 72:
    raise SystemExit("catalog must contain at least 72 examples")
by_name = {entry["name"]: entry for entry in catalog}
if len(by_name) != len(catalog):
    raise SystemExit("catalog contains duplicate names")
examples = {p.stem: p for p in (ROOT / "examples").glob("*.lms")}
if set(examples) != set(by_name):
    raise SystemExit("catalog and examples directory differ")
readme = (ROOT / "README.md").read_text(encoding="utf-8")
for name, entry in by_name.items():
    source = examples[name].read_text(encoding="utf-8")
    if entry["name"] + ".lms" not in readme:
        raise SystemExit(f"README missing catalog example: {name}")
    if "live()" in source:
        raise SystemExit(f"default gallery may not call live(): {name}")
    lines = [line.strip() for line in source.splitlines() if line.strip()]
    literal = 'println(' + json.dumps(entry["cost_output"]) + ');'
    if lines[-1] != literal and not re.fullmatch(r'println\(getstr\([A-Za-z_][A-Za-z0-9_]*, "estimated_cost_text"\)\);', lines[-1]):
        raise SystemExit(f"missing final cost output statement: {name}")
    url = f"https://raw.githubusercontent.com/lmctl-ai/lmazure/main/examples/{name}.lms"
    if not any(f'docs/images/{name}.png' in line and url in line and 'running this example costs about $' in line for line in readme.splitlines()):
        raise SystemExit(f"README missing illustrated autorun/cost caption: {name}")

images = sorted((ROOT / "docs/images").glob("*.png"))
image_names = {image.stem for image in images}
if image_names != set(by_name):
    raise SystemExit("catalog and docs/images differ")
for raw in __import__("re").findall(r'(?:src|href)="([^"]+.png)"', readme):
    if not (ROOT / raw).is_file():
        raise SystemExit(f"README image missing: {raw}")
for image in images:
    data = image.read_bytes()
    if data[:8] != b"\x89PNG\r\n\x1a\n":
        raise SystemExit(f"invalid PNG: {image}")
    if image.stat().st_size > 300_000:
        raise SystemExit(f"gallery PNG exceeds 300 KB: {image}")

compiler = os.environ.get("LMAZURE_LMSCRIPT")
if compiler:
    compiler_path = shutil.which(compiler) or compiler
    env = os.environ.copy()
    env["LMAZURE_LIVE"] = "0"
    with tempfile.TemporaryDirectory(prefix="lmazure-local-examples-") as source_dir:
        source_root = Path(source_dir)
        local_examples = {}
        for name, source in examples.items():
            text = source.read_text(encoding="utf-8")
            if os.environ.get("LMAZURE_LOCAL") == "1":
                for host in ("lmazure", "lmplot", "lmdraw"):
                    text = text.replace(f' install="github:lmctl-ai/{host}@latest"', "")
            local = source_root / source.name
            local.write_text(text, encoding="utf-8")
            local_examples[name] = local
        for name, entry in by_name.items():
            with tempfile.TemporaryDirectory(prefix="lmazure-gallery-") as scratch:
                result = subprocess.run(
                    [compiler_path, str(local_examples[name])], cwd=scratch, env=env,
                    check=True, capture_output=True, text=True,
                    timeout=int(os.environ.get("LMAZURE_EXAMPLE_TIMEOUT", "120")),
                )
                output_lines = [line.strip() for line in result.stdout.splitlines() if line.strip()]
                expected = entry["cost_output"]
                if output_lines[-1:] != [expected]:
                    raise SystemExit(f"wrong final cost line for {name}: {output_lines[-3:]}")
                image = Path(scratch) / "images" / (name + ".png")
                if not image.is_file():
                    raise SystemExit(f"example did not write its PNG: {name}")
                data = image.read_bytes()
                if data[:8] != b"\x89PNG\r\n\x1a\n" or len(data) > 300000 or int.from_bytes(data[16:20],"big") != 900 or int.from_bytes(data[20:24],"big") != 560:
                    raise SystemExit(f"invalid generated PNG dimensions or data: {name}")
                evidence = Path(scratch) / entry["evidence_file"]
                if not evidence.is_file():
                    raise SystemExit(f"cost evidence missing: {name}: {entry['evidence_file']}")
                report = json.loads(evidence.read_text(encoding="utf-8"))
                mode = entry["ledger_mode"]
                if mode == "readonly":
                    if report.get("mode") != "fake" or report.get("estimated_cost_usd") != 0:
                        raise SystemExit(f"readonly cost evidence mismatch: {name}")
                    if report.get("resources") != [] or report.get("owned") != [] or report.get("metadata_inventory",{}).get("list_requests",0) < 1:
                        raise SystemExit(f"readonly resource evidence is not empty: {name}")
                elif mode == "owned":
                    if report.get("mode") != "fake" or report.get("estimated_cost_usd") != entry["cost_usd"]:
                        raise SystemExit(f"owned cost evidence mismatch: {name}")
                    if len(report.get("requests", [])) != entry["requests"]:
                        raise SystemExit(f"owned request count mismatch: {name}")
                    if any(resource.get("owned") is not False for resource in report.get("resources", [])):
                        raise SystemExit(f"owned resources remain after example: {name}")
                    if report.get("reserved_cost_usd", 0) != 0:
                        raise SystemExit(f"owned reservation remains: {name}")
                elif mode == "budget":
                    if report.get("mode") != "fake" or report.get("estimated_cost_usd") != 0 or report.get("requests") != [] or report.get("resources") != []:
                        raise SystemExit(f"budget evidence mismatch: {name}")
    print(f"executed {len(examples)} catalog examples in isolated directories")
else:
    print("example execution SKIP: set LMAZURE_LMSCRIPT with the pinned compiler")
print(f"checked {len(examples)} catalog examples and {len(images)} images")
