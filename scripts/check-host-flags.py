#!/usr/bin/env python3
"""Hard-cutover CLI contract. Retired spellings must fail before opening a file."""
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

expected = sys.argv[1]
schema = None if expected.startswith("sha256:") else Path(expected).resolve()
command = sys.argv[2:]
assert command, 'schema path and host command required'
old = '--' + 'dsl'
with tempfile.TemporaryDirectory(prefix='host flag control ') as directory:
    scratch = Path(directory)
    missing = scratch / 'must-not-open.dslb'
    env = dict(os.environ, HOME=str(scratch), LMCTL_HOME=str(scratch / '.lmctl'))
    def run(args):
        return subprocess.run([*command, *map(str, args)], capture_output=True, env=env)
    # Collect refusal controls so a baseline proves every retired spelling fails.
    failures = []
    for flag, rest in [(old, [missing]), (old + '-schema', ['--json']),
                       (old + '-signatures', [])]:
        result = run([flag, *rest])
        if result.returncode != 2: failures.append((flag, result.returncode, 'retired option accepted or reached execution'))
        if str(missing).encode() in result.stdout + result.stderr: failures.append((flag, 'opened retired-flag input'))
    assert not failures, failures
    assert not list(scratch.iterdir()), 'retired option created scratch-HOME state'
    result = run(['--lmcode-schema', '--json'])
    assert (result.returncode, result.stderr) == (0, b''), result
    if schema is not None: assert result.stdout == schema.read_bytes(), result
    else: assert hashlib.sha256(result.stdout).hexdigest() == expected[7:], 'schema bytes changed'
    json.loads(result.stdout)
    schema_bytes = result.stdout
    result = run(['--lmcode-signatures'])
    assert result.returncode == 0 and result.stdout and not result.stderr, result
    # Azure advertises host-only signatures; compare exact compiler inspection.
    source = scratch / 'host.schema.json'
    source.write_bytes(schema_bytes)
    compiler = os.environ.get('LMSCRIPT_BIN') or shutil.which('lmscript')
    assert compiler, 'source compiler required for exact signature parity'
    inspected = subprocess.run([compiler, '--lmcode-signatures', str(source), '--without-std'], capture_output=True)
    assert inspected.returncode == 0 and not inspected.stderr and inspected.stdout == result.stdout, ('signature bytes differ', inspected, result)
    # The selector must recognize execution, yielding a loader error rather than usage.
    result = run(['--lmcode', missing])
    assert result.returncode == 3, ('new execution option did not reach the loader', result)
    assert not missing.exists()
print('PASS hard flag cutover: exact schema/signatures, new loader route, retired options refused before file effects')
