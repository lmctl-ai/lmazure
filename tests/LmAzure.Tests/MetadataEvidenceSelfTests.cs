using System.Text.Json;
using LmAzure.Cloud;
namespace LmAzure.Tests;
public static class MetadataEvidenceSelfTests
{
    public static int Run()
    {
        CostLedger.ResetForTests();
        var path=Path.Combine(Path.GetTempPath(),"lmazure-metadata-"+Guid.NewGuid().ToString("N")+".json");
        using var session=AzureSession.Mock();
        using var run=session.BeginRun(path);
        var resource=run.CreateQueue("fingerprint");run.Tag(resource.Text(0,0),"password","private-test-value-never-export");run.Cleanup();run.CostReport(path+".report");
        var text=File.ReadAllText(path+".report");using var doc=JsonDocument.Parse(text);var entry=doc.RootElement.GetProperty("resources")[0];
        if(!entry.GetProperty("metadata_owner_verified").GetBoolean())throw new InvalidOperationException("ownership metadata not observed");
        if(entry.GetProperty("tags").GetProperty("lmazure_test").GetString()!=doc.RootElement.GetProperty("run_id").GetString())throw new InvalidOperationException("ownership tag mismatch");
        if(entry.GetProperty("metadata_sha256").GetString()?.Length!=64)throw new InvalidOperationException("metadata fingerprint missing");
        if(text.Contains("private-test-value-never-export",StringComparison.Ordinal)||entry.GetProperty("tags").TryGetProperty("password",out _))throw new InvalidOperationException("metadata secret leaked");
        File.Delete(path+".report");CostLedger.ResetForTests();return 4;
    }
}
