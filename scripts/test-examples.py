#!/usr/bin/env python3
"""Check gallery coverage and optionally execute every example in a clean directory."""
from pathlib import Path
import os
import re
import shutil
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[1]
examples = sorted((ROOT / "examples").glob("*.lms"))
if len(examples) < 25:
    raise SystemExit(f"expected at least 25 examples, found {len(examples)}")
readme = (ROOT / "README.md").read_text(encoding="utf-8")
for example in examples:
    if example.name not in readme:
        raise SystemExit(f"README missing example: {example.name}")
    if "live()" in example.read_text(encoding="utf-8"):
        raise SystemExit(f"default gallery may not call live(): {example.name}")
    lines = [line.strip() for line in example.read_text(encoding="utf-8").splitlines() if line.strip()]
    if example.stem not in {"owned_container_cycle", "owned_queue_cycle"}:
        if not re.fullmatch(r'println\("estimated cost: \$0\.00"\);', lines[-1]):
            raise SystemExit(f"read-only example must end with estimated cost: $0.00: {example.name}")
    elif lines[-1] != 'println(getstr(cost_report, "estimated_cost_text"));':
        raise SystemExit(f"owned example must print its ledger cost: {example.name}")

images = sorted((ROOT / "docs/images").glob("*.png"))
if not images:
    raise SystemExit("gallery images missing: run scripts/render-examples.py first")
image_names = {image.stem for image in images}
missing_example_images = [example.stem for example in examples if example.stem not in image_names]
if missing_example_images:
    raise SystemExit("gallery images missing for examples: " + ", ".join(missing_example_images))
for raw in re.findall(r'(?:src|href)="([^"]+\.png)"', readme):
    image = ROOT / raw
    if not image.is_file():
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
        source = Path(source_dir)
        local_examples = {}
        for example in examples:
            text = example.read_text(encoding="utf-8")
            if os.environ.get("LMAZURE_LOCAL") == "1":
                for host in ("lmazure", "lmplot", "lmdraw"):
                    text = text.replace(f' install="github:lmctl-ai/{host}@latest"', "")
            local = source / example.name
            local.write_text(text, encoding="utf-8")
            local_examples[example] = local
        for example in examples:
            with tempfile.TemporaryDirectory(prefix="lmazure-gallery-") as scratch:
                result = subprocess.run([compiler_path, str(local_examples[example])], cwd=scratch, env=env,
                                        check=True, capture_output=True, text=True,
                                        timeout=int(os.environ.get("LMAZURE_EXAMPLE_TIMEOUT", "120")))
                output_lines = [line.strip() for line in result.stdout.splitlines() if line.strip()]
                expected_cost = "estimated cost: $0.0005" if example.stem.startswith("owned_") else "estimated cost: $0.00"
                if not output_lines or output_lines[-1] != expected_cost:
                    raise SystemExit(f"wrong final cost line for {example.name}: {output_lines[-3:]}")
                image = Path(scratch) / "images" / (example.stem + ".png")
                if not image.is_file():
                    raise SystemExit(f"example did not write its PNG: {example.name}")
                if example.stem in {"owned_container_cycle", "owned_queue_cycle"}:
                    evidence = Path(scratch) / "data" / (example.stem + "_cost_evidence.json")
                    if not evidence.is_file():
                        raise SystemExit(f"owned example cost evidence missing: {example.name}")
                    import json
                    report = json.loads(evidence.read_text(encoding="utf-8"))
                    if report.get("mode") != "fake" or report.get("estimated_cost_usd") != 0.0005:
                        raise SystemExit(f"owned example evidence cost mismatch: {example.name}")
                    if len(report.get("requests", [])) != 5 or report.get("reserved_cost_usd") != 0:
                        raise SystemExit(f"owned example evidence request/reservation mismatch: {example.name}")
                    resources = report.get("resources", [])
                    if len(resources) != 1 or resources[0].get("owned") is not False:
                        raise SystemExit(f"owned example evidence ownership mismatch: {example.name}")
    print(f"executed {len(examples)} examples in an empty directory")
else:
    print("example execution SKIP: set LMAZURE_LMSCRIPT with the pinned compiler")
print(f"checked {len(examples)} examples and {len(images)} images")
