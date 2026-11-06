# lmazure recipes against azuresim

Runnable lmscript recipes that drive the real `lmazure` hosts against the local
[azuresim](https://github.com/lmctl-ai/azuresim) emulator on `127.0.0.1`. Nothing
here contacts Azure: the released host's `--simulator` command only accepts a literal
loopback origin and never enters the live ARM session, the Go lmcloud build takes a
loopback `LMCLOUD_ENDPOINT`, and no Entra credential is ever discovered. Every recipe
creates `lmazure-sim-`prefixed containers and queues in the `devstoreaccount1`
account, asserts what the emulator did, writes a JSON report under
`data/sim/<recipe>/` and ends with `PASS <recipe>`.

Two host surfaces are used:

- **lmazure --simulator** (the released .NET host): real Azure Blob and Queue SDK
  operations with SharedKey or dummy Bearer credentials, one JSON envelope per call.
- **lmazure cloud** (`cmd/lmazure`, the Go build serving the shared
  [lmcloud](https://github.com/lmctl-ai/lmcloud) schema): portable container, blob
  and queue operations plus `last_error()` classification used by the permissions recipe.

## Start the emulator

```bash
azuresim serve                                                 # open mode, :4577
lmscript recipes/sim/auth_enforced_denial.lms --prepare        # writes data/sim/auth_enforced_denial/azuresim-auth.json
azuresim serve --port 4578 --auth enforce --auth-config data/sim/auth_enforced_denial/azuresim-auth.json
```

## Run

```bash
export PATH="$PWD/artifacts/native:$PATH"       # or wherever the lmazure release binary lives
go build -o build/sim/lmazure-cloud ./cmd/lmazure
lmscript recipes/sim/static_site_container.lms --cloud-host build/sim/lmazure-cloud
lmscript recipes/sim/blob_site_cli.lms
lmscript recipes/sim/auth_enforced_cli.lms
```

Common flags: `--endpoint URL` (default `AZURESIM_ENDPOINT` or `http://127.0.0.1:4577`),
`--host PATH` (released host for `--simulator`, default `LMAZURE_BIN` or `lmazure`),
`--cloud-host PATH` (Go build, default `LMAZURE_CLOUD_BIN` or `lmazure`),
`--account NAME`, `--key BASE64` (the public development key by default), `--run ID`,
`--out DIR`, `--help`.

`python3 scripts/test-sim-recipes.py` starts azuresim in open and enforce mode on free
ports, runs every recipe in a temporary directory and tears everything down. It takes
`--azuresim`, `--lmazure`, `--cloud-host`, `--lmscript` (or `AZURESIM_BIN`,
`LMAZURE_BIN`, `LMAZURE_CLOUD_BIN`, `LMSCRIPT_BIN`) and `--only NAME`.

## Catalog

| Recipe | Host | azuresim services | What it proves |
| --- | --- | --- | --- |
| `static_site_container.lms` | lmazure cloud | Blob Storage | Publish a five-file static site with content types and cache metadata, list by prefix, republish with a new ETag, tear down |
| `queue_worker.lms` | lmazure cloud | Queue Storage | Enqueue site jobs, drain with ack, nack one for redelivery (pop receipts rotate), end empty, delete the queue |
| `auth_enforced_denial.lms` | lmazure cloud | SharedKey/Bearer policy engine (`--auth enforce`) | Author the account-key/token policy file with `_json_` (Bearer administrator, SharedKey deployer); the deployer's own container and queue work, blob writes and deletes on a protected container and a protected queue fail `PERMISSION_DENIED`/403, a wrong key fails `UNAUTHENTICATED`; SKIPs on an open-mode emulator |
| `auth_enforced_cli.lms` | lmazure --simulator | same policy | The same boundary through the real SDK: protected delete and upload exit 3 with HTTP 403 `AuthorizationPermissionMismatch`, a wrong key exits 3 with `AuthenticationFailed` |
| `blob_site_cli.lms` | lmazure --simulator | Blob Storage | Container, HTML/CSS/JS uploads, byte-exact downloads, container delete |
| `queue_pipeline_cli.lms` | lmazure --simulator | Queue Storage | Queue, JSON jobs, peek, receive and delete by pop receipt until empty, queue delete |
| `inventory_snapshot.lms` | lmazure cloud | Blob and Queue Storage | Read-only: every container with blob count and bytes, every queue, as JSON plus CSV |

## What azuresim cannot cover

azuresim 0.2.x (Floci-AZ 0.15) certifies Blob and Queue Storage. Its ARM routes
(`subscriptions`, resource groups, `Microsoft.Compute/virtualMachines`,
`Microsoft.ContainerService/managedClusters`, `Microsoft.KeyVault/vaults`) answer
with empty lists, but the released lmazure ARM client is pinned to
`management.azure.com` and its `live()` session needs `DefaultAzureCredential`, so
there is no lmazure path to those control planes on the emulator. Virtual machines,
AKS, App Service and Key Vault therefore have no recipes here; the lmazure gallery
covers their metadata with the in-process mock instead.

## Emulator findings

Recorded while verifying against azuresim v0.2.15 and lmazure v0.1.5:

- In `--auth enforce` mode azuresim answers every blob write whose name contains a `/`
  (for example `audit/2026.log`) with `AuthorizationPermissionMismatch`, for the Bearer
  administrator as well as the SharedKey deployer; open mode accepts such names. The
  permissions recipes therefore use flat blob names.
- Queue Storage on azuresim returns the newest message first for both peek and
  receive; the CLI recipe asserts the processed set, not the order.
- `lmazure --simulator` refuses to run when both `LMAZURE_SIM_KEY` and
  `LMAZURE_SIM_TOKEN` are present, even empty (`AmbiguousSimulatorCredential`);
  the helpers unset the unused one. The Go build prefers the key when both are set.
- A SharedKey principal is the account name, so the policy distinguishes the
  deployer (`devstoreaccount1`) from the administrator by a Bearer token mapping.
