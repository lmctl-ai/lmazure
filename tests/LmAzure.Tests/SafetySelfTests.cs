using System.Net;
using System.Net.Http.Headers;
using System.Text;
using System.Text.Json;
using LmAzure.Cloud;

namespace LmAzure.Tests;

/// <summary>In-process mutation and ledger safety checks. No Azure credential path is used.</summary>
public static class SafetySelfTests
{
    public static int Run()
    {
        var checks = 0;
        ChecksFailedCreateIsNotClaimed(ref checks);
        ChecksProtectedMetadataAndOwnership(ref checks);
        ChecksDrySweepDoesNotDelete(ref checks);
        ChecksCleanupAttemptsAllAndRetainsLedger(ref checks);
        ChecksForeignLedgersAreRejected(ref checks);
        ChecksSecretValuesNeverProject(ref checks);
        ChecksTransportSafety(ref checks);
        ChecksUnknownNumbersRemainUnknown(ref checks);
        return checks;
    }

    private static void ChecksFailedCreateIsNotClaimed(ref int checks)
    {
        using var handler = new SafetyHandler { PutStatus = HttpStatusCode.Conflict };
        using var session = AzureSession.Mock(handler);
        string ledger = TempLedger();
        using var run = session.BeginRun(ledger);
        ExpectThrows(() => run.CreateContainer("conflict"));
        Require(File.ReadAllText(ledger).Contains("\"owned\":[]", StringComparison.Ordinal), "409 create must not claim"); checks++;
        Require(handler.Requests.Any(r => r.Method == HttpMethod.Put), "create request recorded"); checks++;
        using var queueHandler = new SafetyHandler { PutStatus = HttpStatusCode.NoContent };
        using var queueSession = AzureSession.Mock(queueHandler);
        string queueLedger = TempLedger();
        using var queueRun = queueSession.BeginRun(queueLedger);
        ExpectThrows(() => queueRun.CreateQueue("not-created"));
        Require(File.ReadAllText(queueLedger).Contains("\"owned\":[]", StringComparison.Ordinal), "204 queue create must not claim"); checks++;
    }

    private static void ChecksProtectedMetadataAndOwnership(ref int checks)
    {
        using var handler = new SafetyHandler();
        using var session = AzureSession.Mock(handler);
        string ledger = TempLedger();
        using var run = session.BeginRun(ledger);
        var created = run.CreateContainer("owned"); string name = created.Text(0, 0);
        run.Tag(name, "lmazure_test", "attacker-value");
        Require(handler.LastMetadata != "attacker-value" && handler.LastMetadata.Length != 0, "protected ownership metadata was overwritten"); checks++;
        handler.ForcedResponseMetadata = "foreign-run";
        ExpectThrows(() => run.Tag(name, "audit", "blocked")); checks++;
        ExpectThrows(() => run.Delete(name)); checks++;
        ExpectThrows(() => run.Delete("lmazure-foreign-resource")); checks++;
        ExpectThrows(() => run.Tag("lmazure-foreign-resource", "x", "y")); checks++;
        handler.ForcedResponseMetadata = handler.LastMetadata;
        run.Cleanup(); File.Delete(ledger);
    }

    private static void ChecksDrySweepDoesNotDelete(ref int checks)
    {
        using var handler = new SafetyHandler();
        using var session = AzureSession.Mock(handler);
        string ledger = TempLedger();
        var run = session.BeginRun(ledger); run.CreateQueue("dry");
        handler.Requests.Clear();
        var result = session.Sweep(ledger, 0, false);
        Require(result.Count() == 1, "dry sweep reports the owned candidate"); checks++;
        Require(!handler.Requests.Any(r => r.Method == HttpMethod.Delete), "dry sweep must not delete"); checks++;
        run.Cleanup(); File.Delete(ledger);
    }

