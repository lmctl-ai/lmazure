using System.Text;
using LmCode;
using LmAzure.Cloud;
using LmAzure.Tests;
using VmProgram = LmCode.Program;

namespace LmAzure;

public static class EntryPoint
{
    public const string Version = "0.1.4";
    public static int Main(string[] args)
    {
        Console.OutputEncoding = new UTF8Encoding(false);
        Console.InputEncoding = new UTF8Encoding(false);
        try
        {
            RuntimeBudget.Configure();
            int? relaunched = RuntimeBudget.RelaunchIfNeeded(args);
            if (relaunched.HasValue) return relaunched.Value;
            RuntimeBudget.Reset();
            if (args is ["--version"]) { Console.WriteLine("lmazure " + Version); return 0; }
            if (args is ["--help"] or []) { Console.WriteLine(Usage); return 0; }
            if (args is ["--notices"])
            {
                var assembly=typeof(EntryPoint).Assembly;
                foreach(var name in new[]{"THIRD_PARTY_NOTICES"}.Concat(assembly.GetManifestResourceNames().Where(n=>n.StartsWith("licenses/",StringComparison.Ordinal)).Order(StringComparer.Ordinal)))
                { using var s=assembly.GetManifestResourceStream(name)!; using var reader=new StreamReader(s); Console.WriteLine(name); Console.WriteLine(reader.ReadToEnd()); }
                return 0;
            }
            if (args is ["--dsl-schema"] or ["--dsl-schema", "--json"]) { Console.OpenStandardOutput().Write(Host.Schema); return 0; }
            if (args is ["--dsl-signatures"]) { Console.WriteLine(string.Join('\n',Host.CreateRegistry().HostSignatures())); return 0; }
            if (args is ["--self-test"])
            {
                int checks;
                try { checks=CoreSelfTests.Run(); } catch { Console.Error.WriteLine("fake transport smoke suite failed"); throw; }
                try { checks+=SafetySelfTests.Run(); } catch { Console.Error.WriteLine("fake transport safety suite failed"); throw; }
                try { checks+=CostSelfTests.Run(); } catch { Console.Error.WriteLine("fake transport cost suite failed"); throw; }
                try { checks+=CostRecoverySelfTests.Run(); } catch { Console.Error.WriteLine("fake transport cost recovery suite failed"); throw; }
                try { checks+=ReadonlyLedgerSelfTests.Run(); } catch { Console.Error.WriteLine("fake transport readonly ledger suite failed"); throw; }
                try { checks+=MetadataEvidenceSelfTests.Run(); } catch { Console.Error.WriteLine("fake metadata evidence suite failed"); throw; }
                Console.WriteLine($"lmazure fake-transport checks: {checks} passed"); return 0;
            }
            if (args is ["--dsl", var path]) return Run(path,false);
            if (args is ["--dsl", "--dry-run", var dryPath]) return Run(dryPath,true);
            if (args is ["--inventory", var kind]) { using var s=AzureSession.Mock(); var table=s.Inventory(kind); RuntimeBudget.Check(); Console.Write(table.CsvString()); return 0; }
            if (args is ["--live", "--inventory", var liveKind]) { using var s=AzureSession.Live(); var table=s.Inventory(liveKind); RuntimeBudget.Check(); Console.Write(table.CsvString()); return 0; }
            Console.Error.WriteLine("invalid arguments; use --help"); return 2;
        }
        catch (ArgumentException) { Console.Error.WriteLine("invalid configuration; LMAZURE_MAX_MEMORY_MB must be a positive whole number of MiB"); return 2; }
        catch (OutOfMemoryException) { Console.Error.WriteLine("LMAZURE_MAX_MEMORY_MB memory budget exceeded"); return Reports.ReportError("run",Error.Host("LMAZURE_MAX_MEMORY_MB memory budget exceeded")); }
        catch (Error e) { if(e.Message.Contains("LMAZURE_MAX_MEMORY_MB",StringComparison.Ordinal)) Console.Error.WriteLine("LMAZURE_MAX_MEMORY_MB memory budget exceeded"); return Reports.ReportError("run",e); }
        catch (Exception) { return Reports.ReportError("run",Error.Host("Azure operation failed; diagnostics suppressed to protect credentials")); }
    }

    private static int Run(string path,bool dry)
    {
        var registry=Host.CreateRegistry();
        VmProgram program;
        try { var info=new FileInfo(path); if(info.Length>16*1024*1024) throw Error.Host("bytecode exceeds 16 MiB"); program=VmProgram.Load(File.ReadAllBytes(path),Host.Schema,registry); }
        catch(Error e) { return Reports.ReportError("load",e); }
        catch(Exception) { return Reports.ReportError("load",new Error("LOAD_BAD_BYTECODE","cannot read bytecode")); }
        if(dry) return Reports.ReportOk("null");
        var context=new Context();
        try { RuntimeBudget.Reset(); program.Run(context,registry); }
        finally { Host.Dispose(context); }
        return Reports.ReportOk("null");
    }

    private const string Usage="""
        lmazure — Azure SDK host; fake transport by default
        lmazure --dsl [--dry-run] FILE | --dsl-schema [--json] | --dsl-signatures
        lmazure --inventory KIND | --live --inventory KIND
        lmazure --self-test | --version | --notices | --help
        Live reads: LMAZURE_LIVE=1, LMAZURE_SUBSCRIPTION_ID, LMAZURE_REGION;
        DefaultAzureCredential uses your configured environment/profile/login.
        Writes also require LMAZURE_ALLOW_WRITES=1; only ledger-owned prefixed
        containers and queues may be changed/deleted. Sweep defaults to dry-run.
        LMAZURE_MAX_MEMORY_MB: positive managed allocation budget, default 4096 MiB.
        Cost ledger: $0.05 per example by default; $0.25 per process hard ceiling.
        Declare run.cost_cap before creation; run.cost_report exports estimates.
        Estimates use a conservative price table, not live billing measurements.
        CI uses fakes only, with no Azure credentials or OIDC.
        No live testing has been performed. See docs/LIVE_TESTING.md.
        """;
}
