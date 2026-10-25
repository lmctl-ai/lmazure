using System.Text;
using System.Text.Json;
using LmCode;
using LmAzure.Cloud;

namespace LmAzure;

/// <summary>Typed lmcode-csharp bindings for the Azure host.</summary>
public static class Host
{
    // Keep this byte-for-byte stable: lmcode verifies the schema hash in every program.
    private sealed record Spec(string? Type, string Name, string[] Parameters, string Returns);
    private static readonly Spec[] Specs =
    [
        new(null, "mock", [], "Session"), new(null, "live", [], "Session"),
        new("Session", "inventory", ["string"], "Table"), new("Session", "begin_run", ["string"], "Run"),
        new("Session", "sweep", ["string", "float"], "Table"), new("Session", "sweep_apply", ["string", "float"], "Table"),
        new("Run", "container", ["string"], "Table"), new("Run", "queue", ["string"], "Table"),
        new("Run", "tag", ["string", "string", "string"], "void"), new("Run", "delete", ["string"], "void"), new("Run", "cleanup", [], "void"),
        new("Run", "cost_cap", ["float"], "void"), new("Run", "estimated_cost", [], "float"), new("Run", "cost_report", ["string"], "void"),
        new("Table", "rows", [], "int"), new("Table", "columns", [], "int"), new("Table", "column", ["int"], "string"),
        new("Table", "text", ["int", "int"], "string"), new("Table", "number", ["int", "int"], "float"),
        new("Table", "csv", ["string"], "void"), new("Table", "json", ["string"], "void"),
        new("Table", "csv_string", [], "string"), new("Table", "json_string", [], "string")
    ];

    public static byte[] Schema => BuildSchema();

    public static Registry Registry() => CreateRegistry();
    public static Registry BuildRegistry() => CreateRegistry();

    public static Registry CreateRegistry()
    {
        var r = new Registry();
        r.Function("mock", [], "Session", static (c, _) => Safe(() => SessionValue(c, AzureSession.Mock())));
        r.Function("live", [], "Session", static (c, _) => Safe(() => SessionValue(c, AzureSession.Live())));

        r.Method("Session", "inventory", ["string"], "Table", static (c, self, a) =>
            Safe(() => TableValue(c, c.Get<SessionHandle>(self).Session.Inventory(Text(a[0])))));
        r.Method("Session", "begin_run", ["string"], "Run", static (c, self, a) =>
            Safe(() => RunValue(c, c.Get<SessionHandle>(self).Session.BeginRun(Text(a[0])))));
        r.Method("Session", "sweep", ["string", "float"], "Table", static (c, self, a) =>
            Safe(() => TableValue(c, c.Get<SessionHandle>(self).Session.Sweep(Text(a[0]), a[1].AsFloat(), false))));
        r.Method("Session", "sweep_apply", ["string", "float"], "Table", static (c, self, a) =>
            Safe(() => TableValue(c, c.Get<SessionHandle>(self).Session.Sweep(Text(a[0]), a[1].AsFloat(), true))));

        r.Method("Run", "container", ["string"], "Table", static (c, self, a) =>
            Safe(() => TableValue(c, c.Get<RunHandle>(self).Run.CreateContainer(Text(a[0])))));
        r.Method("Run", "queue", ["string"], "Table", static (c, self, a) =>
            Safe(() => TableValue(c, c.Get<RunHandle>(self).Run.CreateQueue(Text(a[0])))));
        r.Method("Run", "tag", ["string", "string", "string"], "void", static (c, self, a) =>
        {
            return Safe(() => { c.Get<RunHandle>(self).Run.Tag(Text(a[0]), Text(a[1]), Text(a[2])); return Value.None; });
        });
        r.Method("Run", "delete", ["string"], "void", static (c, self, a) =>
        {
            return Safe(() => { c.Get<RunHandle>(self).Run.Delete(Text(a[0])); return Value.None; });
        });
        r.Method("Run", "cleanup", [], "void", static (c, self, _) =>
        {
            return Safe(() => { c.Get<RunHandle>(self).Run.Cleanup(); return Value.None; });
        });
        r.Method("Run", "cost_cap", ["float"], "void", static (c, self, a) =>
            Safe(() => { c.Get<RunHandle>(self).Run.SetCostCap(a[0].AsFloat()); return Value.None; }));
        r.Method("Run", "estimated_cost", [], "float", static (c, self, _) =>
            Safe(() => Value.Float(c.Get<RunHandle>(self).Run.EstimatedCost())));
        r.Method("Run", "cost_report", ["string"], "void", static (c, self, a) =>
            Safe(() => { c.Get<RunHandle>(self).Run.CostReport(Text(a[0])); return Value.None; }));

        r.Method("Table", "rows", [], "int", static (c, self, _) => Safe(() => Value.Int(c.Get<ResourceTable>(self).Count())));
        r.Method("Table", "columns", [], "int", static (c, self, _) => Safe(() => Value.Int(c.Get<ResourceTable>(self).Columns())));
        r.Method("Table", "column", ["int"], "string", static (c, self, a) =>
            Safe(() => Value.Str(VmString.FromString(c.Get<ResourceTable>(self).Column(CheckedIndex(a[0]))))));
        r.Method("Table", "text", ["int", "int"], "string", static (c, self, a) =>
            Safe(() => Value.Str(VmString.FromString(c.Get<ResourceTable>(self).Text(CheckedIndex(a[0]), CheckedIndex(a[1]))))));
        r.Method("Table", "number", ["int", "int"], "float", static (c, self, a) =>
            Safe(() => Value.Float(c.Get<ResourceTable>(self).Number(CheckedIndex(a[0]), CheckedIndex(a[1])))));
        r.Method("Table", "csv", ["string"], "void", static (c, self, a) =>
        {
            return Safe(() => { c.Get<ResourceTable>(self).Csv(Text(a[0])); return Value.None; });
        });
        r.Method("Table", "json", ["string"], "void", static (c, self, a) =>
        {
            return Safe(() => { c.Get<ResourceTable>(self).Json(Text(a[0])); return Value.None; });
        });
        r.Method("Table", "csv_string", [], "string", static (c, self, _) =>
            Safe(() => Value.Str(VmString.FromString(c.Get<ResourceTable>(self).CsvString()))));
        r.Method("Table", "json_string", [], "string", static (c, self, _) =>
            Safe(() => Value.Str(VmString.FromString(c.Get<ResourceTable>(self).JsonString()))));

        r.CheckSchemaSignatures(Signatures);
        return r;
    }

