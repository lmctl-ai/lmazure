// SPDX-License-Identifier: MIT

package azure

import (
	"context"
	"encoding/base64"
	"net/http"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azqueue"
	"github.com/lmctl-ai/lmcloud/hostkit"
)

// EncodeReceipt encodes an Azure Queue message ID and pop receipt into the
// portable lmcloud receipt token (spec decision H1: ONLY messageId + popReceipt,
// never MessageText).
func EncodeReceipt(messageID, popReceipt string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(messageID + "\n" + popReceipt))
}

// DecodeReceipt decodes a portable lmcloud receipt token into (messageID, popReceipt, ok).
// It rejects any token that is not base64url(messageId + "\n" + popReceipt) with
// non-empty messageId and popReceipt (and no extra newline-separated fields).
func DecodeReceipt(receipt string) (string, string, bool) {
	if receipt == "" {
		return "", "", false
	}
	raw, err := base64.RawURLEncoding.DecodeString(receipt)
	if err != nil {
		return "", "", false
	}
	parts := strings.Split(string(raw), "\n")
	if len(parts) != 2 {
		return "", "", false
	}
	msgID, popReceipt := parts[0], parts[1]
	if msgID == "" || popReceipt == "" {
		return "", "", false
	}
	return msgID, popReceipt, true
}

// QueueCaps reports Azure Queue's capabilities:
//   - LongPoll: false (Azure Queue DequeueMessages has no server-side wait;
//     hostkit.WaitLoop or Receive polls client-side when wait > 0).
//   - TextOnlyWire: false (Send base64-encodes the entire wire string for Azure
//     Queue's XML MessageText element, so hostkit's __lmcloud_b64__ layer is
//     not needed).
func (r *remote) QueueCaps() hostkit.QueueCaps {
	return hostkit.QueueCaps{
		LongPoll:     false,
		TextOnlyWire: false,
	}
}

func (r *remote) CreateQueue(ctx context.Context, q string) (bool, error) {
	base := hostkit.EndpointFrom(ctx)
	svc, cErr := r.newQueueServiceClient(base)
	if cErr != nil {
		return false, cErr
	}
	qClient := svc.NewQueueClient(q)

	// Azure Queue REST returns 201 Created for a new queue and 204 No Content
	// when the queue already exists with identical metadata, and azqueue.QueueClient.Create
	// returns err == nil for both. Probe GetProperties first to distinguish
	// newly-created (true) from already-existing (false).
	_, propErr := qClient.GetProperties(ctx, nil)
	if propErr == nil {
		return false, nil
	}
	if !isQueueNotFoundError(propErr) && !isStatus(propErr, http.StatusNotFound) {
		return false, classifyAzureError(propErr)
	}

	if _, err := qClient.Create(ctx, nil); err != nil {
		if hasAzureErrorCode(err, "QueueAlreadyExists") {
			return false, nil
		}
		return false, classifyAzureError(err)
	}
	r.recordQueueCreated(base, q)
	return true, nil
}

func (r *remote) DeleteQueue(ctx context.Context, q string) error {
	base := hostkit.EndpointFrom(ctx)
	svc, cErr := r.newQueueServiceClient(base)
	if cErr != nil {
		return cErr
	}
	qClient := svc.NewQueueClient(q)

	// Compat probe for pre-7903c9c azuresim where DELETE on a missing queue
	// returned 204 No Content instead of 404 QueueNotFound.
	if r.compatProbe() {
		if _, err := qClient.GetProperties(ctx, nil); err != nil {
			return classifyAzureError(err)
		}
	}

	if _, err := qClient.Delete(ctx, nil); err != nil {
		return classifyAzureError(err)
	}
	r.deleteQueueCreated(base, q)
	return nil
}

func (r *remote) LookupQueue(ctx context.Context, q string) error {
	base := hostkit.EndpointFrom(ctx)
	svc, cErr := r.newQueueServiceClient(base)
	if cErr != nil {
		return cErr
	}
	qClient := svc.NewQueueClient(q)
	if _, err := qClient.GetProperties(ctx, nil); err != nil {
		return classifyAzureError(err)
	}
	return nil
}

func (r *remote) ListQueues(ctx context.Context, token string) ([]string, string, error) {
	base := hostkit.EndpointFrom(ctx)
	svc, cErr := r.newQueueServiceClient(base)
	if cErr != nil {
		return nil, "", cErr
	}
	opts := &azqueue.ListQueuesOptions{}
	if token != "" {
		opts.Marker = &token
	}
	pager := svc.NewListQueuesPager(opts)
	if !pager.More() {
		return []string{}, "", nil
	}
	page, err := pager.NextPage(ctx)
	if err != nil {
		return nil, "", classifyAzureError(err)
	}
	names := make([]string, 0, len(page.Queues))
	for _, item := range page.Queues {
		if item != nil && item.Name != nil {
			names = append(names, *item.Name)
		}
	}
	next := ""
	if page.NextMarker != nil {
		next = *page.NextMarker
	}
	return names, next, nil
}

