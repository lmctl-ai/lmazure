# Verification and local acceptance gate

User decision: local build and tests gate changes. Do not wait for CI approval
before committing/merging/pushing locally verified work. Keep the existing fake-only
five-platform/release automation; its job dependencies still control publication.
Documentation-only pushes are ignored by that workflow.

Current source acceptance (2026-10-26 UTC): `test.sh` passed with .NET SDK 10.0.402,
zero build warnings/errors, 43 CLI checks (including 120 fake-transport assertions),
17 CI safety guard checks, version agreement, 72 gallery/catalog contracts,
72 comparison configuration contracts, 11 baseline comparison-engine regressions
plus live approval/provenance/failure regressions, fake report-producer/consumer
integration, and
74 harness admission/source checks. No Azure provider is initialized by these tests.

`test.sh` does not execute the gallery unless `LMAZURE_LMSCRIPT` is set, and does
not run the standalone DSL suite or full comparison matrix. Use the explicit
commands in [03-build-and-runtime.md](03-build-and-runtime.md) for those checks.
The memory refresh also rebuilt NativeAOT successfully: 43 native CLI checks,
120 fake assertions, 14 Linux DSL checks (including 6 GiB virtual-address and
default-budget probes), all 72 isolated gallery executions, and glibc-only linkage
(maximum ABI 2.34) passed. Native publish emitted a nonfatal clang unused
`-static-libstdc++` argument warning; managed build had zero warnings/errors.

Release v0.1.3 already passed five-platform main/tag workflows, all five downloaded
asset hashes, 120 released native assertions, 72 fresh-cache README autoruns,
72 fresh-cache lmcloud URLs and the 72-case released-binary offline matrix.
See [release evidence](v0.1.3-verification.json); these are fake/fixture checks,
not AzureRM schema, authentication, billing or live cleanup validation.
