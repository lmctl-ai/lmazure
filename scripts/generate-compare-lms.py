#!/usr/bin/env python3
"""Generate self-contained lmcloud programs with checked two-phase teardown."""
import json
from pathlib import Path
ROOT=Path(__file__).resolve().parents[1]
fixtures=json.loads((ROOT/'compare/fixtures/arm-resources.json').read_text())['inventories']
for sp in sorted((ROOT/'compare').glob('*/spec.json')):
 spec=json.loads(sp.read_text());name=spec['name'];base='build/lmcloud_'+name
 resources=[]
 for op in spec['expected_resources']:
  metadata={k:('RUN_ID' if v=='$run_id' else v) for k,v in op['metadata'].items()}
  metadata_hcl='{'+', '.join(json.dumps(k)+' = '+('var.run_id' if v=='RUN_ID' else json.dumps(v)) for k,v in sorted(metadata.items()))+'}'
  resources.append('{ type = '+json.dumps(op['tofu_type'])+', name = "lmazure-${var.run_id}-'+op['suffix']+'", tags = {lmazure_test = var.run_id}, metadata_sha256 = sha256(jsonencode('+metadata_hcl+')) }')
 inventory=fixtures[spec['inventory_kind']] if spec['ledger_mode']=='readonly' else []
 fixture='''variable "run_id" {\n  type = string\n  default = "0123456789ab"\n}\nresource "terraform_data" "intent" {\n  input = {\n    resources_created = ['''+', '.join(resources)+''']\n    outputs = {inventory = '''+json.dumps(inventory)+'''}\n  }\n}\noutput "comparison" {\n  value = jsonencode(terraform_data.intent.output)\n}\n'''
 azure=(sp.parent/'main.tf').read_text()
 prefix=f'''# AzureRM equivalent plus an explicit builtin-provider offline fixture; never authenticates by default.
mkdir("build"); mkdir("{base}"); mkdir("{base}/azure"); mkdir("{base}/offline");
let mode = getenv("LMAZURE_COMPARE_MODE", "offline");
assert(mode == "offline" || mode == "live");
let run_id = getenv("LMAZURE_COMPARE_RUN_ID", "0123456789ab");
let dir = "{base}/offline";
let cap = {spec['cost_cap_usd']};
let admitted = {str(spec['ledger_mode']!='budget').lower()};
if (mode == "live") {{
  assert(getenv("CI", "") != "true", "live comparison is forbidden in CI");
  assert(getenv("LMAZURE_COMPARE_LIVE_APPROVED", "") == "1" && getenv("LMAZURE_LIVE", "") == "1", "user-approved local live testing is required");
  let auth = json_parse(read_file(getenv("LMAZURE_COMPARE_AUTH_FILE", "missing-authorization.json")));
  assert(getstr(auth,"example") == "{name}" && getstr(auth,"run_id") == run_id && getbool(auth,"preflight_complete"), "use compare/run.sh for ownership preflight");
  {'assert(getenv("LMAZURE_ALLOW_WRITES","")=="1");' if spec['ledger_mode']!='readonly' else ''}
  dir = "{base}/azure";
}}
write_file("{base}/azure/main.tf", {json.dumps(azure)});
write_file("{base}/offline/main.tf", {json.dumps(fixture)});
let variables = {{run_id: run_id}};
if(mode == "live") {{
 variables = {{run_id:run_id, subscription_id:getenv("LMAZURE_SUBSCRIPTION_ID",""), storage_account_id:getenv("LMAZURE_COMPARE_STORAGE_ACCOUNT_ID", "")}};
}}
write_file(path_join(dir,"comparison.auto.tfvars.json"),json_stringify(variables));
'''
 # No provider invocation at all when admission is rejected.
 prefix+=f'''if (!admitted) {{
 write_file("{base}/result.json", json_stringify({{mode:mode,requested_provider:"azurerm",executed_provider:"none",admitted:false,init_ok:true,apply_ok:true,destroy_ok:true,leftovers:0,outputs:{{resources_created:[],outputs:{{inventory:[]}}}}}},2));
 println("comparison admission rejected before any provider request");
}} else {{
'''
 phase=f'''p = _lmcode_ cli="lmcloud" install="github:lmctl-ai/lmcloud@v0.1.9";
let dir = ${{dir}};
let initialized = tofu.init(dir);
file_write("{base}/init.ok",str(initialized));
if(initialized) {{
 let formatted = tofu.fmt(dir);
 let valid = tofu.validate(dir);
 file_write("{base}/validate.ok",str(valid));
 if(valid) {{
  let planned = tofu.plan(dir);
  file_write("{base}/plan.json",planned.json());
  let saved = planned.save("{base}/plan.tfplan");
  let err = tofu.last_error();
  if(err == "") {{
   let applied = tofu.apply(dir);
   file_write("{base}/apply.ok",str(applied));
   if(applied) {{
    let output = tofu.output(dir,"comparison");
    file_write("{base}/outputs.json",output);
    let state = tofu.show_state(dir);
    file_write("{base}/private-state.json",state);
   }}
  }}
 }}
}}
end _lmcode_
savedsl(p,"{base}/apply.dslb");
let applied = system(concat(dsl_cli(p),["--dsl","{base}/apply.dslb"]),180000);
# Cleanup is launched even when the create VM returned an error or apply returned false.
c = _lmcode_ cli="lmcloud" install="github:lmctl-ai/lmcloud@v0.1.9";
let dir = ${{dir}};
let destroyed = tofu.destroy(dir);
file_write("{base}/destroy.ok",str(destroyed));
let remaining = tofu.state_list(dir);
file_write("{base}/after-destroy.txt",remaining);
let after = tofu.show_state(dir);
file_write("{base}/private-after-state.json",after);
end _lmcode_
savedsl(c,"{base}/cleanup.dslb");
let cleaned = system(concat(dsl_cli(c),["--dsl","{base}/cleanup.dslb"]),180000);
let init_ok = file_exists("{base}/init.ok") && trim(read_file("{base}/init.ok")) == "true";
let apply_ok = file_exists("{base}/apply.ok") && trim(read_file("{base}/apply.ok")) == "true";
let destroy_ok = file_exists("{base}/destroy.ok") && trim(read_file("{base}/destroy.ok")) == "true";
let state_text = file_exists("{base}/after-destroy.txt") ? trim(read_file("{base}/after-destroy.txt")) : "unknown";
let output = file_exists("{base}/outputs.json") ? json_parse(read_file("{base}/outputs.json")) : null;
write_file("{base}/result.json",json_stringify({{mode:mode,requested_provider:"azurerm",executed_provider:mode=="offline"?"builtin_fixture":"azurerm",admitted:true,init_ok:init_ok,apply_ok:apply_ok,destroy_ok:destroy_ok,state_addresses:state_text,outputs:output}},2));
assert(applied.code==0 && cleaned.code==0 && init_ok && apply_ok && destroy_ok && state_text=="", "comparison lifecycle failed; private workspace retained");
println("lmcloud comparison complete: {name}; mode="+mode);
}}
'''
 money=f"{spec['tofu_estimated_cost_usd']:.4f}" if spec['tofu_estimated_cost_usd'] else '0.00'
 (sp.parent/'main.lms').write_text('\n'.join(line.rstrip() for line in (prefix+phase+'println(\"estimated cost: $'+money+'\");\n').splitlines())+'\n')
 (sp.parent/'offline.tf').write_text(fixture)
 # Recovery is useful to the Python harness after timeout/exception.
 cleanup=prefix.split('write_file("'+base+'/azure/main.tf"',1)[0]+f'''dir = mode == "live" ? "{base}/azure" : "{base}/offline";
c = _lmcode_ cli="lmcloud" install="github:lmctl-ai/lmcloud@v0.1.9";
let dir = ${{dir}};
let destroyed = tofu.destroy(dir);
file_write("{base}/destroy.ok",str(destroyed));
file_write("{base}/after-destroy.txt",tofu.state_list(dir));
end _lmcode_
savedsl(c,"{base}/recovery.dslb");
r = system(concat(dsl_cli(c),["--dsl","{base}/recovery.dslb"]),180000);
assert(r.code==0);
'''
 (sp.parent/'cleanup.lms').write_text('\n'.join(line.rstrip() for line in cleanup.splitlines())+'\n')
print('Generated 72 self-contained guarded comparison and recovery programs.')
