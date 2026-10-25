# Build and runtime decisions

Install the exact SDK in `global.json` (.NET SDK 10.0.402). The .NET 8 lmcode-csharp
sources are compiled directly into the .NET 10 host; a separate managed SDK
assembly is not shipped. `scripts/sync-sdk.py --ensure` verifies the archive hash,
every source file, and the local VM allocation-checkpoint patch. The patch calls
`RuntimeBudget.Check()` every 1024 VM instructions. Changing the SDK pin requires
checking that the patch still applies and running the DSL runaway cases.

```sh
bash test.sh
bash scripts/build-native.sh
python3 scripts/test-cli.py artifacts/native/lmazure
LMAZURE_LMSCRIPT=lmscript python3 scripts/test-dsl.py artifacts/native/lmazure
PATH="$PWD/artifacts/native:$PATH" LMAZURE_LMSCRIPT=lmscript python3 scripts/test-examples.py
```

Use compiler `@lmctl-ai/lmctl@0.2.420` for the CI compatibility checks. All provider
operations in these commands use fake transports. `test.sh` reports an explicit
gallery execution skip when `LMAZURE_LMSCRIPT` is absent; the native CI stage sets
it and executes every example in its own empty directory.

The default application allocation budget is 4096 MiB. A positive whole
`LMAZURE_MAX_MEMORY_MB` selects a smaller budget; invalid values exit 2. A VM
budget error exits 3 and names that variable. .NET 10 embeds a 3 GiB managed heap
cap and a 4 GiB GC region reservation. A child process establishes a smaller heap
cap before runtime startup when requested. These are managed allocation and heap
limits; runtime/native bookkeeping contributes additional process memory.

.NET 8's NativeAOT GC reserved approximately 15 GiB before Main and failed the
Linux `ulimit -v 6291456` probe. .NET 10's embedded region setting starts and runs
the default-budget runaway test under that 6 GiB virtual-memory limit without a
user-supplied GC environment variable. CI also checks a 16 MiB application budget
on every platform.

Linux builds use Ubuntu 22.04, static OpenSSL and zlib, and a dynamic system glibc.
`check-linux-linkage.py` rejects other DT_NEEDED libraries and glibc symbols newer
than 2.35. The local x64 build's maximum required glibc symbol version was 2.34.
The executable needs no .NET runtime installation. macOS and Windows use system
crypto APIs. Do not replace the nonempty publish-options array with an empty one:
macOS system Bash 3 rejects an empty array expansion under `set -u`.

The 72 examples generate CSV data and 900×560 PNG images through lmplot/lmdraw.
`scripts/render-examples.py` refreshes `docs/images`. Golden assertions cover mock
age, exposure, encryption and ownership-tag totals. Boolean CSV cells are typed
booleans in lmscript, not strings. All gallery calls explicitly use `mock()`.

Live validation is deliberately outstanding. Follow `docs/LIVE_TESTING.md` only
after the user approves testing their own subscription.
