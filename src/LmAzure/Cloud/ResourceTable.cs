using System.Globalization;
using System.Text;
using System.Text.Json;

namespace LmAzure.Cloud;

public sealed class ResourceTable
{
    public static readonly string[] StableColumns = ["name", "kind", "location", "status", "count", "age_hours", "public", "encrypted", "tagged", "id"];
    private readonly List<object?[]> _rows = new();
    internal ResourceTable(IEnumerable<object?[]> rows) { _rows.AddRange(rows); }
    public int Count() => _rows.Count;
    public int Columns() => StableColumns.Length;
    public string Column(int index) => StableColumns[index];
    public string Text(int row, int column) => Convert.ToString(_rows[row][column], CultureInfo.InvariantCulture) ?? "";
    public double Number(int row, int column) => _rows[row][column] is null ? throw new InvalidOperationException("resource value is unknown") : Convert.ToDouble(_rows[row][column], CultureInfo.InvariantCulture);
    public void Csv(string path) => File.WriteAllText(path, CsvString(), new UTF8Encoding(false));
    public void Json(string path) => File.WriteAllText(path, JsonString(), new UTF8Encoding(false));
    public string CsvString()
    {
        var b = new StringBuilder(); AppendCsvRow(b, StableColumns);
        foreach (var row in _rows) AppendCsvRow(b, row);
        return b.ToString();
    }
    public string JsonString()
    {
        using var stream = new MemoryStream();
        using (var writer = new Utf8JsonWriter(stream, new JsonWriterOptions { Indented = true }))
        {
            writer.WriteStartArray();
            foreach (var row in _rows)
            {
                writer.WriteStartObject();
                for (var i = 0; i < StableColumns.Length; i++)
                {
                    writer.WritePropertyName(StableColumns[i]);
                    switch (row[i]) { case null: writer.WriteNullValue(); break; case string s: writer.WriteStringValue(s); break; case bool b: writer.WriteBooleanValue(b); break; case int n: writer.WriteNumberValue(n); break; case double d: writer.WriteNumberValue(d); break; default: writer.WriteStringValue(row[i]!.ToString()); break; }
                }
                writer.WriteEndObject();
            }
            writer.WriteEndArray();
        }
        return Encoding.UTF8.GetString(stream.ToArray()) + "\n";
    }
    internal static ResourceTable Empty() => new([]);
    internal static object?[] Row(string name, string kind, string location, string status, int count, double age, bool? @public, bool? encrypted, bool tagged, string id)
        => [name, kind, location, status, count, age, @public, encrypted, tagged, id];
    private static void AppendCsvRow(StringBuilder b, IEnumerable<object?> values)
    {
        var first = true;
        foreach (var value in values)
        {
            if (!first) b.Append(','); first = false;
            var text = value switch { null => "", bool v => v ? "true" : "false", IFormattable f => f.ToString(null, CultureInfo.InvariantCulture), _ => value.ToString() ?? "" };
            if (text.IndexOfAny([',', '"', '\n', '\r']) >= 0) b.Append('"').Append(text.Replace("\"", "\"\"")) .Append('"'); else b.Append(text);
        }
        b.Append('\n');
    }
}
