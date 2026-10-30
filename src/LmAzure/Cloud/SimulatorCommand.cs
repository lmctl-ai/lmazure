using Azure;
using Azure.Core;
using Azure.Core.Pipeline;
using Azure.Storage;
using Azure.Storage.Blobs;
using Azure.Storage.Queues;
using System.Text.Json;
using System.Text.RegularExpressions;

namespace LmAzure.Cloud;

/// <summary>Explicit local-only SDK client. Never enters AzureSession.Live or discovers credentials.</summary>
internal static class SimulatorCommand
{
    internal static int Run(string[] args)
    {
        try
        {
            var endpoint = Environment.GetEnvironmentVariable("LMAZURE_SIM_ENDPOINT") ?? "http://127.0.0.1:4577";
            if (!Uri.TryCreate(endpoint, UriKind.Absolute, out var uri) || uri.Scheme != "http" || uri.Host != "127.0.0.1"
                || uri.AbsolutePath != "/" || uri.UserInfo.Length != 0 || uri.Query.Length != 0 || uri.Fragment.Length != 0)
                return Result(false, "InvalidSimulatorEndpoint", 2);
            var account = Environment.GetEnvironmentVariable("LMAZURE_SIM_ACCOUNT") ?? "devstoreaccount1";
            if (!Regex.IsMatch(account, "\\A[a-z0-9]{3,24}\\z")) return Result(false, "InvalidSimulatorAccount", 2);
            var key = Environment.GetEnvironmentVariable("LMAZURE_SIM_KEY");
            var token = Environment.GetEnvironmentVariable("LMAZURE_SIM_TOKEN");
            if (key is not null && token is not null) return Result(false, "AmbiguousSimulatorCredential", 2);
            if (token is not null && !Regex.IsMatch(token, "\\A[A-Za-z0-9._~+/-]+=*\\z")) return Result(false, "InvalidSimulatorCredential", 2);
            StorageSharedKeyCredential? credential = key is null ? null : new StorageSharedKeyCredential(account, key);
            var operations = new Dictionary<string, int> {
                ["container-create"]=2, ["container-delete"]=2, ["blob-put"]=4, ["blob-get"]=4,
                ["queue-create"]=2, ["queue-delete"]=2, ["queue-send"]=3, ["queue-peek"]=2,
                ["queue-receive"]=2, ["queue-delete-message"]=4
            };
            if (args.Length == 0 || !operations.TryGetValue(args[0], out var count) || args.Length != count
                || !Regex.IsMatch(args[1], "\\A[a-z0-9][a-z0-9-]{1,61}[a-z0-9]\\z"))
                return Result(false, "InvalidSimulatorArguments", 2);
            using var client = new HttpClient(new HttpClientHandler { AllowAutoRedirect = false, UseProxy = false }) { Timeout = TimeSpan.FromSeconds(15) };
            var transport = new HttpClientTransport(client);
            var blobOptions = new BlobClientOptions { Transport = transport };
            var queueOptions = new QueueClientOptions { Transport = transport };
            blobOptions.Retry.MaxRetries = queueOptions.Retry.MaxRetries = 0;
            blobOptions.Retry.NetworkTimeout = queueOptions.Retry.NetworkTimeout = TimeSpan.FromSeconds(15);
            if (token is not null) {
                blobOptions.AddPolicy(new DummyBearerPolicy(token), HttpPipelinePosition.PerCall);
                queueOptions.AddPolicy(new DummyBearerPolicy(token), HttpPipelinePosition.PerCall);
            }
            var blobUri = new Uri(endpoint.TrimEnd('/') + "/" + account + "/" + args[1]);
            var queueUri = new Uri(endpoint.TrimEnd('/') + "/" + account + "-queue/" + args[1]);
            var container = credential is null ? new BlobContainerClient(blobUri, blobOptions) : new BlobContainerClient(blobUri, credential, blobOptions);
            var queue = credential is null ? new QueueClient(queueUri, queueOptions) : new QueueClient(queueUri, credential, queueOptions);
            switch (args[0]) {
                case "container-create": container.Create(); break;
                case "container-delete": container.Delete(); break;
                case "blob-put":
                    using (var stream = File.OpenRead(args[3])) container.GetBlobClient(args[2]).Upload(stream, overwrite:true);
                    break;
                case "blob-get": container.GetBlobClient(args[2]).DownloadTo(args[3]); break;
                case "queue-create": queue.Create(); break;
                case "queue-delete": queue.Delete(); break;
                case "queue-send": queue.SendMessage(args[2]); break;
                case "queue-peek":
                    var peek = queue.PeekMessages(1).Value;
                    return Result(true, null, 0, peek.Length == 0 ? null : peek[0].MessageText);
                case "queue-receive":
                    var messages = queue.ReceiveMessages(1, TimeSpan.FromSeconds(30)).Value;
                    return messages.Length == 0 ? Result(true, null, 0) : Result(true, null, 0, messages[0].MessageText, messages[0].MessageId, messages[0].PopReceipt);
                case "queue-delete-message": queue.DeleteMessage(args[2], args[3]); break;
            }
            return Result(true, null, 0);
        }
        catch (RequestFailedException ex) {
            var code = ex.ErrorCode is "AuthenticationFailed" or "AuthorizationPermissionMismatch" or "ContainerNotFound" or "BlobNotFound" or "QueueNotFound" or "ContainerAlreadyExists" ? ex.ErrorCode : "StorageError";
            return Result(false, code, 3, status:ex.Status);
        }
        catch (FormatException) { return Result(false, "InvalidSimulatorCredential", 2); }
        catch (ArgumentException) { return Result(false, "InvalidSimulatorArguments", 2); }
        catch (Exception) { return Result(false, "SimulatorOperationFailed", 3); }
    }

    private static int Result(bool ok, string? error, int exit, string? message=null, string? id=null, string? receipt=null, int? status=null)
    {
        using var writer = new Utf8JsonWriter(Console.OpenStandardOutput());
        writer.WriteStartObject(); writer.WriteBoolean("ok", ok);
        if (error is not null) writer.WriteString("error", error);
        if (status is not null) writer.WriteNumber("status", status.Value);
        if (message is not null) writer.WriteString("message", message);
        if (id is not null) writer.WriteString("id", id);
        if (receipt is not null) writer.WriteString("receipt", receipt);
        writer.WriteEndObject(); writer.Flush();
        return exit;
    }

    private sealed class DummyBearerPolicy(string token) : HttpPipelineSynchronousPolicy
    {
        public override void OnSendingRequest(HttpMessage message) => message.Request.Headers.SetValue("Authorization", "Bearer " + token);
    }
}