func (r *remote) Send(ctx context.Context, q, wire string) (string, error) {
	base := hostkit.EndpointFrom(ctx)
	svc, cErr := r.newQueueServiceClient(base)
	if cErr != nil {
		return "", cErr
	}
	qClient := svc.NewQueueClient(q)

	// Base64-encode the wire payload (Azure Queue Storage convention) so arbitrary
	// UTF-8/binary payloads pass safely through XML MessageText and survive well
	// under Azure Queue's 64 KiB XML request limit.
	b64Wire := base64.StdEncoding.EncodeToString([]byte(wire))
	resp, err := qClient.EnqueueMessage(ctx, b64Wire, nil)
	if err != nil {
		return "", classifyAzureError(err)
	}
	if len(resp.Messages) == 0 || resp.Messages[0] == nil || resp.Messages[0].MessageID == nil {
		return "", hostkit.ProviderError(hostkit.Internal, http.StatusInternalServerError, "EmptyEnqueueResponse", "Azure EnqueueMessage returned no MessageID")
	}
	return *resp.Messages[0].MessageID, nil
}

func (r *remote) Receive(ctx context.Context, q string, max, wait, vis int) ([]hostkit.RawMessage, error) {
	base := hostkit.EndpointFrom(ctx)
	svc, cErr := r.newQueueServiceClient(base)
	if cErr != nil {
		return nil, cErr
	}
	qClient := svc.NewQueueClient(q)

	// Azure Queue DequeueMessages requires visibilitytimeout >= 1. For vis == 0,
	// dequeue with visibilitytimeout=1 and immediately call UpdateMessage with
	// visibilitytimeout=0, using the rotated PopReceipt (design 07 §4.6).
	reqVis := int32(vis)
	if vis == 0 {
		reqVis = 1
	}
	numMsgs := int32(max)
	deadline := time.Now().Add(time.Duration(wait) * time.Second)

	for {
		resp, err := qClient.DequeueMessages(ctx, &azqueue.DequeueMessagesOptions{
			NumberOfMessages:  &numMsgs,
			VisibilityTimeout: &reqVis,
		})
		if err != nil {
			return nil, classifyAzureError(err)
		}
		if len(resp.Messages) > 0 {
			out := make([]hostkit.RawMessage, 0, len(resp.Messages))
			for _, m := range resp.Messages {
				if m == nil || m.MessageID == nil || m.PopReceipt == nil {
					continue
				}
				msgID := *m.MessageID
				popReceipt := *m.PopReceipt
				rawB64 := ""
				if m.MessageText != nil {
					rawB64 = *m.MessageText
				}
				if r.compatProbe() {
					r.storeMsgBody(base, q, msgID, rawB64)
				}

				if vis == 0 {
					updateBody := ""
					if r.compatProbe() {
						updateBody = rawB64
					}
					zeroVis := int32(0)
					upResp, upErr := qClient.UpdateMessage(ctx, msgID, popReceipt, updateBody, &azqueue.UpdateMessageOptions{
						VisibilityTimeout: &zeroVis,
					})
					if upErr == nil && upResp.PopReceipt != nil && *upResp.PopReceipt != "" {
						popReceipt = *upResp.PopReceipt
					}
				}

				wire := rawB64
				if decoded, decErr := base64.StdEncoding.DecodeString(rawB64); decErr == nil {
					wire = string(decoded)
				}
				out = append(out, hostkit.RawMessage{
					ID:      msgID,
					Wire:    wire,
					Receipt: EncodeReceipt(msgID, popReceipt),
				})
			}
			return out, nil
		}

		if wait <= 0 || time.Now().After(deadline) {
			return []hostkit.RawMessage{}, nil
		}
		sleepDur := 100 * time.Millisecond
		if rem := time.Until(deadline); rem < sleepDur {
			sleepDur = rem
		}
		if sleepDur <= 0 {
			return []hostkit.RawMessage{}, nil
		}
		select {
		case <-ctx.Done():
			return nil, classifyAzureError(ctx.Err())
		case <-time.After(sleepDur):
		}
	}
}

func (r *remote) Ack(ctx context.Context, q, receipt string) error {
	msgID, popReceipt, ok := DecodeReceipt(receipt)
	if !ok {
		return hostkit.Preflightf("invalid Azure queue receipt token")
	}
	base := hostkit.EndpointFrom(ctx)
	svc, cErr := r.newQueueServiceClient(base)
	if cErr != nil {
		return cErr
	}
	qClient := svc.NewQueueClient(q)
	if _, err := qClient.DeleteMessage(ctx, msgID, popReceipt, nil); err != nil {
		return classifyAzureError(err)
	}
	r.deleteMsgBody(base, q, msgID)
	return nil
}

func (r *remote) Nack(ctx context.Context, q, receipt string, vis int) (string, error) {
	msgID, popReceipt, ok := DecodeReceipt(receipt)
	if !ok {
		return "", hostkit.Preflightf("invalid Azure queue receipt token")
	}
	base := hostkit.EndpointFrom(ctx)
	svc, cErr := r.newQueueServiceClient(base)
	if cErr != nil {
		return "", cErr
	}
	updateBody := ""
	if r.compatProbe() {
		updateBody = r.loadMsgBody(base, q, msgID)
	}
	qClient := svc.NewQueueClient(q)
	visInt := int32(vis)
	resp, err := qClient.UpdateMessage(ctx, msgID, popReceipt, updateBody, &azqueue.UpdateMessageOptions{
		VisibilityTimeout: &visInt,
	})
	if err != nil {
		return "", classifyAzureError(err)
	}
	newPop := popReceipt
	if resp.PopReceipt != nil && *resp.PopReceipt != "" {
		newPop = *resp.PopReceipt
	}
	return EncodeReceipt(msgID, newPop), nil
}
