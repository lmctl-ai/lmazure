#!/usr/bin/env python3
"""Regression checks for live admission and source equivalence; never authenticates."""
import importlib.util,os,sys,tempfile,json
from pathlib import Path
from unittest.mock import patch
root=Path(__file__).resolve().parents[1];sys.path.insert(0,str(root/'compare'))
spec=importlib.util.spec_from_file_location('harness',root/'compare/run.py');h=importlib.util.module_from_spec(spec);spec.loader.exec_module(h)
checks=0
with patch.dict(os.environ,{},clear=True):
 try:h.live_environment({'ledger_mode':'owned'});raise AssertionError('live enabled without approval')
 except h.ComparisonFailure:checks+=1
with patch.dict(os.environ,{'CI':'true','LMAZURE_COMPARE_LIVE_APPROVED':'1','LMAZURE_LIVE':'1'},clear=True):
 try:h.live_environment({'ledger_mode':'readonly'});raise AssertionError('CI live enabled')
 except h.ComparisonFailure:checks+=1
for entry in h.CATALOG:
 name=entry['name'];original=(root/'examples'/(name+'.lms')).read_text();live=h.native_source(name,True)
 assert 'mock()' not in live and 'live()' in live
 import re
 assert re.findall(r'run\.(?:queue|container)\("[^"]+"\)',original)==re.findall(r'run\.(?:queue|container)\("[^"]+"\)',live)
 assert 'install="github:lmctl-ai/lmazure@latest"' not in h.native_source(name)
 checks+=1
print(f'comparison harness admission/source checks: {checks} passed; no Azure calls')
