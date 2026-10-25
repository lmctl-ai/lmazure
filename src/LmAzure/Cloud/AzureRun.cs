using Azure;
using System.Text.Json;

namespace LmAzure.Cloud;

public sealed class AzureRun : IDisposable
{
    private readonly AzureSession _session;
    private readonly string _ledger, _runId;
    private readonly CostLedger _cost;
    private readonly DateTimeOffset _createdUtc = DateTimeOffset.UtcNow;
    private readonly List<(string Kind, string Name)> _owned = [];
    private bool _closed, _completed;

    internal AzureRun(AzureSession session, string ledgerPath, bool mock)
    {
        _session = session; _ledger = ledgerPath; _runId = Guid.NewGuid().ToString("N")[..12];
        var example = Path.GetFileNameWithoutExtension(ledgerPath);
        if (example.EndsWith(".ledger", StringComparison.Ordinal)) example = example[..^7];
        _cost = new CostLedger(_runId, example, session.Region, mock);
        Directory.CreateDirectory(Path.GetDirectoryName(Path.GetFullPath(ledgerPath))!);
        using var stream = new FileStream(ledgerPath, FileMode.CreateNew, FileAccess.Write, FileShare.None);
        WriteLedger(stream);
    }
    public ResourceTable CreateContainer(string suffix) => Create(suffix, "blob");
    public ResourceTable CreateQueue(string suffix) => Create(suffix, "queue");
    private ResourceTable Create(string suffix, string kind)
    {
        EnsureWrite(); EnsureCapacity(); var name = Name(suffix);
        try
        {
            using var operation = _cost.BeginCreate(name, kind);
            using var timeout = new CancellationTokenSource(TimeSpan.FromSeconds(15));
            var metadata = new Dictionary<string, string> { ["lmazure_test"] = _runId };
            var status = kind == "blob"
                ? _session.BlobContainer(name).CreateAsync(metadata: metadata, cancellationToken: timeout.Token).GetAwaiter().GetResult().GetRawResponse().Status
                : _session.Queue(name).CreateAsync(metadata: metadata, cancellationToken: timeout.Token).GetAwaiter().GetResult().Status;
            if (status != 201) throw new AzureCloudException("resource was not newly created");
            operation.MarkCreated(); _owned.Add((kind, name));
            return One(name, kind, "created");
        }
        catch (CostLimitException) { throw; }
        catch (AzureCloudException) { throw; }
        catch (Exception ex) { throw new AzureCloudException("resource creation failed", ex); }
        finally { Persist(); }
    }
    public ResourceTable Tag(string name, string key, string value)
    {
        EnsureWrite(); EnsureOwned(name);
        if (string.IsNullOrWhiteSpace(key) || key.Length > 128 || key.Contains(':')) throw new AzureCloudException("invalid metadata key");
        try
        {
            using var operation = _cost.BeginOperation(name, "tag", 2);
            var kind = _owned.First(x => x.Name == name).Kind;
            if (!_session.OwnsMetadataAsync(kind, name, _runId).GetAwaiter().GetResult()) throw new AzureCloudException("resource ownership metadata check failed");
            using var timeout = new CancellationTokenSource(TimeSpan.FromSeconds(15));
            var metadata = new Dictionary<string, string> { [key] = value, ["lmazure_test"] = _runId };
            if (kind == "blob") _session.BlobContainer(name).SetMetadataAsync(metadata, cancellationToken: timeout.Token).GetAwaiter().GetResult();
            else _session.Queue(name).SetMetadataAsync(metadata, cancellationToken: timeout.Token).GetAwaiter().GetResult();
            return One(name, "owned", "tagged");
        }
        catch (CostLimitException) { throw; }
        catch (AzureCloudException) { throw; }
        catch (Exception ex) { throw new AzureCloudException("resource tagging failed", ex); }
        finally { Persist(); }
    }
    public ResourceTable Delete(string name)
    {
        EnsureWrite(); EnsureOwned(name); var item = _owned.First(x => x.Name == name);
        try
        {
            using var operation = _cost.BeginOperation(name, "delete", 2);
            if (!_session.OwnsMetadataAsync(item.Kind, name, _runId).GetAwaiter().GetResult()) throw new AzureCloudException("resource ownership metadata check failed");
            using var timeout = new CancellationTokenSource(TimeSpan.FromSeconds(15));
            if (item.Kind == "blob") _session.BlobContainer(name).DeleteAsync(cancellationToken: timeout.Token).GetAwaiter().GetResult();
            else _session.Queue(name).DeleteAsync(cancellationToken: timeout.Token).GetAwaiter().GetResult();
            _owned.Remove(item); _cost.MarkDeleted(name);
            return One(name, item.Kind, "deleted");
        }
        catch (CostLimitException) { throw; }
        catch (AzureCloudException) { throw; }
        catch (Exception ex) { throw new AzureCloudException("resource deletion failed", ex); }
        finally { Persist(); }
    }
    public void Cleanup()
    {
        if (_completed) return;
        EnsureWrite(); var failures = new List<string>();
        foreach (var item in _owned.ToArray())
        {
            using var operation = _cost.BeginOperation(item.Name, "cleanup", 2, cleanup: true);
            try
            {
                if (!_session.OwnsMetadataAsync(item.Kind, item.Name, _runId).GetAwaiter().GetResult()) { failures.Add(item.Name); continue; }
                using var timeout = new CancellationTokenSource(TimeSpan.FromSeconds(15));
                if (item.Kind == "blob") _session.BlobContainer(item.Name).DeleteAsync(cancellationToken: timeout.Token).GetAwaiter().GetResult();
                else _session.Queue(item.Name).DeleteAsync(cancellationToken: timeout.Token).GetAwaiter().GetResult();
                _owned.Remove(item); _cost.MarkDeleted(item.Name);
            }
            catch (RequestFailedException ex) when (ex.Status == 404) { _owned.Remove(item); _cost.MarkDeleted(item.Name); }
            catch { failures.Add(item.Name); }
        }
        try
        {
            Persist();
            if (failures.Count != 0) throw new AzureCloudException("cleanup left owned resources in the ledger");
            File.Delete(_ledger); _completed = true;
        }
        finally { _cost.ReportDefault(); }
    }
    internal CostLedger Cost => _cost;
    internal bool MatchesLedger(string path) => Path.GetFullPath(path).Equals(Path.GetFullPath(_ledger), OperatingSystem.IsWindows() ? StringComparison.OrdinalIgnoreCase : StringComparison.Ordinal);
    internal void MarkSwept(string name) { _owned.RemoveAll(x => x.Name == name); _cost.MarkDeleted(name); }
    private void EnsureCapacity() { if (_owned.Count >= 16) throw new AzureCloudException("run ownership limit exceeded"); }
    private void EnsureOwned(string name) { if (!_owned.Any(x => x.Name == name && x.Name.StartsWith("lmazure-", StringComparison.Ordinal))) throw new AzureCloudException("resource is not owned by this run"); }
    private string Name(string suffix) { if (string.IsNullOrWhiteSpace(suffix) || suffix.Length > 42 || !suffix.All(c => char.IsAsciiLetterOrDigit(c) || c == '-')) throw new AzureCloudException("invalid resource suffix"); return $"lmazure-{_runId}-{suffix.ToLowerInvariant()}"; }
    private ResourceTable One(string name, string kind, string status) => new([ResourceTable.Row(name, kind, _session.Region, status, 1, 0, false, true, true, AzureSession.MaskId(name))]);
    private void WriteLedger(Stream stream)
    {
        using var writer = new Utf8JsonWriter(stream);
        writer.WriteStartObject(); writer.WriteString("run_id", _runId); writer.WriteString("subscription", _session.Subscription); writer.WriteString("blob_endpoint", _session.BlobEndpoint); writer.WriteString("queue_endpoint", _session.QueueEndpoint); writer.WriteString("created_utc", _createdUtc);
        writer.WritePropertyName("cost"); _cost.Write(writer);
        writer.WriteStartArray("owned"); foreach (var item in _owned) { writer.WriteStartObject(); writer.WriteString("kind", item.Kind); writer.WriteString("name", item.Name); writer.WriteEndObject(); } writer.WriteEndArray(); writer.WriteEndObject(); writer.Flush();
    }
    private void Persist()
    {
        var temp = _ledger + ".tmp-" + Guid.NewGuid().ToString("N");
        using (var stream = new FileStream(temp, FileMode.CreateNew, FileAccess.Write, FileShare.None)) WriteLedger(stream);
        if (new FileInfo(temp).Length > 1024 * 1024) { File.Delete(temp); throw new AzureCloudException("run ledger exceeds 1 MiB"); }
        File.Move(temp, _ledger, true);
    }
    private void EnsureWrite() { if (_closed || _completed) throw new ObjectDisposedException(nameof(AzureRun)); _session.EnsureWrite(); }
    public void SetCostCap(double cap) { EnsureWrite(); _cost.SetCap(cap); Persist(); }
    public double EstimatedCost() => _cost.EstimatedCost();
    public void CostReport(string path) => _cost.Report(path);
    public void Dispose() { if (!_closed) { try { Cleanup(); } finally { _closed = true; _session.Unregister(this); } } }
}
