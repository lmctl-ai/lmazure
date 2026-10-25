#!/usr/bin/env python3
"""Rule 14: dynamic system glibc only, with ABI at most 2.35."""
import re
import subprocess
import sys

path=sys.argv[1]
allowed={'libc.so.6','libm.so.6','libpthread.so.0','libdl.so.2','librt.so.1','ld-linux-x86-64.so.2','ld-linux-aarch64.so.1'}
dynamic=subprocess.check_output(['readelf','-d',path],text=True,timeout=20)
needed=set(re.findall(r'\(NEEDED\).*\[(.*?)\]',dynamic))
assert needed and needed<=allowed, 'unexpected dynamic libraries: '+str(sorted(needed-allowed))
symbols=subprocess.check_output(['readelf','--version-info',path],text=True,timeout=20)
versions=[tuple(map(int,m.split('.'))) for m in re.findall(r'GLIBC_(\d+(?:\.\d+)+)',symbols)]
assert versions and max(versions)<=(2,35), 'glibc ABI too new: '+str(max(versions,default=()))
print('Linux linkage: glibc only; maximum ABI '+'.'.join(map(str,max(versions))))
