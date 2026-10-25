# lmazure durable memory

`lmazure` is a typed Azure SDK host for lmscript. The implementation uses
Azure.ResourceManager generic resources for read-only inventory and the Azure
Storage Blobs and Queues SDKs for tightly scoped, ledger-owned test resources.
The public API is exposed through `AzureSession`, `AzureRun`, and
`ResourceTable` in `src/LmAzure/Cloud`.

The default execution path is fake-only. `AzureSession.Mock()` uses the real
Azure SDK request pipeline with an in-process HTTP handler. It never discovers
credentials or opens a network connection. `AzureSession.Live()` requires
`LMAZURE_LIVE=1`, a subscription ID, and a region; writes additionally require
`LMAZURE_ALLOW_WRITES=1`.

Run ledgers are created exclusively, bind the subscription and storage endpoint
configuration, and contain only prefixed resources claimed after an HTTP 201
creation response. Cleanup, deletion, and sweeping require the ledger entry,
the `lmazure-` prefix, and matching `lmazure_test` metadata. Sweeps are dry by
default and retain failed or mismatched entries.

The current verification evidence is recorded in [01-verification.md](01-verification.md).
The pinned SDK and legal inventory are recorded in [02-sdk-and-legal.md](02-sdk-and-legal.md).
Build commands and runtime constraints are recorded in [03-build-and-runtime.md](03-build-and-runtime.md).

Release and CI status remains pending until the five platform NativeAOT matrix,
gallery execution, and release artifact checks complete. No live Azure subscription
test has been performed.
