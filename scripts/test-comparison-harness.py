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

# Exercise the actual report producer and consumer together, with provider execution
# replaced by local artifacts. Approval validation still runs; no credential loads.
from argparse import Namespace
import contextlib,io
with tempfile.TemporaryDirectory() as directory:
 fake=Path(directory);name='resource_inventory'
 (fake/'compare'/name).mkdir(parents=True)
 (fake/'compare'/name/'spec.json').write_text(json.dumps({'ledger_mode':'readonly','semantic_differences':[],'tofu_estimated_cost_usd':0}))
 (fake/'compare'/name/'main.lms').write_text('fake transport fixture')
 def fake_execute(argv,cwd,env,log,limit=240):
  if Path(argv[-1]).name=='main.lms':
   base=Path(cwd)/'build'/('lmcloud_'+name);base.mkdir(parents=True)
   (base/'result.json').write_text(json.dumps({'admitted':True,'destroy_ok':True,'state_addresses':'','executed_provider':'azurerm','outputs':{'resources_created':[],'outputs':{'inventory':[]}}}))
   (base/'plan.json').write_text(json.dumps({'resource_changes':[]}))
  return 0,1.0
 args=Namespace(output=str(fake/'runs'),live=True,native_binary=sys.executable,lmcloud_binary=sys.executable,compiler=sys.executable,_tools_dir=str(fake/'tools'))
 environment={'PATH':os.environ.get('PATH',os.defpath),'LMAZURE_COMPARE_LIVE_APPROVED':'1','LMAZURE_LIVE':'1','LMAZURE_SUBSCRIPTION_ID':'11111111-1111-1111-1111-111111111111'}
 with patch.object(h,'ROOT',fake),patch.object(h,'native_source',return_value='fixture'),patch.object(h,'execute',side_effect=fake_execute),patch.object(h,'native_facts',return_value=({'estimated_cost_usd':0},[],{'inventory':[]},0)),patch.dict(os.environ,environment,clear=True),contextlib.redirect_stdout(io.StringIO()) as output:
  result=h.run_one(name,args)
 produced=json.loads((fake/'runs'/name/'report.json').read_text())
 assert produced.get('user_approved_actual') is True,'runner must emit the engine approval field'
 assert 'user_approved' not in produced,'one canonical approval field'
 assert result['ok'] and result['live']['status']=='same','approved fake-produced actual-provider report must reach live acceptance'
 assert 'same live comparison' in output.getvalue(),'live runs must not be called offline fixtures'
print('comparison report integration: passed; provider execution replaced by local fake artifacts')
