#!/usr/bin/env bash
set -euo pipefail
version="${1:?version required}"
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo 'invalid version' >&2; exit 2; }
root="$(cd "$(dirname "$0")/.." && pwd)"
python3 - "$root" "$version" <<'PY'
from pathlib import Path
import re,sys
root=Path(sys.argv[1]); version=sys.argv[2]
replacements={
 'src/LmAzure/Program.cs': (r'public const string Version = "[0-9]+\.[0-9]+\.[0-9]+";', f'public const string Version = "{version}";'),
 'src/LmAzure/LmAzure.csproj': (r'(<Version>)[^<]+(</Version>)', rf'\g<1>{version}\g<2>'),
 'THIRD_PARTY_NOTICES': (r'(?m)^(lmazure )\d+\.\d+\.\d+( — third-party notices)$', rf'\g<1>{version}\g<2>'),
}
for relative,(pattern,replacement) in replacements.items():
 p=root/relative; source=p.read_text(); updated,count=re.subn(pattern,replacement,source,count=1)
 if count != 1: raise SystemExit(f'expected one version marker in {relative}')
 p.write_text(updated)
PY
printf '%s\n' "$version" > "$root/VERSION"
