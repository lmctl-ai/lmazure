#!/usr/bin/env python3
"""Run real native/fake SDK and lmcloud fixture comparisons; live is explicit user-only."""
import argparse,csv,hashlib,io,json,os,re,secrets,shutil,subprocess,sys,tempfile,time,uuid
from pathlib import Path
ROOT=Path(__file__).resolve().parents[1];sys.path.insert(0,str(ROOT/'compare'))
from compare_results import compare,write_results
CATALOG=json.loads((ROOT/'examples/catalog.json').read_text());BY_NAME={e['name']:e for e in CATALOG}
class ComparisonFailure(RuntimeError): pass

def digest_account(value):return hashlib.sha256(value.lower().encode()).hexdigest()
def execute(argv,cwd,env,log,limit=240):
 start=time.perf_counter()
 with open(log,'w',encoding='utf-8') as output:
  result=subprocess.run(argv,cwd=cwd,env=env,stdout=output,stderr=subprocess.STDOUT,timeout=limit)
 return result.returncode,time.perf_counter()-start

def selected_binary(value,name):
 path=value or shutil.which(name)
 if not path:
  base=Path(os.environ.get('LMSCRIPT_TOOL_CACHE',str(Path(os.environ.get('XDG_CACHE_HOME',str(Path.home()/'.cache')))/'lmscript/tools')))
  tag='latest' if name=='lmazure' else 'v0.1.9'
  cached=base/'github/lmctl-ai'/name/tag/(name+('.exe' if os.name=='nt' else ''))
  if cached.is_file():path=str(cached)
 if not path:raise ComparisonFailure(f'{name} is required; set its comparison binary option or install it')
 return str(Path(path).resolve())
def compiler_command(compiler,source):return [compiler,str(source)]
def native_source(name,live=False):
 source=(ROOT/'examples'/(name+'.lms')).read_text()
 # PATH is authoritative for reproducible local/source builds.
 source=source.replace(' install="github:lmctl-ai/lmazure@latest"','')
 if not live:return source
 match=re.search(r'(?:let\s+)?(\w+)\s*=\s*_dsl_[^;]*;\n(.*?)^end _dsl_',source,re.M|re.S)
 if not match:raise ComparisonFailure('native provider block not found')
 body=match[2].replace('mock()','live()')
 expected=3 if BY_NAME[name]['ledger_mode']=='budget' else 0
 return 'mkdir("build");mkdir("data");mkdir("evidence");\n'+match[1]+' = _dsl_ cli="lmazure";\n'+body+'end _dsl_\nsavedsl('+match[1]+',"build/native-live.dslb");\nr = system ${dsl_cli('+match[1]+')} --dsl build/native-live.dslb;\nassert(r.code == '+str(expected)+');\n'

def common_inventory(name,directory):
 source=(ROOT/'examples'/(name+'.lms')).read_text().split('end _dsl_',1)[0]
 files=re.findall(r'\.csv\("([^"]+)"\)',source)
 if not files:return []
 path=directory/files[0]
 with path.open(newline='',encoding='utf-8') as f:
  return [{'type':r['kind'],'name':r['name'],'location':r['location'],'tagged':r['tagged']=='true'} for r in csv.DictReader(f)]
def native_facts(name,directory):
 entry=BY_NAME[name];cost=json.loads((directory/entry['evidence_file']).read_text())
 records=[]
 for r in cost.get('resources',[]):
  if r.get('created_utc') is None:continue
  records.append({'type':r['kind'],'name':r['resource'],'tags':r.get('tags',{}),'metadata_sha256':r.get('metadata_sha256')})
 leftovers=sum(r.get('owned') is not False for r in cost.get('resources',[]))
 if (directory/'build'/(name+'.ledger.json')).exists():leftovers=max(leftovers,1)
 return cost,records,{'inventory':common_inventory(name,directory) if entry['ledger_mode']=='readonly' else []},leftovers

