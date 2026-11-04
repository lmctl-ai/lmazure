// SPDX-License-Identifier: MIT

package azure

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lmctl-ai/lmcloud/hostkit"
	"github.com/lmctl-ai/lmcloud/hostkit/cloudhost"
	"github.com/lmctl-ai/lmcloud/hostkit/httpguard"
)

func TestClassifyTable(t *testing.T) {
	cases := []struct {
		status int
		code   string
		msg    string
		want   hostkit.Code
	}{
		{404, "ContainerNotFound", "The specified container does not exist.", hostkit.NotFound},
		{404, "BlobNotFound", "The specified blob does not exist.", hostkit.NotFound},
		{404, "QueueNotFound", "The specified queue does not exist.", hostkit.NotFound},
		{404, "MessageNotFound", "The specified message does not exist.", hostkit.NotFound},
		{409, "ContainerAlreadyExists", "The specified container already exists.", hostkit.Conflict},
		{409, "QueueAlreadyExists", "The specified queue already exists.", hostkit.Conflict},
		{409, "ContainerBeingDeleted", "Container is being deleted.", hostkit.Conflict},
		{412, "ConditionNotMet", "Precondition failed.", hostkit.PreconditionFailed},
		{400, "PopReceiptMismatch", "The specified pop receipt did not match.", hostkit.InvalidArgument},
		{400, "InvalidMarker", "Invalid marker.", hostkit.InvalidArgument},
		{400, "InvalidMetadata", "Invalid metadata.", hostkit.InvalidArgument},
		{400, "InvalidQueryParameterValue", "Invalid query parameter.", hostkit.InvalidArgument},
		{400, "OutOfRangeQueryParameterValue", "Out of range.", hostkit.InvalidArgument},
		{400, "InvalidXmlDocument", "Malformed XML.", hostkit.InvalidArgument},
		{413, "RequestBodyTooLarge", "Too large.", hostkit.InvalidArgument},
		{401, "NoAuthenticationInformation", "Missing auth.", hostkit.Unauthenticated},
		{403, "AuthenticationFailed", "MAC signature mismatch.", hostkit.Unauthenticated},
		{403, "InvalidAuthenticationInfo", "Malformed auth header.", hostkit.Unauthenticated},
		{403, "", "Server failed to authenticate the request. Make sure the value of Authorization header is formed correctly including the signature.", hostkit.Unauthenticated},
		{403, "AuthorizationFailure", "This request is not authorized to perform this operation.", hostkit.PermissionDenied},
		{403, "AuthorizationPermissionMismatch", "Permission mismatch.", hostkit.PermissionDenied},
		{403, "InsufficientAccountPermissions", "Insufficient permissions.", hostkit.PermissionDenied},
		{429, "ServerBusy", "The server is busy.", hostkit.Internal},
		{503, "ServerBusy", "The server is busy.", hostkit.Internal},
		{500, "OperationTimedOut", "Operation timed out.", hostkit.Internal},
		{504, "OperationTimedOut", "Gateway timed out.", hostkit.Internal},
		{408, "", "Request timeout.", hostkit.Internal},
		{500, "InternalError", "Internal server error.", hostkit.Internal},
		{503, "", "Service Unavailable", hostkit.Internal},
	}

	for _, tc := range cases {
		got := Classify(tc.status, tc.code, tc.msg)
		if got.Code != tc.want {
			t.Errorf("Classify(%d, %q, %q) = %s; want %s", tc.status, tc.code, tc.msg, got.Code, tc.want)
		}
	}
}

func TestReceiptCodecH1StrictTwoPart(t *testing.T) {
	msgID := "msg-12345"
	pop := "pop-67890"
	tok := EncodeReceipt(msgID, pop)

	raw, err := base64.RawURLEncoding.DecodeString(tok)
	if err != nil {
		t.Fatalf("expected base64url receipt token, got err: %v", err)
	}
	if string(raw) != msgID+"\n"+pop {
		t.Fatalf("expected receipt token to contain ONLY messageId + popReceipt, got %q", string(raw))
	}

	gotMsg, gotPop, ok := DecodeReceipt(tok)
	if !ok || gotMsg != msgID || gotPop != pop {
		t.Fatalf("DecodeReceipt(%q) = (%q, %q, %v); want (%q, %q, true)", tok, gotMsg, gotPop, ok, msgID, pop)
	}

	// Legacy 3-part tokens (containing MessageText) must be rejected per spec H1.
	legacyThreePart := base64.RawURLEncoding.EncodeToString([]byte(msgID + "\n" + pop + "\naGVsbG8="))
	if _, _, ok := DecodeReceipt(legacyThreePart); ok {
		t.Fatalf("expected 3-part receipt containing MessageText to be rejected")
	}
	if _, _, ok := DecodeReceipt("not-valid-base64!"); ok {
		t.Fatalf("expected invalid base64 to be rejected")
	}
	if _, _, ok := DecodeReceipt(""); ok {
		t.Fatalf("expected empty receipt to be rejected")
	}
}

