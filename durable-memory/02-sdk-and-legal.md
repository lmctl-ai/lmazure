# SDK, runtime, and legal record

Pinned package versions are:

| Package | Version | License |
| --- | --- | --- |
| Azure.Core | 1.63.0 | MIT |
| Azure.Identity | 1.21.0 | MIT |
| Azure.ResourceManager | 1.14.0 | MIT |
| Azure.Storage.Blobs | 12.30.1 | MIT |
| Azure.Storage.Queues | 12.28.1 | MIT |

The lmcode-csharp SDK is pinned by `sdk.lock` to commit
`e68575a5463fc18a18de3045586ad34cd3131fb3`.

Apache.Arrow 22.1.0 is transitive. Its upstream NOTICE is embedded at
`src/LmAzure/Resources/licenses/arrow-NOTICE.txt`, taken from
`apache/arrow-dotnet` commit `fcc16d879fe3618b2a6806851922c51beb1a3fd2`.
The Apache-2.0 license text is retained alongside it.

NativeAOT targets .NET 10. The configured runtime envelope is a 3 GiB managed
heap inside a 4 GiB reserved address space, with the default application
allocation budget documented as 4096 MiB. Linux permits dynamic system glibc;
other native dependencies are handled by the selected .NET toolchain. GCC
static runtime components are covered by the GCC Runtime Library Exception.
The product does not add GPL or AGPL code as a linked dependency.

The complete embedded .NET, cryptography, zlib, Azure SDK, Arrow, and SDK
notices are in `THIRD_PARTY_NOTICES` and `src/LmAzure/Resources/licenses`.
