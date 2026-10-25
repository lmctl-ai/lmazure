using Azure;
using Azure.Core;
using Azure.Core.Pipeline;
using Azure.Identity;
using Azure.ResourceManager;
using Azure.ResourceManager.Resources;
using Azure.Storage.Blobs;
using Azure.Storage.Queues;
using System.Net;
using System.Net.Http.Headers;
using System.Text;
using System.Text.Json;

namespace LmAzure.Cloud;

public sealed class AzureSession : IDisposable
{
    private static readonly IReadOnlyDictionary<string, string> Kinds = new Dictionary<string, string>(StringComparer.OrdinalIgnoreCase)
    {
        ["compute"] = "Microsoft.Compute/", ["storage"] = "Microsoft.Storage/storageAccounts", ["network"] = "Microsoft.Network/",
        ["identity"] = "Microsoft.ManagedIdentity/", ["database"] = "Microsoft.Sql/", ["queues"] = "Microsoft.Storage/storageAccounts/queueServices",
        ["serverless"] = "Microsoft.Web/", ["monitoring"] = "Microsoft.Insights/", ["dns"] = "Microsoft.Network/dns",
        ["vaults"] = "Microsoft.KeyVault/", ["containers"] = "Microsoft.ContainerService/"
    };
    private readonly ArmClient _arm;
    private readonly HttpPipelineTransport? _transport;
    private readonly string _subscription;
    private readonly string _region;
    private readonly bool _mock;
    private readonly string _blobEndpoint;
    private readonly string _queueEndpoint;
    private readonly HashSet<AzureRun> _runs = new();
    private bool _disposed;

