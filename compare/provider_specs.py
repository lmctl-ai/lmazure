"""Derive comparison intent from the actual native provider blocks and catalog."""
from pathlib import Path
import json,re
ROOT=Path(__file__).resolve().parents[1]
CATALOG={e['name']:e for e in json.loads((ROOT/'examples/catalog.json').read_text())}
EXAMPLES=sorted(CATALOG)
READONLY_KINDS={"compute":"Microsoft.Compute/","storage":"Microsoft.Storage/storageAccounts","network":"Microsoft.Network/","identity":"Microsoft.ManagedIdentity/","database":"Microsoft.Sql/","queues":"Microsoft.Storage/storageAccounts/queueServices","serverless":"Microsoft.Web/","monitoring":"Microsoft.Insights/","dns":"Microsoft.Network/dns","vaults":"Microsoft.KeyVault/","containers":"Microsoft.ContainerService/","functions":"Microsoft.Web/sites","servicebus":"Microsoft.ServiceBus/","nsg":"Microsoft.Network/networkSecurityGroups","public_ips":"Microsoft.Network/publicIPAddresses","routes":"Microsoft.Network/routeTables","logs":"Microsoft.OperationalInsights/","alerts":"Microsoft.Insights/metricAlerts","app_plans":"Microsoft.Web/serverfarms","postgresql":"Microsoft.DBforPostgreSQL/","registries":"Microsoft.ContainerRegistry/","backup":"Microsoft.RecoveryServices/","cosmos":"Microsoft.DocumentDB/"}
CURATED_TYPES=["Microsoft.Compute/virtualMachines","Microsoft.Storage/storageAccounts","Microsoft.Network/virtualNetworks","Microsoft.ManagedIdentity/userAssignedIdentities","Microsoft.Sql/servers","Microsoft.Storage/storageAccounts/queueServices","Microsoft.Web/sites","Microsoft.Insights/components","Microsoft.Network/dnsZones","Microsoft.KeyVault/vaults","Microsoft.ContainerService/managedClusters","Microsoft.ServiceBus/namespaces","Microsoft.Network/networkSecurityGroups","Microsoft.Network/publicIPAddresses","Microsoft.Network/routeTables","Microsoft.OperationalInsights/workspaces","Microsoft.Insights/metricAlerts","Microsoft.Web/serverfarms","Microsoft.DBforPostgreSQL/flexibleServers","Microsoft.ContainerRegistry/registries","Microsoft.RecoveryServices/vaults","Microsoft.DocumentDB/databaseAccounts"]
OWNED=[n for n in EXAMPLES if CATALOG[n]['ledger_mode']!='readonly']
def provider_block(name):
 s=(ROOT/'examples'/(name+'.lms')).read_text()
 return s.split('end _dsl_',1)[0]
def kind_for(name):
 m=re.search(r'\.inventory\("([^"]+)"\)',provider_block(name));return m[1] if m else None
def resource_types_for(kind):
 prefix=READONLY_KINDS.get(kind)
 return [t for t in CURATED_TYPES if not prefix or t.startswith(prefix)]
def owned_operations(name):
 source=provider_block(name);resources=[];byvar={};aliases={}
 for m in re.finditer(r'let\s+(\w+)\s*=\s*run\.(container|queue)\("([A-Za-z0-9-]+)"\)',source):
  var,kind,suffix=m.groups();item={'logical_name':f'owned_{len(resources)}','tofu_type':'azurerm_storage_container' if kind=='container' else 'azurerm_storage_queue','native_kind':'blob' if kind=='container' else 'queue','suffix':suffix,'metadata':{'lmazure_test':'$run_id'}};resources.append(item);byvar[var]=item
 for a,b in re.findall(r'let\s+(\w+)\s*=\s*(\w+)\.text\(0,\s*0\)',source):aliases[a]=b
 for ref,key,value in re.findall(r'run\.tag\((\w+(?:\.text\(0,\s*0\))?),\s*"([^"]+)",\s*"([^"]*)"\)',source):
  var=ref.split('.text',1)[0];var=aliases.get(var,var)
  if var not in byvar:raise ValueError(f'{name}: unresolved tagged resource {ref}')
  byvar[var]['metadata']={'lmazure_test':'$run_id',**({key:value} if key!='lmazure_test' else {})}
 return resources
