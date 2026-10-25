#!/usr/bin/env python3
import re
from pathlib import Path
import subprocess

root=Path(__file__).resolve().parents[1]
version=(root/'VERSION').read_text().strip()
assert re.fullmatch(r'\d+\.\d+\.\d+',version)
for file,pattern in [('src/LmAzure/Program.cs',r'const string Version = "([^"]+)"'),('src/LmAzure/LmAzure.csproj',r'<Version>([^<]+)</Version>'),('THIRD_PARTY_NOTICES',r'lmazure ([0-9.]+)')]:
    match=re.search(pattern,(root/file).read_text());assert match and match[1]==version,file
tag=subprocess.run(['git','describe','--exact-match','--tags','HEAD'],cwd=root,capture_output=True,text=True,timeout=15)
assert tag.returncode!=0 or tag.stdout.strip()=='v'+version,'tag and source version disagree'
print('Version metadata agrees: lmazure '+version)