    private AzureSession(ArmClient arm, string subscription, string region, bool mock, HttpPipelineTransport? transport, string blobEndpoint, string queueEndpoint)
    { _arm = arm; _subscription = subscription; _region = region; _mock = mock; _transport = transport; _blobEndpoint = blobEndpoint; _queueEndpoint = queueEndpoint; }
    public static AzureSession Mock() {
        return Mock(new FakeAzureHandler());
    }
    internal static AzureSession Mock(HttpMessageHandler handler) {
        var transport = new HttpClientTransport(new HttpClient(TransportSafety.Wrap(handler, ["blob.mock", "queue.mock", "management.azure.com"], false)) { Timeout = TimeSpan.FromSeconds(30) });
        var options = new ArmClientOptions { Transport = transport };
        options.Retry.MaxRetries = 0; options.Retry.NetworkTimeout = TimeSpan.FromSeconds(15);
        return new AzureSession(new ArmClient(new MockTokenCredential(), "00000000-0000-0000-0000-000000000000", options), "00000000-0000-0000-0000-000000000000", "eastus", true, transport, "https://blob.mock", "https://queue.mock");
    }
    public static AzureSession Live()
    {
        if (!string.Equals(Environment.GetEnvironmentVariable("LMAZURE_LIVE"), "1", StringComparison.Ordinal)) throw new AzureCloudException("set LMAZURE_LIVE=1 to enable live Azure access");
        var subscription = Required("LMAZURE_SUBSCRIPTION_ID"); var region = Required("LMAZURE_REGION");
        if (!Guid.TryParse(subscription, out _)) throw new AzureCloudException("invalid Azure subscription id");
        var armOptions = new ArmClientOptions(); armOptions.Retry.MaxRetries = 0; armOptions.Retry.NetworkTimeout = TimeSpan.FromSeconds(15);
        var blobEndpoint = OptionalStorageEndpoint("LMAZURE_STORAGE_BLOB_URL", "blob.core.windows.net"); var queueEndpoint = OptionalStorageEndpoint("LMAZURE_STORAGE_QUEUE_URL", "queue.core.windows.net");
        var handler = new HttpClientHandler { AllowAutoRedirect = false }; var hosts = new HashSet<string>(StringComparer.OrdinalIgnoreCase) { "management.azure.com" }; if (Uri.TryCreate(blobEndpoint, UriKind.Absolute, out var blobUri)) hosts.Add(blobUri.Host); if (Uri.TryCreate(queueEndpoint, UriKind.Absolute, out var queueUri)) hosts.Add(queueUri.Host);
        var transport = new HttpClientTransport(new HttpClient(TransportSafety.Wrap(handler, hosts, true)) { Timeout = TimeSpan.FromSeconds(30) }); armOptions.Transport = transport;
        return new AzureSession(new ArmClient(new DefaultAzureCredential(), subscription, armOptions), subscription, region, false, transport, blobEndpoint, queueEndpoint);
    }
    public ResourceTable Inventory(string kind)
    {
        Ensure();
        if (kind.Equals("resources", StringComparison.OrdinalIgnoreCase)) kind = "all";
        if (!string.Equals(kind, "all", StringComparison.OrdinalIgnoreCase) && !Kinds.ContainsKey(kind)) throw new AzureCloudException("unsupported Azure inventory kind");
        try {
            var subscription = _arm.GetSubscriptionResource(new ResourceIdentifier($"/subscriptions/{_subscription}"));
            using var timeout = new CancellationTokenSource(TimeSpan.FromSeconds(15));
            return InventoryAsync(subscription, kind, timeout.Token).GetAwaiter().GetResult();
        } catch (AzureCloudException) { throw; } catch (Exception ex) { throw new AzureCloudException("Azure inventory failed", ex); }
    }
    public AzureRun BeginRun(string ledgerPath) { Ensure(); var run = new AzureRun(this, ledgerPath, _mock); _runs.Add(run); return run; }
    public ResourceTable Sweep(string ledgerPath, double olderHours, bool apply)
    {
        Ensure();
        if (olderHours < 0 || double.IsNaN(olderHours) || double.IsInfinity(olderHours)) throw new AzureCloudException("olderHours must be nonnegative");
        if (apply) EnsureWrite();
        if (!File.Exists(ledgerPath)) return ResourceTable.Empty();
        try {
            byte[] ledgerBytes;
            using (var ledgerStream = new FileStream(ledgerPath, FileMode.Open, FileAccess.Read, FileShare.Read))
            {
                var ledgerLength = ledgerStream.Length;
                if (ledgerLength > 1024 * 1024) throw new AzureCloudException("ledger exceeds 1 MiB");
                ledgerBytes = new byte[(int)ledgerLength]; ledgerStream.ReadExactly(ledgerBytes);
                if (ledgerStream.ReadByte() != -1) throw new AzureCloudException("ledger changed while reading");
            }
            using var doc = JsonDocument.Parse(ledgerBytes, new JsonDocumentOptions { MaxDepth = 16 }); var run = doc.RootElement;
            var created = run.GetProperty("created_utc").GetDateTimeOffset();
            var runId = run.GetProperty("run_id").GetString() ?? "";
            if (runId.Length != 12 || runId.Any(c => !IsLowerHex(c)) || !run.TryGetProperty("subscription", out var subscription) || subscription.GetString() != _subscription || !run.TryGetProperty("blob_endpoint", out var blobEndpoint) || blobEndpoint.GetString() != _blobEndpoint || !run.TryGetProperty("queue_endpoint", out var queueEndpoint) || queueEndpoint.GetString() != _queueEndpoint) throw new AzureCloudException("ledger account binding failed");
            if (!run.TryGetProperty("owned", out var owned) || owned.ValueKind != JsonValueKind.Array || owned.GetArrayLength() > 64) throw new AzureCloudException("ledger owned resource limit exceeded");
            foreach (var item in owned.EnumerateArray())
            {
                if (item.ValueKind != JsonValueKind.Object || !item.TryGetProperty("name", out var rawName) || !item.TryGetProperty("kind", out var rawKind)) throw new AzureCloudException("ledger contains an unsafe resource");
                var name = rawName.GetString() ?? ""; var kind = rawKind.GetString() ?? "";
                if (kind is not ("blob" or "queue") || !SafeResourceName(name) || !name.StartsWith($"lmazure-{runId}-", StringComparison.Ordinal)) throw new AzureCloudException("ledger contains an unsafe resource");
            }
            if ((DateTimeOffset.UtcNow - created).TotalHours < olderHours) return ResourceTable.Empty();
            var rows = new List<object?[]>(); var remaining = new List<(string Kind, string Name)>(); var failures = new List<string>();
            foreach (var item in owned.EnumerateArray()) {
                var name = item.GetProperty("name").GetString() ?? ""; var kind = item.GetProperty("kind").GetString() ?? "";
                if (kind is not ("blob" or "queue") || !name.StartsWith($"lmazure-{runId}-", StringComparison.Ordinal)) throw new AzureCloudException("ledger contains an unsafe resource");
                if (!OwnsMetadataAsync(kind, name, runId).GetAwaiter().GetResult()) { remaining.Add((kind, name)); continue; }
                if (apply) { try { DeleteOwnedAsync(kind, name, runId).GetAwaiter().GetResult(); } catch (RequestFailedException ex) when (ex.Status == 404) { } catch { remaining.Add((kind, name)); failures.Add(name); continue; } }
                rows.Add(ResourceTable.Row(name, kind, _region, apply ? "deleted" : "candidate", 1, (DateTimeOffset.UtcNow - created).TotalHours, false, false, true, MaskId(name)));
            }
            if (apply) { if (remaining.Count == 0) File.Delete(ledgerPath); else WriteSweepLedger(ledgerPath, runId, created, remaining); if (failures.Count != 0) throw new AzureCloudException("Azure sweep left owned resources in the ledger"); }
            return new ResourceTable(rows);
        } catch (AzureCloudException) { throw; } catch (Exception ex) { throw new AzureCloudException("Azure sweep failed", ex); }
    }
    internal bool IsMock => _mock;
    internal string Region => _region;
    internal string Subscription => _subscription;
    internal string BlobEndpoint => _blobEndpoint;
    internal string QueueEndpoint => _queueEndpoint;
    internal BlobContainerClient BlobContainer(string name)
    {
        var endpoint = string.IsNullOrEmpty(_blobEndpoint) ? ValidateStorageEndpoint("LMAZURE_STORAGE_BLOB_URL", "blob.core.windows.net") : _blobEndpoint;
        if (string.IsNullOrEmpty(endpoint)) throw new AzureCloudException("Azure blob endpoint is not configured");
        var options = new BlobClientOptions(); options.Retry.MaxRetries = 0; options.Retry.NetworkTimeout = TimeSpan.FromSeconds(15); if (_transport is not null) options.Transport = _transport;
        return new BlobContainerClient(new Uri(endpoint.TrimEnd('/') + "/" + name), _mock ? new MockTokenCredential() : new DefaultAzureCredential(), options);
    }
    internal QueueClient Queue(string name)
    {
        var endpoint = string.IsNullOrEmpty(_queueEndpoint) ? ValidateStorageEndpoint("LMAZURE_STORAGE_QUEUE_URL", "queue.core.windows.net") : _queueEndpoint;
        if (string.IsNullOrEmpty(endpoint)) throw new AzureCloudException("Azure queue endpoint is not configured");
        var options = new QueueClientOptions(); options.Retry.MaxRetries = 0; options.Retry.NetworkTimeout = TimeSpan.FromSeconds(15); if (_transport is not null) options.Transport = _transport;
        return new QueueClient(new Uri(endpoint.TrimEnd('/') + "/" + name), _mock ? new MockTokenCredential() : new DefaultAzureCredential(), options);
    }
    internal void EnsureWrite() { if (!_mock && !string.Equals(Environment.GetEnvironmentVariable("LMAZURE_ALLOW_WRITES"), "1", StringComparison.Ordinal)) throw new AzureCloudException("set LMAZURE_ALLOW_WRITES=1 to enable Azure writes"); }
    private async Task<ResourceTable> InventoryAsync(SubscriptionResource subscription, string kind, CancellationToken cancellationToken)
    {
        var rows = new List<object?[]>(); var visited = 0;
        await foreach (var resource in subscription.GetGenericResourcesAsync(filter: null, expand: "createdTime,changedTime").WithCancellation(cancellationToken))
        {
            if (++visited > 10000) throw new AzureCloudException("Azure inventory exceeded the 10000-resource safety limit");
            var tags = resource.Data.Tags ?? new Dictionary<string, string>(); var type = resource.Id.ResourceType.ToString();
            if (!kind.Equals("all", StringComparison.OrdinalIgnoreCase) && !type.StartsWith(Kinds[kind], StringComparison.OrdinalIgnoreCase)) continue;
            var created = resource.Data.CreatedOn; var now = _mock ? new DateTimeOffset(2026, 1, 1, 0, 0, 0, TimeSpan.Zero) : DateTimeOffset.UtcNow; var age = created.HasValue ? Math.Max(0, (now - created.Value).TotalHours) : -1d;
            var status = "observed";
            rows.Add(ResourceTable.Row(resource.Id.Name, type, resource.Data.Location.ToString(), status, 1, age, _mock ? TagBool(tags, "public") : null, _mock ? TagBool(tags, "encrypted") : null, tags.ContainsKey("lmazure_test"), MaskId(resource.Id.ToString())));
        }
        return new ResourceTable(rows);
    }
    internal async Task<bool> OwnsMetadataAsync(string kind, string name, string runId)
    {
        if (!name.StartsWith("lmazure-", StringComparison.Ordinal)) return false;
        try {
            using var timeout = new CancellationTokenSource(TimeSpan.FromSeconds(15));
            if (kind == "blob") { var p = await BlobContainer(name).GetPropertiesAsync(cancellationToken: timeout.Token); return p.Value.Metadata.TryGetValue("lmazure_test", out var value) && value == runId; }
            var q = await Queue(name).GetPropertiesAsync(cancellationToken: timeout.Token); return q.Value.Metadata.TryGetValue("lmazure_test", out var queueValue) && queueValue == runId;
        } catch (RequestFailedException) { return false; }
    }
    private async Task DeleteOwnedAsync(string kind, string name, string runId)
    { if (!await OwnsMetadataAsync(kind, name, runId)) throw new AzureCloudException("sweeper ownership metadata check failed"); using var timeout = new CancellationTokenSource(TimeSpan.FromSeconds(15)); if (kind == "blob") await BlobContainer(name).DeleteIfExistsAsync(cancellationToken: timeout.Token); else await Queue(name).DeleteIfExistsAsync(cancellationToken: timeout.Token); }
    private static bool? TagBool(IDictionary<string, string> tags, string key)
    {
        if (!tags.TryGetValue(key, out var value)) return null;
        if (value == "1" || value.Equals("true", StringComparison.OrdinalIgnoreCase)) return true;
        if (value == "0" || value.Equals("false", StringComparison.OrdinalIgnoreCase)) return false;
        return null;
    }
    private static bool IsLowerHex(char value) => value is >= '0' and <= '9' or >= 'a' and <= 'f';
    private static bool SafeResourceName(string value) => value.Length is > 0 and <= 63 && value.All(c => c is >= 'a' and <= 'z' or >= '0' and <= '9' or '-');
    internal static string MaskId(string id) => id.Length <= 8 ? "masked" : id[..4] + "…" + id[^4..];
    private void WriteSweepLedger(string path, string runId, DateTimeOffset created, IEnumerable<(string Kind, string Name)> items)
    {
        var temp = path + ".tmp-" + Guid.NewGuid().ToString("N"); using (var stream = new FileStream(temp, FileMode.CreateNew, FileAccess.Write, FileShare.None)) using (var writer = new Utf8JsonWriter(stream)) { writer.WriteStartObject(); writer.WriteString("run_id", runId); writer.WriteString("subscription", _subscription); writer.WriteString("blob_endpoint", _blobEndpoint); writer.WriteString("queue_endpoint", _queueEndpoint); writer.WriteString("created_utc", created); writer.WriteStartArray("owned"); foreach (var item in items) { writer.WriteStartObject(); writer.WriteString("kind", item.Kind); writer.WriteString("name", item.Name); writer.WriteEndObject(); } writer.WriteEndArray(); writer.WriteEndObject(); writer.Flush(); } File.Move(temp, path, true);
    }
    private static string Required(string name) => Environment.GetEnvironmentVariable(name) is { Length: > 0 } value ? value : throw new AzureCloudException($"missing {name}");
    private static string OptionalStorageEndpoint(string name, string suffix) => Environment.GetEnvironmentVariable(name) is { Length: > 0 } ? ValidateStorageEndpoint(name, suffix) : "";
    private static string ValidateStorageEndpoint(string variable, string suffix)
    {
        var raw = Required(variable); if (!Uri.TryCreate(raw, UriKind.Absolute, out var uri) || uri.Scheme != Uri.UriSchemeHttps || !string.IsNullOrEmpty(uri.UserInfo) || !string.IsNullOrEmpty(uri.Query) || !string.IsNullOrEmpty(uri.Fragment) || (uri.Port != -1 && uri.Port != 443) || !uri.Host.EndsWith("." + suffix, StringComparison.OrdinalIgnoreCase) || uri.Host[..^(suffix.Length + 1)].Contains('.') || uri.AbsolutePath is not ("" or "/")) throw new AzureCloudException($"{variable} must be a public Azure HTTPS endpoint"); return raw;
    }
    private void Ensure() { if (_disposed) throw new ObjectDisposedException(nameof(AzureSession)); }
    internal void Unregister(AzureRun run) => _runs.Remove(run);
    public void Dispose() { if (!_disposed) { Exception? failure = null; foreach (var run in _runs.ToArray()) { try { run.Dispose(); } catch (Exception ex) { failure ??= ex; } } _runs.Clear(); if (_transport is IDisposable disposable) disposable.Dispose(); _disposed = true; if (failure != null) throw new AzureCloudException("Azure run cleanup failed; ledgers retained", failure); } }

