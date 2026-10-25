# v0.1.0 release verification

Release source: `80921a4170867bf591796794c5302da3e8e16422`.

The [main CI matrix](https://github.com/lmctl-ai/lmazure/actions/runs/40158390117)
passed all five NativeAOT jobs on 2026-10-25: Linux x64/ARM64, macOS
x64/ARM64 and Windows x64. Each job ran the standalone executable, fake SDK
tests, DSL contract/error tests and all 27 self-executing gallery programs.

Measured checks: 62 fake-transport assertions, 31 CLI checks, 11 DSL checks on
Linux (10 elsewhere), and 27 example renders. Linux checks include the default
budget under a 6 GiB virtual-address-space limit. All provider requests were
in-process fake requests; no real Azure subscription or authentication was used.

The [tag workflow](https://github.com/lmctl-ai/lmazure/actions/runs/40159332980)
passed all five native jobs and the publication job. The public
[v0.1.0 release](https://github.com/lmctl-ai/lmazure/releases/tag/v0.1.0) contains
all five executables plus SHA256SUMS; every downloaded asset matched its checksum.
Both Linux release binaries require only glibc libraries, maximum ABI 2.34
(documented build baseline remains glibc 2.35).

| Asset | Bytes |
| --- | ---: |
| lmazure-linux-arm64 | 20,358,576 |
| lmazure-linux-x64 | 20,187,736 |
| lmazure-macos-arm64 | 15,306,520 |
| lmazure-macos-x64 | 15,641,344 |
| lmazure-windows-x64.exe | 16,179,200 |

Every one of the 27 README raw URLs was then run with `lmctl autorun` in a
separate empty temporary directory. A new LMSCRIPT_TOOL_CACHE and LMCTL_HOME,
with no lmazure on PATH, forced the `install=@latest` path. The installed host
reported `lmazure 0.1.0`; the installer record named `v0.1.0`, and its executable
SHA-256 matched the published linux-x64 asset. All 27 runs wrote valid 900×560
PNG images under 300 KB. Explicit LMAZURE_LIVE=0 and LMAZURE_ALLOW_WRITES=0 were
retained throughout; the source gallery calls mock() regardless of those flags.

Status: **READY TO TEST**. The remaining validation is real authentication,
subscription permissions and live service behavior, which has not been attempted.
The user must approve and run `docs/LIVE_TESTING.md`; writes are a separate,
optional step after the initial read-only inventory.