func TestRetry503ThreeAttemptsAndSendIdempotency(t *testing.T) {
	srv := httpguard.New(t)
	srv.Script(httpguard.Status(
		http.StatusServiceUnavailable,
		`<?xml version="1.0" encoding="utf-8"?><Error><Code>InternalError</Code><Message>transient 503</Message></Error>`,
		"Content-Type: application/xml",
		"x-ms-error-code: InternalError",
	))

	fastRetry := &hostkit.RetryPolicy{
		Sleep: func(_ context.Context, _ time.Duration) error { return nil },
	}
	a := New(&Options{
		Getenv: func(string) string { return "" },
		Retry:  fastRetry,
	})
	ctx := hostkit.WithEndpoint(context.Background(), srv.URL)

	// Idempotent HeadBucket must be retried 3 times by hostkit.RetryPolicy (SDK retries disabled).
	err := a.HeadBucket(ctx, "retry-bucket")
	if err == nil {
		t.Fatalf("expected HeadBucket to fail after 3x 503")
	}
	var ce *hostkit.CloudError
	if !errors.As(err, &ce) || ce.Code != hostkit.Internal {
		t.Fatalf("expected INTERNAL CloudError, got %T %v", err, err)
	}
	httpguard.AssertCount(t, srv, "GET", 3)

	// Non-idempotent Send (POST /messages) must NOT be retried on 503 (idempotent=false).
	if _, sErr := a.Send(ctx, "retry-queue", "hello"); sErr == nil {
		t.Fatalf("expected Send to fail on 503")
	}
	httpguard.AssertCount(t, srv, "POST", 1)
}

func TestPostRetryRules429AndPostCommitEOF(t *testing.T) {
	srv := httpguard.New(t)
	fastRetry := &hostkit.RetryPolicy{
		Sleep: func(_ context.Context, _ time.Duration) error { return nil },
	}
	a := New(&Options{
		Getenv: func(string) string { return "" },
		Retry:  fastRetry,
	})
	ctx := hostkit.WithEndpoint(context.Background(), srv.URL)

	busy429 := httpguard.Status(
		http.StatusTooManyRequests,
		`<Error><Code>ServerBusy</Code><Message>busy</Message></Error>`,
		"Content-Type: application/xml",
		"x-ms-error-code: ServerBusy",
		"Retry-After: 0",
	)

	// Repro 1 (post-mode-5): POST 429 must be retried 3 times and fail with INTERNAL.
	srv.Script(busy429)
	_, err := a.Send(ctx, "valid-queue", "body")
	if err == nil {
		t.Fatalf("expected Send on persistent 429 to fail")
	}
	if ce := hostkit.AsCloudError(err); ce.Code != hostkit.Internal || ce.HTTPStatus != http.StatusTooManyRequests {
		t.Fatalf("expected INTERNAL (429) after 3 attempts, got %+v", ce)
	}
	httpguard.AssertCount(t, srv, "POST", 3)

	// Repro 2 (post-mode-6): Server accepts POST then hangs up (EOF) -> must NOT retry (1 attempt).
	srv.Script(httpguard.HangUp())
	if _, err := a.Send(ctx, "valid-queue", "body"); err == nil {
		t.Fatalf("expected Send on EOF to fail")
	}
	httpguard.AssertCount(t, srv, "POST", 1)

	// Repro 3: POST 429 twice then 201 Created succeeds on attempt 3.
	srv.Script(
		busy429,
		busy429,
		httpguard.Status(
			http.StatusCreated,
			`<?xml version="1.0" encoding="utf-8"?><QueueMessagesList><QueueMessage><MessageId>msg-ok-3</MessageId><InsertionTime>Sun, 01 Nov 2026 00:00:00 GMT</InsertionTime><ExpirationTime>Sun, 08 Nov 2026 00:00:00 GMT</ExpirationTime><PopReceipt>pop-1</PopReceipt><TimeNextVisible>Sun, 01 Nov 2026 00:00:00 GMT</TimeNextVisible></QueueMessage></QueueMessagesList>`,
			"Content-Type: application/xml",
		),
	)
	id, err := a.Send(ctx, "valid-queue", "body")
	if err != nil || id != "msg-ok-3" {
		t.Fatalf("expected Send to succeed on attempt 3 after 2x 429, got id=%q err=%v", id, err)
	}
	httpguard.AssertCount(t, srv, "POST", 3)
}

