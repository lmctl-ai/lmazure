#!/usr/bin/env python3
"""Generate reviewed AzureRM configs, never initialize or contact a provider."""
import json,sys,hashlib
from pathlib import Path
ROOT=Path(__file__).resolve().parents[1];sys.path.insert(0,str(ROOT/'compare'))
from provider_specs import CATALOG,EXAMPLES,kind_for,owned_operations,resource_types_for
PIN='5.10.0'
for name in EXAMPLES:
 entry=CATALOG[name];mode=entry['ledger_mode'];ops=owned_operations(name);kind=kind_for(name)
 assert (mode=='readonly')==(len(ops)==0),name
 expected=ops if mode=='owned' else []
 native=entry['cost_usd'];allowance=round(len(ops)*64*.0001,8) if mode=='owned' else 0
 gate=.0003 if mode=='budget' else allowance
 differences=[]
 if mode=='readonly':
  differences=['Offline runs use a builtin-provider fixture plan, not an AzureRM plan.','AzureRM exposes common metadata but not SDK-created age, synthetic access/encryption flags, or function Kind.','Live AzureRM queries the explicitly curated type set; native all-resource inventory can include additional provider types.']
 else:differences=['Native SDK performs imperative create/tag/delete/sweep phases; AzureRM converges to desired metadata and destroys state-owned resources.','Native estimates count SDK transport attempts; AzureRM estimate reserves 64 provider requests per intended resource and is not a bill.']
 if kind=='functions':differences.append('Function-app Kind filtering is unsupported by azurerm_resources; live output can include other Web sites.')
 spec={'name':name,'ledger_mode':mode,'inventory_kind':kind,'resource_types':resource_types_for(kind) if mode=='readonly' else [],'expected_resources':expected,'intended_resources':ops,'semantic_differences':differences,'native_expected_cost_usd':native,'tofu_estimated_cost_usd':allowance,'admission_cost_usd':gate,'cost_cap_usd':.0002 if mode=='budget' else .05,'source_sha256':hashlib.sha256((ROOT/'examples'/(name+'.lms')).read_bytes()).hexdigest()}
 variables='''variable "subscription_id" {\n  type = string\n  sensitive = true\n}\nvariable "storage_account_id" {\n  type = string\n  default = ""\n  sensitive = true\n}\nvariable "run_id" {\n  type = string\n  default = "0123456789ab"\n  validation {\n    condition = can(regex("^[0-9a-f]{12}$", var.run_id))\n    error_message = "run_id must be twelve lowercase hexadecimal characters"\n  }\n}\n'''
 cap=spec['cost_cap_usd'];tf=f'''terraform {{
  required_providers {{
    azurerm = {{ source = "hashicorp/azurerm", version = "={PIN}" }}
  }}
}}
provider "azurerm" {{
  features {{}}
  subscription_id = var.subscription_id
  resource_provider_registrations = "none"
  use_oidc = false
  use_cli = true
  use_msi = false
  storage_use_azuread = true
}}
'''+variables+f'''variable "cost_cap_usd" {{
  type = number
  default = {cap}
  validation {{
    condition = var.cost_cap_usd >= 0 && var.cost_cap_usd <= 0.25
    error_message = "example cap must be between zero and the 0.25 USD hard ceiling"
  }}
}}
resource "terraform_data" "admission" {{
  input = {gate}
  lifecycle {{
    precondition {{
      condition = var.cost_cap_usd >= {gate}
      error_message = "estimated provider allowance exceeds the example cap"
    }}
  }}
}}
'''
 facts=[]
 for op in expected:
  addr=op['tofu_type']+'.'+op['logical_name'];metadata=', '.join(k+' = '+('var.run_id' if v=='$run_id' else json.dumps(v)) for k,v in sorted(op['metadata'].items()))
  tf+=f'''resource "{op['tofu_type']}" "{op['logical_name']}" {{
  name = "lmazure-${{var.run_id}}-{op['suffix']}"
  storage_account_id = var.storage_account_id
  metadata = {{ {metadata} }}
  depends_on = [terraform_data.admission]
  timeouts {{
    create = "1m"
    read = "1m"
    update = "1m"
    delete = "1m"
  }}
}}
'''
  facts.append(f'{{ type = "{op["tofu_type"]}", name = {addr}.name, tags = {{ lmazure_test = var.run_id }}, metadata_sha256 = sha256(jsonencode({addr}.metadata)) }}')
 if mode=='readonly':
  tf+='variable "resource_types" {\n  type = set(string)\n  default = '+json.dumps(spec['resource_types'])+'\n}\ndata "azurerm_resources" "inventory" {\n  for_each = var.resource_types\n  type = each.value\n}\n'
  inventory='flatten([for collection in data.azurerm_resources.inventory : [for r in collection.resources : { name = r.name, type = r.type, location = r.location, tagged = contains(keys(r.tags), "lmazure_test") }]])'
 else:inventory='[]'
 tf+='output "comparison" {\n  value = jsonencode({ resources_created = ['+', '.join(facts)+'], outputs = { inventory = '+inventory+' } })\n}\n'
 d=ROOT/'compare'/name;d.mkdir(parents=True,exist_ok=True);(d/'main.tf').write_text(tf);(d/'spec.json').write_text(json.dumps(spec,indent=2)+'\n')
print('Generated 72 source-derived AzureRM equivalents; no provider initialized.')
