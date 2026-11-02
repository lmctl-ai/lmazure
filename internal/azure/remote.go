// SPDX-License-Identifier: MIT

package azure

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/streaming"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azqueue"
	"github.com/lmctl-ai/lmcloud/hostkit"
)

const (
	// DefaultDevAccountName is the well-known Azurite / azuresim account name.
	DefaultDevAccountName = "devstoreaccount1"
	// DefaultDevAccountKey is the well-known Azurite / azuresim shared key.
	DefaultDevAccountKey = "Eby8vdM02xNOcqFlqUwJPLlmEtlCDXJ1OUzFT50uSRZ6IFsuFq2UVErCz4I6tq/K1SZFPTOtr/KBHBeksoGMGw=="
)

type remote struct {
	getenv                   func(string) string
	httpClient               *http.Client
	retry                    hostkit.RetryPolicy
	compatProbeMissingDelete *bool

	mu        sync.Mutex
	bucketsAt map[string]time.Time
	queuesAt  map[string]time.Time
	msgBodies map[string]string
}

func wrapRedirectPolicy(hc *http.Client) *http.Client {
	cloned := *hc
	prev := cloned.CheckRedirect
	sameOrigin := hostkit.SameOriginRedirects(3)
	cloned.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		// Never forward Authorization on any redirect.
		req.Header.Del("Authorization")
		if prev != nil {
			if err := prev(req, via); err != nil {
				return err
			}
		}
		return sameOrigin(req, via)
	}
	return &cloned
}

func newRemote(opts Options) *remote {
	hc := opts.HTTPClient
	if hc == nil {
		hc = hostkit.NewHTTPClient(60 * time.Second)
	}
	hc = wrapRedirectPolicy(hc)
	rp := hostkit.DefaultRetryPolicy()
	if opts.Retry != nil {
		rp = *opts.Retry
	}
	return &remote{
		getenv:                   opts.Getenv,
		httpClient:               hc,
		retry:                    rp,
		compatProbeMissingDelete: opts.CompatProbeMissingDelete,
		bucketsAt:                make(map[string]time.Time),
		queuesAt:                 make(map[string]time.Time),
		msgBodies:                make(map[string]string),
	}
}

func (r *remote) compatProbe() bool {
	if r.compatProbeMissingDelete != nil {
		return *r.compatProbeMissingDelete
	}
	v := strings.TrimSpace(strings.ToLower(r.getenv("LMAZURE_COMPAT_PROBE_EXISTENCE")))
	if v == "0" || v == "false" || v == "no" || v == "off" {
		return false
	}
	return true
}

// accountName reads ONLY the explicit simulator variable LMAZURE_SIM_ACCOUNT,
// defaulting to DefaultDevAccountName ("devstoreaccount1"). Ambient AZURE_*
// variables are never consulted for the loopback simulator client (design 02 §3.3).
func (r *remote) accountName() string {
	if v := strings.TrimSpace(r.getenv("LMAZURE_SIM_ACCOUNT")); v != "" {
		return v
	}
	return DefaultDevAccountName
}

// bearerToken reads ONLY LMAZURE_SIM_TOKEN. Ambient AZURE_STORAGE_TOKEN /
// AZURE_* variables are never sent to loopback endpoints.
func (r *remote) bearerToken() string {
	return strings.TrimSpace(r.getenv("LMAZURE_SIM_TOKEN"))
}

// sharedKey reads ONLY LMAZURE_SIM_KEY (or DefaultDevAccountKey on loopback).
// Ambient AZURE_STORAGE_KEY / AZURE_* variables are never sent to loopback endpoints.
func (r *remote) sharedKey(base string) string {
	if v := strings.TrimSpace(r.getenv("LMAZURE_SIM_KEY")); v != "" {
		return v
	}
	if isLoopbackURL(base) {
		return DefaultDevAccountKey
	}
	return ""
}

func isLoopbackURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return hostkit.IsLoopbackHost(u.Hostname())
}

