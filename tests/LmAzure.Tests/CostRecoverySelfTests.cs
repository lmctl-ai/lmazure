using System.Net;
using System.Text;
using System.Text.Json;
using LmAzure.Cloud;

namespace LmAzure.Tests;

public static class CostRecoverySelfTests
{
    public static int Run()
    {
        CostLedger.ResetForTests(); var checks = 0;
        var path = Path.Combine(Path.GetTempPath(), "lmazure-recovery-" + Guid.NewGuid().ToString("N") + ".ledger.json");
        string owner;
        using (var session = AzureSession.Mock(new RecoveryHandler { FailDelete = true }))
        {
            var run = session.BeginRun(path); run.CreateQueue("recovery");
            var failed = false; try { run.Dispose(); } catch (AzureCloudException) { failed = true; }
            Require(failed && File.Exists(path), "failed teardown retains ledger"); checks++;
            using var document = JsonDocument.Parse(File.ReadAllText(path)); owner = document.RootElement.GetProperty("run_id").GetString()!;
            var cost = document.RootElement.GetProperty("cost");
            Require(cost.GetProperty("requests").GetArrayLength() == 3 && cost.GetProperty("reserved_cost_usd").GetDecimal() == 0.0002m, "failed cleanup keeps request history and reservation"); checks++;
        }
        CostLedger.ResetForTests(); // a new host process has no in-memory cost state
        using (var session = AzureSession.Mock(new RecoveryHandler { Owner = owner }))
        {
            Require(session.Sweep(path, 0, false).Count() == 1, "recovered dry sweep finds the owned resource"); checks++;
            using (var document = JsonDocument.Parse(File.ReadAllText(path)))
            {
                var cost = document.RootElement.GetProperty("cost");
                Require(cost.GetProperty("requests").GetArrayLength() == 4 && cost.GetProperty("estimated_cost_usd").GetDecimal() == 0.0004m, "dry sweep retains prior costs and charges one read"); checks++;
            }
            Require(session.Sweep(path, 0, true).Count() == 1 && !File.Exists(path), "apply sweep removes resource and ledger"); checks++;
            var reports = Directory.GetFiles("evidence", "fake-cost-*-" + owner + ".json");
            using var final = reports.Select(File.ReadAllText).Select(text => JsonDocument.Parse(text)).Single(x => x.RootElement.GetProperty("requests").GetArrayLength() == 6);
            var data = final.RootElement;
            Require(data.GetProperty("estimated_cost_usd").GetDecimal() == 0.0006m && data.GetProperty("process_estimated_cost_usd").GetDecimal() == 0.0006m, "repeated restore does not double count historic spend"); checks++;
            Require(data.GetProperty("reserved_cost_usd").GetDecimal() == 0 && !data.GetProperty("resources")[0].GetProperty("owned").GetBoolean(), "sweep releases cleanup reservation and records deletion"); checks++;
        }
        CostLedger.ResetForTests();
        var legacy = CostLedger.Restore("abcdef012345", "legacy", "eastus", true, DateTimeOffset.UtcNow, default, []);
        using var buffer = new MemoryStream(); using (var writer = new Utf8JsonWriter(buffer)) legacy.Write(writer);
        using var legacyReport = JsonDocument.Parse(buffer.ToArray());
        Require(!legacyReport.RootElement.GetProperty("historical_cost_known").GetBoolean(), "legacy ledgers never invent historic costs"); checks++;
        CostLedger.ResetForTests(); return checks;
    }
    private static void Require(bool condition, string message) { if (!condition) throw new InvalidOperationException(message); }
    private sealed class RecoveryHandler : HttpMessageHandler
    {
        public bool FailDelete { get; init; }
        public string Owner { get; set; } = "";
        protected override Task<HttpResponseMessage> SendAsync(HttpRequestMessage request, CancellationToken cancellationToken)
        {
            if (request.Headers.TryGetValues("x-ms-meta-lmazure_test", out var owner)) Owner = owner.Single();
            var status = request.Method == HttpMethod.Put ? HttpStatusCode.Created : request.Method == HttpMethod.Delete ? (FailDelete ? HttpStatusCode.InternalServerError : HttpStatusCode.NoContent) : HttpStatusCode.OK;
            var response = new HttpResponseMessage(status) { Content = new StringContent("{}", Encoding.UTF8, "application/json") };
            if (request.Method == HttpMethod.Get || request.Method == HttpMethod.Head) response.Headers.TryAddWithoutValidation("x-ms-meta-lmazure_test", Owner);
            return Task.FromResult(response);
        }
    }
}
