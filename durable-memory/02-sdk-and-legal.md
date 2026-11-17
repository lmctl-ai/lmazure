# SDK pins and legal inventory

`global.json` pins .NET SDK **10.0.402**, with roll-forward disabled.
`src/LmAzure/LmAzure.csproj` targets net10.0; `packages.lock.json` is restored in
locked mode. Azure package pins (all MIT):

| Package | Version |
| --- | --- |
| Azure.Core | 1.63.0 |
| Azure.Identity | 1.21.0 |
| Azure.ResourceManager | 1.14.0 |
| Azure.Storage.Blobs | 12.30.1 |
| Azure.Storage.Queues | 12.28.1 |

`sdk.lock` pins `lmcode-go` commit `2027887521b3d34a44e67849070bf7e39df11b1d`, and
`csharp-sdk.lock` pins `lmcode-csharp` commit `3c575d2dd9132010efdbe959b5716e6c2fab1811`
(`lmcode_std_v3` with 226 standard signatures and 10 standard REST/CLI interfaces).
`scripts/sync-sdk.py --ensure` verifies its archive/source hashes and the local VM
allocation-checkpoint patch. SDK sources compile directly into the host; no
separate lmcode managed assembly or .NET runtime installation is needed by users.

`THIRD_PARTY_NOTICES`, `src/LmAzure/Resources/licenses/` and `--notices` retain
Azure, .NET, Arrow, OpenSSL/zlib and SDK notices. Apache.Arrow 22.1.0 is transitive;
its Apache-2.0 license and upstream NOTICE are embedded. GCC runtime components
use the GCC Runtime Library Exception; its license and exception are embedded.
Do not remove embedded notices or alter package/SDK pins without rebuilding and
checking NativeAOT/trimming warnings, schema agreement and the DSL budget cases.
