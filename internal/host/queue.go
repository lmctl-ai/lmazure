package host

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azqueue"
)

func encodeAzureReceipt(messageID, popReceipt string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(messageID + "\n" + popReceipt))
}

func decodeAzureReceipt(receipt string) (messageID, popReceipt string, ok bool) {
	data, err := base64.RawURLEncoding.DecodeString(receipt)
	if err != nil {
		// Also try standard base64 if raw URL failed
		if data, err = base64.StdEncoding.DecodeString(receipt); err != nil {
			return "", "", false
		}
	}
	parts := strings.Split(string(data), "\n")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// QueueCreate ensures queue exists. Returns true if newly created, false if already existed.
func (h *Host) QueueCreate(queue string) (bool, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if cerr := ValidateQueueName(queue); cerr != nil {
		return false, h.finishErrorLocked("queue_create", queue, cerr)
	}

	ep, useFake, cerr := h.resolveBackendEndpointLocked("queue")
	if cerr != nil {
		return false, h.finishErrorLocked("queue_create", queue, cerr)
	}
	h.recordEntry("queue", "queue", "CreateQueue", queue)

	if useFake {
		if _, exists := h.fakeQueues[queue]; exists {
			h.finishOKLocked()
			return false, nil
		}
		h.fakeQueues[queue] = &fakeQueue{
			name:   queue,
			nextID: 1,
			msgs:   make([]*fakeQueueMsg, 0),
		}
		h.finishOKLocked()
		return true, nil
	}

	qClient, err := h.createQueueClient(ep, queue)
	if err != nil {
		return false, h.finishErrorLocked("queue_create", queue, mapAzureError(err, "ClientInitError"))
	}

	ctx := context.Background()
	_, err = qClient.Create(ctx, nil)
	if err == nil {
		h.finishOKLocked()
		return true, nil
	}

	mapped := mapAzureError(err, "QueueAlreadyExists")
	if mapped.HTTPStatus == http.StatusNoContent || mapped.HTTPStatus == http.StatusConflict || mapped.ProviderCode == "QueueAlreadyExists" {
		h.finishOKLocked()
		return false, nil
	}

	return false, h.finishErrorLocked("queue_create", queue, mapped)
}

// QueueDelete deletes a queue. Returns true if deleted, false if did not exist.
func (h *Host) QueueDelete(queue string) (bool, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if cerr := ValidateQueueName(queue); cerr != nil {
		return false, h.finishErrorLocked("queue_delete", queue, cerr)
	}

	ep, useFake, cerr := h.resolveBackendEndpointLocked("queue")
	if cerr != nil {
		return false, h.finishErrorLocked("queue_delete", queue, cerr)
	}
	h.recordEntry("queue", "queue", "DeleteQueue", queue)

	if useFake {
		if _, exists := h.fakeQueues[queue]; !exists {
			h.finishOKLocked()
			return false, nil
		}
		delete(h.fakeQueues, queue)
		h.finishOKLocked()
		return true, nil
	}

	qClient, err := h.createQueueClient(ep, queue)
	if err != nil {
		return false, h.finishErrorLocked("queue_delete", queue, mapAzureError(err, "ClientInitError"))
	}

	ctx := context.Background()
	_, err = qClient.Delete(ctx, nil)
	if err == nil {
		h.finishOKLocked()
		return true, nil
	}

	mapped := mapAzureError(err, "QueueNotFound")
	if mapped.HTTPStatus == http.StatusNotFound || mapped.ProviderCode == "QueueNotFound" {
		h.finishOKLocked()
		return false, nil
	}

	return false, h.finishErrorLocked("queue_delete", queue, mapped)
}

// QueueExists probes whether a queue exists and is accessible. Returns true on 200, false on 404.
func (h *Host) QueueExists(queue string) (bool, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if cerr := ValidateQueueName(queue); cerr != nil {
		return false, h.finishErrorLocked("queue_exists", queue, cerr)
	}

	ep, useFake, cerr := h.resolveBackendEndpointLocked("queue")
	if cerr != nil {
		return false, h.finishErrorLocked("queue_exists", queue, cerr)
	}

	if useFake {
		_, exists := h.fakeQueues[queue]
		h.finishOKLocked()
		return exists, nil
	}

	qClient, err := h.createQueueClient(ep, queue)
	if err != nil {
		return false, h.finishErrorLocked("queue_exists", queue, mapAzureError(err, "ClientInitError"))
	}

	ctx := context.Background()
	_, err = qClient.GetProperties(ctx, nil)
	if err == nil {
		h.finishOKLocked()
		return true, nil
	}

	mapped := mapAzureError(err, "QueueNotFound")
	if mapped.Code == CodeNotFound {
		h.finishOKLocked()
		return false, nil
	}

	return false, h.finishErrorLocked("queue_exists", queue, mapped)
}

// QueueList returns all queue names sorted lexicographically.
func (h *Host) QueueList() ([]string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	ep, useFake, cerr := h.resolveBackendEndpointLocked("queue")
	if cerr != nil {
		return nil, h.finishErrorLocked("queue_list", "", cerr)
	}

	if useFake {
		names := make([]string, 0, len(h.fakeQueues))
		for q := range h.fakeQueues {
			names = append(names, q)
		}
		sort.Strings(names)
		h.finishOKLocked()
		return names, nil
	}

	serviceClient, err := h.createQueueServiceClient(ep)
	if err != nil {
		return nil, h.finishErrorLocked("queue_list", "", mapAzureError(err, "ClientInitError"))
	}

	ctx := context.Background()
	pager := serviceClient.NewListQueuesPager(nil)
	var names []string
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, h.finishErrorLocked("queue_list", "", mapAzureError(err, "ListQueuesFailed"))
		}
		for _, q := range page.Queues {
			if q.Name != nil {
				names = append(names, *q.Name)
			}
		}
	}
	sort.Strings(names)
	h.finishOKLocked()
	return names, nil
}

