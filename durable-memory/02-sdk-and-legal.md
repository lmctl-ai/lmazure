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

`sdk.lock` pins lmcode-csharp commit `e68575a5463fc18a18de3045586ad34cd3131fb3`.
`scripts/sync-sdk.py --ensure` verifies its archive/source hashes and the local VM
allocation-checkpoint patch. SDK sources compile directly into the host; no
separate lmcode managed assembly or .NET runtime installation is needed by users.

`THIRD_PARTY_NOTICES`, `src/LmAzure/Resources/licenses/` and `--notices` retain
Azure, .NET, Arrow, OpenSSL/zlib and SDK notices. Apache.Arrow 22.1.0 is transitive;
its Apache-2.0 license and upstream NOTICE are embedded. GCC runtime components
use the GCC Runtime Library Exception; its license and exception are embedded.
Do not remove embedded notices or alter package/SDK pins without rebuilding and
checking NativeAOT/trimming warnings, schema agreement and the DSL budget cases.
