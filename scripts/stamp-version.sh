#!/usr/bin/env bash
set -euo pipefail
version="${1:?version required}"
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo 'invalid version' >&2; exit 2; }
root="$(cd "$(dirname "$0")/.." && pwd)"
python3 - "$root/src/LmAzure/Program.cs" "$version" <<'PY'
from pathlib import Path
import re,sys
p=Path(sys.argv[1]); s=p.read_text()
s,n=re.subn(r'public const string Version = "[0-9]+\.[0-9]+\.[0-9]+";',f'public const string Version = "{sys.argv[2]}";',s,count=1)
if n != 1: raise SystemExit('expected one Program.Version constant')
p.write_text(s)
PY
printf '%s\n' "$version" > "$root/VERSION"
