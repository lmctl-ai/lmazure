// SPDX-License-Identifier: MIT

// Package azure implements the lmcloud hostkit ObjectStore and Queue adapters
// for Azure Blob Storage and Azure Queue Storage using the official Azure Go
// SDKs (github.com/Azure/azure-sdk-for-go/sdk/storage/azblob and azqueue).
package azure

import (
	"context"
	"net/http"
	"os"

	"github.com/lmctl-ai/lmcloud/hostkit"
	"github.com/lmctl-ai/lmcloud/hostkit/fakeadapter"
)

// Options configures the Azure adapter. Zero values use production defaults.
type Options struct {
	Getenv     func(string) string
	HTTPClient *http.Client
	Retry      *hostkit.RetryPolicy
	// CompatProbeMissingDelete enables pre-operation container/queue existence
	// probes on DeleteBucket, ListObjects, HeadObject (on 404), DeleteObject
	// (on 404), and DeleteQueue, plus in-process message-body caching for
	// UpdateMessage, so released azuresim binaries built before commit 7903c9c
	// return NOT_FOUND on missing containers/queues and preserve message bodies
	// on visibility updates.
	//
	// Defaults to true unless LMAZURE_COMPAT_PROBE_EXISTENCE=0.
	// Removal condition: delete once an azuresim release containing commit
	// 7903c9c is published and pinned by lmcloud/scripts/sims.lock.
	CompatProbeMissingDelete *bool
}

// Adapter implements hostkit.ObjectStore, hostkit.Queue,
// hostkit.QueueCapsProvider, hostkit.BucketCreateTokenReader, and
// hostkit.QueueCreateTokenReader by delegating through hostkit.Router and
// hostkit.EndpointRoutes:
//   - "" and LMAZURE_LIVE unset: offline in-process fake (hostkit fakeadapter);
//   - "" with LMAZURE_LIVE=1, or any non-loopback endpoint: live Azure, which
//     returns an explicit UNAUTHENTICATED error until live DefaultAzureCredential
//     support is wired in a later phase;
//   - a loopback endpoint (azuresim): Azure Blob / Queue REST over the official
//     azblob/azqueue SDKs with SDK retries disabled so hostkit.RetryPolicy owns
//     all retry behavior.
type Adapter struct {
	router hostkit.Router
	remote *remote
}

// New constructs an Adapter with the given options (or defaults when nil).
func New(opts *Options) *Adapter {
	var o Options
	if opts != nil {
		o = *opts
	}
	if o.Getenv == nil {
		o.Getenv = os.Getenv
	}
	rem := newRemote(o)
	fk := fakeadapter.New(fakeadapter.Options{})
	routes := hostkit.EndpointRoutes{
		Offline: fk,
		Sim:     rem,
		UseLive: func() bool { return o.Getenv("LMAZURE_LIVE") == "1" },
		Unavailable: func(reason string) error {
			return errLiveNotImplemented(reason)
		},
	}
	return &Adapter{
		router: hostkit.Router{Pick: routes.Pick},
		remote: rem,
	}
}

// Endpoints returns the hostkit EndpointResolver configured with Azure's native
// environment variables (lmcloud design 02 §3.3).
func Endpoints() *hostkit.EndpointResolver {
	return &hostkit.EndpointResolver{
		Prefix: "LMAZURE",
		Native: map[string][]hostkit.NativeEnv{
			"storage": {
				{Name: "AZURE_STORAGE_BLOB_ENDPOINT"},
				{Name: "AZURE_BLOB_ENDPOINT"},
				{Name: "AZURE_STORAGE_ENDPOINT"},
				{Name: "AZURE_ENDPOINT"},
			},
			"queue": {
				{Name: "AZURE_STORAGE_QUEUE_ENDPOINT"},
				{Name: "AZURE_QUEUE_ENDPOINT"},
				{Name: "AZURE_STORAGE_ENDPOINT"},
				{Name: "AZURE_ENDPOINT"},
			},
		},
	}
}

