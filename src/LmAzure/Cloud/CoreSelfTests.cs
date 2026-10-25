namespace LmAzure;

using LmAzure.Cloud;

public static class CoreSelfTests
{
    public static int Run()
    {
        var checks = 0;
        using var session = AzureSession.Mock();
        var inventory = session.Inventory("resources");
        Require(inventory.Count() == 24, "mock inventory fixture"); checks++;
        Require(inventory.Columns() == ResourceTable.StableColumns.Length, "stable table columns"); checks++;
        Require(!inventory.JsonString().Contains("accessToken", StringComparison.OrdinalIgnoreCase), "secret projection"); checks++;
        foreach (var kind in new[] { "compute", "storage", "network", "identity", "database", "queues", "serverless", "monitoring", "dns", "vaults", "containers", "servicebus", "nsg", "public_ips", "routes", "logs", "alerts", "app_plans", "postgresql", "registries", "backup", "cosmos" }) { Require(session.Inventory(kind).Count() > 0, "curated inventory kind"); checks++; }
        Require(session.Inventory("queues").Count() == 1, "queue inventory filtering"); checks++;
        Require(session.Inventory("dns").Count() == 1, "DNS inventory filtering"); checks++;
        Require(session.Inventory("storage").Count() == 3, "storage inventory filtering"); checks++;
        Require(session.Inventory("functions").Count() == 1, "functions kind filtering"); checks++;
        var golden = session.Inventory("all"); var publicTrue = 0; var publicFalse = 0; var publicUnknown = 0; var tagged = 0; var ageSum = 0d;
        for (var i = 0; i < golden.Count(); i++) { var value = golden.Text(i, 6); if (value == "true") publicTrue++; else if (value == "false") publicFalse++; else publicUnknown++; if (golden.Text(i, 8) == "true") tagged++; ageSum += golden.Number(i, 5); }
        Require(golden.Count() == 24 && publicTrue == 8 && publicFalse == 8 && publicUnknown == 8, "golden public flags"); checks++;
        Require(tagged == 16 && Math.Abs(ageSum - 7524d) < 0.001, "golden ages and tags"); checks++;
        var storage = session.Inventory("storage"); var encryptedTrue = 0; var encryptedFalse = 0; var encryptedUnknown = 0; for (var i = 0; i < storage.Count(); i++) { var value = storage.Text(i, 7); if (value == "true") encryptedTrue++; else if (value == "false") encryptedFalse++; else encryptedUnknown++; } Require(encryptedTrue == 1 && encryptedFalse == 1 && encryptedUnknown == 1, "golden storage encryption flags"); checks++;
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
