#!/usr/bin/env python3
"""Static generation/safety contracts; never initialize a cloud provider."""
import json,re,sys
from pathlib import Path
ROOT=Path(__file__).resolve().parents[1];sys.path.insert(0,str(ROOT/'compare'))
from provider_specs import CATALOG,owned_operations,kind_for
checks=0
for name,entry in CATALOG.items():
 d=ROOT/'compare'/name
 for file in ('main.tf','offline.tf','main.lms','cleanup.lms','spec.json'):assert (d/file).is_file(),(name,file)
 spec=json.loads((d/'spec.json').read_text());assert spec['inventory_kind']==kind_for(name)
 assert spec['intended_resources']==owned_operations(name)
 assert len(spec['expected_resources'])==(len(owned_operations(name)) if entry['ledger_mode']=='owned' else 0)
 tf=(d/'main.tf').read_text();lms=(d/'main.lms').read_text()
 assert 'use_oidc' in tf and re.search(r'use_oidc\s*=\s*false',tf)
 assert 'storage_use_azuread' in tf and 'resource_provider_registrations' in tf
 assert set(re.findall(r'resource\s+"([^"]+)"',tf))<={'terraform_data','azurerm_storage_queue','azurerm_storage_container'}
 assert 'resource "azurerm_storage_account"' not in tf and 'resource "azurerm_resource_group"' not in tf
 assert all('lmazure-${var.run_id}-'+r['suffix'] in tf for r in spec['expected_resources'])
 assert json.dumps(tf) in lms,'inline AzureRM must equal checked-in HCL'
 assert 'LMAZURE_COMPARE_MODE' in lms and 'LMAZURE_COMPARE_LIVE_APPROVED' in lms and 'LMAZURE_COMPARE_AUTH_FILE' in lms
 assert 'tofu.destroy(' in lms and 'destroy.ok' in lms and 'apply.ok' in lms
 assert '_lmcode_' in lms and 'github:lmctl-ai/lmcloud@v0.1.9' in lms
 cleanup=(d/'cleanup.lms').read_text()
 assert 'r = run_lmcode(c);' in cleanup and 'assert(r.ok,' in cleanup
 assert 'savedsl(' not in cleanup and 'dsl_cli(' not in cleanup
 assert 'LMAZURE_COMPARE_LIVE_APPROVED' in cleanup and 'LMAZURE_COMPARE_AUTH_FILE' in cleanup
 checks+=1
print(f'comparison configuration contracts: {checks} examples passed; no provider initialized')