def live_environment(spec):
 if os.environ.get('CI')=='true' or os.environ.get('LMAZURE_COMPARE_LIVE_APPROVED')!='1' or os.environ.get('LMAZURE_LIVE')!='1':
  raise ComparisonFailure('live comparison is pending user test; explicit local approval flags are required and CI is forbidden')
 subscription=str(uuid.UUID(os.environ.get('LMAZURE_SUBSCRIPTION_ID','')))
 env=dict(os.environ)
 for key in list(env):
  if re.match(r'^(AZURE|ARM)_(CLIENT|TENANT|FEDERATED|ACCESS_TOKEN|PASSWORD|CREDENTIAL)',key):env.pop(key)
 env.update(LMAZURE_SUBSCRIPTION_ID=subscription,AZURE_TOKEN_CREDENTIALS='AzureCliCredential',ARM_USE_OIDC='false',ARM_USE_MSI='false',ARM_USE_CLI='true',ARM_SUBSCRIPTION_ID=subscription)
 account=None
 if spec['ledger_mode']!='readonly':
  if env.get('LMAZURE_ALLOW_WRITES')!='1':raise ComparisonFailure('owned live comparison requires LMAZURE_ALLOW_WRITES=1')
  raw=env.get('LMAZURE_COMPARE_STORAGE_ACCOUNT_ID','')
  m=re.fullmatch(r'/subscriptions/([^/]+)/resourceGroups/([^/]+)/providers/Microsoft.Storage/storageAccounts/([a-z0-9]{3,24})',raw,re.I)
  if not m or m[1].lower()!=subscription.lower():raise ComparisonFailure('existing storage account must be in the selected subscription')
  account=m[3]
  for variable,service in [('LMAZURE_STORAGE_BLOB_URL','blob'),('LMAZURE_STORAGE_QUEUE_URL','queue')]:
   expected='https://'+account+'.'+service+'.core.windows.net'
   if env.get(variable,expected).rstrip('/')!=expected:raise ComparisonFailure('native storage endpoint does not match comparison account')
   env[variable]=expected
 return env,digest_account(subscription),account

def storage_cli(env,account,record,action):
 # Only called by the user-approved live branch; no credentials are passed as arguments.
 kind='container' if record['type'] in ('blob','container','azurerm_storage_container') else 'queue'
 args=['az','storage',kind,action,'--account-name',account,'--name',record['name'],'--auth-mode','login','--only-show-errors','--output','json']
 result=subprocess.run(args,env=env,capture_output=True,text=True,timeout=75)
 if result.returncode:raise ComparisonFailure('owned-resource verification failed; no cleanup success claimed')
 return json.loads(result.stdout or '{}')
def check_or_sweep(env,account,records,run_id,allow_delete=False,preflight_absent=None):
 leftovers=0
 for record in records:
  if not record['name'].startswith('lmazure-'+run_id+'-'):raise ComparisonFailure('unsafe comparison resource name')
  exists=storage_cli(env,account,record,'exists').get('exists')
  if not isinstance(exists,bool):raise ComparisonFailure('unknown resource existence response')
  if exists and allow_delete:
   if not preflight_absent or record['name'] not in preflight_absent:raise ComparisonFailure('cleanup resource lacks the run absence ledger')
   details=storage_cli(env,account,record,'show');metadata=details.get('metadata',{})
   if metadata.get('lmazure_test')!=run_id:raise ComparisonFailure('cleanup ownership metadata changed; resource retained')
   storage_cli(env,account,record,'delete')
   exists=storage_cli(env,account,record,'exists').get('exists')
  leftovers+=int(exists is not False)
 return leftovers

