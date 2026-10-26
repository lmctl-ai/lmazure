# Published versions and release workflow

Current source, CLI and project version: **0.1.3**. `scripts/check-version.py`
checks version agreement. Published versions:

| Version | Scope |
| --- | --- |
| [v0.1.0](https://github.com/lmctl-ai/lmazure/releases/tag/v0.1.0) | Initial Azure SDK host |
| [v0.1.1](https://github.com/lmctl-ai/lmazure/releases/tag/v0.1.1) | Cost-ledger policy |
| [v0.1.2](https://github.com/lmctl-ai/lmazure/releases/tag/v0.1.2) | 72-example gallery |
| [v0.1.3](https://github.com/lmctl-ai/lmazure/releases/tag/v0.1.3) | lmcloud equivalents and metadata evidence |

The retained `.github/workflows/ci.yml` builds NativeAOT on Linux x64/ARM64,
macOS x64/ARM64 and Windows x64. Each job checks fake CLI/DSL/gallery behavior and
the offline comparison matrix. No Azure credentials, Azure login or OIDC are used.
On `v*` tags, the release job depends on all five native jobs and publishes five
executables plus SHA256SUMS. This workflow dependency exists independently of the
user policy that local build/tests, not CI approval, gate repository work.

v0.1.3's five assets/checksums were downloaded and verified; Linux maximum glibc
ABI was 2.34 (supported baseline remains 2.35). Windows is a full native executable.
Current-release hashes, sizes, workflow links and public-URL acceptance are in
[v0.1.3-verification.json](v0.1.3-verification.json). Publishing need not wait for
user live testing. Do not infer live Azure validation from a release or CI badge.
