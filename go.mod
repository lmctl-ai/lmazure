module github.com/lmctl-ai/lmazure

go 1.26.10

require (
	github.com/Azure/azure-sdk-for-go/sdk/azcore v1.23.3
	github.com/Azure/azure-sdk-for-go/sdk/storage/azblob v1.8.0
	github.com/Azure/azure-sdk-for-go/sdk/storage/azqueue v1.0.1
	github.com/lmctl-ai/lmcloud v0.0.0-00010101000000-000000000000
	github.com/lmctl-ai/lmcode-go v0.6.39-0.20261031234045-77932bf7134a
)

require (
	github.com/Azure/azure-sdk-for-go/sdk/internal v1.13.0 // indirect
	golang.org/x/net v0.58.0 // indirect
	golang.org/x/text v0.41.0 // indirect
)

// github.com/lmctl-ai/lmcloud is not published as a tagged module yet; resolve
// to the sibling checkout until lmcloud publishes a release.
replace github.com/lmctl-ai/lmcloud => ../lmcloud

// TEMPORARY: lmcode-go Registry.Enum (dry enum results) is not published yet.
// Remove when it is merged upstream and the lmcode-go pin is bumped.
replace github.com/lmctl-ai/lmcode-go => ../lmcode-go-patched
