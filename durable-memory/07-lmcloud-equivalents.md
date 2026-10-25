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
binary and checksums were used. The current fresh-cache asset reports the correct
0.1.9 version; the earlier version discrepancy is no longer reproduced. Findings
and the process-local-variable pitfall are recorded in compare/LMCLOUD_ISSUES.md.
Provider mocking via tofu test is not exposed by the current lmcloud API, so no
real AzureRM offline plan is claimed. HCL syntax was checked without initializing
an Azure provider; live AzureRM validation/RBAC/results remain for the user.

Local acceptance: 72 full native/fixture comparisons, 72 generated-config contracts,
74 live-admission/source checks, 11 comparison-engine regressions, standard native
checks and NativeAOT. Five-platform CI now runs the comparison matrix in addition
to the existing native gallery; comparisons install compiler 0.2.420 after the
native gallery's pinned 0.2.369 pass. Main and tag five-platform CI passed; v0.1.3 is published.


## Published v0.1.3 verification — 2026-10-25

[Main CI](https://github.com/lmctl-ai/lmazure/actions/runs/40202476387) and
[tag CI/release](https://github.com/lmctl-ai/lmazure/actions/runs/40203481748)
passed all five NativeAOT platforms, including the 72-case comparison matrix on
each platform. [v0.1.3](https://github.com/lmctl-ai/lmazure/releases/tag/v0.1.3)
contains all five assets and SHA256SUMS. All five downloaded hashes passed;
both Linux binaries use glibc only, maximum required ABI 2.34. The published
Linux x64 executable reports v0.1.3 and passes 120 fake-transport checks.

| Asset | Bytes |
| --- | ---: |
| lmazure-linux-arm64 | 20,524,584 |
| lmazure-linux-x64 | 20,353,752 |
| lmazure-macos-arm64 | 15,472,312 |
| lmazure-macos-x64 | 15,805,800 |
| lmazure-windows-x64.exe | 16,393,216 |

All 72 README raw autoruns passed with a new empty tool cache and LMCTL_HOME,
no lmazure on PATH, and a separate empty working directory per example. Every
PNG and expected final estimate was checked. Native ledgers had zero ownership
and reserved cost; budget refusal dispatched no requests. The installed @latest
binary's version and SHA256 matched the published Linux x64 v0.1.3 asset.
All 72 public lmcloud raw comparison URLs independently passed with another
fresh cache; their fixture outputs, lifecycle return values, estimates and empty
post-destroy state were checked. The pinned lmcloud asset's hash and embedded
0.1.9 version match its currently published SHA256SUMS.

The complete offline harness also passed all 72 cases using the downloaded
v0.1.3 executable: 72 same resource/ownership/common-output results, zero native
fake and local fixture leftovers, 56 same cost estimates and 16 differing
request allowances. Wall time differs in all 72 and is informational. These are
SDK-fake versus builtin-provider fixture comparisons, not AzureRM plans. Every
live row remains pending user test. No Azure authentication or subscription was
used, and the lmcloud repository was not edited.

Machine-readable per-example checks, release hashes and timing/cost observations
are in [v0.1.3-verification.json](v0.1.3-verification.json).

The current-asset provenance follow-up also passed all five platforms:
[main follow-up CI](https://github.com/lmctl-ai/lmazure/actions/runs/40204299497).
The subsequent commits contain documentation and durable verification records only.