func TestAmbientCredentialIsolationAndRedirectRefusal(t *testing.T) {
	srv := httpguard.New(t)
	srv.Script(httpguard.Status(
		http.StatusOK,
		"",
		`ETag: "e1"`,
		"Last-Modified: "+time.Now().UTC().Format(http.TimeFormat),
		"x-ms-lease-state: available",
		"x-ms-lease-status: unlocked",
		"x-ms-has-immutability-policy: false",
		"x-ms-has-legal-hold: false",
	))

	// Set synthetic ambient AZURE_* env vars; loopback client must ignore all of them.
	a := New(&Options{
		Getenv: func(k string) string {
			switch k {
			case "AZURE_STORAGE_TOKEN":
				return "ambient-secret-bearer-token"
			case "AZURE_STORAGE_KEY":
				return base64.StdEncoding.EncodeToString([]byte("ambient-secret-key-bytes-32-long"))
			case "AZURE_STORAGE_ACCOUNT":
				return "ambientaccount"
			default:
				return ""
			}
		},
	})
	ctx := hostkit.WithEndpoint(context.Background(), srv.URL)
	if err := a.HeadBucket(ctx, "valid-bucket"); err != nil {
		t.Fatalf("HeadBucket failed: %v", err)
	}
	reqs := srv.Requests()
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}
	authHdr := reqs[0].Authorization
	if strings.Contains(authHdr, "ambient-secret-bearer-token") || strings.Contains(authHdr, "Bearer") {
		t.Fatalf("ambient AZURE_STORAGE_TOKEN leaked to loopback simulator: Authorization=%q", authHdr)
	}
	if !strings.HasPrefix(authHdr, "SharedKey devstoreaccount1:") {
		t.Fatalf("expected SharedKey devstoreaccount1:..., got Authorization=%q", authHdr)
	}
	if !strings.HasPrefix(reqs[0].Path, "/devstoreaccount1/") {
		t.Fatalf("expected default devstoreaccount1 path, got %q", reqs[0].Path)
	}

	// Cross-port 307 redirect must be refused without contacting destination or forwarding Authorization.
	foreign := httpguard.New(t)
	origin := httpguard.New(t)
	origin.Script(httpguard.RedirectTo(foreign.URL + "/key"))

	if _, err := a.GetObject(hostkit.WithEndpoint(context.Background(), origin.URL), "valid-bucket", "key", 100); err == nil {
		t.Fatalf("expected cross-port redirect to fail")
	}
	httpguard.AssertCount(t, foreign, "", 0)
	httpguard.AssertNoCredentials(t, foreign)

	// Same-origin redirect must also strip Authorization on the redirected hop.
	sameOrigin := httpguard.New(t)
	sameOrigin.Script(
		httpguard.RedirectTo("/devstoreaccount1/valid-bucket/redirected"),
		httpguard.Status(
			http.StatusOK,
			"ok",
			"Content-Type: application/octet-stream",
			`ETag: "e1"`,
			"Last-Modified: "+time.Now().UTC().Format(http.TimeFormat),
		),
	)
	if _, err := a.GetObject(hostkit.WithEndpoint(context.Background(), sameOrigin.URL), "valid-bucket", "key", 100); err != nil {
		t.Fatalf("expected same-origin redirect to succeed, got %v", err)
	}
	sameReqs := sameOrigin.Requests()
	if len(sameReqs) != 2 || sameReqs[0].Authorization == "" || sameReqs[1].Authorization != "" {
		t.Fatalf("expected Authorization on hop 1 and stripped on hop 2, got %+v", sameReqs)
	}
}

