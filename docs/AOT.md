# Azure SDK NativeAOT compatibility

The host targets .NET 10 and pins SDK 10.0.402 in global.json. The independent
lmcode-csharp source pin is recorded in sdk.lock; its .NET 8 source API is
compiled into this host without reflection or dynamic code generation.

| Direct package | Version | Reachable API exercised by fake tests |
| --- | --- | --- |
| Azure.Core | 1.63.0 | HTTP pipeline, response and error handling, TokenCredential |
| Azure.Identity | 1.21.0 | DefaultAzureCredential is compiled; fake credentials replace authentication in tests |
| Azure.ResourceManager | 1.14.0 | Subscription generic resource enumeration, pagination and model parsing |
| Azure.Storage.Blobs | 12.30.1 | Container creation, properties, metadata and deletion |
| Azure.Storage.Queues | 12.28.1 | Queue creation, properties, metadata and deletion |

Azure.Identity 1.21 is a type-forwarding facade to the identity types included
in Azure.Core. Package versions and SHA-512 content hashes, including all
transitive dependencies, are locked in packages.lock.json and deps.lock.

The project enables IsAotCompatible, TreatWarningsAsErrors and
IlcTreatWarningsAsErrors. CI publishes and executes NativeAOT on Linux x64/ARM64,
macOS x64/ARM64 and Windows x64. No blanket trimming-warning suppressions or
reflection-based JSON serializers are used. Host JSON is written explicitly
with Utf8JsonWriter, and the fake responses exercise the actual SDK models.

No real Azure authentication or subscription operation has been tested. The
compiled DefaultAzureCredential chain, actual RBAC grants and real service
behavior still require the user's opt-in live test. See LIVE_TESTING.md.

Linux uses NativeAOT's StaticOpenSslLinking option and an explicit static zlib
archive. The ELF check rejects any dynamic dependency outside glibc and any
glibc symbol version newer than 2.35. Invariant globalization avoids an ICU
runtime dependency. macOS and Windows use their system cryptographic APIs.

.NET 8 successfully compiled the SDK surface, but its GC reserved five times
the heap limit before Main and failed a 6 GiB virtual-address-space probe.
.NET 10 supports System.GC.RegionRange in embedded runtime configuration. The
host reserves 4 GiB and caps the managed heap at at most 3 GiB. Smaller memory
budgets select a smaller heap, with a 64 MiB minimum for runtime overhead.
Allocation checkpoints enforce the requested per-run budget independently.