// --- hostkit.ObjectStore ---

func (a *Adapter) CreateBucket(ctx context.Context, b string) (bool, error) {
	return a.router.CreateBucket(ctx, b)
}

func (a *Adapter) DeleteBucket(ctx context.Context, b string) error {
	return a.router.DeleteBucket(ctx, b)
}

func (a *Adapter) HeadBucket(ctx context.Context, b string) error {
	return a.router.HeadBucket(ctx, b)
}

func (a *Adapter) ListBuckets(ctx context.Context, token string) ([]string, string, error) {
	return a.router.ListBuckets(ctx, token)
}

func (a *Adapter) PutObject(ctx context.Context, b, k string, body []byte, ct string, meta map[string]string) (hostkit.ObjectMeta, error) {
	return a.router.PutObject(ctx, b, k, body, ct, meta)
}

func (a *Adapter) GetObject(ctx context.Context, b, k string, limit int64) ([]byte, error) {
	return a.router.GetObject(ctx, b, k, limit)
}

func (a *Adapter) HeadObject(ctx context.Context, b, k string) (hostkit.ObjectMeta, error) {
	return a.router.HeadObject(ctx, b, k)
}

func (a *Adapter) DeleteObject(ctx context.Context, b, k string) error {
	return a.router.DeleteObject(ctx, b, k)
}

func (a *Adapter) ListObjects(ctx context.Context, b, prefix string, max int, token string) (hostkit.ObjectPage, error) {
	return a.router.ListObjects(ctx, b, prefix, max, token)
}

// --- hostkit.BucketCreateTokenReader & hostkit.QueueCreateTokenReader ---

func (a *Adapter) BucketCreateToken(ctx context.Context, b string) (string, error) {
	return a.router.BucketCreateToken(ctx, b)
}

func (a *Adapter) QueueCreateToken(ctx context.Context, q string) (string, error) {
	return a.router.QueueCreateToken(ctx, q)
}

// --- hostkit.Queue & hostkit.QueueCapsProvider ---

// QueueCaps returns the default queue capabilities. Per-call capabilities are
// resolved through QueueCapsFor via hostkit.Router.
func (a *Adapter) QueueCaps() hostkit.QueueCaps {
	return a.remote.QueueCaps()
}

// QueueCapsFor returns the queue capabilities for the target selected by ctx
// (offline fake uses TextOnlyWire: true; remote Azure Queue adapter base64-encodes
// the entire wire message and uses TextOnlyWire: false).
func (a *Adapter) QueueCapsFor(ctx context.Context, q string) hostkit.QueueCaps {
	return a.router.QueueCapsFor(ctx, q)
}

func (a *Adapter) CreateQueue(ctx context.Context, q string) (bool, error) {
	return a.router.CreateQueue(ctx, q)
}

func (a *Adapter) DeleteQueue(ctx context.Context, q string) error {
	return a.router.DeleteQueue(ctx, q)
}

func (a *Adapter) LookupQueue(ctx context.Context, q string) error {
	return a.router.LookupQueue(ctx, q)
}

func (a *Adapter) ListQueues(ctx context.Context, token string) ([]string, string, error) {
	return a.router.ListQueues(ctx, token)
}

func (a *Adapter) Send(ctx context.Context, q, wire string) (string, error) {
	return a.router.Send(ctx, q, wire)
}

func (a *Adapter) Receive(ctx context.Context, q string, max, wait, vis int) ([]hostkit.RawMessage, error) {
	return a.router.Receive(ctx, q, max, wait, vis)
}

func (a *Adapter) Ack(ctx context.Context, q, receipt string) error {
	return a.router.Ack(ctx, q, receipt)
}

func (a *Adapter) Nack(ctx context.Context, q, receipt string, vis int) (string, error) {
	return a.router.Nack(ctx, q, receipt, vis)
}
