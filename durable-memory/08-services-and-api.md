# Curated services and API

`src/LmAzure/Host.cs` defines **25 signatures, schema version 2**. Inspect the
actual interface with `lmazure --dsl-schema` and `--dsl-signatures`.

- Functions: `mock()`, `live()` → Session.
- Session: `inventory(kind)`, `begin_run(ledger)`, `readonly_cost_report(path)`,
  `sweep(ledger, age_hours)`, `sweep_apply(ledger, age_hours)`.
- Run: `container(suffix)`, `queue(suffix)`, `tag(name,key,value)`, `delete(name)`,
  `cleanup()`, `cost_cap(amount)`, `estimated_cost()`, `cost_report(path)`.
- Table: `rows`, `columns`, `column`, `text`, `number`, `csv`, `json`,
  `csv_string`, `json_string`; use schema for parameter types.

Inventory accepts `resources`/`all`, `compute`, `storage`, `network`, `identity`,
`database`, `queues`, `serverless`, `monitoring`, `dns`, `vaults`, `containers`,
`functions`, `servicebus`, `nsg`, `public_ips`, `routes`, `logs`, `alerts`,
`app_plans`, `postgresql`, `registries`, `backup`, `cosmos`.
These cover ARM metadata for VMs, storage, networking/DNS, managed identities,
SQL/PostgreSQL/Cosmos, Web/Functions, Insights/Log Analytics, Key Vault, AKS,
Service Bus, ACR and Recovery Services. Functions additionally filter Web-site Kind.
The host does not execute functions, query logs, read secrets/messages/blob data,
audit full IAM policy or provision those service families.

Tables have `name,kind,location,status,count,age_hours,public,encrypted,tagged,id`.
IDs are masked; names remain visible. Generic ARM metadata leaves unavailable
security/age fields unknown and does not infer topology/dependencies. Resource
writes use Blob/Queue data SDKs and are limited to owned empty containers/queues.