func TestGetObjectOversizeRejected(t *testing.T) {
	srv := httpguard.New(t)
	srv.Script(httpguard.Oversize(128))

	a := New(&Options{Getenv: func(string) string { return "" }})
	ctx := hostkit.WithEndpoint(context.Background(), srv.URL)

	_, err := a.GetObject(ctx, "test-bucket", "big.bin", 64)
	if err == nil {
		t.Fatalf("expected oversize GetObject to fail")
	}
	var ce *hostkit.CloudError
	if !errors.As(err, &ce) || ce.Code != hostkit.Internal {
		t.Fatalf("expected INTERNAL for oversize GetObject, got %T %v", err, err)
	}
}

func TestStalledListContinuationTokenRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Query().Get("comp") == "list" {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?>
<EnumerationResults ServiceEndpoint="http://127.0.0.1/" ContainerName="stalled-bucket">
  <Marker>tok-same</Marker>
  <MaxResults>10</MaxResults>
  <Blobs>
    <Blob><Name>a.txt</Name><Properties><Content-Length>1</Content-Length><Etag>"e1"</Etag><Last-Modified>Sun, 01 Nov 2026 00:00:00 GMT</Last-Modified></Properties></Blob>
  </Blobs>
  <NextMarker>tok-same</NextMarker>
</EnumerationResults>`)
			return
		}
		w.Header().Set("ETag", `"c-etag"`)
		w.Header().Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	f := false
	a := New(&Options{
		Getenv:                   func(string) string { return "" },
		CompatProbeMissingDelete: &f,
	})
	ctx := hostkit.WithEndpoint(context.Background(), srv.URL)

	_, err := a.ListObjects(ctx, "stalled-bucket", "", 10, "tok-same")
	if err == nil {
		t.Fatalf("expected stalled continuation token to fail")
	}
	var ce *hostkit.CloudError
	if !errors.As(err, &ce) || ce.Code != hostkit.Internal {
		t.Fatalf("expected INTERNAL for stalled token, got %T %v", err, err)
	}
}

func TestNackVisibilityOnlyXMLAndRotatedReceipt(t *testing.T) {
	var gotPutBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut && r.URL.Query().Get("popreceipt") != "" {
			b, _ := io.ReadAll(r.Body)
			gotPutBody = string(b)
			w.Header().Set("x-ms-popreceipt", "pop-new-999")
			w.Header().Set("x-ms-time-next-visible", time.Now().UTC().Format(http.TimeFormat))
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	f := false
	a := New(&Options{
		Getenv:                   func(string) string { return "" },
		CompatProbeMissingDelete: &f,
	})
	ctx := hostkit.WithEndpoint(context.Background(), srv.URL)

	oldRcpt := EncodeReceipt("msg-1", "pop-old-111")
	newRcpt, err := a.Nack(ctx, "work-queue", oldRcpt, 45)
	if err != nil {
		t.Fatalf("Nack failed: %v", err)
	}
	if newRcpt == oldRcpt {
		t.Fatalf("expected rotated receipt token, got unchanged %q", newRcpt)
	}
	msgID, pop, ok := DecodeReceipt(newRcpt)
	if !ok || msgID != "msg-1" || pop != "pop-new-999" {
		t.Fatalf("decoded rotated receipt = (%q, %q, %v); want (msg-1, pop-new-999, true)", msgID, pop, ok)
	}
	if strings.TrimSpace(gotPutBody) != "<QueueMessage></QueueMessage>" {
		t.Fatalf("expected visibility-only UpdateMessage XML <QueueMessage></QueueMessage>, got %q", gotPutBody)
	}
}

func TestM5ReceiptPrecedenceMissingQueueVsInvalidReceipt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/missing-queue") {
			w.Header().Set("Content-Type", "application/xml")
			w.Header().Set("x-ms-error-code", "QueueNotFound")
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?><Error><Code>QueueNotFound</Code><Message>The specified queue does not exist.</Message></Error>`)
			return
		}
		w.Header().Set("x-ms-approximate-messages-count", "0")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	a := New(&Options{
		Getenv: func(k string) string {
			if k == "LMCLOUD_ENDPOINT" {
				return srv.URL
			}
			return ""
		},
	})
	h := cloudhost.New(cloudhost.Config{
		Provider: "azure",
		Store:    a,
		Queue:    a,
		Endpoints: &hostkit.EndpointResolver{
			Prefix: "LMAZURE",
			Getenv: func(k string) string {
				if k == "LMCLOUD_ENDPOINT" {
					return srv.URL
				}
				return ""
			},
		},
	})

	// Missing queue + malformed non-empty receipt -> NOT_FOUND (M5 precedence).
	if err := h.QueueAck(false, "missing-queue", "malformed-receipt"); err == nil {
		t.Fatalf("expected QueueAck on missing-queue to fail")
	}
	if h.LastError().Code != hostkit.NotFound {
		t.Fatalf("expected NOT_FOUND on missing-queue + malformed receipt, got %s (%s)", h.LastError().Code, h.LastError().Message)
	}

	// Existing queue + malformed non-empty receipt -> INVALID_ARGUMENT.
	if err := h.QueueAck(false, "existing-queue", "malformed-receipt"); err == nil {
		t.Fatalf("expected QueueAck with malformed receipt on existing-queue to fail")
	}
	if h.LastError().Code != hostkit.InvalidArgument {
		t.Fatalf("expected INVALID_ARGUMENT on existing-queue + malformed receipt, got %s", h.LastError().Code)
	}
}

