# User-run live Azure test

No real Azure subscription has been accessed during development. All 27 gallery examples explicitly use `mock()` and remain fake regardless of environment flags. Run the following live steps yourself only after approving live testing.

## 1. Download and verify (Linux x64)

Use an empty directory. Other platforms use the matching `lmazure-macos-arm64`, `lmazure-macos-x64`, `lmazure-linux-arm64`, or `lmazure-windows-x64.exe` release asset.

```sh
mkdir lmazure-live-check
cd lmazure-live-check
gh release download v0.1.0 --repo lmctl-ai/lmazure --pattern lmazure-linux-x64 --pattern SHA256SUMS
sha256sum --ignore-missing --check SHA256SUMS
chmod +x lmazure-linux-x64
./lmazure-linux-x64 --version
./lmazure-linux-x64 --self-test
```

The self-test uses in-process fake transports and needs no credentials. Native binaries need no .NET runtime. To build instead, install the SDK pinned in global.json, run `scripts/build-native.sh` from the checkout, and use `./artifacts/native/lmazure` below.

## 2. Read-only test

Use your existing Azure CLI profile. Sign in only if needed; do not create identities, credentials, keys or role assignments for this test. The configured identity needs Reader access to the intended subscription. The commands capture the subscription ID without printing it.

```sh
az login --output none
export LMAZURE_SUBSCRIPTION_ID="$(az account show --query id --output tsv)"
export LMAZURE_REGION=eastus
export LMAZURE_LIVE=1
export AZURE_TOKEN_CREDENTIALS=AzureCliCredential
unset LMAZURE_ALLOW_WRITES
./lmazure-linux-x64 --live --inventory resources > live_inventory.csv
```

Inspect `live_inventory.csv` locally. Resource identifiers are masked; resource names and types remain visible. Generic ARM listings do not establish public-access or encryption policy, so unavailable values are unknown. Do not treat these fields as a security attestation. No secret values, keys, connection strings or private keys are requested.

Use `--live --inventory compute`, `storage`, `network`, `identity`, `database`, `queues`, `serverless`, `monitoring`, `dns`, `vaults` or `containers` for curated ARM metadata. Child resources not exposed by ARM's subscription list are not synthesized. This command does not list blob contents, queue messages, vault secret values or database connection strings.

## 3. Optional owned-resource write test

Perform this only after reviewing the read-only result and explicitly approving writes. Use an existing storage account and your already-configured identity with Storage Blob Data Contributor and Storage Queue Data Contributor permissions. The host does not grant permissions. No storage account or resource group is created.

```sh
export LMAZURE_STORAGE_BLOB_URL='https://<existing-account>.blob.core.windows.net'
export LMAZURE_STORAGE_QUEUE_URL='https://<existing-account>.queue.core.windows.net'
export LMAZURE_ALLOW_WRITES=1
```

Keep account URLs in your protected shell configuration rather than screenshots. SAS URLs and connection strings are rejected. Create a new local `live-write.lms` with the following contents, using a new ledger path for each run:

```lmscript
mkdir("build");
azurehost = _dsl_ cli="lmazure" install="github:lmctl-ai/lmazure@latest";
let azure = live();
let run = azure.begin_run("build/lmazure-user-smoke.ledger.json");
let queue = run.queue("smoke");
let container = run.container("smoke");
queue.csv("build/live-queue.csv");
container.csv("build/live-container.csv");
run.cleanup();
end _dsl_
savedsl(azurehost,"build/live-write.dslb");
r = system ${dsl_cli(azurehost)} --dsl build/live-write.dslb;
assert(r.code == 0);
```

Execute it with `lmscript live-write.lms` or `lmctl autorun live-write.lms` only after approving writes. The script's `install=@latest` binding installs the host if it is absent from PATH. Each resource has a generated `lmazure-<run-id>-...` name and `lmazure_test=<run-id>` metadata. The host claims only HTTP 201 creations, checks prefix, metadata and ledger membership before mutation, and cleans up on normal or exceptional VM exit. Existing resources are never claimed.

After success, the ledger should be removed. A failed cleanup retains an account-bound ledger for recovery; never delete that file until all entries have been resolved. Abrupt process/OS termination can also leave resources requiring recovery.

## 4. Recover a retained ledger

Call `azure.sweep("path/to/retained.ledger.json", 1.0)` from an explicit `live()` DSL block and export its table to review candidates at least one hour old. The default sweep is read-only. After reviewing the exact ledger and account endpoints, replace that call with `sweep_apply` only with `LMAZURE_ALLOW_WRITES=1`. Only ledger entries with the matching run prefix, ownership metadata, account endpoints and age qualify. Failed entries stay in the ledger. There is no subscription-wide prefix deletion.

Unset `LMAZURE_ALLOW_WRITES` and `LMAZURE_LIVE` when finished. Never put credentials, access tokens, secret values or private keys into scripts, command arguments, CSV output, images or logs.
