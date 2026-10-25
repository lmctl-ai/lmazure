# Per-example cost estimates

CI uses only in-process fake transports. It has no Azure credentials, Azure login
step, GitHub OIDC permission, or cloud federation. Workload-identity federation
is excluded from the host's live credential chain. Live testing is a user-run,
explicitly approved local operation with an existing profile.

The catalog has 55 read-only examples, 16 owned-resource lifecycle examples,
and one budget-admission rejection. Every program writes cost evidence and ends
with `estimated cost: $...`. Read-only ARM metadata reports print `$0.00` and
record actual management request dispatches, including pagination. The lifecycle
examples count real SDK requests through in-process fakes and report planning
estimates from `$0.0005` to `$0.0010`. The admission example declares a cap below
creation plus teardown reservation and proves rejection before any HTTP request.
All 72 currently use mock(): their actual Azure charge is zero. Nonzero figures
are **estimated from price table, not yet measured live**. Exact per-example
costs, request counts and evidence paths are recorded in `examples/catalog.json`.

| Planning item | USD per unit |
| --- | ---: |
| Storage HTTP request attempt, including failed requests and teardown | 0.0001 |
| Empty container/queue minute | 0 |
| ARM metadata inventory | 0 |

Table version: `2026-10-25-planning-v1`. The storage request amount is a deliberately
conservative local planning allowance, not a quoted Azure tariff. These APIs
create only empty containers and queues in an existing account: no objects,
messages, VMs, data transfer, new storage accounts, or new hourly infrastructure.
Existing storage-account charges are outside an example's incremental estimate.
Resource lifetimes in minutes are recorded even when the modeled time rate is zero.

For current provider tariffs, consult [Azure Blob Storage pricing](https://azure.microsoft.com/en-us/pricing/details/storage/blobs/)
and [Azure Queue Storage pricing](https://azure.microsoft.com/en-us/pricing/details/storage/queues/).
Prices depend on region, redundancy, account options and date. Before any local
live test, review the planning table against the intended account. An estimate
is not an invoice or a guarantee of a maximum Azure bill. Neither the host nor
its tests query billing, Cost Explorer, or a pricing endpoint.

## Budget enforcement

Each owned run defaults to a $0.05 estimate cap. Call `run.cost_cap(0.05)` before
creating anything to declare the example's cap. Values must be finite and between
zero and $0.25. The same host process also shares an immutable $0.25 ceiling across
all sessions and ledgers. Spent estimates are never refunded after deletion.

Before dispatching a create request, the ledger reserves three request units:
one creation plus an ownership check and deletion at teardown. The next create
is refused before HTTP if spent cost plus reservations would exceed either cap.
Tagging reserves its ownership read and write as well. The transport records
actual request attempts; unused operation reservations are released.

Cleanup uses its reserved units and is never blocked by an exhausted estimate.
Repeated failed cleanup can consume additional requests; those are recorded and
block subsequent creation. This is why the cap is a creation/admission gate,
not a substitute for reviewing regional prices or resolving retained ledgers.
A run can own at most 16 resources concurrently. Run teardown still executes on
VM failure, and recovery requires prefix, ownership metadata and account-bound
ledger checks.

## Evidence

The ownership ledger embeds the price table, cap, spent/reserved costs, request
history, resource names, unit prices, and minutes alive. Successful and failed
cleanup also writes:

```text
evidence/fake-cost-<example>-<run-id>.json
# Future approved local live runs use:
evidence/live-cost-<example>-<run-id>.json
```

`run.cost_report(path)` exports the same evidence to a known filename after
cleanup; `run.estimated_cost()` returns the numeric estimate. JSON includes
`estimated_cost_text` for the exact final output line. Evidence contains no
credentials, access tokens, subscription IDs or account endpoints. The ownership
ledger itself retains account binding for recovery and should stay local.

Recovery preserves cost history and adds its own request attempts. Ledgers from
v0.1.0 have no prior cost history: their reports explicitly set
`historical_cost_known=false` and account for recovery requests only. The user
must resolve any leftover resource before treating recovery as complete.

Committed gallery evidence is fake-only. No live evidence or billing calibration
has been claimed. The Lead/user may perform a separate optional billing comparison
later; the host and CI never make that measurement call.

`session.readonly_cost_report(path)` exports a separate zero-cost metadata ledger.
Its request counter observes actual ARM dispatches, so an empty listing still
counts one request and a paginated listing counts every page. It contains no
credentials, subscription IDs or raw resource properties.
