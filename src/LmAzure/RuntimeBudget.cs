using System.Diagnostics;
using System.Globalization;
using LmCode;

namespace LmAzure;

public static class RuntimeBudget
{
    private static long limit;
    private static long baseline;
    private static long checkpoints;
    public static long LimitBytes => limit;
    public static void Configure()
    {
        string raw=Environment.GetEnvironmentVariable("LMAZURE_MAX_MEMORY_MB") ?? "4096";
        if(!long.TryParse(raw,NumberStyles.None,CultureInfo.InvariantCulture,out long mb) || mb<=0 || mb>long.MaxValue/(1024*1024)) throw new ArgumentException();
        limit=mb*1024*1024;
        if(mb<16) throw Error.Host("LMAZURE_MAX_MEMORY_MB budget is too small to initialize Azure clients");
    }
    public static int? RelaunchIfNeeded(string[] args)
    {
        long heap=Math.Min(3072L*1024*1024,Math.Max(64L*1024*1024,limit/4*3));
        string cap=heap.ToString("X",CultureInfo.InvariantCulture);
        if(Environment.GetEnvironmentVariable("DOTNET_GCHeapHardLimit")==cap && Environment.GetEnvironmentVariable("LMAZURE_GC_CONFIGURED")=="1") return null;
        string executable=Environment.ProcessPath ?? throw Error.Host("cannot set LMAZURE_MAX_MEMORY_MB startup heap limit");
        var start=new ProcessStartInfo(executable) { UseShellExecute=false };
        if(Path.GetFileNameWithoutExtension(executable).Equals("dotnet",StringComparison.OrdinalIgnoreCase)) start.ArgumentList.Add(Environment.GetCommandLineArgs()[0]);
        foreach(string arg in args) start.ArgumentList.Add(arg);
        start.Environment["DOTNET_GCHeapHardLimit"]=cap;
        start.Environment["LMAZURE_GC_CONFIGURED"]="1";
        using var process=Process.Start(start) ?? throw Error.Host("cannot start budgeted Azure host");
        process.WaitForExit();
        return process.ExitCode;
    }
    public static void Reset() { baseline=GC.GetTotalAllocatedBytes(false); checkpoints=0; }
    public static void Check()
    {
        if(GC.GetTotalAllocatedBytes(false)-baseline>limit) throw Error.Host("LMAZURE_MAX_MEMORY_MB memory budget exceeded");
        if(++checkpoints>50000) throw Error.Host("execution instruction limit exceeded");
    }
}