    public static IReadOnlyList<string> Signatures { get; } = Specs.Select(Signature).Order(StringComparer.Ordinal).ToArray();

    /// Dispose all provider handles held by a VM context. Call this from the runner's finally block.
    private static readonly Dictionary<Context, List<IDisposable>> Handles = new();
    private static readonly object HandleLock = new();

    public static void Dispose(Context context)
    {
        List<IDisposable>? handles;
        lock (HandleLock) { Handles.Remove(context, out handles); }
        if (handles is null) return;
        Exception? failure = null;
        for (int i = handles.Count - 1; i >= 0; --i)
        {
            try { handles[i].Dispose(); } catch (Exception ex) { failure ??= ex; }
        }
        if (failure is not null) throw Error.Host("Azure cleanup failed");
    }

    private static Value Safe(Func<Value> operation)
    {
        try { return operation(); }
        catch (Error) { throw; }
        catch (OutOfMemoryException) { throw; }
        catch (CostLimitException e) { throw Error.Host(e.Message); }
        catch (AzureCloudException) { throw Error.Host("Azure operation failed"); }
        catch (Exception) { throw Error.Host("Azure operation failed"); }
    }

    private static byte[] BuildSchema()
    {
        using var stream = new MemoryStream();
        using (var json = new Utf8JsonWriter(stream, new JsonWriterOptions { Indented = false }))
        {
            json.WriteStartObject(); json.WriteNumber("dsl.schema", 2); json.WriteString("name", "lmazure");
            json.WriteStartObject("types");
            foreach (var type in Specs.Where(x => x.Type is not null).Select(x => x.Type!).Distinct(StringComparer.Ordinal).Order())
            {
                json.WriteStartObject(type); json.WriteStartObject("methods");
                foreach (var spec in Specs.Where(x => x.Type == type).OrderBy(x => x.Name, StringComparer.Ordinal)) WriteOverload(json, spec);
                json.WriteEndObject(); json.WriteEndObject();
            }
            json.WriteEndObject(); json.WriteStartObject("functions");
            foreach (var spec in Specs.Where(x => x.Type is null).OrderBy(x => x.Name, StringComparer.Ordinal)) WriteOverload(json, spec);
            json.WriteEndObject(); json.WriteStartObject("context"); json.WriteEndObject(); json.WriteEndObject(); json.Flush();
        }
        return stream.ToArray();
    }

    private static void WriteOverload(Utf8JsonWriter json, Spec spec)
    {
        json.WritePropertyName(spec.Name); json.WriteStartArray(); json.WriteStartObject();
        json.WriteStartArray("params"); foreach (var parameter in spec.Parameters) json.WriteStringValue(parameter); json.WriteEndArray();
        json.WriteString("returns", spec.Returns); json.WriteEndObject(); json.WriteEndArray();
    }

    private static string Signature(Spec spec) => $"{(spec.Type is null ? "fn " : $"method {spec.Type}.")}{spec.Name}({string.Join(", ", spec.Parameters)}) -> {spec.Returns}";
    private static void Track(Context context, IDisposable handle)
    {
        lock (HandleLock) { if (!Handles.TryGetValue(context, out var list)) Handles.Add(context, list = []); list.Add(handle); }
    }

    private static Value SessionValue(Context c, AzureSession session)
    {
        RuntimeBudget.Check();
        var handle = new SessionHandle(session); Track(c, handle); return Value.Object("Session", c.Insert("Session", handle));
    }

    private static Value RunValue(Context c, AzureRun run)
    {
        RuntimeBudget.Check();
        var handle = new RunHandle(run); Track(c, handle); return Value.Object("Run", c.Insert("Run", handle));
    }

    private static Value TableValue(Context c, ResourceTable table)
    {
        RuntimeBudget.Check();
        return Value.Object("Table", c.Insert("Table", table));
    }

    private static string Text(Value value) => value.AsStr().ToString();
    private static int CheckedIndex(Value value)
    {
        long index = value.AsInt();
        if (index is < 0 or > int.MaxValue) throw Error.Host("table index is out of range");
        return (int)index;
    }

    private sealed class SessionHandle(AzureSession session) : IDisposable
    {
        public AzureSession Session { get; } = session;
        private int disposed;
        public void Dispose() { if (Interlocked.Exchange(ref disposed, 1) == 0) Session.Dispose(); }
    }

    private sealed class RunHandle(AzureRun run) : IDisposable
    {
        public AzureRun Run { get; } = run;
        private int disposed;
        public void Dispose() { if (Interlocked.Exchange(ref disposed, 1) == 0) Run.Dispose(); }
    }
}