// QueueSend enqueues a body onto queue and returns the message ID.
func (h *Host) QueueSend(queue, body string) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if cerr := ValidateQueueName(queue); cerr != nil {
		return "", h.finishErrorLocked("queue_send", queue, cerr)
	}
	if cerr := ValidateQueueBody(body); cerr != nil {
		return "", h.finishErrorLocked("queue_send", queue, cerr)
	}

	ep, useFake, cerr := h.resolveBackendEndpointLocked("queue")
	if cerr != nil {
		return "", h.finishErrorLocked("queue_send", queue, cerr)
	}
	h.recordEntry("queue", "queue", "SendMessage", queue)

	wireBody := EncodeQueueBody(body)

	if useFake {
		q, exists := h.fakeQueues[queue]
		if !exists {
			cerr := newCloudError(CodeNotFound, http.StatusNotFound, "QueueNotFound",
				fmt.Sprintf("queue %q does not exist", queue))
			return "", h.finishErrorLocked("queue_send", queue, cerr)
		}
		msgID := fmt.Sprintf("fake-msg-%d", q.nextID)
		q.nextID++
		h.fakeReceiptSeq++
		rcpt := fmt.Sprintf("fake-rcpt-%d", h.fakeReceiptSeq)
		q.msgs = append(q.msgs, &fakeQueueMsg{
			id:           q.nextID - 1,
			wireBody:     wireBody,
			receipt:      rcpt,
			visibleAfter: time.Now(),
			acked:        false,
		})
		h.finishOKLocked()
		return msgID, nil
	}

	qClient, err := h.createQueueClient(ep, queue)
	if err != nil {
		return "", h.finishErrorLocked("queue_send", queue, mapAzureError(err, "ClientInitError"))
	}

	ctx := context.Background()

	// Base64 encode wire body for Azure Queue XML <MessageText>
	b64Text := base64.StdEncoding.EncodeToString([]byte(wireBody))
	resp, err := qClient.EnqueueMessage(ctx, b64Text, nil)
	if err != nil {
		return "", h.finishErrorLocked("queue_send", queue, mapAzureError(err, "EnqueueMessageFailed"))
	}

	msgID := ""
	if len(resp.Messages) > 0 && resp.Messages[0].MessageID != nil {
		msgID = *resp.Messages[0].MessageID
	}
	h.finishOKLocked()
	return msgID, nil
}

