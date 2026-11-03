#!/usr/bin/env python3
import shutil, subprocess, tempfile
from pathlib import Path
root=Path(__file__).resolve().parents[1]
with tempfile.TemporaryDirectory() as d:
 repo=Path(d)/'repo'; shutil.copytree(root,repo,ignore=shutil.ignore_patterns('.git','artifacts','build'))
 major,minor,patch=map(int,(repo/'VERSION').read_text().strip().split('.'))
 calc=repo/'scripts/derive-release-version.sh'
 assert subprocess.check_output([calc,'1'],text=True).strip()==f'{major}.{minor}.{patch+1}'
 assert subprocess.check_output([calc,'31'],text=True).strip()==f'{major}.{minor}.{patch+31}'
 version=f'{major}.{minor}.{patch+31}'
 subprocess.check_call([repo/'scripts/stamp-version.sh',version])
 assert (repo/'VERSION').read_text().strip()==version
 assert f'public const string Version = "{version}";' in (repo/'src/LmAzure/Program.cs').read_text()
 assert subprocess.check_output([calc,'--diagnostic'],text=True).strip()==version
 print('PASS lmazure run-derived version and stamp')
