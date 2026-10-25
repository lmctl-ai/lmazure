#!/usr/bin/env python3
"""Fetch pinned SDK sources and apply the host's allocation checkpoint seam."""
import hashlib
import io
import json
from pathlib import Path, PurePosixPath
import sys
import tarfile
import urllib.request

root = Path(__file__).resolve().parents[1]
lock = json.loads((root / 'sdk.lock').read_text())
target = root / 'third_party/lmcode-csharp'
manifest = target / 'manifest.json'
if '--verify' in sys.argv or '--ensure' in sys.argv and manifest.exists():
    expected = json.loads(manifest.read_text())
    assert expected['source'] == lock, 'SDK source pin changed; rerun sync-sdk.py'
    actual={p.relative_to(target).as_posix() for p in target.rglob('*.cs')}|{'LICENSE'}
    assert actual==set(expected['files']), 'SDK source file list changed'
    for name,digest in expected['files'].items():
        assert hashlib.sha256((target/name).read_bytes()).hexdigest() == digest, 'SDK source drift: '+name
    print('SDK source and host checkpoint patch verified')
    raise SystemExit(0)
cached = root / 'build/sdk-source.tar.gz'
if cached.exists(): data=cached.read_bytes()
else: data=urllib.request.urlopen(f"https://codeload.github.com/{lock['repo']}/tar.gz/{lock['commit']}",timeout=60).read()
assert hashlib.sha256(data).hexdigest() == lock['sha256'], 'SDK archive checksum mismatch'
files = {}
with tarfile.open(fileobj=io.BytesIO(data),mode='r:gz') as archive:
    for member in archive.getmembers():
        if not member.isfile(): continue
        path = PurePosixPath(*PurePosixPath(member.name).parts[1:])
        name = path.as_posix()
        if '..' in path.parts or not (name == 'LICENSE' or name.startswith('src/LmCode/') and name.endswith('.cs')): continue
        source = archive.extractfile(member).read()
        if name == 'src/LmCode/Execution.cs':
            old = b'        while (!reader.Done)\n        {'
            new = b'        int hostCheckpoints = 0;\n        while (!reader.Done)\n        {\n            if ((++hostCheckpoints & 1023) == 0) global::LmAzure.RuntimeBudget.Check();'
            assert source.count(old) == 1, 'SDK checkpoint patch needs review'
            source = source.replace(old,new)
        destination = target / name
        destination.parent.mkdir(parents=True,exist_ok=True)
        destination.write_bytes(source)
        files[name] = hashlib.sha256(source).hexdigest()
manifest.write_text(json.dumps({'source':lock,'patch':'allocation checkpoint every 1024 VM instructions','files':files},indent=2)+'\n')
print('Pinned SDK sources synchronized:',len(files),'files')
