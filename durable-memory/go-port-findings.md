# Go Port Findings (`lmazure` on `lmcode-go` + `lmcloud`)

## 1. Coexistence of Shared `lmcloud` Schema and Legacy `lmazure` Surface

Following the `lmcode` multi-schema subcommand pattern established in `lmoci`:

- **`lmazure cloud --lmcode FILE | --lmcode-schema [--json] | --lmcode-signatures [--without-std]`**:
  Selects the provider-neutral **`lmcloud`** schema (`schema/lmcloud.dsl.json`, context `cloud: Cloud`), identical across `lmaws`, `lmazure`, `lmgcp`, and `lmoci`.
- **Default top-level `lmazure ...`**:
  During Phase A–C (before legacy Azure-specific C# methods are ported to Go), top-level `lmazure --lmcode FILE | --lmcode-schema [--json] | --lmcode-signatures [--without-std]` also serves the shared `lmcloud` schema so `/home/ubuntu/cloud/lmcloud/tests/run-conformance.sh` works directly without wrapper scripts.

---

## 2. Azure Semantics & Conformance Findings

### Finding 1 — Azure Container Delete on Non-Empty Containers
- **Azure REST / SDK behavior:** Azure Blob Storage REST `DELETE /<account>/<container>?restype=container` automatically deletes non-empty containers and all contained blobs.
- **`lmcloud` Spec requirement:** `bucket_delete(bucket)` must fail with `ErrorCode.CONFLICT` (HTTP 409) if the bucket is non-empty.
- **Implementation in `lmazure`:** In `BucketDelete`, `lmazure` performs a pre-flight probe via `NewListBlobsFlatPager` with `MaxResults=1`. If any blob exists, `lmazure` immediately returns `ErrorCode.CONFLICT` (`ContainerNotEmpty`) without deleting the container.

### Finding 2 — Azuresim Loopback Routing: Blob vs Queue
- **Live Azure vs Emulator:**
  - Live Azure routes Blob at `https://<account>.blob.core.windows.net` and Queue at `https://<account>.queue.core.windows.net`.
  - On `azuresim` loopback path-style (`http://127.0.0.1:4577`), `azuresim` routes Blob Storage at `/<account>/<container>` and Queue Storage at **`/<account>-queue/<queue>`** (e.g., `/devstoreaccount1-queue/<queue>`).
- **Implementation in `lmazure`:** `formatBlobServiceURL` ensures path has `/<account>` suffix, while `formatQueueServiceURL` automatically formats `/<account>-queue` on loopback hosts when `.queue.` is absent from the hostname.

### Finding 3 — Composite Receipt Token & Pop-Receipt Rotation
- **Azure Queue REST contract:** Azure `DeleteMessage` and `UpdateMessage` require both `MessageId` and `PopReceipt`. Every `UpdateMessage` rotates the `PopReceipt` (`x-ms-popreceipt` response header).
- **Implementation in `lmazure`:**
  - Receipts are encoded as `base64url(MessageId + "\n" + PopReceipt)` without storing `MessageText`.
  - `queue_nack` updates message visibility and returns the rotated composite receipt token.
  - When a caller requests `visibility_timeout == 0` on `queue_receive`, `lmazure` dequeues with `visibilitytimeout=1` (since Azure requires `1 <= visibilitytimeout <= 604800` on dequeue) and immediately issues `UpdateMessage(0)` to obtain the rotated `PopReceipt` for immediate redelivery.

### Finding 4 — Missing Queue (`NOT_FOUND`) vs Invalid Receipt Precedence (M5)
- **Spec requirement:** When `queue_ack` or `queue_nack` is given an invalid/malformed non-empty receipt, missing queue must return `ErrorCode.NOT_FOUND` before reporting `ErrorCode.INVALID_ARGUMENT`.
- **Implementation in `lmazure`:** If a non-empty receipt fails `decodeAzureReceipt`, `lmazure` probes `queueClient.GetProperties()`. If the queue is not found, it returns `ErrorCode.NOT_FOUND`; only if the queue exists does it return `ErrorCode.INVALID_ARGUMENT`.

### Finding 5 — Binary Queue Payload Encoding & 48 KiB Portable Limit
- **Limit derivation:** Azure Queue Storage restricts `<MessageText>` to 64 KiB (65,536 bytes) of XML. With Base64 encoding (4/3 expansion), the maximum raw binary payload is `49,152` bytes (`48 KiB`).
- **Implementation in `lmazure`:** Enforces `len(body) <= 49152` pre-flight, and transparently handles the `__lmcloud_empty__` sentinel and `__lmcloud_esc__:` escaping.

### Finding 6 — `lmcode.RegisterTesting` vs `print(bool|float|int)` Overloads
- **Parity with `schema/lmcloud.dsl.json`:** `host.Register` registers `print(bool)`, `print(float)`, and `print(int)` in addition to `lmcode.RegisterTesting`'s `print(string)` so host signatures are 100% byte-identical to `lmscript --lmcode-signatures schema/lmcloud.dsl.json`.
