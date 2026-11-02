// SPDX-License-Identifier: MIT

package azure

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lmctl-ai/lmcloud/hostkit"
	"github.com/lmctl-ai/lmcloud/hostkit/cloudhost"
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
	var headCalls atomic.Int32
	var postCalls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			postCalls.Add(1)
		} else {
			headCalls.Add(1)
		}
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?><Error><Code>InternalError</Code><Message>transient 503</Message></Error>`)
	}))
	defer srv.Close()

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
	if got := headCalls.Load(); got != 3 {
		t.Fatalf("expected exactly 3 HTTP attempts for idempotent HeadBucket, got %d", got)
	}

	// Non-idempotent Send (POST /messages) must NOT be retried on 503 (hostkit v2 idempotent=false).
	_, sErr := a.Send(ctx, "retry-queue", "hello")
	if sErr == nil {
		t.Fatalf("expected Send to fail on 503")
	}
	if got := postCalls.Load(); got != 1 {
		t.Fatalf("expected exactly 1 HTTP attempt for non-idempotent Send on 503, got %d", got)
	}
}

func TestGetObjectOversizeRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("ETag", `"etag-1"`)
		w.Header().Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(bytes.Repeat([]byte("A"), 128))
	}))
	defer srv.Close()

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

	// With LMAZURE_COMPAT_PROBE_EXISTENCE=0, DeleteQueue issues DELETE directly without GetProperties.
	aNoProbe := New(&Options{
		Getenv: func(k string) string {
			if k == "LMAZURE_COMPAT_PROBE_EXISTENCE" {
				return "0"
			}
			return ""
		},
	})
	ctx := hostkit.WithEndpoint(context.Background(), srv.URL)
	if err := aNoProbe.DeleteQueue(ctx, "test-q"); err != nil {
		t.Fatalf("DeleteQueue (no probe) failed: %v", err)
	}
	if got := headCount.Load(); got != 0 {
		t.Fatalf("expected 0 GetProperties probes when LMAZURE_COMPAT_PROBE_EXISTENCE=0, got %d", got)
	}

	// With default (compat probe enabled), DeleteQueue issues GetProperties before DELETE.
	aProbe := New(&Options{Getenv: func(string) string { return "" }})
	if err := aProbe.DeleteQueue(ctx, "test-q"); err != nil {
		t.Fatalf("DeleteQueue (with probe) failed: %v", err)
	}
	if got := headCount.Load(); got != 1 {
		t.Fatalf("expected 1 GetProperties probe when compat probe enabled, got %d", got)
	}
}
