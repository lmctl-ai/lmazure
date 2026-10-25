# v0.1.2 expanded Azure gallery

There are 72 self-contained examples and 72 PNGs: the original 27 plus 12
service metadata cards, 18 inventory/audit/matrix/graph analyses, and 15
owned-resource, cleanup and estimate workflows. `examples/catalog.json` is the
acceptance catalog: source/image parity, evidence mode and path, expected
request counts, estimate and final output line. Every README entry includes
an image, raw lmctl autorun URL and cost caption.

The fake ARM fixture now has 24 valid resource IDs across 15 provider families.
New curated aliases cover Functions, Service Bus namespaces, NSGs, public IPs,
routes, log workspaces, alerts, App Service plans, PostgreSQL, ACR, backup and
Cosmos metadata. Functions require a Web site whose ARM kind includes functionapp.
The host does not invoke functions, read queue messages or secrets, evaluate
firewall rules, provision compute, or run billed log queries. Writes remain
limited to empty, prefix/tag/ledger-owned containers and storage queues.

All 55 read-only examples export zero-cost metadata ledgers. The new
Session.readonly_cost_report(path) records actual ARM dispatch count, including
empty responses and pagination, without subscription IDs or raw properties.
There are 24 host signatures. Inventory grouping graph edges do not claim
verified application/network dependencies. Synthetic public/encryption flags
are explicitly marked; live generic ARM fields remain unknown.

Sixteen owned workflows exercise queue/container tags, mixed pairs and batches,
explicit deletion, dry/apply/age-gated sweeping, prefix evidence, request-method
breakdown and prepaid teardown. Their costs range from $0.0005 to $0.0010 in
the conservative planning model. A separate $0.0002 admission cap proves a
create is rejected before HTTP with zero requests and zero estimate. Every
lifecycle leaves zero ownership and zero reserved cost. The immutable $0.25
process admission ceiling and $0.05 default example cap remain unchanged.

Local checks: 116 fake assertions (39 core, 35 safety, 20 cost, 8 recovery,
14 readonly-ledger), 43 CLI checks, 14 Linux DSL checks (13 elsewhere), 17 CI
policy checks, and 72 isolated-directory gallery executions with image and
ledger validation. Visual review replaced overlapping rotated labels, marked
service cards MOCK, and rendered diagram metrics from their actual evidence.
All PNGs are 900 by 560 and below 300 KB.

CI and release verification are pending. No real Azure subscription or
authentication was used. CI remains fake-only, with no OIDC, Azure login or
Azure credentials. Publishing does not wait for later user-run live testing.