def run_one(name,args):
 entry=BY_NAME[name];spec=json.loads((ROOT/'compare'/name/'spec.json').read_text())
 out=(Path(args.output).resolve() if args.output else ROOT/'compare/runs')/name
 out.mkdir(parents=True,exist_ok=True);os.chmod(out,0o700)
 workspace=Path(tempfile.mkdtemp(prefix='lmazure-compare-',dir=out));os.chmod(workspace,0o700)
 native_dir=workspace/'native';cloud_dir=workspace/'lmcloud';native_dir.mkdir();cloud_dir.mkdir()
 if args.live:env,fingerprint,account=live_environment(spec)
 else:env={**os.environ,'LMAZURE_LIVE':'0','LMAZURE_ALLOW_WRITES':'0'};fingerprint=digest_account('offline-native-mock');account=None
 native_binary=selected_binary(args.native_binary or env.get('LMAZURE_BINARY'),'lmazure')
 cloud_binary=selected_binary(args.lmcloud_binary or env.get('LMCLOUD_BINARY'),'lmcloud')
 compiler=selected_binary(args.compiler or env.get('LMAZURE_COMPARE_COMPILER'),'lmscript')
 # Give the fixed CLI names an isolated PATH without altering either repository.
 tools=Path(args._tools_dir);tools.mkdir(exist_ok=True)
 for name_,binary in [('lmazure',native_binary),('lmcloud',cloud_binary)]:
  target=tools/(name_+('.exe' if os.name=='nt' else ''))
  if not target.exists():
   if os.name=='nt':shutil.copy2(binary,target)
   else:target.symlink_to(binary)
 env['PATH']=str(tools)+os.pathsep+env['PATH']
 env.update(LMAZURE_COMPARE_MODE='live' if args.live else 'offline',LMAZURE_ALLOW_WRITES=env.get('LMAZURE_ALLOW_WRITES','0'))
 program=native_dir/'native.lms';program.write_text(native_source(name,args.live))
 code,native_time=execute(compiler_command(compiler,program),native_dir,env,workspace/'native.log')
 if code:raise ComparisonFailure(f'{name}: native example failed; private workspace retained at {workspace}')
 cost,resources,outputs,leftovers=native_facts(name,native_dir)
 run_id=cost.get('run_id') or secrets.token_hex(6)
 if not re.fullmatch('[0-9a-f]{12}',run_id):raise ComparisonFailure('invalid native ledger run ID')
 if args.live and resources:leftovers=check_or_sweep(env,account,resources,run_id)
 if leftovers:raise ComparisonFailure('native resources remain; lmcloud apply refused')
 cloud_env={**env,'LMAZURE_COMPARE_RUN_ID':run_id}
 auth=workspace/'live-authorization.json';preflight=[]
 if args.live:
  for record in resources:
   if check_or_sweep(env,account,[record],run_id):raise ComparisonFailure('comparison name collision; apply refused')
   preflight.append(record['name'])
  auth.write_text(json.dumps({'example':name,'run_id':run_id,'preflight_complete':True,'account_fingerprint':fingerprint,'verified_absent':preflight}))
  cloud_env['LMAZURE_COMPARE_AUTH_FILE']=str(auth)
 cloud_program=cloud_dir/'main.lms';cloud_program.write_text((ROOT/'compare'/name/'main.lms').read_text())
 cloud_time=0;result=None
 try:
  code,cloud_time=execute(compiler_command(compiler,cloud_program),cloud_dir,cloud_env,workspace/'lmcloud.log',420)
  result_file=cloud_dir/'build'/('lmcloud_'+name)/'result.json'
  if result_file.exists():result=json.loads(result_file.read_text())
  if code or not result:raise ComparisonFailure('lmcloud lifecycle failed; recovery attempted and private workspace retained')
 finally:
  if not result or not result.get('destroy_ok'):
   recovery=cloud_dir/'cleanup.lms';recovery.write_text((ROOT/'compare'/name/'cleanup.lms').read_text())
   try:execute(compiler_command(compiler,recovery),cloud_dir,cloud_env,workspace/'recovery.log',240)
   except Exception:pass
  if args.live and resources:
   remaining=check_or_sweep(env,account,resources,run_id,True,preflight)
   if remaining:raise ComparisonFailure('lmcloud owned leftovers remain after teardown')
 cloud_result=result['outputs'];cloud_created=cloud_result['resources_created']
 base=cloud_dir/'build'/('lmcloud_'+name)
 if result['admitted']:
  plan=json.loads((base/'plan.json').read_text());assert 'resource_changes' in plan
  if not args.live:
   changes=[r for r in plan['resource_changes'] if r.get('type')=='terraform_data' and r.get('name')=='intent']
   if len(changes)!=1:raise ComparisonFailure('missing executed builtin fixture plan')
   planned=changes[0]['change']['after']['input']['resources_created']
   if planned!=cloud_created:raise ComparisonFailure('plan resource intent differs from actual fixture outputs')
  if result.get('state_addresses','unknown')!='':raise ComparisonFailure('lmcloud state leftovers remain')
 cloud_leftovers=0 if result.get('destroy_ok') else None
 report={'example':name,'mode':'live' if args.live else 'offline','user_approved_actual':bool(args.live),'semantic_differences':spec['semantic_differences'],'lmazure':{'mode':'live' if args.live else 'fake','executed_provider':'azure_sdk_live' if args.live else 'azure_sdk_fake','requested_provider':'azurerm','account_fingerprint':fingerprint,'resources_created':resources,'outputs':outputs,'wall_seconds':native_time,'estimated_cost_usd':cost['estimated_cost_usd'],'leftovers':leftovers},'lmcloud':{'mode':'live' if args.live else 'offline','executed_provider':result['executed_provider'],'requested_provider':'azurerm','account_fingerprint':fingerprint,'resources_created':cloud_created,'outputs':cloud_result['outputs'],'wall_seconds':cloud_time,'estimated_cost_usd':spec['tofu_estimated_cost_usd'],'leftovers':cloud_leftovers}}
 compared=compare(report);compared['semantic_differences']=spec['semantic_differences'];report['comparison']=compared
 (out/'report.json').write_text(json.dumps(report,indent=2)+'\n')
 # Public summary excludes state, IDs, logs, variable files and account credentials.
 public=ROOT/'compare/evidence'/(name+'.json');public.parent.mkdir(exist_ok=True)
 public.write_text(json.dumps({'example':name,'mode':report['mode'],'comparison':compared},indent=2)+'\n')
 print(name+': '+(('same live comparison' if args.live else 'same offline fixture intents') if compared['ok'] else 'differs (see report)'))
 return compared

