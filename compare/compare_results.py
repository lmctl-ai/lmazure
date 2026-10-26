"""Pure, secret-safe comparison of canonical lmazure/lmcloud reports."""
from pathlib import Path
import json, math, re

SENSITIVE = re.compile(r"(secret|token|password|credential|private.?key|connection.?string|access.?key|sas|subscription|tenant|account.?id|project.?id|client.?id)", re.I)
OWNERS = ("lmazure_test",)
TYPES = {
 "microsoft.storage/storageaccounts/blobservices/containers":"azurerm_storage_container",
 "microsoft.storage/storageaccounts/queueservices":"azurerm_storage_queue",
 "microsoft.storage/storageaccounts":"azurerm_storage_account",
 "microsoft.network/virtualnetworks":"azurerm_virtual_network",
 "microsoft.network/networksecuritygroups":"azurerm_network_security_group",
 "microsoft.network/publicipaddresses":"azurerm_public_ip",
 "microsoft.network/routetables":"azurerm_route_table",
 "microsoft.web/sites":"azurerm_linux_web_app",
 "microsoft.web/serverfarms":"azurerm_service_plan",
 "microsoft.insights/components":"azurerm_application_insights",
 "microsoft.keyvault/vaults":"azurerm_key_vault",
 "microsoft.containerregistry/registries":"azurerm_container_registry",
 "microsoft.sql/servers":"azurerm_mssql_server",
 "microsoft.documentdb/databaseaccounts":"azurerm_cosmosdb_account",
 "blob":"azurerm_storage_container",
 "container":"azurerm_storage_container",
 "queue":"azurerm_storage_queue",
}

def _safe(v, key=""):
    if SENSITIVE.search(key): return "<redacted>"
    if isinstance(v, dict): return {str(k): _safe(x, str(k)) for k,x in v.items() if not SENSITIVE.search(str(k))}
    if isinstance(v, list): return [_safe(x, key) for x in v]
    return v

def _type(v):
    s=str(v or "").strip().lower()
    if s in TYPES: return TYPES[s]
    for prefix, name in TYPES.items():
        if s.startswith(prefix+"/"): return name
    return s

def _owner(item):
    merged={}
    if isinstance(item.get("tags"),dict): merged.update(item["tags"])
    if isinstance(item.get("metadata"),dict): merged.update(item["metadata"])
    for k in OWNERS:
        if merged.get(k) not in (None,""): return str(merged[k])
    return None

def _resources(side):
    raw=side.get("resources_created",[])
    if not isinstance(raw,list): return [],["resources_created is not a list"]
    out=[]; errors=[]
    for i,item in enumerate(raw):
        if not isinstance(item,dict) or not item.get("type") or not item.get("name"):
            errors.append(f"resource[{i}] lacks type or name"); continue
        owner=_owner(item)
        if owner is None: errors.append(f"resource[{i}] lacks ownership metadata")
        out.append({"type":_type(item["type"]),"name":str(item["name"]),"owner":owner,
                    "metadata_sha256":item.get("metadata_sha256")})
    keys=[(x["type"],x["name"],x["owner"]) for x in out]
    if len(keys)!=len(set(keys)): errors.append("duplicate resource identity")
    return out,errors

def _out(v):
    if isinstance(v,dict):
        return {str(k):_out(x) for k,x in sorted(v.items(),key=lambda p:str(p[0]))
                if not SENSITIVE.search(str(k)) and str(k) not in {"account_fingerprint","wall_seconds","estimated_cost_usd"}}
    if isinstance(v,list): return sorted((_out(x) for x in v),key=lambda x:json.dumps(x,sort_keys=True))
    if isinstance(v,float) and math.isnan(v): return "<nan>"
    return v

def _st(status,why,**extra): return {"status":status,"why":why,**extra}

def _cleanup(side):
    if "leftovers" not in side or side["leftovers"] is None:
        return _st("pending","leftover result is missing or unknown",ok=False)
    v=side["leftovers"]
    count=len(v) if isinstance(v,list) else v if isinstance(v,int) and v>=0 else None
    if count is None: return _st("pending","leftover result has an unknown shape",ok=False)
    return _st("same" if count==0 else "differs","cleanup reported zero leftovers" if count==0 else "owned leftovers remain after cleanup",count=count,ok=count==0)

