# Local simulator mode

`lmazure --simulator` uses the Azure Blob and Queue SDKs against a local emulator.
It never enters the live Azure session, discovers credentials, or sends ARM calls.
The endpoint must be an HTTP origin at the literal address `127.0.0.1`; redirects
and proxies are disabled. Existing public Azure endpoint and live-write guards
remain unchanged.

```bash
export LMAZURE_SIM_ENDPOINT=http://127.0.0.1:4577
export LMAZURE_SIM_ACCOUNT=devstoreaccount1
# Set a dummy base64 storage key for real SDK SharedKey signing:
export LMAZURE_SIM_KEY=YWp1c3QtbG9jYWwtZGV2ZWxvcG1lbnQta2V5IQ==
lmazure --simulator container-create local-container
printf 'hello' > input.txt
lmazure --simulator blob-put local-container hello.txt input.txt
lmazure --simulator blob-get local-container hello.txt output.txt
lmazure --simulator queue-create local-queue
lmazure --simulator queue-send local-queue 'hello queue'
lmazure --simulator queue-receive local-queue
```

Configure the same dummy key in the emulator. Alternatively, unset the key and
set `LMAZURE_SIM_TOKEN` to an emulator-configured opaque token. This uses a custom
SDK policy to send the local test token over loopback HTTP; it is not an Entra
credential and never invokes DefaultAzureCredential. With neither setting, the
SDK sends anonymous requests (useful for testing authentication rejection).
Setting both credentials is an error.

Operations and positional arguments:

| Operation | Arguments |
| --- | --- |
| container-create, container-delete | container |
| blob-put, blob-get | container blob file |
| queue-create, queue-delete | queue |
| queue-send | queue message |
| queue-peek, queue-receive | queue |
| queue-delete-message | queue message-id pop-receipt |

Success returns a JSON `{"ok":true}` envelope and exit 0; queue reads may include
`message`, `id`, and `receipt`. Invalid configuration returns exit 2. Service and
transport failures return exit 3. Recognized Azure errors preserve `error` and
HTTP `status`, including AuthenticationFailed and AuthorizationPermissionMismatch.
Exception text and response bodies are never printed, protecting credentials.

This explicit local command permits local storage mutations without the cloud
write gate. It does not weaken cloud endpoint validation or permit remote hosts.
The host VM is responsible for restricting untrusted local users and processes.