    private sealed class MockTokenCredential : TokenCredential { public override AccessToken GetToken(TokenRequestContext requestContext, CancellationToken cancellationToken) => new("mock", DateTimeOffset.UtcNow.AddHours(1)); public override ValueTask<AccessToken> GetTokenAsync(TokenRequestContext requestContext, CancellationToken cancellationToken) => ValueTask.FromResult(GetToken(requestContext, cancellationToken)); }
    private sealed class FakeAzureHandler : HttpMessageHandler
    {
        private readonly Dictionary<string, string> _metadata = new(StringComparer.OrdinalIgnoreCase);
        private readonly HashSet<string> _created = new(StringComparer.OrdinalIgnoreCase);
        protected override Task<HttpResponseMessage> SendAsync(HttpRequestMessage request, CancellationToken cancellationToken)
        {
            var path = request.RequestUri?.AbsolutePath ?? "";
            if (path.Contains("providers/Microsoft.Resources/subscriptions", StringComparison.OrdinalIgnoreCase) || path.Contains("/subscriptions/", StringComparison.OrdinalIgnoreCase))
            {
                var types = new[] { "Microsoft.Compute/virtualMachines", "Microsoft.Storage/storageAccounts", "Microsoft.Network/virtualNetworks", "Microsoft.ManagedIdentity/userAssignedIdentities", "Microsoft.Sql/servers", "Microsoft.Storage/storageAccounts/queueServices", "Microsoft.Web/sites", "Microsoft.Insights/components", "Microsoft.Network/dnsZones", "Microsoft.KeyVault/vaults", "Microsoft.ContainerService/managedClusters", "Microsoft.Storage/storageAccounts" };
                var query = Uri.UnescapeDataString(request.RequestUri?.Query ?? "");
                var selected = types.Where(type => !query.Contains("substringof('", StringComparison.Ordinal) || query.Contains($"substringof('{type[..(type.IndexOf('/') + 1)]}", StringComparison.Ordinal) || query.Contains($"substringof('{type}", StringComparison.Ordinal)).ToArray();
                var mockNow = new DateTimeOffset(2026, 1, 1, 0, 0, 0, TimeSpan.Zero); var ages = new[] { 6, 48, 240, 960 }; var locations = new[] { "eastus", "westus2", "westeurope" };
                using var stream = new MemoryStream(); using (var writer = new Utf8JsonWriter(stream)) { writer.WriteStartObject(); writer.WriteStartArray("value"); var i = 0; foreach (var type in selected) { i++; var isQueue = type.Equals("Microsoft.Storage/storageAccounts/queueServices", StringComparison.Ordinal); var account = $"lmazure-account-{i}"; var id = isQueue ? $"/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/lmazure-rg/providers/Microsoft.Storage/storageAccounts/{account}/queueServices/default" : $"/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/lmazure-rg/providers/{type}/lmazure-{i}"; writer.WriteStartObject(); writer.WriteString("id", id); writer.WriteString("name", isQueue ? "default" : $"lmazure-{i}"); writer.WriteString("type", type); writer.WriteString("location", locations[(i - 1) % locations.Length]); writer.WriteString("createdTime", mockNow.AddHours(-ages[(i - 1) % ages.Length]).ToString("O")); writer.WriteString("changedTime", mockNow.ToString("O")); writer.WriteStartObject("tags"); if (i % 3 != 0) writer.WriteString("lmazure_test", "mock"); if (type.StartsWith("Microsoft.Storage/", StringComparison.Ordinal)) { if (i == 2) writer.WriteString("encrypted", "true"); else if (i == 6) writer.WriteString("encrypted", "false"); } else writer.WriteString("encrypted", i % 2 == 0 ? "true" : "false"); if (i % 3 == 1) writer.WriteString("public", "true"); else if (i % 3 == 2) writer.WriteString("public", "false"); writer.WriteEndObject(); writer.WriteEndObject(); } writer.WriteEndArray(); writer.WriteEndObject(); }
                var content = new ByteArrayContent(stream.ToArray()); content.Headers.ContentType = new MediaTypeHeaderValue("application/json");
                return Task.FromResult(new HttpResponseMessage(HttpStatusCode.OK) { Content = content });
            }
            if (!request.RequestUri!.Host.Equals("blob.mock", StringComparison.OrdinalIgnoreCase) && !request.RequestUri.Host.Equals("queue.mock", StringComparison.OrdinalIgnoreCase)) return Task.FromResult(new HttpResponseMessage(HttpStatusCode.NotFound));
            var resourceName = path.Trim('/').Split('/').LastOrDefault() ?? "";
            if (request.Method == HttpMethod.Put)
            {
                IEnumerable<string>? metadata = null;
                if (request.Headers.TryGetValues("x-ms-meta-lmazure_test", out var headerMetadata)) metadata = headerMetadata;
                else if (request.Content?.Headers.TryGetValues("x-ms-meta-lmazure_test", out var contentMetadata) == true) metadata = contentMetadata;
                var metadataUpdate = request.RequestUri?.Query.Contains("comp=metadata", StringComparison.OrdinalIgnoreCase) ?? false;
                if (metadataUpdate) { if (!_created.Contains(resourceName)) return Task.FromResult(new HttpResponseMessage(HttpStatusCode.NotFound)); if (metadata != null) _metadata[resourceName] = metadata.Single(); }
                else if (!_created.Add(resourceName)) return Task.FromResult(new HttpResponseMessage(request.RequestUri!.Host == "queue.mock" ? HttpStatusCode.NoContent : HttpStatusCode.Conflict));
                if (metadata != null) _metadata[resourceName] = metadata.Single();
            }
            if (request.Method == HttpMethod.Delete) { if (!_created.Remove(resourceName)) return Task.FromResult(new HttpResponseMessage(HttpStatusCode.NotFound)); _metadata.Remove(resourceName); }
            if ((request.Method == HttpMethod.Get || request.Method == HttpMethod.Head) && !_created.Contains(resourceName)) return Task.FromResult(new HttpResponseMessage(HttpStatusCode.NotFound));
            var status = (request.Method == HttpMethod.Get || request.Method == HttpMethod.Head) ? HttpStatusCode.OK : request.Method == HttpMethod.Delete ? (request.RequestUri!.Host == "blob.mock" ? HttpStatusCode.Accepted : HttpStatusCode.NoContent) : request.Method == HttpMethod.Put && (request.RequestUri?.Query.Contains("comp=metadata", StringComparison.OrdinalIgnoreCase) ?? false) ? (request.RequestUri!.Host == "queue.mock" ? HttpStatusCode.NoContent : HttpStatusCode.OK) : HttpStatusCode.Created;
            var response = new HttpResponseMessage(status) { Content = new StringContent("{}", Encoding.UTF8, "application/json") };
            if (request.Method == HttpMethod.Head) { response.Headers.ETag = new EntityTagHeaderValue("\"mock\""); response.Headers.TryAddWithoutValidation("x-ms-blob-type", "BlockBlob"); response.Headers.TryAddWithoutValidation("x-ms-server-encrypted", "true"); response.Headers.TryAddWithoutValidation("x-ms-request-id", "mock"); response.Headers.TryAddWithoutValidation("x-ms-version", "2023-11-03"); response.Headers.TryAddWithoutValidation("Last-Modified", DateTimeOffset.UtcNow.ToString("R")); response.Content.Headers.ContentLength = 0; }
            if ((request.Method == HttpMethod.Get || request.Method == HttpMethod.Head) && _metadata.TryGetValue(resourceName, out var owned)) response.Headers.TryAddWithoutValidation("x-ms-meta-lmazure_test", owned);
            return Task.FromResult(response);
        }
    }
}
