#!/usr/bin/env python3
"""Render every mock-only gallery example with the local lmscript tool."""
from pathlib import Path
import os
import shutil
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[1]
compiler = os.environ.get("LMAZURE_LMSCRIPT", "lmscript")
timeout = int(os.environ.get("LMAZURE_EXAMPLE_TIMEOUT", "120"))
if os.environ.get("LMAZURE_LIVE") == "1":
    raise SystemExit("refusing to render gallery with LMAZURE_LIVE=1")

examples = sorted((ROOT / "examples").glob("*.lms"))
if os.environ.get("LMAZURE_LOCAL") == "1":
    with tempfile.TemporaryDirectory(prefix="lmazure-local-examples-") as source_dir:
        source = Path(source_dir)
        for example in examples:
            text = example.read_text(encoding="utf-8")
            for host in ("lmazure", "lmplot", "lmdraw"):
                text = text.replace(f' install="github:lmctl-ai/{host}@latest"', "")
            local = source / example.name
            local.write_text(text, encoding="utf-8")
            subprocess.run([compiler, str(local)], cwd=ROOT, check=True, timeout=timeout)
else:
    for example in examples:
        subprocess.run([compiler, str(example)], cwd=ROOT, check=True, timeout=timeout)

(ROOT / "docs/images").mkdir(parents=True, exist_ok=True)
for image in sorted((ROOT / "images").glob("*.png")):
    shutil.copy2(image, ROOT / "docs/images" / image.name)
print(f"rendered {len(examples)} mock examples")
