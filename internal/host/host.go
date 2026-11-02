package host

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azqueue"

	"github.com/lmctl-ai/lmazure/internal/safety"
)

const (
	DefaultAccountName = "devstoreaccount1"
	// Well-known Azurite / local emulator development account key.
	DefaultDevAccountKey = "Eby8vdM02xNOcqFlqUwJPLlmEtlCDXJ1OUzFT50uSRZ6IFsuFq2UVErCz4I6tq/K1SZFPTOtr/KBHBeksoGMGw=="
	DefaultRegion        = "eastus"
)

// Options configures a new Host instance.
type Options struct {
	DefaultEndpoint string
	Region          string
	Ledger          *safety.RunLedger
}

type fakeObject struct {
	data        []byte
	contentType string
	etag        string
	metadata    map[string]string
}

type fakeQueueMsg struct {
	id           int64
	wireBody     string
	receipt      string
	visibleAfter time.Time
	acked        bool
}

type fakeQueue struct {
	name   string
	nextID int64
	msgs   []*fakeQueueMsg
}

// Host implements the shared lmcloud Cloud context for Azure (lmazure).
type Host struct {
	mu              sync.Mutex
	abortOnError    bool
	lastError       *CloudError
	endpoints       map[string]string
	defaultEndpoint string
	region          string
	ledger          *safety.RunLedger
	httpClient      *http.Client
	fakeBuckets     map[string]map[string]*fakeObject
	fakeQueues      map[string]*fakeQueue
	fakeReceiptSeq  int64
}

// New creates a new Host instance.
func New(opts Options) *Host {
	defEp := strings.TrimSpace(opts.DefaultEndpoint)
	if defEp == "" {
		defEp = firstNonEmpty(os.Getenv("LMAZURE_ENDPOINT"), os.Getenv("LMCLOUD_ENDPOINT"), os.Getenv("AZURE_STORAGE_ENDPOINT"))
	}
	defEp = strings.TrimRight(defEp, "/")

	reg := strings.TrimSpace(opts.Region)
	if reg == "" {
		reg = firstNonEmpty(os.Getenv("LMAZURE_REGION"), os.Getenv("AZURE_REGION"), DefaultRegion)
	}

	transport := &http.Transport{
		Proxy:               nil, // never consult HTTP(S)_PROXY
		MaxIdleConns:        32,
		MaxIdleConnsPerHost: 16,
		IdleConnTimeout:     30 * time.Second,
	}

	return &Host{
		abortOnError:    true,
		lastError:       okCloudError(),
		endpoints:       make(map[string]string),
		defaultEndpoint: defEp,
		region:          reg,
		ledger:          opts.Ledger,
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   30 * time.Second,
		},
		fakeBuckets: make(map[string]map[string]*fakeObject),
		fakeQueues:  make(map[string]*fakeQueue),
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if t := strings.TrimSpace(v); t != "" {
			return t
		}
	}
	return ""
}

// Provider returns the canonical provider identifier ("azure").
func (h *Host) Provider() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.lastError = okCloudError()
	return "azure"
}

// AbortOnError returns whether operations abort on error.
func (h *Host) AbortOnError() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.abortOnError
}

// SetAbortOnError updates the abort-on-error flag and returns the receiver.
func (h *Host) SetAbortOnError(abort bool) *Host {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.abortOnError = abort
	h.lastError = okCloudError()
	return h
}

// LastError returns the structured CloudError from the most recent operation.
func (h *Host) LastError() *CloudError {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.lastError == nil {
		return okCloudError()
	}
	return h.lastError
}

// Endpoint returns the configured endpoint for service (or default endpoint).
func (h *Host) Endpoint(service string) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if cerr := ValidateServiceName(service); cerr != nil {
		return "", h.finishErrorLocked("endpoint", service, cerr)
	}
	h.lastError = okCloudError()
	return h.resolveServiceEndpointLocked(service), nil
}

// SetEndpoint validates and sets the endpoint override for service.
func (h *Host) SetEndpoint(service, rawURL string) (*Host, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if cerr := ValidateServiceName(service); cerr != nil {
		return h, h.finishErrorLocked("set_endpoint", service, cerr)
	}
	norm, cerr := ValidateEndpointURL(rawURL)
	if cerr != nil {
		return h, h.finishErrorLocked("set_endpoint", rawURL, cerr)
	}
	h.endpoints[service] = norm
	if service == "" || service == "all" {
		h.defaultEndpoint = norm
	}
	h.lastError = okCloudError()
	return h, nil
}

