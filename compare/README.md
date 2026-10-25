# Native SDK and lmcloud equivalents

Every one of the 72 native examples has `compare/<example>/main.lms`, a small
`main.tf` using pinned AzureRM 5.10.0, an explicit `offline.tf` fixture, and a
recovery `cleanup.lms`. See [RESULTS.md](RESULTS.md) for all per-example results.

Default execution is **offline**. It runs the original native example through
fake SDK transports and executes a genuine OpenTofu plan/apply/destroy with the
built-in `terraform_data` fixture provider. The fixture's planned Azure resource
intents, safe outputs and final local state are checked against the native
ledger. **This is not an AzureRM provider plan or live Azure equivalence.**
AzureRM has not been initialized or authenticated during development.

```sh
compare/run.sh owned_mixed_lifecycle
compare/run.sh all --offline
```

Requirements: Python 3, LMScript/lmctl 0.2.420 or newer, lmazure v0.1.3, and
lmcloud v0.1.9. The harness resolves CLI programs from PATH or their lmctl tool
cache. You can specify `--native-binary`, `--lmcloud-binary`, and `--compiler`.
Native renderers lmplot/lmdraw install from their normal example bindings.
No lmcloud repository files are modified. The tool lock records the public
release checksum and the known embedded-version discrepancy.

Each equivalent is self-contained and directly autorunnable, for example:

```sh
lmctl autorun https://raw.githubusercontent.com/lmctl-ai/lmazure/main/compare/owned_mixed_lifecycle/main.lms
```

It writes the checked AzureRM HCL and the separate offline fixture in an isolated
`build/lmcloud_<example>/` directory. It checks individual init, validation,
apply and destroy booleans rather than mistaking VM success for provider success.
Create and cleanup are separate host phases; persistent `comparison.auto.tfvars.json`
prevents process-local variable loss. The harness retries the recovery program
on VM errors/timeouts. Abrupt OS termination still requires the retained ledger
and recovery program; state is never discarded to pretend cleanup succeeded.

## What is compared

- Created resource types and **exact** names: the native run ID seeds the matching
  `lmazure-<run-id>-<suffix>` AzureRM names. Native owned resources are never
  imported into Terraform state: native teardown completes before the equivalent.
- The mandatory `lmazure_test` ownership metadata and a SHA-256 fingerprint of
  complete metadata observed by the SDK before deletion. Only the ownership tag
  and fingerprint are exported; other metadata values are not printed.
- Safe common resource metadata outputs (type, name, location, ownership-tag
  presence), actual elapsed wall times, price-table estimates, and cleanup state.
- Zero leftovers are required. Missing/unknown cleanup results are failures,
  not silently converted into zero.

AzureRM's generic resource data source does not expose the SDK's creation-age or
function-app Kind field. It cannot reproduce synthetic public/encryption flags.
The native gallery's plots/presentation and those unsupported audit fields are
outside the common-output projection; the results table states these differences.
The AzureRM configurations query the explicit curated type set; native all-resource
inventory can include additional provider types on a real subscription.

Native estimates count SDK request attempts. AzureRM estimates reserve 64 requests
per intended owned resource at the same conservative $0.0001 planning allowance.
That is $0.0064 per resource, with a $0.05 example cap and $0.25 ceiling, checked
before provider initialization and again in HCL. These are **estimated from price
table, not yet measured live**. Provider requests and invoices are not measured.
Prices vary by region/time. Offline execution has zero Azure charge. Wall-time
figures include gallery rendering on the native offline side and are informational.

## Later user-run live comparison — pending

No live comparison has been run. Do not enable this until the user approves local
Azure testing. CI never enables it and has no Azure credentials or OIDC.

After user approval, use an existing Azure CLI profile, the same subscription for
both tools, and (for writes) an existing storage account with the necessary
management/data permissions. The harness grants no roles and creates no account,
resource group, identity, or other billed infrastructure.

```sh
export LMAZURE_COMPARE_LIVE_APPROVED=1
export LMAZURE_LIVE=1
export LMAZURE_SUBSCRIPTION_ID='<approved-subscription-guid>'
export LMAZURE_REGION=eastus
# Owned cases only, after separately approving writes:
export LMAZURE_ALLOW_WRITES=1
export LMAZURE_COMPARE_STORAGE_ACCOUNT_ID='/subscriptions/<same-guid>/resourceGroups/<existing-rg>/providers/Microsoft.Storage/storageAccounts/<existing-account>'
compare/run.sh owned_mixed_lifecycle --live
```

The live native driver runs the exact provider block extracted from its native
example, changing only `mock()` to `live()` and omitting mock-golden presentation
assertions. It does not fabricate a live image. Read-only comparisons use safe
common inventory facts; unsupported facts remain explicitly different/pending.

The storage endpoints must belong to the supplied account. Before Terraform
apply, the harness verifies the exact native names are absent and writes a
private, account-bound absence ledger. AzureRM only creates those planned names,
with the same ownership metadata. Teardown always attempts state destroy. A final
Azure CLI check verifies actual resource absence; recovery deletion requires the
prefix, ownership metadata, and the preflight absence ledger. Foreign resources
are retained and cause failure. Direct live autoruns require the harness's private
authorization record so they cannot skip this preflight accidentally.

Private logs, state, variable files and account bindings remain under ignored,
mode-0700 `compare/runs/` workspaces (files use an owner-only umask). Do not publish
those files. Public evidence contains safe generated names, metadata hashes,
comparison statuses and timings. Read-only resource names can still be private:
review local live evidence before sharing it.

The harness never calls a billing/Cost Explorer API. Real billing calibration,
IAM limitations and live provider/RBAC behavior remain user work. See
[LMCLOUD_ISSUES.md](LMCLOUD_ISSUES.md) for documented lmcloud API/version findings.
