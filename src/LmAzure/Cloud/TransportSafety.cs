using System.Net;
using System.Net.Http.Headers;

namespace LmAzure.Cloud;

internal static class TransportSafety
{
    internal const int MaxResponseBytes = 8 * 1024 * 1024;
    internal static HttpMessageHandler Wrap(HttpMessageHandler inner, IEnumerable<string> hosts, bool validateDestination)
        => new BoundedHandler(inner, new HashSet<string>(hosts, StringComparer.OrdinalIgnoreCase), validateDestination);

    private sealed class BoundedHandler : DelegatingHandler
    {
        private readonly HashSet<string> _hosts; private readonly bool _validateDestination;
        internal BoundedHandler(HttpMessageHandler inner, HashSet<string> hosts, bool validateDestination) : base(inner) { _hosts = hosts; _validateDestination = validateDestination; }
        protected override async Task<HttpResponseMessage> SendAsync(HttpRequestMessage request, CancellationToken cancellationToken)
        {
            if (_validateDestination && !Allowed(request.RequestUri)) throw new HttpRequestException("Azure request destination rejected");
            CostLedger.RecordRequest(request.Method.Method);
            var response = await base.SendAsync(request, cancellationToken).ConfigureAwait(false);
            if (response.Content is null) return response;
            await using var source = await response.Content.ReadAsStreamAsync(cancellationToken).ConfigureAwait(false);
            await using var buffer = new MemoryStream(); var chunk = new byte[8192]; var total = 0;
            while (true)
            {
                var read = await source.ReadAsync(chunk.AsMemory(), cancellationToken).ConfigureAwait(false);
                if (read == 0) break;
                total += read; if (total > MaxResponseBytes) { response.Dispose(); throw new HttpRequestException("Azure response exceeded the 8 MiB safety limit"); }
                await buffer.WriteAsync(chunk.AsMemory(0, read), cancellationToken).ConfigureAwait(false);
            }
            var replacement = new ByteArrayContent(buffer.ToArray());
            foreach (var header in response.Content.Headers) replacement.Headers.TryAddWithoutValidation(header.Key, header.Value);
            response.Content.Dispose(); response.Content = replacement;
            return response;
        }
        private bool Allowed(Uri? uri)
        {
            if (uri is null || uri.Scheme != Uri.UriSchemeHttps || !_hosts.Contains(uri.Host) || !string.IsNullOrEmpty(uri.UserInfo) || uri.Port != -1 && uri.Port != 443) return false;
            var query = uri.Query; return !query.Contains("sig=", StringComparison.OrdinalIgnoreCase) && !query.Contains("se=", StringComparison.OrdinalIgnoreCase) && !query.Contains("sp=", StringComparison.OrdinalIgnoreCase) && !query.Contains("token=", StringComparison.OrdinalIgnoreCase) && !query.Contains("password=", StringComparison.OrdinalIgnoreCase);
        }
    }
}