func (h *Host) resolveServiceEndpointLocked(service string) string {
	if ep, ok := h.endpoints[service]; ok && ep != "" {
		return ep
	}
	if ep, ok := h.endpoints["all"]; ok && ep != "" {
		return ep
	}
	switch service {
	case "storage", "object-storage", "object_storage", "objectstorage", "s3", "blob", "gcs":
		for _, k := range []string{"storage", "blob", "objectstorage", "object-storage", "object_storage", "all", ""} {
			if ep, ok := h.endpoints[k]; ok && ep != "" {
				return ep
			}
		}
		if ep := firstNonEmpty(os.Getenv("LMCLOUD_STORAGE_ENDPOINT"), os.Getenv("LMAZURE_STORAGE_ENDPOINT"), os.Getenv("AZURE_BLOB_ENDPOINT")); ep != "" {
			return strings.TrimRight(ep, "/")
		}
	case "queue", "sqs", "pubsub":
		for _, k := range []string{"queue", "all", ""} {
			if ep, ok := h.endpoints[k]; ok && ep != "" {
				return ep
			}
		}
		if ep := firstNonEmpty(os.Getenv("LMCLOUD_QUEUE_ENDPOINT"), os.Getenv("LMAZURE_QUEUE_ENDPOINT"), os.Getenv("AZURE_QUEUE_ENDPOINT")); ep != "" {
			return strings.TrimRight(ep, "/")
		}
	case "":
		for _, k := range []string{"", "all", "storage", "blob", "queue"} {
			if ep, ok := h.endpoints[k]; ok && ep != "" {
				return ep
			}
		}
	}
	if h.defaultEndpoint != "" {
		return h.defaultEndpoint
	}
	return strings.TrimRight(firstNonEmpty(
		os.Getenv("LMCLOUD_ENDPOINT"),
		os.Getenv("LMAZURE_ENDPOINT"),
		os.Getenv("AZURE_STORAGE_ENDPOINT"),
	), "/")
}

// resolveBackendEndpoint determines whether to use the in-memory fake, a configured endpoint, or live Azure.
func (h *Host) resolveBackendEndpointLocked(serviceKind string) (endpoint string, useFake bool, cerr *CloudError) {
	ep := h.resolveServiceEndpointLocked(serviceKind)
	if ep != "" {
		return ep, false, nil
	}
	if os.Getenv("LMAZURE_LIVE") == "1" {
		if os.Getenv("CI") != "" && os.Getenv("LMAZURE_ALLOW_LIVE_IN_CI") != "1" {
			return "", false, newCloudError(CodePermissionDenied, 0, "LiveInCIDisabled",
				"live Azure calls in CI require both LMAZURE_LIVE=1 and LMAZURE_ALLOW_LIVE_IN_CI=1")
		}
		acct := h.resolveAccountName()
		if serviceKind == "queue" {
			return fmt.Sprintf("https://%s.queue.core.windows.net", acct), false, nil
		}
		return fmt.Sprintf("https://%s.blob.core.windows.net", acct), false, nil
	}
	return "", true, nil
}

func (h *Host) resolveAccountName() string {
	return firstNonEmpty(
		os.Getenv("LMAZURE_SIM_ACCOUNT"),
		os.Getenv("AZURE_STORAGE_ACCOUNT"),
		DefaultAccountName,
	)
}

func (h *Host) resolveCredentials() (accountName, accountKey, bearerToken string) {
	accountName = h.resolveAccountName()
	accountKey = firstNonEmpty(
		os.Getenv("LMAZURE_SIM_KEY"),
		os.Getenv("AZURE_STORAGE_KEY"),
	)
	bearerToken = firstNonEmpty(
		os.Getenv("LMAZURE_SIM_TOKEN"),
		os.Getenv("AZURE_STORAGE_TOKEN"),
	)
	// If neither key nor token is set, fall back to the well-known Azurite development key
	if accountKey == "" && bearerToken == "" {
		accountKey = DefaultDevAccountKey
	}
	return
}

// formatBlobServiceURL formats the base URL for Blob Storage client.
// In loopback path-style emulator mode, appends /<account> if not already present.
func (h *Host) formatBlobServiceURL(baseEndpoint string) string {
	u, err := url.Parse(baseEndpoint)
	if err != nil {
		return baseEndpoint
	}
	acct := h.resolveAccountName()
	if isLoopbackHost(u.Hostname()) {
		path := strings.TrimRight(u.Path, "/")
		if !strings.HasSuffix(path, "/"+acct) && path != "/"+acct {
			u.Path = path + "/" + acct
		}
	}
	return strings.TrimRight(u.String(), "/")
}