def compare(report):
    if not isinstance(report,dict) or not isinstance(report.get("lmazure"),dict) or not isinstance(report.get("lmcloud"),dict):
        raise ValueError("report requires lmazure and lmcloud objects")
    a,b=report["lmazure"],report["lmcloud"]
    ar,ae=_resources(a); br,be=_resources(b)
    ak=sorted((x["type"],x["name"],x["owner"]) for x in ar); bk=sorted((x["type"],x["name"],x["owner"]) for x in br)
    fingerprints="same"
    if ak==bk and not ae and not be:
        for left,right in zip(sorted(ar,key=lambda x:(x["type"],x["name"],x["owner"])),
                              sorted(br,key=lambda x:(x["type"],x["name"],x["owner"]))):
            if left["metadata_sha256"] is None or right["metadata_sha256"] is None:
                fingerprints="pending"
                break
            if left["metadata_sha256"] != right["metadata_sha256"]:
                fingerprints="differs"
                break
    resources=_st("same" if ak==bk and not ae and not be and fingerprints=="same" else fingerprints if ak==bk and not ae and not be else "differs",
                   "normalized type, exact name, ownership metadata, and observed metadata fingerprints match"
                   if ak==bk and not ae and not be and fingerprints=="same"
                   else "metadata fingerprint is unknown on one or both owned resources" if fingerprints=="pending"
                   else "resource type/name/ownership sets or metadata fingerprints differ",
                   lmazure=ar,lmcloud=br,lmazure_errors=ae,lmcloud_errors=be)
    if "outputs" not in a or "outputs" not in b or a.get("outputs") is None or b.get("outputs") is None:
        outputs=_st("pending","outputs are absent or null; comparison cannot assume empty objects",lmazure=a.get("outputs"),lmcloud=b.get("outputs"))
    else:
        ao,bo=_out(a["outputs"]),_out(b["outputs"])
        outputs=_st("same" if ao==bo else "differs","normalized output objects match" if ao==bo else "normalized outputs differ",lmazure=ao,lmcloud=bo)
    time=_st("same" if a.get("wall_seconds")==b.get("wall_seconds") else "differs","wall-time differences are informational",lmazure=a.get("wall_seconds"),lmcloud=b.get("wall_seconds"))
    cost=_st("same" if a.get("estimated_cost_usd")==b.get("estimated_cost_usd") else "differs","cost differences reflect SDK versus conservative provider allowances",lmazure=a.get("estimated_cost_usd"),lmcloud=b.get("estimated_cost_usd"))
    cleanup={"lmazure":_cleanup(a),"lmcloud":_cleanup(b)}
    af,bf=a.get("account_fingerprint"),b.get("account_fingerprint")
    account=_st("same" if af is not None and bf is not None and af==bf else "pending" if af is None or bf is None else "differs",
                "valid account fingerprints match" if af is not None and bf is not None and af==bf else "account fingerprint is missing" if af is None or bf is None else "account fingerprints differ")
    requested=(a.get("requested_provider"),b.get("requested_provider"))
    executed=(a.get("executed_provider"),b.get("executed_provider"))
    provenance=_st("same offline fixture intents" if requested==("azurerm","azurerm") and executed in {("azure_sdk_fake","builtin_fixture"),("builtin_fixture","builtin_fixture")} else "differs",
                   "native SDK fakes and an executed builtin-provider fixture plan were compared; this is not live AzureRM equivalence" if requested==("azurerm","azurerm") and executed in {("azure_sdk_fake","builtin_fixture"),("builtin_fixture","builtin_fixture")} else "requested or executed providers differ",
                   requested_provider=requested,executed_provider=executed)
    live_requested = a.get("mode")=="live" or b.get("mode")=="live"
    live_approved = (a.get("mode")=="live" and b.get("mode")=="live"
                     and report.get("user_approved_actual") is True
                     and requested==("azurerm","azurerm")
                     and executed==("azure_sdk_live","azurerm"))
    ok=resources["status"]=="same" and outputs["status"]=="same" and account["status"]=="same" and all(x.get("ok") for x in cleanup.values())
    if report.get("fixture_inputs_identical") is True:
        ok = False
        outputs = _st("pending", "fixture inputs were declared identical without an actual provider report")
    if live_requested and not live_approved:
        ok = False
    live=_st(("same" if ok else "differs") if live_approved else "pending",
             ("user-approved actual live results match" if ok else "user-approved actual live results differ or are incomplete")
             if live_approved else "pending user-approved live records; offline fixtures cannot pass live comparison")
    return _safe({"ok":ok,"offline":{"resources":resources,"outputs":outputs,"time":time,"cost":cost,"cleanup":cleanup,"account":account,"provenance":provenance},"live":live})

def write_results(catalog,reports,path):
    root=Path(path).parent
    rows=[]
    for entry in catalog:
        name=entry["name"];result=reports.get(name)
        native=f"https://raw.githubusercontent.com/lmctl-ai/lmazure/main/examples/{name}.lms"
        cloud=f"https://raw.githubusercontent.com/lmctl-ai/lmazure/main/compare/{name}/main.lms"
        if result is None:
            rows.append(f"| {name} | pending | pending | pending | pending | pending | pending user test | no offline report | [native]({native}) · [lmcloud]({cloud}) |")
            continue
        data=result["offline"]
        why=[data[k]["why"] for k in ("resources","outputs","time","cost")]
        spec=root/name/"spec.json"
        if spec.exists():why+=json.loads(spec.read_text()).get("semantic_differences",[])
        why_text="; ".join(dict.fromkeys(why)).replace("|","/")
        cleanup="/".join(data["cleanup"][side]["status"] for side in ("lmazure","lmcloud"))
        live="pending user test" if result["live"]["status"]=="pending" else result["live"]["status"]
        rows.append(f"| {name} | {data['resources']['status']} | {data['outputs']['status']} | {data['time']['status']} | {data['cost']['status']} | {cleanup} | {live} | {why_text} | [native]({native}) · [lmcloud]({cloud}) |")
    live_supplied=any(r.get("live",{}).get("status") in {"same","differs"} for r in reports.values())
    live_summary=("Live statuses reflect supplied user-approved actual-provider reports; review the individual evidence. " if live_supplied
                  else "No live Azure subscription was used. Every live row remains pending user test. ")
    text="# lmazure / lmcloud comparison results\n\nAll offline rows compare SDK fake results with an **executed builtin-provider OpenTofu fixture plan**, not an AzureRM plan. "+live_summary+"Wall times are informational (native offline timing includes image rendering); the cost model differences are explicit planning allowances. For offline rows, cleanup means zero native fake ownership and zero local OpenTofu fixture state, **not a live Azure absence measurement**.\n\n| Example | Resources | Common outputs | Wall time | Estimated cost | Cleanup native/tofu | Live | Why / limits | Autorun |\n| --- | --- | --- | --- | --- | --- | --- | --- | --- |\n"+"\n".join(rows)+"\n"
    Path(path).write_text(text,encoding="utf-8")
