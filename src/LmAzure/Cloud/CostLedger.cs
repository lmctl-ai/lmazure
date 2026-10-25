using System.Globalization;
using System.Text.Json;
using System.Security.Cryptography;

namespace LmAzure.Cloud;

/// Planning estimates only: no billing, pricing, or credential endpoint is called.
public sealed class CostLedger
{
    public const decimal DefaultCap = 0.05m;
    public const decimal HardCap = 0.25m;
    public const decimal RequestPrice = 0.0001m;
    private static readonly object Gate = new();
    private static readonly AsyncLocal<Operation?> Active = new();
    private static readonly Dictionary<string, CostLedger> Instances = new(StringComparer.Ordinal);
    private static decimal ProcessSpent, ProcessReserved;
    private readonly string _runId, _example, _region;
    private readonly bool _mock;
    private bool _historicalCostKnown = true;
    private decimal _cap = DefaultCap, _spent, _reserved;
    private readonly Dictionary<string, ResourceCost> _resources = new(StringComparer.Ordinal);
    private readonly List<RequestCost> _requests = [];
    internal sealed class ResourceCost(string name, string kind)
    {
        public string Name = name, Kind = kind;
        public bool Owned;
        public DateTimeOffset? Created, Deleted;
        public decimal CleanupReserved;
        public string? MetadataSha256;
        public bool MetadataOwnerVerified;
    }
    private sealed record RequestCost(string Resource, string Operation, string Method, DateTimeOffset At);
    public CostLedger(string runId, string example, string region, bool mock)
        : this(runId, example, region, mock, true) { }
    private CostLedger(string runId, string example, string region, bool mock, bool register)
    { _runId = runId; _example = example; _region = region; _mock = mock; if (register) lock (Gate) Instances.TryAdd(runId, this); }
    public void SetCap(double cap)
    {
        if (!double.IsFinite(cap) || cap < 0 || cap > (double)HardCap) throw new CostLimitException("cost cap must be between $0.00 and $0.25");
        lock (Gate) { if (_requests.Count != 0 || _reserved != 0) throw new CostLimitException("declare the example cost cap before resource operations"); _cap = (decimal)cap; }
    }
    private void Reserve(decimal amount)
    {
        if (_spent + _reserved + amount > _cap || ProcessSpent + ProcessReserved + amount > HardCap)
            throw new CostLimitException("estimated cost would exceed the example cap or $0.25 run ceiling");
        _reserved += amount; ProcessReserved += amount;
    }
    private void Release(decimal amount) { _reserved -= amount; ProcessReserved -= amount; }
    public Operation BeginCreate(string name, string kind)
    {
        lock (Gate)
        {
            if (_resources.TryGetValue(name, out var prior) && prior.Owned) throw new AzureCloudException("resource is already owned");
            Reserve(3 * RequestPrice); // one create plus ownership read and delete at teardown
            var resource = prior ?? new ResourceCost(name, kind); _resources[name] = resource;
            resource.CleanupReserved = 2 * RequestPrice;
            return new Operation(this, resource, "create", RequestPrice, true, false);
        }
    }
    public Operation BeginOperation(string name, string operation, int requestAllowance = 2, bool cleanup = false)
    {
        lock (Gate)
        {
            if (!_resources.TryGetValue(name, out var resource)) throw new AzureCloudException("cost resource is not owned");
            var reserve = cleanup ? 0 : requestAllowance * RequestPrice;
            if (!cleanup) Reserve(reserve);
            return new Operation(this, resource, operation, reserve, false, cleanup);
        }
    }
    public void MarkDeleted(string name)
    {
        lock (Gate) { var resource = _resources[name]; resource.Owned = false; resource.Deleted = DateTimeOffset.UtcNow; Release(resource.CleanupReserved); resource.CleanupReserved = 0; }
    }
    // Called by the actual SDK transport immediately before dispatch. Failed attempts count.
    internal static void RecordRequest(string method) { Active.Value?.Record(method); }
    internal static void ObserveMetadata(IDictionary<string, string> metadata)
    {
        var operation = Active.Value;
        if (operation is null) return;
        lock (Gate)
        {
            using var buffer = new MemoryStream();
            using (var writer = new Utf8JsonWriter(buffer))
            {
                writer.WriteStartObject();
                foreach (var pair in metadata.OrderBy(x => x.Key, StringComparer.Ordinal)) writer.WriteString(pair.Key, pair.Value);
                writer.WriteEndObject();
            }
            operation.Resource.MetadataSha256 = Convert.ToHexStringLower(SHA256.HashData(buffer.ToArray()));
            operation.Resource.MetadataOwnerVerified = metadata.TryGetValue("lmazure_test", out var owner) && owner == operation.Ledger._runId;
        }
    }
    public sealed class Operation : IDisposable
    {
        private readonly CostLedger _ledger;
        private readonly ResourceCost _resource;
        private readonly string _operation;
        private readonly bool _create, _cleanup;
        private readonly Operation? _prior;
        private decimal _remaining;
        private bool _created, _disposed;
        internal ResourceCost Resource => _resource;
        internal CostLedger Ledger => _ledger;
        internal Operation(CostLedger ledger, ResourceCost resource, string operation, decimal remaining, bool create, bool cleanup)
        { _ledger = ledger; _resource = resource; _operation = operation; _remaining = remaining; _create = create; _cleanup = cleanup; _prior = Active.Value; Active.Value = this; }
        public void MarkCreated() { lock (Gate) { _created = true; _resource.Owned = true; _resource.Created = DateTimeOffset.UtcNow; _resource.Deleted = null; } }
        internal void Record(string method)
        {
            lock (Gate)
            {
                if (_disposed) throw new AzureCloudException("cost scope is closed");
                if (_remaining >= RequestPrice) { _remaining -= RequestPrice; _ledger.Release(RequestPrice); }
                else if (_cleanup && _resource.CleanupReserved >= RequestPrice) { _resource.CleanupReserved -= RequestPrice; _ledger.Release(RequestPrice); }
                else if (!_cleanup) { _ledger.Reserve(RequestPrice); _ledger.Release(RequestPrice); }
                // Cleanup is never blocked by a depleted estimate. Any overrun prevents later creation.
                _ledger._spent += RequestPrice; ProcessSpent += RequestPrice;
                _ledger._requests.Add(new RequestCost(_resource.Name, _operation, method, DateTimeOffset.UtcNow));
            }
        }
        public void Dispose()
        {
            lock (Gate)
            {
                if (_disposed) return; _disposed = true; _ledger.Release(_remaining); _remaining = 0;
                if (_create && !_created) { _ledger.Release(_resource.CleanupReserved); _resource.CleanupReserved = 0; }
                if (_cleanup && _resource.Owned && _resource.CleanupReserved < 2 * RequestPrice)
                { var refill = 2 * RequestPrice - _resource.CleanupReserved; _resource.CleanupReserved += refill; _ledger._reserved += refill; ProcessReserved += refill; }
                Active.Value = _prior;
            }
        }
    }
    public double EstimatedCost() { lock (Gate) return (double)_spent; }
    public string EstimatedCostText() { lock (Gate) return "estimated cost: $" + (_spent == 0 ? "0.00" : (decimal.Ceiling(_spent * 10000) / 10000).ToString("0.0000", CultureInfo.InvariantCulture)); }
    public void Write(Utf8JsonWriter writer)
    {
        lock (Gate)
        {
            writer.WriteStartObject(); writer.WriteString("mode", _mock ? "fake" : "live"); writer.WriteString("example", _example); writer.WriteString("run_id", _runId); writer.WriteString("region", _region);
            writer.WriteString("measurement", _mock ? "estimated from price table, not yet measured live" : "estimated from price table and local live request counts; not a billing measurement"); writer.WriteString("estimated_cost_text", EstimatedCostText()); writer.WriteBoolean("historical_cost_known", _historicalCostKnown);
            writer.WriteNumber("estimated_cost_usd", _spent); writer.WriteNumber("reserved_cost_usd", _reserved); writer.WriteNumber("cap_usd", _cap); writer.WriteNumber("hard_cap_usd", HardCap); writer.WriteNumber("process_estimated_cost_usd", ProcessSpent);
            writer.WriteStartObject("price_table"); writer.WriteString("version", "2026-10-25-planning-v1"); writer.WriteString("basis", "Conservative local planning allowances; not a provider quote. Prices vary by region and time.");
            writer.WriteNumber("storage_request_usd", RequestPrice); writer.WriteNumber("empty_resource_minute_usd", 0); writer.WriteString("scope", "Empty containers and queues only; no objects, messages, storage account creation, data transfer, or existing account charges.");
            writer.WriteString("blob_pricing_url", "https://azure.microsoft.com/en-us/pricing/details/storage/blobs/"); writer.WriteString("queue_pricing_url", "https://azure.microsoft.com/en-us/pricing/details/storage/queues/"); writer.WriteEndObject();
            writer.WriteStartArray("resources");
            foreach (var resource in _resources.Values)
            {
                writer.WriteStartObject(); writer.WriteString("resource", resource.Name); writer.WriteString("kind", resource.Kind); writer.WriteBoolean("owned", resource.Owned);
                writer.WriteStartObject("tags"); if (resource.MetadataOwnerVerified) writer.WriteString("lmazure_test", _runId); writer.WriteEndObject();
                writer.WriteBoolean("metadata_owner_verified", resource.MetadataOwnerVerified);
                if (resource.MetadataSha256 is not null) writer.WriteString("metadata_sha256", resource.MetadataSha256); else writer.WriteNull("metadata_sha256");
                if (resource.Created.HasValue) writer.WriteString("created_utc", resource.Created.Value); else writer.WriteNull("created_utc");
                if (resource.Deleted.HasValue) writer.WriteString("deleted_utc", resource.Deleted.Value); else writer.WriteNull("deleted_utc");
                var minutes = resource.Created.HasValue ? Math.Max(0, ((resource.Deleted ?? DateTimeOffset.UtcNow) - resource.Created.Value).TotalMinutes) : 0;
                writer.WriteNumber("minutes_alive", minutes); writer.WriteNumber("unit_price_usd_per_minute", 0); writer.WriteNumber("unit_price_usd_per_request", RequestPrice);
                var count = _requests.Count(x => x.Resource == resource.Name); writer.WriteNumber("requests", count); writer.WriteNumber("estimated_cost_usd", count * RequestPrice); writer.WriteEndObject();
            }
            writer.WriteEndArray(); writer.WriteStartArray("requests"); foreach (var request in _requests) { writer.WriteStartObject(); writer.WriteString("resource", request.Resource); writer.WriteString("operation", request.Operation); writer.WriteString("method", request.Method); writer.WriteString("at_utc", request.At); writer.WriteNumber("unit_price_usd", RequestPrice); writer.WriteEndObject(); } writer.WriteEndArray(); writer.WriteEndObject();
        }
    }
    public void Report(string path)
    {
        Directory.CreateDirectory(Path.GetDirectoryName(Path.GetFullPath(path))!);
        var temporary = path + ".tmp-" + Guid.NewGuid().ToString("N");
        using (var stream = new FileStream(temporary, FileMode.CreateNew, FileAccess.Write, FileShare.None)) using (var writer = new Utf8JsonWriter(stream, new JsonWriterOptions { Indented = true })) Write(writer);
        File.Move(temporary, path, true);
    }
    public void ReportDefault()
    {
        var safe = new string(_example.Where(c => char.IsAsciiLetterOrDigit(c) || c is '-' or '_').Take(80).ToArray());
        Report(Path.Combine("evidence", $"{(_mock ? "fake" : "live")}-cost-{safe}-{_runId}.json"));
    }
    internal static CostLedger Restore(string runId, string example, string region, bool mock, DateTimeOffset createdUtc, JsonElement saved, IEnumerable<(string Kind, string Name)> owned)
    {
        var items = owned.ToArray();
        lock (Gate)
        {
            if (Instances.TryGetValue(runId, out var current))
            {
                if (current._region != region || current._mock != mock) throw new AzureCloudException("cost ledger mode or region changed");
                return current;
            }
            var ledger = new CostLedger(runId, example, region, mock, false);
            if (saved.ValueKind != JsonValueKind.Undefined)
            {
                if (saved.GetProperty("run_id").GetString() != runId || saved.GetProperty("mode").GetString() != (mock ? "fake" : "live") || saved.GetProperty("price_table").GetProperty("version").GetString() != "2026-10-25-planning-v1") throw new AzureCloudException("unsupported or mismatched cost ledger");
                var cap = saved.GetProperty("cap_usd").GetDecimal();
                if (cap < 0 || cap > HardCap) throw new AzureCloudException("unsafe recovered cost cap");
                ledger._cap = cap;
                var requests = saved.GetProperty("requests");
                if (requests.ValueKind != JsonValueKind.Array || requests.GetArrayLength() > 4096) throw new AzureCloudException("recovered cost history exceeds safety limit");
                foreach (var request in requests.EnumerateArray())
                {
                    var name = request.GetProperty("resource").GetString() ?? "";
                    if (name.Length > 63 || !name.StartsWith($"lmazure-{runId}-", StringComparison.Ordinal) || !name.All(c => char.IsAsciiLetterOrDigit(c) || c == '-')) throw new AzureCloudException("unsafe recovered cost resource");
                    ledger._requests.Add(new RequestCost(name, "prior_run", request.GetProperty("method").GetString() ?? "unknown", request.GetProperty("at_utc").GetDateTimeOffset()));
                    ledger._resources.TryAdd(name, new ResourceCost(name, "historical") { Created = createdUtc, Deleted = createdUtc });
                }
                ledger._spent = ledger._requests.Count * RequestPrice;
                if (saved.GetProperty("estimated_cost_usd").GetDecimal() != ledger._spent) throw new AzureCloudException("recovered cost estimate does not match requests");
                foreach (var resource in saved.GetProperty("resources").EnumerateArray())
                {
                    var name = resource.GetProperty("resource").GetString() ?? "";
                    if (!ledger._resources.TryGetValue(name, out var entry)) continue;
                    entry.Kind = resource.GetProperty("kind").GetString() ?? "historical";
                    if (resource.TryGetProperty("metadata_sha256", out var digest) && digest.ValueKind == JsonValueKind.String) entry.MetadataSha256 = digest.GetString();
                    if (resource.TryGetProperty("metadata_owner_verified", out var verified)) entry.MetadataOwnerVerified = verified.GetBoolean();
                    if (resource.TryGetProperty("created_utc", out var start)) entry.Created = start.ValueKind == JsonValueKind.Null ? null : start.GetDateTimeOffset();
                    if (resource.TryGetProperty("deleted_utc", out var end)) entry.Deleted = end.ValueKind == JsonValueKind.Null ? null : end.GetDateTimeOffset();
                }
                if (saved.TryGetProperty("historical_cost_known", out var known)) ledger._historicalCostKnown = known.GetBoolean();
            }
            else ledger._historicalCostKnown = false;
            foreach (var item in items)
            {
                if (!ledger._resources.TryGetValue(item.Name, out var entry)) { entry = new ResourceCost(item.Name, item.Kind); ledger._resources.Add(item.Name, entry); }
                entry.Kind = item.Kind; entry.Owned = true; entry.Created ??= createdUtc; entry.Deleted = null; entry.CleanupReserved = 2 * RequestPrice;
            }
            ledger._reserved = items.Length * 2 * RequestPrice;
            ProcessSpent += ledger._spent; ProcessReserved += ledger._reserved;
            Instances.Add(runId, ledger);
            return ledger;
        }
    }
    internal static void ResetForTests() { lock (Gate) { ProcessSpent = 0; ProcessReserved = 0; Active.Value = null; Instances.Clear(); } }
}
public sealed class CostLimitException(string message) : Exception(message);