func blobServiceURL(base, account string) string {
	base = strings.TrimRight(base, "/")
	u, err := url.Parse(base)
	if err != nil {
		return base
	}
	if hostkit.IsLoopbackHost(u.Hostname()) {
		p := strings.Trim(u.Path, "/")
		if p == "" {
			u.Path = "/" + account
			return u.String()
		}
	}
	return base
}

func queueServiceURL(base, account string) string {
	base = strings.TrimRight(base, "/")
	u, err := url.Parse(base)
	if err != nil {
		return base
	}
	if hostkit.IsLoopbackHost(u.Hostname()) {
		p := strings.Trim(u.Path, "/")
		if p == "" {
			u.Path = "/" + account + "-queue"
			return u.String()
		}
		if p == account {
			u.Path = "/" + account + "-queue"
			return u.String()
		}
	}
	return base
}

type staticBearerTokenCredential struct {
	token string
}

func (c staticBearerTokenCredential) GetToken(_ context.Context, _ policy.TokenRequestOptions) (azcore.AccessToken, error) {
	return azcore.AccessToken{
		Token:     c.token,
		ExpiresOn: time.Now().Add(1 * time.Hour),
	}, nil
}

// visibilityOnlyUpdatePolicy rewrites empty-message-text UpdateMessage XML
// payloads (<QueueMessage><MessageText></MessageText></QueueMessage>) into
// <QueueMessage></QueueMessage> before SharedKey signing so azuresim 7903c9c
// and Azure Queue REST preserve the existing message body during visibility
// updates when no body override is supplied.
type visibilityOnlyUpdatePolicy struct{}

func (visibilityOnlyUpdatePolicy) Do(req *policy.Request) (*http.Response, error) {
	raw := req.Raw()
	if raw.Method == http.MethodPut && raw.URL != nil && raw.URL.Query().Get("popreceipt") != "" && req.Body() != nil {
		b, err := io.ReadAll(req.Body())
		_ = req.RewindBody()
		if err == nil {
			if bytes.Contains(b, []byte("<MessageText></MessageText>")) || bytes.Contains(b, []byte("<MessageText/>")) {
				_ = req.SetBody(streaming.NopCloser(bytes.NewReader([]byte("<QueueMessage></QueueMessage>"))), "application/xml")
			}
		}
	}
	return req.Next()
}

// hostkitTransporter implements policy.Transporter by delegating directly to
// hostkit.RetryPolicy.DoHTTP with hostkit.IdempotentMethod(req.Method) and the
// shared same-origin HTTP client.
type hostkitTransporter struct {
	client *http.Client
	retry  hostkit.RetryPolicy
}

func (t hostkitTransporter) Do(req *http.Request) (*http.Response, error) {
	var bodyBytes []byte
	if req.Body != nil && req.Body != http.NoBody {
		b, err := io.ReadAll(req.Body)
		_ = req.Body.Close()
		if err != nil {
			return nil, err
		}
		bodyBytes = b
	}
	resp, _, err := t.retry.DoHTTP(req.Context(), t.client, hostkit.IdempotentMethod(req.Method), func(ctx context.Context) (*http.Request, error) {
		attemptReq := req.Clone(ctx)
		if bodyBytes != nil {
			attemptReq.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			attemptReq.ContentLength = int64(len(bodyBytes))
		}
		return attemptReq, nil
	})
	return resp, err
}

func (r *remote) clientOptions(base string) policy.ClientOptions {
	return policy.ClientOptions{
		Retry: policy.RetryOptions{
			MaxRetries: -1, // Disable SDK retries; hostkit.RetryPolicy.DoHTTP owns retries.
		},
		Transport:                       hostkitTransporter{client: r.httpClient, retry: r.retry},
		PerCallPolicies:                 []policy.Policy{visibilityOnlyUpdatePolicy{}},
		InsecureAllowCredentialWithHTTP: isLoopbackURL(base),
	}
}

