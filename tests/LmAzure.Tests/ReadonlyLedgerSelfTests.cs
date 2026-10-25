using System.Net;
using System.Text;
using System.Text.Json;
using LmAzure.Cloud;

namespace LmAzure.Tests;

public static class ReadonlyLedgerSelfTests
{
    public static int Run()
    {
        CostLedger.ResetForTests(); var checks = 0;
        using (var emptyHandler = new PageHandler(false))
        using (var session = AzureSession.Mock(emptyHandler))
        {
            var table = session.Inventory("resources"); Require(table.Count() == 0, "empty ARM page");
            var report = TempPath(); session.ReadonlyCostReport(report); using var doc = JsonDocument.Parse(File.ReadAllText(report));
            var metadata = doc.RootElement.GetProperty("metadata_inventory"); Require(metadata.GetProperty("list_requests").GetInt32() == 1, "empty list request count"); Require(doc.RootElement.GetProperty("last_inventory_rows").GetInt32() == 0, "empty list row evidence"); Require(doc.RootElement.GetProperty("estimated_cost_usd").GetDecimal() == 0m, "readonly zero cost"); Require(doc.RootElement.GetProperty("owned").GetArrayLength() == 0, "readonly owned empty"); Require(!File.ReadAllText(report).Contains("00000000-0000-0000-0000-000000000000", StringComparison.Ordinal), "subscription not projected"); checks += 6; File.Delete(report);
        }
        using (var pageHandler = new PageHandler(true))
        using (var session = AzureSession.Mock(pageHandler))
        {
            var table = session.Inventory("resources"); Require(table.Count() == 1, "paged ARM row"); Require(pageHandler.Requests == 2, "paged ARM requests"); var report = TempPath(); session.ReadonlyCostReport(report); var before = pageHandler.Requests; session.ReadonlyCostReport(report); Require(pageHandler.Requests == before, "report does not dispatch"); using var doc = JsonDocument.Parse(File.ReadAllText(report)); Require(doc.RootElement.GetProperty("last_inventory_rows").GetInt32() == 1, "paged row evidence"); Require(doc.RootElement.GetProperty("metadata_inventory").GetProperty("list_requests").GetInt32() == 2, "paged ledger request count"); Require(doc.RootElement.GetProperty("mode").GetString() == "fake", "fake evidence mode"); Require(doc.RootElement.GetProperty("estimated_cost_text").GetString() == "estimated cost: $0.00", "readonly cost text"); Require(!File.ReadAllText(report).Contains("injected-secret", StringComparison.Ordinal), "properties redacted"); checks += 8; File.Delete(report);
        }
        CostLedger.ResetForTests(); return checks;
    }
    private static string TempPath() => Path.Combine(Path.GetTempPath(), "lmazure-readonly-" + Guid.NewGuid().ToString("N") + ".json");
    private static void Require(bool value, string message) { if (!value) throw new InvalidOperationException(message); }
    private sealed class PageHandler(bool paged) : HttpMessageHandler
    {
        public int Requests; private bool _second;
        protected override Task<HttpResponseMessage> SendAsync(HttpRequestMessage request, CancellationToken cancellationToken)
        {
            Requests++; object payload;
            if (!paged) payload = new { value = Array.Empty<object>() };
            else if (!_second) { _second = true; payload = new { value = new[] { new { id = "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/test/providers/Microsoft.Compute/virtualMachines/lmazure-page", name = "lmazure-page", type = "Microsoft.Compute/virtualMachines", location = "eastus", createdTime = "2025-01-01T00:00:00Z", tags = new { lmazure_test = "mock", injected = "injected-secret" }, properties = new { connectionString = "injected-secret" } } }, nextLink = "https://management.azure.com/subscriptions/00000000-0000-0000-0000-000000000000/resources?api-version=2021-04-01&$skiptoken=page2" }; }
            else payload = new { value = Array.Empty<object>() };
            var body = !paged || payload is { } && _second && Requests > 1 ? "{\"value\":[]}" : "{\"value\":[{\"id\":\"/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/test/providers/Microsoft.Compute/virtualMachines/lmazure-page\",\"name\":\"lmazure-page\",\"type\":\"Microsoft.Compute/virtualMachines\",\"location\":\"eastus\",\"createdTime\":\"2025-01-01T00:00:00Z\",\"tags\":{\"lmazure_test\":\"mock\",\"injected\":\"injected-secret\"},\"properties\":{\"connectionString\":\"injected-secret\"}}],\"nextLink\":\"https://management.azure.com/subscriptions/00000000-0000-0000-0000-000000000000/resources?api-version=2021-04-01&$skiptoken=page2\"}";
            return Task.FromResult(new HttpResponseMessage(HttpStatusCode.OK) { Content = new StringContent(body, Encoding.UTF8, "application/json") });
        }
    }
}