func TestRouterAndQueueCapsFor(t *testing.T) {
	a := New(&Options{Getenv: func(string) string { return "" }})

	// Offline context -> fakeadapter (TextOnlyWire: true).
	offlineCtx := context.Background()
	if caps := a.QueueCapsFor(offlineCtx, "q"); !caps.TextOnlyWire || caps.LongPoll {
		t.Fatalf("offline QueueCapsFor = %+v; want TextOnlyWire=true, LongPoll=false", caps)
	}

	// Loopback sim context -> remote (TextOnlyWire: false).
	simCtx := hostkit.WithEndpoint(context.Background(), "http://127.0.0.1:4577")
	if caps := a.QueueCapsFor(simCtx, "q"); caps.TextOnlyWire || caps.LongPoll {
		t.Fatalf("sim QueueCapsFor = %+v; want TextOnlyWire=false, LongPoll=false", caps)
	}

	// Non-loopback endpoint -> UNAUTHENTICATED (LiveCredentialsNotImplemented).
	liveCtx := hostkit.WithEndpoint(context.Background(), "https://myaccount.blob.core.windows.net")
	_, err := a.CreateBucket(liveCtx, "live-bucket")
	var ce *hostkit.CloudError
	if !errors.As(err, &ce) || ce.Code != hostkit.Unauthenticated {
		t.Fatalf("expected UNAUTHENTICATED for non-loopback endpoint, got %T %v", err, err)
	}
}