    private static void ChecksCleanupAttemptsAllAndRetainsLedger(ref int checks)
    {
        using var handler = new SafetyHandler { DeleteStatus = HttpStatusCode.InternalServerError };
        using var session = AzureSession.Mock(handler);
        string ledger = TempLedger();
        var run = session.BeginRun(ledger);
        run.CreateContainer("one"); run.CreateQueue("two");
        ExpectThrows(() => run.Cleanup());
        Require(handler.Requests.Count(r => r.Method == HttpMethod.Delete) >= 2, "cleanup must attempt every owned resource"); checks++;
        Require(File.Exists(ledger), "failed cleanup must retain ledger"); checks++;
        handler.DeleteStatus = HttpStatusCode.NoContent;
        run.Cleanup(); File.Delete(ledger);
    }

    private static void ChecksForeignLedgersAreRejected(ref int checks)
    {
        using var handler = new SafetyHandler();
        using var session = AzureSession.Mock(handler);
        string ledger = TempLedger();
        File.WriteAllText(ledger, "{\"run_id\":\"0123456789ab\",\"subscription\":\"foreign\",\"created_utc\":\"2000-01-01T00:00:00Z\",\"owned\":[]}");
        ExpectThrows(() => session.Sweep(ledger, 0, true));
        Require(!handler.Requests.Any(r => r.Method == HttpMethod.Delete), "foreign ledger must fail before mutation"); checks++;
        File.Delete(ledger);
    }

    private static void ChecksSecretValuesNeverProject(ref int checks)
    {
        using var handler = new SafetyHandler { Secret = "accessToken=redacted-test;connectionString=never-output" };
        using var session = AzureSession.Mock(handler);
        string json = session.Inventory("resources").JsonString();
        Require(json.Contains("lmazure-resource", StringComparison.Ordinal), "mock resource fixture"); checks++;
        Require(!json.Contains("accessToken", StringComparison.OrdinalIgnoreCase), "access token projected"); checks++;
        Require(!json.Contains("connectionString", StringComparison.OrdinalIgnoreCase), "connection string projected"); checks++;
        Require(!json.Contains("redacted-test", StringComparison.Ordinal), "secret value projected"); checks++;
    }

    private static void ChecksTransportSafety(ref int checks)
    {
        using var inner = new SafetyHandler();
        using var safe = new HttpClient(TransportSafety.Wrap(inner, ["management.azure.com"], true));
        ExpectHttpThrows(() => safe.GetAsync("http://management.azure.com/").GetAwaiter().GetResult()); checks++;
        ExpectHttpThrows(() => safe.GetAsync("https://evil.example/").GetAwaiter().GetResult()); checks++;
        ExpectHttpThrows(() => safe.GetAsync("https://user:pass@management.azure.com/").GetAwaiter().GetResult()); checks++;
        ExpectHttpThrows(() => safe.GetAsync("https://management.azure.com:444/").GetAwaiter().GetResult()); checks++;
        ExpectHttpThrows(() => safe.GetAsync("https://management.azure.com/?sig=secret").GetAwaiter().GetResult()); checks++;
        using var allowed = safe.GetAsync("https://management.azure.com/").GetAwaiter().GetResult();
        Require(inner.Requests.Count == 1, "only the exact safe Azure destination reached the handler"); checks++;

        using var oversizedInner = new SafetyHandler { ResponseBytes = TransportSafety.MaxResponseBytes + 1 };
        using var bounded = new HttpClient(TransportSafety.Wrap(oversizedInner, ["management.azure.com"], true));
        ExpectHttpThrows(() => bounded.GetAsync("https://management.azure.com/").GetAwaiter().GetResult()); checks++;
    }

    private static void ChecksUnknownNumbersRemainUnknown(ref int checks)
    {
        var table = new ResourceTable([ResourceTable.Row("n", "k", "eastus", "observed", 1, 0, null, null, false, "masked")]);
        ExpectThrowsAny(() => table.Number(0, 6)); checks++;
        ExpectThrowsAny(() => table.Number(0, 7)); checks++;
    }