// QueueReceive receives up to maxMessages from queue, waiting up to waitSeconds for messages.
func (h *Host) QueueReceive(queue string, maxMessages, waitSeconds, visibilityTimeoutSeconds int64) (*QueueMessageList, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if cerr := ValidateQueueName(queue); cerr != nil {
		return nil, h.finishErrorLocked("queue_receive", queue, cerr)
	}
	if cerr := ValidateQueueReceiveParams(maxMessages, waitSeconds, visibilityTimeoutSeconds); cerr != nil {
		return nil, h.finishErrorLocked("queue_receive", queue, cerr)
	}

	ep, useFake, cerr := h.resolveBackendEndpointLocked("queue")
	if cerr != nil {
		return nil, h.finishErrorLocked("queue_receive", queue, cerr)
	}

	if useFake {
		q, exists := h.fakeQueues[queue]
		if !exists {
			cerr := newCloudError(CodeNotFound, http.StatusNotFound, "QueueNotFound",
				fmt.Sprintf("queue %q does not exist", queue))
			return nil, h.finishErrorLocked("queue_receive", queue, cerr)
		}
		now := time.Now()
		var received []*QueueMessage
		for _, m := range q.msgs {
			if m.acked {
				continue
			}
			if m.visibleAfter.After(now) {
				continue
			}
			h.fakeReceiptSeq++
			msgIDStr := fmt.Sprintf("fake-msg-%d", m.id)
			popStr := fmt.Sprintf("fake-pop-%d", h.fakeReceiptSeq)
			m.receipt = encodeAzureReceipt(msgIDStr, popStr)
			m.visibleAfter = now.Add(time.Duration(visibilityTimeoutSeconds) * time.Second)
			decodedBody := DecodeQueueBody(m.wireBody)
			received = append(received, &QueueMessage{
				ID:      msgIDStr,
				Body:    decodedBody,
				Receipt: m.receipt,
			})
			if int64(len(received)) >= maxMessages {
				break
			}
		}
		h.finishOKLocked()
		return &QueueMessageList{Items: received}, nil
	}

	qClient, err := h.createQueueClient(ep, queue)
	if err != nil {
		return nil, h.finishErrorLocked("queue_receive", queue, mapAzureError(err, "ClientInitError"))
	}

	ctx := context.Background()

	// Verify queue existence first
	_, err = qClient.GetProperties(ctx, nil)
	if err != nil {
		return nil, h.finishErrorLocked("queue_receive", queue, mapAzureError(err, "QueueNotFound"))
	}

	// Azure Queue REST GET /messages requires visibilitytimeout in 1..604800 seconds.
	// If caller specified 0 seconds, request 1 second and immediately issue UpdateMessage(0).
	reqVis := visibilityTimeoutSeconds
	if reqVis < 1 {
		reqVis = 1
	}

	deadline := time.Now().Add(time.Duration(waitSeconds) * time.Second)
	var rawMessages []*azqueue.DequeuedMessage

	for {
		dequeueOpts := &azqueue.DequeueMessagesOptions{
			NumberOfMessages:  toPtr(int32(maxMessages)),
			VisibilityTimeout: toPtr(int32(reqVis)),
		}
		resp, dErr := qClient.DequeueMessages(ctx, dequeueOpts)
		if dErr != nil {
			return nil, h.finishErrorLocked("queue_receive", queue, mapAzureError(dErr, "DequeueMessagesFailed"))
		}
		if len(resp.Messages) > 0 {
			rawMessages = resp.Messages
			break
		}
		if waitSeconds <= 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}

	items := make([]*QueueMessage, 0, len(rawMessages))
	for _, m := range rawMessages {
		if m.MessageID == nil || m.PopReceipt == nil {
			continue
		}
		msgID := *m.MessageID
		popReceipt := *m.PopReceipt

		// If caller wanted visibility 0 (immediate redelivery): update message now
		if visibilityTimeoutSeconds == 0 {
			upResp, upErr := qClient.UpdateMessage(ctx, msgID, popReceipt, "", &azqueue.UpdateMessageOptions{
				VisibilityTimeout: toPtr(int32(0)),
			})
			if upErr == nil && upResp.PopReceipt != nil {
				popReceipt = *upResp.PopReceipt
			}
		}

		wireBody := ""
		if m.MessageText != nil {
			b64 := *m.MessageText
			if decoded, decErr := base64.StdEncoding.DecodeString(b64); decErr == nil {
				wireBody = string(decoded)
			} else {
				wireBody = b64
			}
		}
		body := DecodeQueueBody(wireBody)
		rcpt := encodeAzureReceipt(msgID, popReceipt)

		items = append(items, &QueueMessage{
			ID:      msgID,
			Body:    body,
			Receipt: rcpt,
		})
	}

	h.finishOKLocked()
	return &QueueMessageList{Items: items}, nil
}

