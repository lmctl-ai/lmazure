# v0.1.4 live-report correctness

The report producer and comparison engine use `user_approved_actual`, a boolean
set by the live runner only after local approval flags pass. Both records must
be live and identify `azure_sdk_live`/`azurerm`; missing approval, legacy approval
fields, fixtures or mixed modes cannot pass as live evidence.

Approved records receive live `same` only when resources, metadata fingerprints,
common outputs, account fingerprints and zero leftovers agree. Differences or
incomplete results produce `differs`. Cost/time differences remain informational.
Console and RESULTS text now reflect live versus offline records correctly.
The documented approval-field open item is resolved; actual live tests are pending.

`scripts/test-comparison-harness.py` exercises the actual producer/engine handoff
with temporary local provider artifacts and real flag validation. The regression
failed before the fix. `scripts/test-compare-results.py` covers approval denial,
legacy/mixed/fake provenance, differing outputs/accounts/leftovers and summary text.
These synthetic records are unit fixtures, never published live Azure evidence.

Local verification (2026-10-26 UTC): managed `test.sh`, NativeAOT publish,
120 fake assertions, 43 native CLI checks, 14 Linux DSL budget/contract checks,
all 72 gallery runs, all 72 native/lmcloud offline comparisons, and glibc-only linkage (maximum ABI 2.34) passed. No actual
Azure authentication or subscription was used. [v0.1.4](https://github.com/lmctl-ai/lmazure/releases/tag/v0.1.4) is published.
The five-platform tag/release workflow passed, all five downloaded checksums
matched, and the released Linux x64 version/self-tests passed.
See [release verification](v0.1.4-verification.json). Local build/tests remain
the repository acceptance gate; all actual Azure comparisons remain pending.
