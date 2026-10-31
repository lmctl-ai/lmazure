#!/usr/bin/env python3
"""Compile and execute host contracts using only mock(), including cleanup errors."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

root=Path(__file__).resolve().parents[1]
binary=str(Path(sys.argv[1]).resolve())
compiler=os.environ.get('LMAZURE_LMSCRIPT') or shutil.which('lmscript')
assert compiler,'lmscript compiler required'
env={**os.environ,'LMAZURE_LIVE':'0','LMAZURE_ALLOW_WRITES':'0','PATH':str(Path(binary).parent)+os.pathsep+os.environ['PATH']}
checks=0
with tempfile.TemporaryDirectory(prefix='lmazure-dsl-') as directory:
    d=Path(directory)
    def compile_case(name,body):
        path=d/(name+'.lms')
        path.write_text('p = _dsl_ cli="lmazure";\n'+body+'\nend _dsl_\nsavedsl(p,"'+name+'.dslb");\n')
        result=subprocess.run([compiler,str(path)],cwd=d,env=env,capture_output=True,text=True,timeout=90)
        assert result.returncode==0,(name,result.stdout,result.stderr)
        return d/(name+'.dslb')
    def execute(path,code=0,memory=None,dry=False):
        global checks
        args=[binary,'--lmcode']+(['--dry-run'] if dry else [])+[str(path)]
        if sys.platform=='linux' and os.environ.get('LMAZURE_TEST_ADDRESS_SPACE_LIMIT')=='1':
            args=['bash','-c','ulimit -v 6291456; exec "$@"','_',*args]
        p=subprocess.run(args,cwd=d,env={**env,**({'LMAZURE_MAX_MEMORY_MB':memory} if memory else {})},capture_output=True,text=True,timeout=90)
        assert p.returncode==code,(path.name,p.returncode,p.stdout,p.stderr)
        report=json.loads(p.stdout)
        assert report['ok']==(code==0),report
        checks+=1
        return p
    p=compile_case('inventory','let s=mock(); let t=s.inventory("resources"); t.csv("inventory.csv"); t.json("inventory.json");')
    execute(p); first=(d/'inventory.csv').read_bytes();execute(p);assert first==(d/'inventory.csv').read_bytes()
    assert len(json.loads((d/'inventory.json').read_text()))>0
    p=compile_case('owned','let s=mock(); let r=s.begin_run("owned.ledger.json"); let q=r.queue("demo"); let n=q.text(0,0); r.tag(n,"purpose","demo"); r.delete(n); r.cleanup();')
    execute(p);assert not (d/'owned.ledger.json').exists()
    p=compile_case('cost_report','let s=mock(); let r=s.begin_run("cost.ledger.json"); r.cost_cap(0.05); let q=r.queue("cost"); r.tag(q.text(0,0),"purpose","cost"); r.cleanup(); r.cost_report("cost.json");')
    execute(p)
    cost=json.loads((d/'cost.json').read_text())
    assert cost['mode']=='fake' and cost['estimated_cost_usd']==0.0005 and cost['reserved_cost_usd']==0
    assert len(cost['requests'])==5 and cost['resources'][0]['owned'] is False
    assert cost['estimated_cost_text']=='estimated cost: $0.0005'
    p=compile_case('cost_denied','let s=mock(); let r=s.begin_run("cap.ledger.json"); r.cost_cap(0.0002); let q=r.queue("blocked");')
    denied=execute(p,3);assert 'cost' in denied.stdout and not (d/'cap.ledger.json').exists()
    p=compile_case('cost_hard_cap','let s=mock(); let r=s.begin_run("hardcap.ledger.json"); r.cost_cap(0.26);')
    execute(p,3);assert not (d/'hardcap.ledger.json').exists()
    p=compile_case('container','let s=mock(); let r=s.begin_run("container.ledger.json"); let c=r.container("demo");')
    execute(p,dry=True);assert not (d/'container.ledger.json').exists()
    execute(p);assert not (d/'container.ledger.json').exists()
    p=compile_case('failure','let s=mock(); let r=s.begin_run("failure.ledger.json"); let c=r.queue("cleanup"); let x=c.number(0,0);')
    execute(p,3);assert not (d/'failure.ledger.json').exists()
    p=compile_case('foreign','let s=mock(); let r=s.begin_run("foreign.ledger.json"); r.delete("other-users-resource");')
    execute(p,3);assert not (d/'foreign.ledger.json').exists()
    p=compile_case('bounds','let s=mock(); let t=s.inventory("resources"); let x=t.text(-1,0);')
    execute(p,3)
    p=compile_case('live_denied','let s=live();')
    execute(p,3)
    p=compile_case('runaway','let text="abcdef"; while (true) { text=text+"allocation-padding-0123456789"; }')
    result=execute(p,3,'16');assert 'LMAZURE_MAX_MEMORY_MB' in result.stdout+result.stderr
    if sys.platform=='linux' and os.environ.get('LMAZURE_TEST_DEFAULT_BUDGET')=='1':
        result=execute(p,3);assert 'LMAZURE_MAX_MEMORY_MB' in result.stdout+result.stderr
print(f'lmazure DSL checks: {checks} passed; every provider operation used fake transport')
