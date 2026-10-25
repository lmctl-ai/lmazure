#!/usr/bin/env python3
"""CLI behavior and fake SDK tests, with live access unconditionally disabled."""
import csv
import io
import json
import os
import re
from pathlib import Path
import subprocess
import sys
import tempfile

root=Path(__file__).resolve().parents[1]
command=[str(Path(sys.argv[1]).resolve())] if len(sys.argv)>1 else ['dotnet',str(root/'src/LmAzure/bin/Release/net10.0/lmazure.dll')]
environment={**os.environ,'LMAZURE_LIVE':'0','LMAZURE_ALLOW_WRITES':'0'}
checks=0
def run(*args,code=0,env=None):
    global checks
    p=subprocess.run(command+list(args),env={**environment,**(env or {})},capture_output=True,text=True,encoding='utf-8',timeout=90)
    assert p.returncode==code,(args[:1],p.returncode,p.stdout,p.stderr)
    checks+=1
    return p

assert run('--version').stdout.strip()=='lmazure '+(root/'VERSION').read_text().strip()
schema=json.loads(run('--dsl-schema').stdout)
assert schema['name']=='lmazure' and schema['dsl.schema']==2
signatures=run('--dsl-signatures').stdout.splitlines()
assert len(signatures)==20 and len(set(signatures))==20
assert 'DefaultAzureCredential' in run('--help').stdout
assert 'LMAZURE_MAX_MEMORY_MB' in run('--help').stdout
assert 'Azure.Core' in run('--notices').stdout
summary=run('--self-test').stdout
match=re.search(r'fake-transport checks: (\d+) passed',summary)
assert match and int(match[1])>=62,summary
for kind in ['resources','compute','storage','network','identity','database','queues','serverless','monitoring','dns','vaults','containers']:
    rows=list(csv.reader(io.StringIO(run('--inventory',kind).stdout)))
    assert len(rows)>1 and rows[0][:2]==['name','kind'],kind
    assert '00000000-0000-0000-0000-000000000000' not in str(rows)
run('--inventory','secret-values',code=3)
run('--live','--inventory','resources',code=3)
run('--unknown',code=2)
for value in ['0','-1','1.5','lots','99999999999999999999999','']:
    run('--version',code=2,env={'LMAZURE_MAX_MEMORY_MB':value})
assert 'LMAZURE_MAX_MEMORY_MB' in run('--version',code=3,env={'LMAZURE_MAX_MEMORY_MB':'1'}).stderr
with tempfile.TemporaryDirectory(prefix='lmazure-invalid-') as d:
    path=Path(d)/'corrupt.dslb';path.write_bytes(b'not-bytecode-with-private-secret')
    p=run('--dsl',str(path),code=3)
    assert 'private-secret' not in p.stdout+p.stderr
    assert json.loads(p.stdout)['phase']=='load'
    run('--dsl',str(Path(d)/'missing.dslb'),code=3)
print(f'lmazure CLI checks: {checks} passed; live access disabled')