def main():
 parser=argparse.ArgumentParser();parser.add_argument('example');parser.add_argument('--live',action='store_true');parser.add_argument('--offline',action='store_true');parser.add_argument('--native-binary');parser.add_argument('--lmcloud-binary');parser.add_argument('--compiler');parser.add_argument('--output')
 args=parser.parse_args();os.umask(0o077)
 if args.live and args.offline:parser.error('choose offline or live')
 if args.example!='all' and args.example not in BY_NAME:parser.error('unknown example')
 base=Path(args.output).resolve() if args.output else ROOT/'compare/runs';base.mkdir(parents=True,exist_ok=True)
 args._tools_dir=tempfile.mkdtemp(prefix='.tools-',dir=base)
 reports={}
 for name in (list(BY_NAME) if args.example=='all' else [args.example]):reports[name]=run_one(name,args)
 for p in (ROOT/'compare/evidence').glob('*.json'):
  d=json.loads(p.read_text());reports.setdefault(d['example'],d['comparison'])
 write_results(CATALOG,reports,ROOT/'compare/RESULTS.md')
 return 0 if all(r['ok'] for r in reports.values()) else 1
if __name__=='__main__':
 try:raise SystemExit(main())
 except (ComparisonFailure,ValueError,subprocess.TimeoutExpired) as error:
  print('comparison failed: '+str(error),file=sys.stderr);raise SystemExit(1)