// QueueAck permanently acknowledges (deletes) a message.
func (h *Host) QueueAck(queue, receipt string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if cerr := ValidateQueueName(queue); cerr != nil {
		return h.finishErrorLocked("queue_ack", queue, cerr)
	}
	if cerr := ValidateQueueReceipt(receipt); cerr != nil {
		return h.finishErrorLocked("queue_ack", queue, cerr)
	}

	ep, useFake, cerr := h.resolveBackendEndpointLocked("queue")
	if cerr != nil {
		return h.finishErrorLocked("queue_ack", queue, cerr)
	}

	msgID, popReceipt, validReceipt := decodeAzureReceipt(receipt)

	if useFake {
		q, exists := h.fakeQueues[queue]
		if !exists {
			cerr := newCloudError(CodeNotFound, http.StatusNotFound, "QueueNotFound",
				fmt.Sprintf("queue %q does not exist", queue))
			return h.finishErrorLocked("queue_ack", queue, cerr)
		}
		if !validReceipt {
			cerr := newCloudError(CodeInvalidArgument, 0, "InvalidReceipt", "malformed receipt")
			return h.finishErrorLocked("queue_ack", queue, cerr)
		}
		for _, m := range q.msgs {
			if m.receipt == receipt || m.receipt == popReceipt {
				m.acked = true
			}
		}
		h.finishOKLocked()
		return nil
	}

	qClient, err := h.createQueueClient(ep, queue)
	if err != nil {
		return h.finishErrorLocked("queue_ack", queue, mapAzureError(err, "ClientInitError"))
	}

	ctx := context.Background()

	// Error precedence: if receipt is invalid, verify queue existence first!
	if !validReceipt {
		_, qErr := qClient.GetProperties(ctx, nil)
		if qErr != nil {
			return h.finishErrorLocked("queue_ack", queue, mapAzureError(qErr, "QueueNotFound"))
		}
		cerr := newCloudError(CodeInvalidArgument, 0, "InvalidReceipt", "malformed receipt token")
		return h.finishErrorLocked("queue_ack", queue, cerr)
	}

	_, err = qClient.DeleteMessage(ctx, msgID, popReceipt, nil)
	if err == nil {
		h.finishOKLocked()
		return nil
	}

	mapped := mapAzureError(err, "DeleteMessageFailed")
	// If 404, verify if queue exists. If queue exists, ack is idempotent (CodeOK)
	if mapped.Code == CodeNotFound {
		_, qErr := qClient.GetProperties(ctx, nil)
		if qErr != nil {
			return h.finishErrorLocked("queue_ack", queue, mapAzureError(qErr, "QueueNotFound"))
		}
		// Queue exists, message was already deleted / acked: idempotent success
		h.finishOKLocked()
		return nil
	}

	return h.finishErrorLocked("queue_ack", queue, mapped)
}