func (r *remote) newBlobClient(base string) (*azblob.Client, *hostkit.CloudError) {
	acct := r.accountName()
	svcURL := blobServiceURL(base, acct)
	opts := azblob.ClientOptions{ClientOptions: r.clientOptions(base)}

	if tok := r.bearerToken(); tok != "" {
		c, err := azblob.NewClient(svcURL, staticBearerTokenCredential{token: tok}, &opts)
		if err != nil {
			return nil, classifyAzureError(err)
		}
		return c, nil
	}
	if key := r.sharedKey(base); key != "" {
		cred, err := azblob.NewSharedKeyCredential(acct, key)
		if err != nil {
			return nil, hostkit.ProviderError(hostkit.Unauthenticated, http.StatusForbidden, "AuthenticationFailed", "invalid shared key credential: "+err.Error())
		}
		c, err := azblob.NewClientWithSharedKeyCredential(svcURL, cred, &opts)
		if err != nil {
			return nil, classifyAzureError(err)
		}
		return c, nil
	}
	c, err := azblob.NewClientWithNoCredential(svcURL, &opts)
	if err != nil {
		return nil, classifyAzureError(err)
	}
	return c, nil
}

func (r *remote) newQueueServiceClient(base string) (*azqueue.ServiceClient, *hostkit.CloudError) {
	acct := r.accountName()
	svcURL := queueServiceURL(base, acct)
	opts := azqueue.ClientOptions{ClientOptions: r.clientOptions(base)}

	if tok := r.bearerToken(); tok != "" {
		c, err := azqueue.NewServiceClient(svcURL, staticBearerTokenCredential{token: tok}, &opts)
		if err != nil {
			return nil, classifyAzureError(err)
		}
		return c, nil
	}
	if key := r.sharedKey(base); key != "" {
		cred, err := azqueue.NewSharedKeyCredential(acct, key)
		if err != nil {
			return nil, hostkit.ProviderError(hostkit.Unauthenticated, http.StatusForbidden, "AuthenticationFailed", "invalid shared key credential: "+err.Error())
		}
		c, err := azqueue.NewServiceClientWithSharedKeyCredential(svcURL, cred, &opts)
		if err != nil {
			return nil, classifyAzureError(err)
		}
		return c, nil
	}
	c, err := azqueue.NewServiceClientWithNoCredential(svcURL, &opts)
	if err != nil {
		return nil, classifyAzureError(err)
	}
	return c, nil
}

func (r *remote) recordBucketCreated(base, b string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.bucketsAt[base+"\x00"+b] = time.Now().UTC()
}

func (r *remote) deleteBucketCreated(base, b string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.bucketsAt, base+"\x00"+b)
}

func (r *remote) BucketCreatedAt(ctx context.Context, b string) (time.Time, error) {
	base := hostkit.EndpointFrom(ctx)
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.bucketsAt[base+"\x00"+b]
	if !ok {
		return time.Time{}, hostkit.ProviderError(hostkit.NotFound, http.StatusNotFound, "ContainerNotFound", "bucket creation time unknown").WithAbsent(hostkit.AbsentBucket)
	}
	return t, nil
}

func (r *remote) recordQueueCreated(base, q string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.queuesAt[base+"\x00"+q] = time.Now().UTC()
}

func (r *remote) deleteQueueCreated(base, q string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.queuesAt, base+"\x00"+q)
	prefix := base + "\x00" + q + "\x00"
	for k := range r.msgBodies {
		if strings.HasPrefix(k, prefix) {
			delete(r.msgBodies, k)
		}
	}
}

func (r *remote) QueueCreatedAt(ctx context.Context, q string) (time.Time, error) {
	base := hostkit.EndpointFrom(ctx)
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.queuesAt[base+"\x00"+q]
	if !ok {
		return time.Time{}, hostkit.ProviderError(hostkit.NotFound, http.StatusNotFound, "QueueNotFound", "queue creation time unknown").WithAbsent(hostkit.AbsentQueue)
	}
	return t, nil
}

func (r *remote) storeMsgBody(base, q, msgID, rawB64 string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.msgBodies[base+"\x00"+q+"\x00"+msgID] = rawB64
}

func (r *remote) loadMsgBody(base, q, msgID string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.msgBodies[base+"\x00"+q+"\x00"+msgID]
}

func (r *remote) deleteMsgBody(base, q, msgID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.msgBodies, base+"\x00"+q+"\x00"+msgID)
}
