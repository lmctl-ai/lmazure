namespace LmAzure.Cloud;

public sealed class AzureCloudException : Exception
{
    public AzureCloudException(string message) : base(message) { }
    public AzureCloudException(string message, Exception inner) : base(message, inner) { }
}