// QueueNack updates message visibility timeout and returns the new receipt token.
func (h *Host) QueueNack(queue, receipt string, visibilityTimeoutSeconds int64) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if cerr := ValidateQueueName(queue); cerr != nil {
		return "", h.finishErrorLocked("queue_nack", queue, cerr)
	}
	if cerr := ValidateQueueReceipt(receipt); cerr != nil {
		return "", h.finishErrorLocked("queue_nack", queue, cerr)
	}
	if cerr := ValidateQueueNackTimeout(visibilityTimeoutSeconds); cerr != nil {
		return "", h.finishErrorLocked("queue_nack", queue, cerr)
	}

	ep, useFake, cerr := h.resolveBackendEndpointLocked("queue")
	if cerr != nil {
		return "", h.finishErrorLocked("queue_nack", queue, cerr)
	}

	msgID, popReceipt, validReceipt := decodeAzureReceipt(receipt)

	if useFake {
		q, exists := h.fakeQueues[queue]
		if !exists {
			cerr := newCloudError(CodeNotFound, http.StatusNotFound, "QueueNotFound",
				fmt.Sprintf("queue %q does not exist", queue))
			return "", h.finishErrorLocked("queue_nack", queue, cerr)
		}
		if !validReceipt {
			cerr := newCloudError(CodeInvalidArgument, 0, "InvalidReceipt", "malformed receipt")
			return "", h.finishErrorLocked("queue_nack", queue, cerr)
		}
		for _, m := range q.msgs {
			if m.receipt == receipt {
				h.fakeReceiptSeq++
				newPop := fmt.Sprintf("fake-pop-%d", h.fakeReceiptSeq)
				m.receipt = encodeAzureReceipt(msgID, newPop)
				m.visibleAfter = time.Now().Add(time.Duration(visibilityTimeoutSeconds) * time.Second)
				h.finishOKLocked()
				return m.receipt, nil
			}
		}
		cerr := newCloudError(CodeInvalidArgument, 0, "InvalidReceipt", "unknown receipt")
		return "", h.finishErrorLocked("queue_nack", queue, cerr)
	}

	qClient, err := h.createQueueClient(ep, queue)
	if err != nil {
		return "", h.finishErrorLocked("queue_nack", queue, mapAzureError(err, "ClientInitError"))
	}

	ctx := context.Background()

	// Error precedence: if receipt is invalid, verify queue existence first!
	if !validReceipt {
		_, qErr := qClient.GetProperties(ctx, nil)
		if qErr != nil {
			return "", h.finishErrorLocked("queue_nack", queue, mapAzureError(qErr, "QueueNotFound"))
		}
		cerr := newCloudError(CodeInvalidArgument, 0, "InvalidReceipt", "malformed receipt token")
		return "", h.finishErrorLocked("queue_nack", queue, cerr)
	}

	resp, err := qClient.UpdateMessage(ctx, msgID, popReceipt, "", &azqueue.UpdateMessageOptions{
		VisibilityTimeout: toPtr(int32(visibilityTimeoutSeconds)),
	})
	if err == nil {
		newPop := popReceipt
		if resp.PopReceipt != nil {
			newPop = *resp.PopReceipt
		}
		newRcpt := encodeAzureReceipt(msgID, newPop)
		h.finishOKLocked()
		return newRcpt, nil
	}

	mapped := mapAzureError(err, "UpdateMessageFailed")
	// If 404, check if queue itself exists
	if mapped.Code == CodeNotFound {
		_, qErr := qClient.GetProperties(ctx, nil)
		if qErr != nil {
			return "", h.finishErrorLocked("queue_nack", queue, mapAzureError(qErr, "QueueNotFound"))
		}
		// Queue exists but message / receipt not found: maps to INVALID_ARGUMENT per spec M5
		invalidRcptErr := newCloudError(CodeInvalidArgument, http.StatusNotFound, mapped.ProviderCode, mapped.Message)
		return "", h.finishErrorLocked("queue_nack", queue, invalidRcptErr)
	}

	return "", h.finishErrorLocked("queue_nack", queue, mapped)
}
