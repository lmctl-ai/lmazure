#!/usr/bin/env python3
"""Check gallery coverage and optionally execute every example in a clean directory."""
from pathlib import Path
import os
import re
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
                subprocess.run([compiler, str(local_examples[example])], cwd=scratch, env=env,
                               check=True, timeout=int(os.environ.get("LMAZURE_EXAMPLE_TIMEOUT", "120")))
    print(f"executed {len(examples)} examples in an empty directory")
else:
    print("example execution SKIP: set LMAZURE_LMSCRIPT with the pinned compiler")
print(f"checked {len(examples)} examples and {len(images)} images")
