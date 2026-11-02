package host

import (
	"testing"

	lmcode "github.com/lmctl-ai/lmcode-go"
)

func TestRegisterAndHostSignatures(t *testing.T) {
	reg := lmcode.NewRegistry()
	h := New(Options{})
	if err := Register(reg, h); err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	hostSigs := reg.HostSignatures()
	if len(hostSigs) == 0 {
		t.Fatalf("expected non-empty host signatures")
	}
}

func TestValidationGuards(t *testing.T) {
	if cerr := ValidateBucketName("Bad_Bucket"); cerr == nil || cerr.Code != CodeInvalidArgument {
		t.Fatalf("expected INVALID_ARGUMENT for Bad_Bucket, got %+v", cerr)
	}
	if cerr := ValidateBucketName("sthree-bucket"); cerr == nil || cerr.Code != CodeInvalidArgument {
		t.Fatalf("expected INVALID_ARGUMENT for sthree-bucket, got %+v", cerr)
	}
	if cerr := ValidateObjectKey("seg./nested.txt"); cerr == nil || cerr.Code != CodeInvalidArgument {
		t.Fatalf("expected INVALID_ARGUMENT for trailing dot segment, got %+v", cerr)
	}
	if cerr := ValidateContentType("invalid-no-slash"); cerr == nil || cerr.Code != CodeInvalidArgument {
		t.Fatalf("expected INVALID_ARGUMENT for content_type without slash, got %+v", cerr)
	}
	if cerr := ValidateQueueName("my-google-queue"); cerr == nil || cerr.Code != CodeInvalidArgument {
		t.Fatalf("expected INVALID_ARGUMENT for reserved queue substring, got %+v", cerr)
	}
	if _, cerr := ValidateEndpointURL("http://127.0.0.1.evil.example:4577"); cerr == nil || cerr.Code != CodeInvalidArgument {
		t.Fatalf("expected INVALID_ARGUMENT for non-loopback http endpoint, got %+v", cerr)
	}
	if norm, cerr := ValidateEndpointURL("http://127.0.0.1:4577/"); cerr != nil || norm != "http://127.0.0.1:4577" {
		t.Fatalf("expected normalized loopback endpoint, got %q, %+v", norm, cerr)
	}
}

func TestEncodeObjectKeyPathAndQueueCodec(t *testing.T) {
	got := EncodeObjectKeyPath("dir/a+b%20c?d#e f.txt")
	want := "dir/a%2Bb%2520c%3Fd%23e%20f.txt"
	if got != want {
		t.Fatalf("EncodeObjectKeyPath = %q; want %q", got, want)
	}

	cases := []string{
		"",
		"__lmcloud_empty__",
		"__lmcloud_esc__:literal",
		"__lmcloud_b64__:literal",
		"hello 🚀 world",
		string([]byte{0x00, 0x01, 0x7f, 0x80, 0xfe, 0xff, 'A', 0x00}),
	}
	for _, tc := range cases {
		wire := EncodeQueueBody(tc)
		if wire == "" {
			t.Fatalf("EncodeQueueBody(%q) produced empty wire payload", tc)
		}
		roundtrip := DecodeQueueBody(wire)
		if roundtrip != tc {
			t.Fatalf("queue codec roundtrip mismatch: got %x, want %x (wire=%q)", roundtrip, tc, wire)
		}
	}
}

