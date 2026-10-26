# lmcloud equivalents and comparison limits

All 72 examples have `compare/<name>/main.lms`, AzureRM `main.tf` (5.10.0),
builtin-provider `offline.tf`, `spec.json` and `cleanup.lms`. Generated equivalents
use `tofu.*` with pinned lmcloud v0.1.9. Suffixes and effective metadata are parsed
from the native provider block. Run `compare/run.sh <name>` or `all --offline`;
see [compare/README.md](../compare/README.md) for prerequisites/live flags.

Offline harness execution compares native SDK-fake ledgers with an executed
builtin `terraform_data` fixture plan/apply/output/destroy. It checks exact logical
resource types/names, ownership metadata/fingerprints, common outputs, elapsed
time, estimated cost and cleanup. This does **not** prove AzureRM provider schema
acceptance, live inventories, permissions, equivalent SDK phases or Azure absence.
Read-only AzureRM queries cover the explicit curated type set, not arbitrary ARM
types; age/public/encryption/function-Kind facts are not claimed equivalent.

[RESULTS.md](../compare/RESULTS.md): 72/72 same offline resources/common outputs,
zero native fake ownership and local fixture state for both; cost estimates match
in 56 and differ in 16 because AzureRM reserves 64 requests per intended resource
instead of observed SDK attempts. All wall times differ and are informational;
native timing includes image rendering. Every live row is **pending user test**.
Public native/comparison URL runs and the released-binary matrix are recorded in
[v0.1.3-verification.json](v0.1.3-verification.json).

[LMCLOUD_ISSUES.md](../compare/LMCLOUD_ISSUES.md) records findings only: no exposed
Azure provider-mock command; `tofu.var` values are process-local, so cleanup needs
persistent auto.tfvars; callers must check operation booleans rather than the VM
success envelope. The current checked v0.1.9 asset reports the correct version;
the earlier discrepancy is no longer reproduced. `lmcloud.lock.json` records its
verified hash/version. No lmcloud repository files were edited.

Live comparison requires explicit approval flags, the same subscription/account,
private isolated state, exact-name absence preflight and ownership-ledger recovery.
It is developed but untested. Never commit raw state, account details or logs;
private workspaces are ignored under `compare/runs/`.
