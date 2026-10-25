namespace LmAzure;

using LmAzure.Cloud;

public static class CoreSelfTests
{
    public static int Run()
    {
        var checks = 0;
        using var session = AzureSession.Mock();
        var inventory = session.Inventory("resources");
        Require(inventory.Count() == 12, "mock inventory fixture"); checks++;
        Require(inventory.Columns() == ResourceTable.StableColumns.Length, "stable table columns"); checks++;
        Require(!inventory.JsonString().Contains("accessToken", StringComparison.OrdinalIgnoreCase), "secret projection"); checks++;
        foreach (var kind in new[] { "compute", "storage", "network", "identity", "database", "queues", "serverless", "monitoring", "dns", "vaults", "containers" }) { Require(session.Inventory(kind).Count() > 0, "curated inventory kind"); checks++; }
        Require(session.Inventory("queues").Count() == 1, "queue inventory filtering"); checks++;
        Require(session.Inventory("dns").Count() == 1, "DNS inventory filtering"); checks++;
        Require(session.Inventory("storage").Count() == 3, "storage inventory filtering"); checks++;
        var ledger = Path.Combine(Path.GetTempPath(), "lmazure-selftest-" + Guid.NewGuid().ToString("N") + ".json");
        using (var run = session.BeginRun(ledger))
        {
            var container = run.CreateContainer("safe"); var queue = run.CreateQueue("queue"); checks += 2;
            run.Tag(container.Text(0, 0), "audit", "ok"); checks++;
            run.Tag(queue.Text(0, 0), "audit", "ok"); checks++;
            run.Delete(queue.Text(0, 0)); checks++;
            run.Cleanup(); checks++;
        }
        Require(!File.Exists(ledger), "ledger cleanup"); checks++;
        return checks;
    }
    private static void Require(bool condition, string message) { if (!condition) throw new InvalidOperationException(message); }
}
