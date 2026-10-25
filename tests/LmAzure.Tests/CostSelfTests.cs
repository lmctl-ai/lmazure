using System.Net;
using System.Text;
using System.Text.Json;
using LmAzure.Cloud;

namespace LmAzure.Tests;

public static class CostSelfTests
{
    public static int Run()
    {
        CostLedger.ResetForTests(); var checks = 0;
        ExactFiveRequests(ref checks);
        PreflightCap(ref checks);
        BoundaryCap(ref checks);
        InvalidCaps(ref checks);
        IndependentCaps(ref checks);
        FailedCreate(ref checks);
        HardCeiling(ref checks);
        CostLedger.ResetForTests();
        return checks;
    }

    private static void ExactFiveRequests(ref int checks)
    {
        CostLedger.ResetForTests();
        using var session = AzureSession.Mock(); var path = LedgerPath();
        using (var run = session.BeginRun(path))
        {
            run.SetCostCap(0.0005); var container = run.CreateContainer("cost"); run.Tag(container.Text(0, 0), "audit", "ok"); run.Cleanup();
            var report = path + ".report.json"; run.CostReport(report);
            using var doc = JsonDocument.Parse(File.ReadAllText(report));
            Require(doc.RootElement.GetProperty("mode").GetString() == "fake", "fake mode");
            Require(doc.RootElement.GetProperty("requests").GetArrayLength() == 5, "five lifecycle requests");
            Require(doc.RootElement.GetProperty("reserved_cost_usd").GetDecimal() == 0m, "no reserved cost");
            Require(doc.RootElement.GetProperty("estimated_cost_usd").GetDecimal() == 0.0005m, "exact cost");
            Require(doc.RootElement.GetProperty("price_table").GetProperty("version").GetString() == "2026-10-25-planning-v1", "price table version");
            checks += 5;
        }
        Delete(path + ".report.json"); Delete(path);
    }

    private static void PreflightCap(ref int checks)
    {
        CostLedger.ResetForTests(); using var handler = new ConflictHandler(); using var session = AzureSession.Mock(handler); var path = LedgerPath(); using var run = session.BeginRun(path); run.SetCostCap(0.0002); ExpectCost(() => run.CreateContainer("blocked")); Require(handler.Requests == 0, "preflight sends no HTTP"); Require(!File.ReadAllText(path).Contains("blocked", StringComparison.Ordinal), "preflight does not claim"); checks += 3; Delete(path);
    }
    private static void BoundaryCap(ref int checks)
    {
        CostLedger.ResetForTests(); using var session = AzureSession.Mock(); var path = LedgerPath(); using var run = session.BeginRun(path); run.SetCostCap(0.0003); var created = run.CreateContainer("boundary"); run.Cleanup(); Require(run.EstimatedCost() == 0.0003, "boundary cap exact"); checks++; Delete(path);
    }
    private static void InvalidCaps(ref int checks)
    {
        CostLedger.ResetForTests(); var ledger = new CostLedger("invalid", "invalid", "eastus", true); foreach (var value in new[] { double.NaN, double.PositiveInfinity, -0.1, 0.25001 }) ExpectCost(() => ledger.SetCap(value)); checks += 4;
    }
    private static void IndependentCaps(ref int checks)
    {
        CostLedger.ResetForTests(); using var lowHandler = new ConflictHandler(); using var lowSession = AzureSession.Mock(lowHandler); var lowPath = LedgerPath(); using (var lowRun = lowSession.BeginRun(lowPath)) { lowRun.SetCostCap(0.0002); ExpectCost(() => lowRun.CreateContainer("blocked")); Require(lowHandler.Requests == 0, "low cap blocks before HTTP"); } using var normalSession = AzureSession.Mock(); var normalPath = LedgerPath(); using (var normalRun = normalSession.BeginRun(normalPath)) { normalRun.CreateContainer("allowed"); normalRun.Cleanup(); } checks += 2; Delete(lowPath); Delete(normalPath);
    }
    private static void FailedCreate(ref int checks)
    {
        CostLedger.ResetForTests(); using var handler = new ConflictHandler(); using var session = AzureSession.Mock(handler); var path = LedgerPath(); using var run = session.BeginRun(path); ExpectAnyCost(() => run.CreateContainer("conflict")); using var ledger = JsonDocument.Parse(File.ReadAllText(path)); Require(ledger.RootElement.GetProperty("owned").GetArrayLength() == 0, "failed create not owned"); Require(ledger.RootElement.GetProperty("cost").GetProperty("requests").GetArrayLength() == 1, "failed create request counted once"); Require(handler.Requests == 1, "failed create HTTP count"); checks += 4; Delete(path);
    }
    private static void HardCeiling(ref int checks)
    {
        CostLedger.ResetForTests(); var first = new CostLedger("hard-one", "hard-one", "eastus", true); first.SetCap(0.25); using var operation = first.BeginCreate("resource-one", "blob"); operation.MarkCreated(); for (var i = 0; i < 1500; i++) CostLedger.RecordRequest("PUT"); var second = new CostLedger("hard-two", "hard-two", "eastus", true); second.SetCap(0.25); using var secondOperation = second.BeginCreate("resource-two", "blob"); secondOperation.MarkCreated(); var failed = false; try { for (var i = 0; i < 1500; i++) CostLedger.RecordRequest("PUT"); } catch (CostLimitException) { failed = true; } Require(failed, "global hard ceiling"); checks++;
    }
    private static string LedgerPath() => Path.Combine(Path.GetTempPath(), "lmazure-cost-" + Guid.NewGuid().ToString("N") + ".json");
    private static void Delete(string path) { if (File.Exists(path)) File.Delete(path); }
    private static void Require(bool value, string message) { if (!value) throw new InvalidOperationException(message); }
    private static void ExpectCost(Action action) { try { action(); throw new InvalidOperationException("expected cost failure"); } catch (CostLimitException) { } }
    private static void ExpectAnyCost(Action action) { try { action(); throw new InvalidOperationException("expected cost failure"); } catch (CostLimitException) { } catch (AzureCloudException) { } }
    private sealed class ConflictHandler : HttpMessageHandler
    {
        public int Requests;
        protected override Task<HttpResponseMessage> SendAsync(HttpRequestMessage request, CancellationToken cancellationToken)
        { Requests++; return Task.FromResult(new HttpResponseMessage(request.Method == HttpMethod.Put ? HttpStatusCode.Conflict : HttpStatusCode.NotFound) { Content = new StringContent("{}", Encoding.UTF8, "application/json") }); }
    }
}