func TestCompatProbeToggle(t *testing.T) {
	var headCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Query().Get("comp") == "metadata" {
			headCount.Add(1)
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// Default (compat probe disabled for azuresim >= v0.2.14): DeleteQueue issues DELETE directly without GetProperties.
	aNoProbe := New(&Options{Getenv: func(string) string { return "" }})
	ctx := hostkit.WithEndpoint(context.Background(), srv.URL)
	if err := aNoProbe.DeleteQueue(ctx, "test-q"); err != nil {
		t.Fatalf("DeleteQueue (no probe) failed: %v", err)
	}
	if got := headCount.Load(); got != 0 {
		t.Fatalf("expected 0 GetProperties probes by default, got %d", got)
	}

	// With LMAZURE_COMPAT_PROBE_EXISTENCE=1, DeleteQueue issues GetProperties before DELETE.
	aProbe := New(&Options{
		Getenv: func(k string) string {
			if k == "LMAZURE_COMPAT_PROBE_EXISTENCE" {
				return "1"
			}
			return ""
		},
	})
	if err := aProbe.DeleteQueue(ctx, "test-q"); err != nil {
		t.Fatalf("DeleteQueue (with probe) failed: %v", err)
	}
	if got := headCount.Load(); got != 1 {
		t.Fatalf("expected 1 GetProperties probe when LMAZURE_COMPAT_PROBE_EXISTENCE=1, got %d", got)
	}
}

func TestCreateOwnershipTokensBucketAndQueue(t *testing.T) {
	var mu sync.Mutex
	containerTokens := map[string]string{}
	queueTokens := map[string]string{}
	var dropFirstContainerPut atomic.Bool
	var dropFirstQueuePut atomic.Bool

	srv := httpguard.New(t)
	srv.Script(func(w http.ResponseWriter, r *http.Request) {
		isQueue := strings.Contains(r.URL.Path, "-queue/")
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		name := ""
		if len(parts) >= 2 {
			name = parts[1]
		}
		reqTok := strings.TrimSpace(r.Header.Get("x-ms-meta-" + createTokenMetaKey))

		mu.Lock()
		if !isQueue && r.URL.Query().Get("restype") == "container" {
			if r.Method == http.MethodPut {
				if _, exists := containerTokens[name]; exists {
					mu.Unlock()
					httpguard.Status(
						http.StatusConflict,
						`<Error><Code>ContainerAlreadyExists</Code><Message>The specified container already exists.</Message></Error>`,
						"Content-Type: application/xml",
						"x-ms-error-code: ContainerAlreadyExists",
					)(w, r)
					return
				}
				containerTokens[name] = reqTok
				mu.Unlock()
				if dropFirstContainerPut.CompareAndSwap(true, false) {
					httpguard.HangUp()(w, r)
					return
				}
				httpguard.Status(
					http.StatusCreated,
					"",
					`ETag: "c-etag"`,
					"Last-Modified: "+time.Now().UTC().Format(http.TimeFormat),
				)(w, r)
				return
			}
			// GET/HEAD container properties
			tok, exists := containerTokens[name]
			mu.Unlock()
			if !exists {
				httpguard.Status(
					http.StatusNotFound,
					`<Error><Code>ContainerNotFound</Code><Message>Not found.</Message></Error>`,
					"Content-Type: application/xml",
					"x-ms-error-code: ContainerNotFound",
				)(w, r)
				return
			}
			hdrs := []string{
				`ETag: "c-etag"`,
				"Last-Modified: " + time.Now().UTC().Format(http.TimeFormat),
				"x-ms-lease-state: available",
				"x-ms-lease-status: unlocked",
				"x-ms-has-immutability-policy: false",
				"x-ms-has-legal-hold: false",
			}
			if tok != "" {
				hdrs = append(hdrs, "x-ms-meta-"+createTokenMetaKey+": "+tok)
			}
			httpguard.Status(http.StatusOK, "", hdrs...)(w, r)
			return
		}

		if isQueue {
			if r.Method == http.MethodPut {
				if _, exists := queueTokens[name]; exists {
					mu.Unlock()
					httpguard.Status(
						http.StatusConflict,
						`<Error><Code>QueueAlreadyExists</Code><Message>The specified queue already exists.</Message></Error>`,
						"Content-Type: application/xml",
						"x-ms-error-code: QueueAlreadyExists",
					)(w, r)
					return
				}
				queueTokens[name] = reqTok
				mu.Unlock()
				if dropFirstQueuePut.CompareAndSwap(true, false) {
					httpguard.HangUp()(w, r)
					return
				}
				httpguard.Status(http.StatusCreated, "")(w, r)
				return
			}
			// GET/HEAD queue metadata
			tok, exists := queueTokens[name]
			mu.Unlock()
			if !exists {
				httpguard.Status(
					http.StatusNotFound,
					`<Error><Code>QueueNotFound</Code><Message>Not found.</Message></Error>`,
					"Content-Type: application/xml",
					"x-ms-error-code: QueueNotFound",
				)(w, r)
				return
			}
			hdrs := []string{"x-ms-approximate-messages-count: 0"}
			if tok != "" {
				hdrs = append(hdrs, "x-ms-meta-"+createTokenMetaKey+": "+tok)
			}
			httpguard.Status(http.StatusOK, "", hdrs...)(w, r)
			return
		}
		mu.Unlock()
		httpguard.Status(http.StatusNotFound, "")(w, r)
	})

	// Seed pre-existing resources with no create token (created outside this call).
	containerTokens["preexisting-bucket"] = ""
	queueTokens["preexisting-queue"] = ""

	fastRetry := &hostkit.RetryPolicy{
		Sleep: func(_ context.Context, _ time.Duration) error { return nil },
	}
	a := New(&Options{
		Getenv: func(k string) string {
			if k == "LMCLOUD_ENDPOINT" {
				return srv.URL
			}
			return ""
		},
		Retry: fastRetry,
	})
	h := cloudhost.New(cloudhost.Config{
		Provider: "azure",
		Store:    a,
		Queue:    a,
		Endpoints: &hostkit.EndpointResolver{
			Prefix: "LMAZURE",
			Getenv: func(k string) string {
				if k == "LMCLOUD_ENDPOINT" {
					return srv.URL
				}
				return ""
			},
		},
	})

	// 1. Create succeeds -> true (token stored atomically in metadata).
	created, err := h.BucketCreate(false, "fresh-bucket")
	if err != nil || !created {
		t.Fatalf("fresh bucket_create: created=%v err=%v", created, err)
	}
	mu.Lock()
	if containerTokens["fresh-bucket"] == "" {
		t.Fatalf("expected non-empty create token stored in container metadata")
	}
	mu.Unlock()

	created, err = h.QueueCreate(false, "fresh-queue")
	if err != nil || !created {
		t.Fatalf("fresh queue_create: created=%v err=%v", created, err)
	}
	mu.Lock()
	if queueTokens["fresh-queue"] == "" {
		t.Fatalf("expected non-empty create token stored in queue metadata")
	}
	mu.Unlock()

	// 2. Conflict on pre-existing resource (no token or another call's token) -> false, OK.
	for _, b := range []string{"preexisting-bucket", "fresh-bucket"} {
		created, err = h.BucketCreate(false, b)
		if err != nil || created || h.LastError().Code != hostkit.OK {
			t.Fatalf("conflict bucket_create(%q): created=%v err=%v last=%+v", b, created, err, h.LastError())
		}
	}
	for _, q := range []string{"preexisting-queue", "fresh-queue"} {
		created, err = h.QueueCreate(false, q)
		if err != nil || created || h.LastError().Code != hostkit.OK {
			t.Fatalf("conflict queue_create(%q): created=%v err=%v last=%+v", q, created, err, h.LastError())
		}
	}

	// 3. Lost-reply create: server commits resource with OUR token on attempt 1,
	// hangs up (EOF), then returns 409 on retry -> BucketCreateToken/QueueCreateToken
	// reads back our token and returns created=true.
	dropFirstContainerPut.Store(true)
	created, err = h.BucketCreate(false, "lost-reply-bucket")
	if err != nil || !created || h.LastError().Code != hostkit.OK {
		t.Fatalf("lost-reply bucket_create: created=%v err=%v last=%+v", created, err, h.LastError())
	}

	dropFirstQueuePut.Store(true)
	created, err = h.QueueCreate(false, "lost-reply-queue")
	if err != nil || !created || h.LastError().Code != hostkit.OK {
		t.Fatalf("lost-reply queue_create: created=%v err=%v last=%+v", created, err, h.LastError())
	}
}

func TestOfflineReceiveFollowsRouteCaps(t *testing.T) {
	a := New(&Options{Getenv: func(string) string { return "" }})
	h := cloudhost.New(cloudhost.Config{
		Provider:     "azure",
		Store:        a,
		Queue:        a,
		PollInterval: 50 * time.Millisecond,
		Endpoints:    &hostkit.EndpointResolver{Prefix: "LMAZURE", Getenv: func(string) string { return "" }},
	})
	if _, err := h.QueueCreate(false, "offline-wait-q"); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	msgs, err := h.QueueReceive(false, "offline-wait-q", 1, 1, 30)
	elapsed := time.Since(start)
	if err != nil || len(msgs) != 0 {
		t.Fatalf("offline QueueReceive: msgs=%v err=%v", msgs, err)
	}
	if elapsed < 900*time.Millisecond || elapsed > 3*time.Second {
		t.Fatalf("offline QueueReceive(wait=1) took %v, want ~1s", elapsed)
	}
}

func TestEnumsDeclaredForDryRun(t *testing.T) {
	if !cloudhost.EnumsDeclared() {
		t.Fatalf("expected cloudhost.EnumsDeclared() == true with lmcode-go >= 06cd9ae (Registry.Enum merged upstream)")
	}
}
