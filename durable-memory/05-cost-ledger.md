# v0.1.1 cost policy

No OIDC, Azure login, or Azure credentials are configured in CI. The guard in
scripts/check-ci-safety.py scans every workflow and has 17 rejection/acceptance
checks. Workload identity federation is excluded from the live credential chain.
All development operations remain in-process fakes. No subscription was accessed.

Each AzureRun owns a decimal cost ledger. Its default cap is $0.05; the host
process also retains spent and reserved estimates across all sessions with a
$0.25 ceiling. BeginCreate reserves one request plus two teardown requests BEFORE
HTTP dispatch. Tagging reserves two requests. The actual SDK transport callback
counts attempts, including failed calls and ownership checks. Cleanup consumes
prepaid units and can never be blocked by an estimate; retry overruns are retained
and block further creation. Deleting a resource never refunds its spent estimate.

Ledger ownership data embeds the versioned conservative planning price table,
request history, resource unit prices and observed lifetime in minutes. Evidence
survives normal and failed teardown. Sweep recovery preserves history, reuses a
loaded ledger rather than charging history twice, and marks unknown historical
costs for pre-v0.1.1 ledgers. There are no provider pricing or billing calls.

Typed API additions: Run.cost_cap(float), Run.estimated_cost(), Run.cost_report(path).
There are now 23 host signatures. Cost JSON includes the exact estimated_cost_text
used as the final line of the two lifecycle examples. Each uses five requests ×
$0.0001 planning allowance = $0.0005; their actual fake Azure charge is zero.
The other 25 examples finish with estimated cost: $0.00. See docs/COSTS.md.

Local acceptance: 90 fake checks (27 core, 35 safety, 20 cost, 8 recovery),
31 CLI checks, 14 Linux DSL checks (13 elsewhere), 17 CI policy guard checks,
and all 27 isolated-directory examples with PNG and exact final cost validation.
The .NET 10 NativeAOT executable passed the same CLI/DSL checks and Linux glibc-only
linkage verification (maximum ABI 2.34). Both [main CI](https://github.com/lmctl-ai/lmazure/actions/runs/40161723523) and
[tag CI](https://github.com/lmctl-ai/lmazure/actions/runs/40162158459) passed all five
platforms. The tag publication job also passed. Release source:
`bfa57696cfa9e181cf4fd8ee32a7d6f8d12555fe`.

The public [v0.1.1 release](https://github.com/lmctl-ai/lmazure/releases/tag/v0.1.1)
contains all five executables and SHA256SUMS. All downloaded asset checksums were
verified; both Linux binaries require only glibc libraries with maximum ABI 2.34.
The documented baseline remains glibc 2.35.

| Asset | Bytes |
| --- | ---: |
| lmazure-linux-arm64 | 20,478,976 |
| lmazure-linux-x64 | 20,304,056 |
| lmazure-macos-arm64 | 15,422,616 |
| lmazure-macos-x64 | 15,760,608 |
| lmazure-windows-x64.exe | 16,340,480 |

Post-release verification used a fresh LMSCRIPT_TOOL_CACHE and LMCTL_HOME, no
lmazure on PATH, and an empty working directory for every raw README autorun URL.
All 27 installed/used v0.1.1 and generated valid 900×560 PNGs under 300 KB. The
installed executable's SHA-256 matched the verified linux-x64 release asset.
All 25 read-only programs ended with estimated cost: $0.00; both lifecycle
programs ended with estimated cost: $0.0005 and produced fake evidence with five
requests, zero remaining ownership and zero reserved cost. CI and post-release
checks explicitly retained LMAZURE_LIVE=0 and LMAZURE_ALLOW_WRITES=0.

Curated fake evidence for both lifecycle examples is committed in evidence/ and
linked from README. Every figure is estimated from the price table, not yet
measured live. Prices vary by region and time; the planning allowance is not an
Azure quote or an invoice guarantee. A user-approved local live test remains
outstanding. Existing v0.1.0 release evidence in 04-release.md stays historical.