// formatQueueServiceURL formats the base URL for Queue Storage client.
// On azuresim loopback path-style, routes to /<account>-queue (e.g. /devstoreaccount1-queue).
func (h *Host) formatQueueServiceURL(baseEndpoint string) string {
	u, err := url.Parse(baseEndpoint)
	if err != nil {
		return baseEndpoint
	}
	acct := h.resolveAccountName()
	queuePathSegment := acct + "-queue"
	if isLoopbackHost(u.Hostname()) {
		path := strings.TrimRight(u.Path, "/")
		if !strings.HasSuffix(path, "/"+queuePathSegment) && path != "/"+queuePathSegment {
			u.Path = path + "/" + queuePathSegment
		}
	}
	return strings.TrimRight(u.String(), "/")
}

type staticBearerTokenCredential string

func (c staticBearerTokenCredential) GetToken(ctx context.Context, options policy.TokenRequestOptions) (azcore.AccessToken, error) {
	return azcore.AccessToken{
		Token:     string(c),
		ExpiresOn: time.Now().Add(24 * time.Hour),
	}, nil
}

func (h *Host) createBlobClient(baseEndpoint string) (*azblob.Client, error) {
	serviceURL := h.formatBlobServiceURL(baseEndpoint)
	acct, key, token := h.resolveCredentials()

	opts := &azblob.ClientOptions{
		ClientOptions: azcore.ClientOptions{
			Transport: h.httpClient,
		},
	}

	if token != "" {
		return azblob.NewClient(serviceURL, staticBearerTokenCredential(token), opts)
	}
	if key != "" {
		cred, err := azblob.NewSharedKeyCredential(acct, key)
		if err != nil {
			return nil, err
		}
		return azblob.NewClientWithSharedKeyCredential(serviceURL, cred, opts)
	}
	return azblob.NewClientWithNoCredential(serviceURL, opts)
}

func (h *Host) createQueueServiceClient(baseEndpoint string) (*azqueue.ServiceClient, error) {
	serviceURL := h.formatQueueServiceURL(baseEndpoint)
	acct, key, token := h.resolveCredentials()

	opts := &azqueue.ClientOptions{
		ClientOptions: azcore.ClientOptions{
			Transport: h.httpClient,
		},
	}

	if token != "" {
		return azqueue.NewServiceClient(serviceURL, staticBearerTokenCredential(token), opts)
	}
	if key != "" {
		cred, err := azqueue.NewSharedKeyCredential(acct, key)
		if err != nil {
			return nil, err
		}
		return azqueue.NewServiceClientWithSharedKeyCredential(serviceURL, cred, opts)
	}
	return azqueue.NewServiceClientWithNoCredential(serviceURL, opts)
}

func (h *Host) createQueueClient(baseEndpoint, queueName string) (*azqueue.QueueClient, error) {
	serviceURL := h.formatQueueServiceURL(baseEndpoint)
	queueURL := strings.TrimRight(serviceURL, "/") + "/" + queueName
	acct, key, token := h.resolveCredentials()

	opts := &azqueue.ClientOptions{
		ClientOptions: azcore.ClientOptions{
			Transport: h.httpClient,
		},
	}

	if token != "" {
		return azqueue.NewQueueClient(queueURL, staticBearerTokenCredential(token), opts)
	}
	if key != "" {
		cred, err := azqueue.NewSharedKeyCredential(acct, key)
		if err != nil {
			return nil, err
		}
		return azqueue.NewQueueClientWithSharedKeyCredential(queueURL, cred, opts)
	}
	return azqueue.NewQueueClientWithNoCredential(queueURL, opts)
}

func (h *Host) finishOKLocked() {
	h.lastError = okCloudError()
}

func (h *Host) finishErrorLocked(op, target string, cerr *CloudError) error {
	if cerr == nil {
		cerr = newCloudError(CodeInternal, 0, "InternalError", "unknown error")
	}
	h.lastError = cerr
	if h.abortOnError {
		return cerr.FormatAbortError(op, target)
	}
	return nil
}

func (h *Host) recordEntry(kind, service, action, id string) {
	if h.ledger != nil {
		h.ledger.Record(kind, id)
	}
}
