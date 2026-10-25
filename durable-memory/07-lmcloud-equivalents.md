# v0.1.3 lmcloud comparison suite

All 72 native examples have compare/<name>/main.lms, main.tf (AzureRM 5.10.0),
offline.tf (builtin terraform_data fixture), spec.json and cleanup.lms. The source
provider block is parsed for exact container/queue suffixes and effective metadata;
no resource kind, operation or suffix is guessed from its filename. Read-only
Terraform queries the explicit curated full type set and exports safe common
facts. It does not pretend the generic data source exposes SDK age or function Kind.

compare/run.sh <name> defaults to offline; `all --offline` runs the entire matrix.
The native example executes unchanged with fake SDK transports, including its
image. The lmcloud program executes a genuine builtin-provider fixture plan,
apply, safe output and separate unconditional destroy phase. Planned resource
intents are compared with actual native ledger history and actual fixture outputs.
Terraform variables persist across host processes through auto.tfvars.json.
Returned booleans and post-destroy state are verified, not the VM success envelope.

All 72 offline comparisons passed: exact resource types/names, mandatory ownership
metadata, complete metadata fingerprints, common outputs, and zero native fake
ownership/local fixture state. Elapsed time differs and is informational; native
offline time includes image rendering. AzureRM planning allowances (64 requests
per intended resource) differ from native observed SDK request counts. RESULTS.md
contains one row per example, separate dimensions, semantic limitations, and both
raw autorun links. Every live comparison is explicitly pending user test.

Cost evidence now includes a SHA256 of complete metadata observed during SDK
ownership reads plus only the safe lmazure_test ownership tag. Arbitrary metadata
values are never exported. The fake storage handler preserves full metadata so
it can exercise fingerprints. A privacy regression verifies password-like metadata
is absent from evidence. Native fake assertions total 120; CLI checks 43.

Live support is developed but NOT exercised. It requires explicit local approval,
matching subscription/account inputs, an isolated state workspace and absence
ledger, prefix/ownership checks, unconditional destroy and final Azure CLI absence
verification. Recovery deletion additionally requires the same run's verified
absence ledger. Unknown/foreign leftovers fail. No role/account/infrastructure
creation is configured. No OIDC or Azure credentials are configured in CI.

The lmcloud repository was only read; no files there were changed. Public v0.1.9
binary and checksums were used. Its embedded --version reports 0.1.0: the discrepancy
and API process-local-variable pitfall are reported in compare/LMCLOUD_ISSUES.md.
Provider mocking via tofu test is not exposed by the current lmcloud API, so no
real AzureRM offline plan is claimed. HCL syntax was checked without initializing
an Azure provider; live AzureRM validation/RBAC/results remain for the user.

Local acceptance: 72 full native/fixture comparisons, 72 generated-config contracts,
74 live-admission/source checks, 11 comparison-engine regressions, standard native
checks and NativeAOT. Five-platform CI now runs the comparison matrix in addition
to the existing native gallery; comparisons install compiler 0.2.420 after the
native gallery's pinned 0.2.369 pass. CI/release verification is pending.