func TestFakeStorageAndQueueLifecycle(t *testing.T) {
	h := New(Options{})
	if h.Provider() != "azure" {
		t.Fatalf("Provider() = %q; want azure", h.Provider())
	}

	created, err := h.BucketCreate("unit-test-bucket")
	if err != nil || !created {
		t.Fatalf("BucketCreate first call = %v, %v; want true, nil", created, err)
	}
	created2, err := h.BucketCreate("unit-test-bucket")
	if err != nil || created2 {
		t.Fatalf("BucketCreate second call = %v, %v; want false, nil", created2, err)
	}

	meta, err := h.ObjectPut("unit-test-bucket", "docs/readme.txt", "hello-azure", "text/plain", map[string]string{"env": "test"})
	if err != nil || !meta.Exists || meta.Size != 11 {
		t.Fatalf("ObjectPut failed: meta=%+v, err=%v", meta, err)
	}

	// Non-empty bucket delete must fail with CONFLICT
	if _, err := h.BucketDelete("unit-test-bucket"); err == nil {
		t.Fatalf("expected BucketDelete on non-empty bucket to fail")
	}
	if h.LastError().Code != CodeConflict {
		t.Fatalf("expected last_error CONFLICT, got %s", h.LastError().Code)
	}

	// Head object
	headMeta, err := h.ObjectHead("unit-test-bucket", "docs/readme.txt")
	if err != nil || !headMeta.Exists || headMeta.Size != 11 {
		t.Fatalf("ObjectHead failed: meta=%+v, err=%v", headMeta, err)
	}

	// Delete object
	if err := h.ObjectDelete("unit-test-bucket", "docs/readme.txt"); err != nil {
		t.Fatalf("ObjectDelete failed: %v", err)
	}

	// Now empty bucket delete succeeds
	del, err := h.BucketDelete("unit-test-bucket")
	if err != nil || !del {
		t.Fatalf("BucketDelete empty failed: %v, %v", del, err)
	}

	// Queue lifecycle in fake
	qCreated, err := h.QueueCreate("unit-test-queue")
	if err != nil || !qCreated {
		t.Fatalf("QueueCreate failed: %v, %v", qCreated, err)
	}

	msgID, err := h.QueueSend("unit-test-queue", "message payload")
	if err != nil || msgID == "" {
		t.Fatalf("QueueSend failed: %v, %v", msgID, err)
	}

	msgs, err := h.QueueReceive("unit-test-queue", 10, 0, 30)
	if err != nil || len(msgs.Items) != 1 {
		t.Fatalf("QueueReceive failed: len=%d, err=%v", len(msgs.Items), err)
	}
	if msgs.Items[0].Body != "message payload" {
		t.Fatalf("QueueReceive body = %q; want %q", msgs.Items[0].Body, "message payload")
	}

	newRcpt, err := h.QueueNack("unit-test-queue", msgs.Items[0].Receipt, 60)
	if err != nil || newRcpt == "" {
		t.Fatalf("QueueNack failed: %v, %v", newRcpt, err)
	}

	if err := h.QueueAck("unit-test-queue", newRcpt); err != nil {
		t.Fatalf("QueueAck failed: %v", err)
	}

	qDeleted, err := h.QueueDelete("unit-test-queue")
	if err != nil || !qDeleted {
		t.Fatalf("QueueDelete failed: %v, %v", qDeleted, err)
	}
}

func TestReceiptCodec(t *testing.T) {
	msgID := "msg-12345"
	pop := "pop-67890"
	encoded := encodeAzureReceipt(msgID, pop)
	gotMsg, gotPop, gotB64, ok := decodeAzureReceipt(encoded)
	if !ok || gotMsg != msgID || gotPop != pop || gotB64 != "" {
		t.Fatalf("receipt codec roundtrip mismatch: got (%q, %q, %q, %v); want (%q, %q, \"\", true)", gotMsg, gotPop, gotB64, ok, msgID, pop)
	}

	b64Payload := "aGVsbG8td29ybGQ="
	encodedWithBody := encodeAzureReceipt(msgID, pop, b64Payload)
	gotMsg, gotPop, gotB64, ok = decodeAzureReceipt(encodedWithBody)
	if !ok || gotMsg != msgID || gotPop != pop || gotB64 != b64Payload {
		t.Fatalf("receipt codec with body mismatch: got (%q, %q, %q, %v); want (%q, %q, %q, true)", gotMsg, gotPop, gotB64, ok, msgID, pop, b64Payload)
	}

	_, _, _, ok = decodeAzureReceipt("not-valid-receipt")
	if ok {
		t.Fatalf("expected decodeAzureReceipt to fail for invalid string")
	}
}
