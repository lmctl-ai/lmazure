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

### Finding 3 — Composite Receipt Token, Message Body Preservation, & Pop-Receipt Rotation
- **Azure Queue REST contract:** Azure `DeleteMessage` and `UpdateMessage` require both `MessageId` and `PopReceipt`. Every `UpdateMessage` rotates the `PopReceipt` (`x-ms-popreceipt` response header).
- **Official Azure REST API vs azqueue SDK & azuresim quirk:**
  - Official Azure Storage REST API states: when updating visibility timeout only, the request body should be empty with `Content-Length: 0`.
  - In `azuresim` (Floci-AZ 0.15.0), `PUT /<account>-queue/<queue>/messages/<id>` with an empty body fails with `HTTP 500 Internal Server Error` (Floci-AZ's XML parser fails on empty input).
  - When sent with `<QueueMessage><MessageText>`, `azuresim` succeeds with `204 No Content` and returns `x-ms-popreceipt`. However, if `<MessageText>` is empty, `azuresim` overwrites the message text with empty string.
  - To prevent message corruption across visibility updates and nacks, `lmazure` encodes `(MessageId, PopReceipt, WireBase64)` inside the composite receipt token (`base64url(MessageId + "\n" + PopReceipt + "\n" + WireBase64)`). On `queue_nack` and visibility-0 `queue_receive`, the existing payload is echoed back into `UpdateMessage`, preserving the exact body and returning the rotated receipt.

### Finding 4 — Missing Queue (`NOT_FOUND`) vs Invalid Receipt Precedence (M5)
- **Spec requirement:** When `queue_ack` or `queue_nack` is given an invalid/malformed non-empty receipt, missing queue must return `ErrorCode.NOT_FOUND` before reporting `ErrorCode.INVALID_ARGUMENT`.
- **Implementation in `lmazure`:** If a non-empty receipt fails `decodeAzureReceipt`, `lmazure` probes `queueClient.GetProperties()`. If the queue is not found, it returns `ErrorCode.NOT_FOUND`; only if the queue exists does it return `ErrorCode.INVALID_ARGUMENT`.

### Finding 5 — Binary Queue Payload Encoding & 48 KiB Portable Limit
- **Limit derivation:** Azure Queue Storage restricts `<MessageText>` to 64 KiB (65,536 bytes) of XML. With Base64 encoding (4/3 expansion), the maximum raw binary payload is `49,152` bytes (`48 KiB`).
- **Implementation in `lmazure`:** Enforces `len(body) <= 49152` pre-flight, and transparently handles the `__lmcloud_empty__` sentinel and `__lmcloud_esc__:` escaping.

### Finding 6 — `lmcode.RegisterTesting` vs `print(bool|float|int)` Overloads
- **Parity with `schema/lmcloud.dsl.json`:** `host.Register` registers `print(bool)`, `print(float)`, and `print(int)` in addition to `lmcode.RegisterTesting`'s `print(string)` so host signatures are 100% byte-identical to `lmscript --lmcode-signatures schema/lmcloud.dsl.json`.

### Finding 7 — Azuresim Container DELETE Returns 202 on Non-Existent Container
- **Spec requirement:** `bucket_delete` on an already-deleted or never-created container must return `false` with `ErrorCode.OK`.
- **Azuresim behavior:** `DELETE /devstoreaccount1/<container>?restype=container` returns `HTTP 202 Accepted` even when the container does not exist.
- **Implementation in `lmazure`:** `BucketDelete` executes a pre-flight probe using `ContainerClient.GetProperties` (HEAD). If it returns `404 ContainerNotFound`, `BucketDelete` immediately returns `false, nil` without issuing `DeleteContainer`.

### Finding 8 — Azuresim Queue DELETE Returns 204 on Non-Existent Queue
- **Spec requirement:** `queue_delete` on an already-deleted or never-created queue must return `false` with `ErrorCode.OK`.
- **Azuresim behavior:** `DELETE /devstoreaccount1-queue/<queue>` returns `HTTP 204 No Content` even when the queue does not exist.
- **Implementation in `lmazure`:** `QueueDelete` probes `QueueClient.GetProperties` (GET metadata) first. If it returns `404 QueueNotFound`, `QueueDelete` returns `false, nil`.

### Finding 9 — Queue Create Idempotency & SDK Status Code Suppression
- **Spec requirement:** `queue_create` returns `true` if newly created, `false` if already existed.
- **Azure REST & azqueue SDK behavior:** Azure Queue REST `PUT /<queue>` returns `201 Created` if newly created, or `204 No Content` if already existed. However, the Azure Go SDK method `QueueClient.Create` suppresses the response status code, treating both 201 and 204 as successful (`err == nil`) and returning an opaque `QueueClientCreateResponse` with only headers (`Date`, `RequestID`, `Version`).
- **Implementation in `lmazure`:** `QueueCreate` probes `QueueClient.GetProperties` before attempting creation. If the queue exists (200 OK), it returns `false, nil`. If it does not exist (404), it proceeds to call `Create` and returns `true, nil`.

### Finding 10 — Non-TLS HTTP Credentials in Official Azure SDK (`azcore`)
- **SDK behavior:** The Azure Go SDK `azcore` library includes `runtime.checkHTTPSForAuth`, which rejects sending credentials over plain HTTP with `"authorized requests are not permitted for non-TLS protected (https) endpoints"` unless `InsecureAllowCredentialWithHTTP: true` is explicitly configured.
- **Implementation in `lmazure`:** Since `lmazure` operates in loopback/simulator mode, all client constructors (`createBlobClient`, `createQueueServiceClient`, `createQueueClient`) configure `ClientOptions.InsecureAllowCredentialWithHTTP = true`.

### Finding 11 — Safe Nil-Receiver Handlers for `abort_on_error(false)`
- **Behavior:** When `cloud.set_abort_on_error(false)` is enabled and an operation fails (e.g., `object_head` on a policy-denied container under `--auth enforce`), the `_lmcode_` runtime assigns `None` (`nil` object) to the result variable. Subsequent method calls on that variable (such as `h.exists()`) invoke the registered Go method with `self == nil`.
- **Implementation in `lmazure`:** All object accessor methods in `internal/host/register.go` (`ObjectMeta`, `ObjectSummary`, `ObjectListPage`, `QueueMessage`, `QueueMessageList`, `CloudError`) defensively check `if self == nil` and return safe default values (e.g., `exists() -> false`) rather than nil-pointer panicking.

---

## 12. Migration onto `lmcloud/hostkit` v2 (`72a891b`)

### 12.1 Architecture & Deleted Duplication
- Removed duplicated provider-neutral packages (`internal/invoke/`, `internal/host/`, `internal/safety/`, `schema/embed.go`).
- Added `internal/azure` (`adapter.go`, `classify.go`, `remote.go`, `storage.go`, `queue.go`, `azure_test.go`) implementing `hostkit.ObjectStore`, `hostkit.Queue`, `hostkit.QueueCapsProvider`, `hostkit.BucketCreationTimer`, and `hostkit.QueueCreationTimer`.
- `cmd/lmazure/main.go` uses `cloudhost.Program.Main` directly with `Hooks: invoke.Hooks{BeforeRun: membudget.ResetRun, StartWatchdog: membudget.StartWatchdog}`, `Init: membudget.InitFromEnv`, `Help`, `Notices`, and `Commands["--sweep"]`.
- `Adapter` routes per-call through `hostkit.Router` + `hostkit.EndpointRoutes`:
  - Offline (`""` and `LMAZURE_LIVE` unset): `fakeadapter.New` (`QueueCaps{LongPoll: false, TextOnlyWire: true}`).
  - Loopback simulator (`azuresim`): `remote` (`QueueCaps{LongPoll: false, TextOnlyWire: false}`).
  - Live (`LMAZURE_LIVE=1` or non-loopback endpoint): explicit `UNAUTHENTICATED` (`LiveCredentialsNotImplemented`) until `DefaultAzureCredential` is wired.

### 12.2 Spec H1 Strict Two-Part Receipt Token
- `EncodeReceipt(messageID, popReceipt)` encodes strictly `base64url(messageID + "\n" + popReceipt)` (2 parts only; `MessageText` is never embedded in the receipt token).
- `DecodeReceipt` rejects any token that does not decode to exactly two non-empty lines (`messageID` and `popReceipt`).
- For visibility-only updates (`Receive` with `vis=0` and `Nack`), `visibilityOnlyUpdatePolicy` (an `azcore` `PerCallPolicy` that runs before `SharedKeyCredential` signing) rewrites empty-message-text `<QueueMessage><MessageText></MessageText></QueueMessage>` payloads to `<QueueMessage></QueueMessage>` so `azuresim` (`7903c9c`+) and Azure Queue REST preserve the existing message body.

### 12.3 Pre-`7903c9c` `azuresim` Compatibility Option (`LMAZURE_COMPAT_PROBE_EXISTENCE`)
- Commit `7903c9c` on `/home/ubuntu/cloud/azuresim` (`branch fix-azure-semantics`) fixes `deleteContainer`, `listBlobs`, `getBlob`, `deleteBlob`, `getBlobMetadata`, and `deleteQueue` on missing containers/queues to return `404 ContainerNotFound` / `404 QueueNotFound`, and fixes `updateMessage` to preserve message content when `<MessageText>` is omitted.
- Because the cached prebuilt binary in `lmcloud-sims` (`v0.2.0`) predates `7903c9c`, `Options.CompatProbeMissingDelete` (env `LMAZURE_COMPAT_PROBE_EXISTENCE`, default `1`) isolates the pre-`7903c9c` existence probes and in-process `UpdateMessage` body echo.
- Setting `LMAZURE_COMPAT_PROBE_EXISTENCE=0` disables all compatibility probes against `azuresim` `7903c9c`+.
- **Removal condition:** Delete `CompatProbeMissingDelete` / `LMAZURE_COMPAT_PROBE_EXISTENCE` once an `azuresim` release containing commit `7903c9c` is published and pinned by `lmcloud-sims`.

### 12.4 `hostkit` v2 Gap Verification
1. **M5 Receipt Precedence (`hostkit.ReceiptPrecedence`):** Verified fixed in `hostkit` v2. `hostkit.Receipt(r)` only rejects `r == ""` preflight; non-empty malformed receipts reach `Adapter.Ack`/`Nack`, which return `INVALID_ARGUMENT`, and `hostkit.ReceiptPrecedence` probes `LookupQueue` so a missing queue takes `NOT_FOUND` precedence over `INVALID_ARGUMENT`.
2. **`ReadBounded` / `NilIfTyped`:** `hostkit` v2 added `NilIfTyped(err)` and `guardStore`/`guardQueue` wrappers around `ObjectStore`/`Queue` in `cloudhost.New`, preventing typed-nil `*CloudError` values returned across the adapter boundary from being treated as errors. Inside adapters, `hostkit.ReadBounded` still returns `([]byte, *CloudError)`, so callers should bind the error to a concrete `*hostkit.CloudError` variable before returning `error`.
3. **Retry Idempotency (`RetryPolicy.Do(ctx, idempotent, fn)`):** Verified fixed in `hostkit` v2. `retryTransport` passes `idempotent = req.Method != http.MethodPost`, so idempotent operations (`GET`, `HEAD`, `PUT`, `DELETE`) retry transient `500/502/503/504` up to 3 attempts while non-idempotent `Send` (`POST /messages`) does not retry on `5xx`.
