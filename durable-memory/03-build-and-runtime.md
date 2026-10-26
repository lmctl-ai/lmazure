# Architecture, build and runtime

`Program.cs` handles CLI/schema/bytecode execution. `Host.cs` binds 25 signatures
(schema 2) to `AzureSession`, `AzureRun` and `ResourceTable`. lmcode validates the
schema hash before execution; preserve registry/schema signature agreement.
`AzureSession.Mock()` uses real Azure SDK pipelines with in-process HTTP fakes;
`Live()` uses DefaultAzureCredential with workload identity excluded.
Run/session handles are disposed in the VM runner's finally block.

NativeAOT and trimming warnings are errors. Linux links OpenSSL/zlib statically
and system glibc dynamically; the supported baseline is glibc 2.35. The linkage
checker rejects other DT_NEEDED libraries and newer glibc symbols. macOS/Windows
use system crypto. All five release executables are implemented; Windows is not
a stub. Native publish options must remain compatible with macOS Bash 3.

`LMAZURE_MAX_MEMORY_MB` defaults to 4096 MiB of cumulative managed allocation
since VM reset. Positive integer overrides are validated; less than 16 MiB fails
initialization. Embedded GC limits are a 3 GiB heap and 4 GiB region reservation;
startup relaunch configures the heap for smaller budgets. These are not RSS caps.
The SDK patch checks every 1024 VM instructions; runtime checks also bound execution.
Transport responses are limited to 8 MiB, inventories to 10,000 rows, owned runs
to 16 concurrent resources. Linux budget probes use a 6 GiB virtual-memory limit.

Install the pinned .NET SDK, Python 3 and `@lmctl-ai/lmctl@0.2.420`. Native Linux
also needs clang/CMake/build tools and static OpenSSL/zlib development archives.
Run from the repository root with bounded commands (all Azure operations fake):

```sh
timeout 900 bash test.sh
timeout 1300 bash scripts/build-native.sh
timeout 180 python3 scripts/test-cli.py artifacts/native/lmazure
timeout 300 env LMAZURE_TEST_ADDRESS_SPACE_LIMIT=1 LMAZURE_TEST_DEFAULT_BUDGET=1 python3 scripts/test-dsl.py artifacts/native/lmazure
timeout 900 env PATH="$PWD/artifacts/native:$PATH" LMAZURE_LMSCRIPT=lmscript python3 scripts/test-examples.py
```

Use `lmazure.exe` on Windows; omit Linux-only address-space/default-budget flags
elsewhere. A plain managed build is included in `test.sh`; NativeAOT uses
`scripts/build-native.sh`. Local build/tests are the gate, not remote CI status.