    private static string TempLedger() => Path.Combine(Path.GetTempPath(), "lmazure-safety-" + Guid.NewGuid().ToString("N") + ".json");
    private static void Require(bool condition, string message) { if (!condition) throw new InvalidOperationException(message); }
    private static void ExpectThrows(Action action) { try { action(); throw new InvalidOperationException("expected safety failure"); } catch (AzureCloudException) { } }
    private static void ExpectThrowsAny(Action action) { var thrown = false; try { action(); } catch (Exception) { thrown = true; } if (!thrown) throw new InvalidOperationException("expected safety failure"); }
    private static void ExpectHttpThrows(Action action) { try { action(); throw new InvalidOperationException("expected transport rejection"); } catch (HttpRequestException) { } }

    private sealed class SafetyHandler : HttpMessageHandler, IDisposable
    {
        public HttpStatusCode PutStatus { get; init; } = HttpStatusCode.Created;
        public HttpStatusCode DeleteStatus { get; set; } = HttpStatusCode.NoContent;
        public string Secret { get; init; } = "";
        public string LastMetadata { get; private set; } = "mock-run";
        public string? ForcedResponseMetadata { get; set; }
        public int ResponseBytes { get; init; }
        public List<(HttpMethod Method, Uri Uri)> Requests { get; } = [];

        protected override Task<HttpResponseMessage> SendAsync(HttpRequestMessage request, CancellationToken cancellationToken)
        {
            Requests.Add((request.Method, request.RequestUri!));
            if (request.Headers.TryGetValues("x-ms-meta-lmazure_test", out var header)) LastMetadata = header.Single();
            if (request.Content?.Headers.TryGetValues("x-ms-meta-lmazure_test", out var content) == true) LastMetadata = content.Single();
            HttpStatusCode status = request.Method == HttpMethod.Put && request.RequestUri!.Query.Contains("comp=metadata", StringComparison.OrdinalIgnoreCase) ? HttpStatusCode.OK : request.Method == HttpMethod.Put ? PutStatus : request.Method == HttpMethod.Delete ? (request.RequestUri!.Host == "blob.mock" && DeleteStatus == HttpStatusCode.NoContent ? HttpStatusCode.Accepted : DeleteStatus) : HttpStatusCode.OK;
            var response = new HttpResponseMessage(status) { Content = ResponseBytes > 0 ? new ByteArrayContent(new byte[ResponseBytes]) : new StringContent(InventoryJson(), Encoding.UTF8, "application/json") };
            if (request.Method == HttpMethod.Head) response.Content.Headers.ContentLength = 0;
            if (request.Method == HttpMethod.Get || request.Method == HttpMethod.Head) response.Headers.TryAddWithoutValidation("x-ms-meta-lmazure_test", ForcedResponseMetadata ?? LastMetadata);
            return Task.FromResult(response);
        }

        private string InventoryJson()
        {
            using var stream = new MemoryStream();
            using (var writer = new Utf8JsonWriter(stream))
            {
                writer.WriteStartObject(); writer.WriteStartArray("value"); writer.WriteStartObject();
                writer.WriteString("id", "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/lmazure-rg/providers/Microsoft.Storage/storageAccounts/lmazure-resource");
                writer.WriteString("name", "lmazure-resource"); writer.WriteString("type", "Microsoft.Storage/storageAccounts"); writer.WriteString("location", "eastus");
                writer.WriteStartObject("tags"); writer.WriteString("lmazure_test", "mock"); writer.WriteEndObject();
                writer.WriteStartObject("properties"); writer.WriteString("accessToken", Secret); writer.WriteString("connectionString", Secret); writer.WriteEndObject();
                writer.WriteEndObject(); writer.WriteEndArray(); writer.WriteEndObject(); writer.Flush();
            }
            return Encoding.UTF8.GetString(stream.ToArray());
        }
    }
}
