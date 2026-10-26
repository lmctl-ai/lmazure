# Ownership safety and cost model

Writes are limited to empty blob containers/storage queues in an existing account.
No compute, accounts, role assignments, identities, keys, messages or objects are
created. Names are `lmazure-<run-id>-<suffix>` and metadata includes
`lmazure_test=<run-id>`. Only HTTP 201 creations enter the exclusively created,
account/endpoint-bound run ledger. Mutation/deletion requires ledger membership,
prefix and matching ownership metadata. Existing resources are never claimed.

Run teardown executes on normal and exceptional VM exit. Failed cleanup retains
the ledger; abrupt process/OS termination may require recovery. Sweeps take an
explicit ledger and age threshold: `sweep` is dry-run, `sweep_apply` requires writes.
There is no subscription-wide prefix deletion. Keep retained ledgers private and
do not delete them before resolving their entries. Public cost evidence exports
safe ownership metadata and a SHA256 fingerprint, not arbitrary metadata values.

`CostLedger` embeds a versioned planning price table and records request attempts,
resource lifetime, spent/reserved estimates and history. Current allowance:
$0.0001/storage request, $0/empty-resource minute, $0/read-only ARM inventory.
`Run.cost_cap` defaults to $0.05; the process shares a hard $0.25 admission ceiling.
Creation reserves one create plus ownership-read/delete teardown before dispatch;
tagging reserves its read/write. Failed requests count; deletion does not refund
spent estimates. Cleanup is never blocked by exhausted estimates, so retry costs
may exceed a reservation and prevent later creation. This is an admission gate,
not a guaranteed maximum Azure invoice. No pricing or billing endpoint is called.

Every example exports evidence and prints `estimated cost: $...` last. Read-only
reports record actual ARM dispatch counts at $0.00. Figures are **estimated from
price table, not yet measured live**; tariffs vary by region/time. See
[docs/COSTS.md](../docs/COSTS.md) for reservation/recovery details and exclusions.
CI uses only fakes, no OIDC or Azure credentials; workload identity is excluded
from the live credential chain. Live use requires explicit local opt-in.
