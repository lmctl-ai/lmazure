package host

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"
)

func mapAzureError(err error, fallbackCode string) *CloudError {
	if err == nil {
		return okCloudError()
	}
	var respErr *azcore.ResponseError
	if errors.As(err, &respErr) {
		status := respErr.StatusCode
		pcode := respErr.ErrorCode
		if pcode == "" && respErr.RawResponse != nil {
			pcode = respErr.RawResponse.Header.Get("x-ms-error-code")
		}
		if pcode == "" {
			pcode = fallbackCode
		}
		msg := respErr.Error()
		switch status {
		case http.StatusUnauthorized:
			return newCloudError(CodeUnauthenticated, status, pcode, msg)
		case http.StatusForbidden:
			if strings.EqualFold(pcode, "AuthenticationFailed") {
				return newCloudError(CodeUnauthenticated, status, pcode, msg)
			}
			return newCloudError(CodePermissionDenied, status, pcode, msg)
		case http.StatusNotFound:
			return newCloudError(CodeNotFound, status, pcode, msg)
		case http.StatusConflict:
			return newCloudError(CodeConflict, status, pcode, msg)
		case http.StatusPreconditionFailed:
			return newCloudError(CodePreconditionFailed, status, pcode, msg)
		case http.StatusBadRequest:
			return newCloudError(CodeInvalidArgument, status, pcode, msg)
		default:
			if status >= 500 {
				return newCloudError(CodeInternal, status, pcode, msg)
			}
			return newCloudError(CodeInternal, status, pcode, msg)
		}
	}
	return newCloudError(CodeInternal, 0, fallbackCode, err.Error())
}

func toPtr[T any](v T) *T {
	return &v
}

// BucketCreate creates a bucket (container) idempotently: returns true if newly created, false if already owned by caller.
func (h *Host) BucketCreate(bucket string) (bool, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if cerr := ValidateBucketName(bucket); cerr != nil {
		return false, h.finishErrorLocked("bucket_create", bucket, cerr)
	}

	ep, useFake, cerr := h.resolveBackendEndpointLocked("storage")
	if cerr != nil {
		return false, h.finishErrorLocked("bucket_create", bucket, cerr)
	}
	h.recordEntry("storage", "blob", "CreateContainer", bucket)

	if useFake {
		if _, exists := h.fakeBuckets[bucket]; exists {
			h.finishOKLocked()
			return false, nil
		}
		h.fakeBuckets[bucket] = make(map[string]*fakeObject)
		h.finishOKLocked()
		return true, nil
	}

	client, err := h.createBlobClient(ep)
	if err != nil {
		return false, h.finishErrorLocked("bucket_create", bucket, mapAzureError(err, "ClientInitError"))
	}

	ctx := context.Background()
	_, err = client.CreateContainer(ctx, bucket, nil)
	if err == nil {
		h.finishOKLocked()
		return true, nil
	}

	mapped := mapAzureError(err, "ContainerAlreadyExists")
	if mapped.HTTPStatus == http.StatusConflict || mapped.ProviderCode == "ContainerAlreadyExists" {
		h.finishOKLocked()
		return false, nil
	}

	return false, h.finishErrorLocked("bucket_create", bucket, mapped)
}

// BucketDelete deletes an empty bucket (container). Returns true if deleted, false if already absent.
// Fails with ErrorCode.CONFLICT if the bucket is non-empty.
func (h *Host) BucketDelete(bucket string) (bool, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if cerr := ValidateBucketName(bucket); cerr != nil {
		return false, h.finishErrorLocked("bucket_delete", bucket, cerr)
	}

	ep, useFake, cerr := h.resolveBackendEndpointLocked("storage")
	if cerr != nil {
		return false, h.finishErrorLocked("bucket_delete", bucket, cerr)
	}
	h.recordEntry("storage", "blob", "DeleteContainer", bucket)

	if useFake {
		objs, exists := h.fakeBuckets[bucket]
		if !exists {
			h.finishOKLocked()
			return false, nil
		}
		if len(objs) > 0 {
			cerr := newCloudError(CodeConflict, http.StatusConflict, "ContainerNotEmpty",
				fmt.Sprintf("container %q is not empty", bucket))
			return false, h.finishErrorLocked("bucket_delete", bucket, cerr)
		}
		delete(h.fakeBuckets, bucket)
		h.finishOKLocked()
		return true, nil
	}

	client, err := h.createBlobClient(ep)
	if err != nil {
		return false, h.finishErrorLocked("bucket_delete", bucket, mapAzureError(err, "ClientInitError"))
	}

	ctx := context.Background()

	// Pre-flight check 1: probe existence via GetProperties (HEAD).
	// In azuresim, DELETE /<bucket>?restype=container returns 202 even if the container does not exist.
	// ContainerClient.GetProperties correctly returns 404 ContainerNotFound when absent.
	containerClient := client.ServiceClient().NewContainerClient(bucket)
	_, propErr := containerClient.GetProperties(ctx, nil)
	if propErr != nil {
		mapped := mapAzureError(propErr, "GetContainerPropertiesFailed")
		if mapped.Code == CodeNotFound {
			h.finishOKLocked()
			return false, nil
		}
		return false, h.finishErrorLocked("bucket_delete", bucket, mapped)
	}

	// Pre-flight check 2: Azure DeleteContainer removes non-empty containers by default.
	// The lmcloud spec requires failing with ErrorCode.CONFLICT when the container has blobs.
	pager := client.NewListBlobsFlatPager(bucket, &azblob.ListBlobsFlatOptions{
		MaxResults: toPtr(int32(1)),
	})
	if pager.More() {
		page, listErr := pager.NextPage(ctx)
		if listErr == nil && len(page.Segment.BlobItems) > 0 {
			cerr := newCloudError(CodeConflict, http.StatusConflict, "ContainerNotEmpty",
				fmt.Sprintf("container %q is not empty", bucket))
			return false, h.finishErrorLocked("bucket_delete", bucket, cerr)
		}
		if listErr != nil {
			mapped := mapAzureError(listErr, "ListBlobsFailed")
			if mapped.Code == CodeNotFound {
				h.finishOKLocked()
				return false, nil
			}
			return false, h.finishErrorLocked("bucket_delete", bucket, mapped)
		}
	}

	_, err = client.DeleteContainer(ctx, bucket, nil)
	if err == nil {
		h.finishOKLocked()
		return true, nil
	}

	mapped := mapAzureError(err, "DeleteContainerFailed")
	if mapped.HTTPStatus == http.StatusNotFound || mapped.ProviderCode == "ContainerNotFound" {
		h.finishOKLocked()
		return false, nil
	}

	return false, h.finishErrorLocked("bucket_delete", bucket, mapped)
}

// BucketExists checks whether a container exists. Returns true if accessible, false if 404.
// Never masks auth / permission errors as false.
func (h *Host) BucketExists(bucket string) (bool, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if cerr := ValidateBucketName(bucket); cerr != nil {
		return false, h.finishErrorLocked("bucket_exists", bucket, cerr)
	}

	ep, useFake, cerr := h.resolveBackendEndpointLocked("storage")
	if cerr != nil {
		return false, h.finishErrorLocked("bucket_exists", bucket, cerr)
	}

	if useFake {
		_, exists := h.fakeBuckets[bucket]
		h.finishOKLocked()
		return exists, nil
	}

	client, err := h.createBlobClient(ep)
	if err != nil {
		return false, h.finishErrorLocked("bucket_exists", bucket, mapAzureError(err, "ClientInitError"))
	}

	ctx := context.Background()
	containerClient := client.ServiceClient().NewContainerClient(bucket)
	_, err = containerClient.GetProperties(ctx, nil)
	if err == nil {
		h.finishOKLocked()
		return true, nil
	}

	mapped := mapAzureError(err, "GetContainerPropertiesFailed")
	if mapped.Code == CodeNotFound {
		h.finishOKLocked()
		return false, nil
	}

	return false, h.finishErrorLocked("bucket_exists", bucket, mapped)
}

// BucketList returns all bucket names in lexicographical order.
func (h *Host) BucketList() ([]string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	ep, useFake, cerr := h.resolveBackendEndpointLocked("storage")
	if cerr != nil {
		return nil, h.finishErrorLocked("bucket_list", "", cerr)
	}

	if useFake {
		names := make([]string, 0, len(h.fakeBuckets))
		for b := range h.fakeBuckets {
			names = append(names, b)
		}
		sort.Strings(names)
		h.finishOKLocked()
		return names, nil
	}

	client, err := h.createBlobClient(ep)
	if err != nil {
		return nil, h.finishErrorLocked("bucket_list", "", mapAzureError(err, "ClientInitError"))
	}

	ctx := context.Background()
	pager := client.NewListContainersPager(nil)
	var names []string
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, h.finishErrorLocked("bucket_list", "", mapAzureError(err, "ListContainersFailed"))
		}
		for _, c := range page.ContainerItems {
			if c.Name != nil {
				names = append(names, *c.Name)
			}
		}
	}
	sort.Strings(names)
	h.finishOKLocked()
	return names, nil
}

// ObjectPut uploads an object with specified contentType and metadata.
func (h *Host) ObjectPut(bucket, key, body, contentType string, metadata map[string]string) (*ObjectMeta, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	target := bucket + "/" + key
	if cerr := ValidateBucketName(bucket); cerr != nil {
		return nil, h.finishErrorLocked("object_put", target, cerr)
	}
	if cerr := ValidateObjectKey(key); cerr != nil {
		return nil, h.finishErrorLocked("object_put", target, cerr)
	}
	if cerr := ValidateContentType(contentType); cerr != nil {
		return nil, h.finishErrorLocked("object_put", target, cerr)
	}
	if cerr := ValidateMetadata(metadata); cerr != nil {
		return nil, h.finishErrorLocked("object_put", target, cerr)
	}

	ep, useFake, cerr := h.resolveBackendEndpointLocked("storage")
	if cerr != nil {
		return nil, h.finishErrorLocked("object_put", target, cerr)
	}
	h.recordEntry("storage", "blob", "PutBlob", target)

	if useFake {
		b, exists := h.fakeBuckets[bucket]
		if !exists {
			cerr := newCloudError(CodeNotFound, http.StatusNotFound, "ContainerNotFound",
				fmt.Sprintf("container %q does not exist", bucket))
			return nil, h.finishErrorLocked("object_put", target, cerr)
		}
		data := []byte(body)
		etag := computeSHA256Hex(data)
		metaCopy := make(map[string]string, len(metadata))
		for k, v := range metadata {
			metaCopy[strings.ToLower(k)] = v
		}
		b[key] = &fakeObject{
			data:        data,
			contentType: contentType,
			etag:        etag,
			metadata:    metaCopy,
		}
		h.finishOKLocked()
		return &ObjectMeta{
			Exists:      true,
			Bucket:      bucket,
			Key:         key,
			Size:        int64(len(data)),
			ETag:        etag,
			ContentType: contentType,
			Metadata:    metaCopy,
		}, nil
	}

	client, err := h.createBlobClient(ep)
	if err != nil {
		return nil, h.finishErrorLocked("object_put", target, mapAzureError(err, "ClientInitError"))
	}

	ctx := context.Background()

	// Azure metadata values must be passed as map[string]*string
	metaPtrMap := make(map[string]*string, len(metadata))
	for k, v := range metadata {
		val := v
		metaPtrMap[strings.ToLower(k)] = &val
	}

	uploadOpts := &azblob.UploadBufferOptions{
		HTTPHeaders: &blob.HTTPHeaders{
			BlobContentType: &contentType,
		},
		Metadata: metaPtrMap,
	}

	resp, err := client.UploadBuffer(ctx, bucket, key, []byte(body), uploadOpts)
	if err != nil {
		return nil, h.finishErrorLocked("object_put", target, mapAzureError(err, "UploadBlobFailed"))
	}

	etag := ""
	if resp.ETag != nil {
		etag = NormalizeETag(string(*resp.ETag))
	}

	metaOut := make(map[string]string, len(metadata))
	for k, v := range metadata {
		metaOut[strings.ToLower(k)] = v
	}

	h.finishOKLocked()
	return &ObjectMeta{
		Exists:      true,
		Bucket:      bucket,
		Key:         key,
		Size:        int64(len(body)),
		ETag:        etag,
		ContentType: contentType,
		Metadata:    metaOut,
	}, nil
}

// ObjectGet downloads raw object bytes.
func (h *Host) ObjectGet(bucket, key string) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	target := bucket + "/" + key
	if cerr := ValidateBucketName(bucket); cerr != nil {
		return "", h.finishErrorLocked("object_get", target, cerr)
	}
	if cerr := ValidateObjectKey(key); cerr != nil {
		return "", h.finishErrorLocked("object_get", target, cerr)
	}

	ep, useFake, cerr := h.resolveBackendEndpointLocked("storage")
	if cerr != nil {
		return "", h.finishErrorLocked("object_get", target, cerr)
	}

	if useFake {
		b, exists := h.fakeBuckets[bucket]
		if !exists {
			cerr := newCloudError(CodeNotFound, http.StatusNotFound, "ContainerNotFound",
				fmt.Sprintf("container %q does not exist", bucket))
			return "", h.finishErrorLocked("object_get", target, cerr)
		}
		obj, objExists := b[key]
		if !objExists {
			cerr := newCloudError(CodeNotFound, http.StatusNotFound, "BlobNotFound",
				fmt.Sprintf("blob %q does not exist", key))
			return "", h.finishErrorLocked("object_get", target, cerr)
		}
		h.finishOKLocked()
		return string(obj.data), nil
	}

	client, err := h.createBlobClient(ep)
	if err != nil {
		return "", h.finishErrorLocked("object_get", target, mapAzureError(err, "ClientInitError"))
	}

	ctx := context.Background()
	resp, err := client.DownloadStream(ctx, bucket, key, nil)
	if err != nil {
		return "", h.finishErrorLocked("object_get", target, mapAzureError(err, "DownloadBlobFailed"))
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", h.finishErrorLocked("object_get", target, newCloudError(CodeInternal, 0, "ReadBodyFailed", err.Error()))
	}

	h.finishOKLocked()
	return string(data), nil
}

// ObjectHead retrieves object metadata without body.
// If the bucket exists and key is missing, returns ObjectMeta with exists() == false and CodeOK.
// If the bucket does not exist or auth fails, returns an error.
func (h *Host) ObjectHead(bucket, key string) (*ObjectMeta, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	target := bucket + "/" + key
	if cerr := ValidateBucketName(bucket); cerr != nil {
		return nil, h.finishErrorLocked("object_head", target, cerr)
	}
	if cerr := ValidateObjectKey(key); cerr != nil {
		return nil, h.finishErrorLocked("object_head", target, cerr)
	}

	ep, useFake, cerr := h.resolveBackendEndpointLocked("storage")
	if cerr != nil {
		return nil, h.finishErrorLocked("object_head", target, cerr)
	}

	if useFake {
		b, exists := h.fakeBuckets[bucket]
		if !exists {
			cerr := newCloudError(CodeNotFound, http.StatusNotFound, "ContainerNotFound",
				fmt.Sprintf("container %q does not exist", bucket))
			return nil, h.finishErrorLocked("object_head", target, cerr)
		}
		obj, objExists := b[key]
		if !objExists {
			h.finishOKLocked()
			return notFoundObjectMeta(bucket, key), nil
		}
		h.finishOKLocked()
		return &ObjectMeta{
			Exists:      true,
			Bucket:      bucket,
			Key:         key,
			Size:        int64(len(obj.data)),
			ETag:        obj.etag,
			ContentType: obj.contentType,
			Metadata:    obj.metadata,
		}, nil
	}

	client, err := h.createBlobClient(ep)
	if err != nil {
		return nil, h.finishErrorLocked("object_head", target, mapAzureError(err, "ClientInitError"))
	}

	ctx := context.Background()
	blobClient := client.ServiceClient().NewContainerClient(bucket).NewBlobClient(key)
	resp, err := blobClient.GetProperties(ctx, nil)
	if err == nil {
		etag := ""
		if resp.ETag != nil {
			etag = NormalizeETag(string(*resp.ETag))
		}
		ct := "application/octet-stream"
		if resp.ContentType != nil {
			ct = *resp.ContentType
		}
		size := int64(0)
		if resp.ContentLength != nil {
			size = *resp.ContentLength
		}
		metaOut := make(map[string]string, len(resp.Metadata))
		for k, v := range resp.Metadata {
			if v != nil {
				metaOut[strings.ToLower(k)] = *v
			}
		}
		h.finishOKLocked()
		return &ObjectMeta{
			Exists:      true,
			Bucket:      bucket,
			Key:         key,
			Size:        size,
			ETag:        etag,
			ContentType: ct,
			Metadata:    metaOut,
		}, nil
	}

	mapped := mapAzureError(err, "GetBlobPropertiesFailed")
	// If 404, we need to verify whether the container exists or is missing
	if mapped.Code == CodeNotFound {
		containerClient := client.ServiceClient().NewContainerClient(bucket)
		_, cErr := containerClient.GetProperties(ctx, nil)
		if cErr != nil {
			// Container does not exist!
			cMapped := mapAzureError(cErr, "ContainerNotFound")
			return nil, h.finishErrorLocked("object_head", target, cMapped)
		}
		// Container exists, blob does not: return exists() == false with CodeOK
		h.finishOKLocked()
		return notFoundObjectMeta(bucket, key), nil
	}

	return nil, h.finishErrorLocked("object_head", target, mapped)
}

// ObjectDelete deletes an object idempotently when the bucket exists.
// Fails with ErrorCode.NOT_FOUND if the bucket does not exist.
func (h *Host) ObjectDelete(bucket, key string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	target := bucket + "/" + key
	if cerr := ValidateBucketName(bucket); cerr != nil {
		return h.finishErrorLocked("object_delete", target, cerr)
	}
	if cerr := ValidateObjectKey(key); cerr != nil {
		return h.finishErrorLocked("object_delete", target, cerr)
	}

	ep, useFake, cerr := h.resolveBackendEndpointLocked("storage")
	if cerr != nil {
		return h.finishErrorLocked("object_delete", target, cerr)
	}
	h.recordEntry("storage", "blob", "DeleteBlob", target)

	if useFake {
		b, exists := h.fakeBuckets[bucket]
		if !exists {
			cerr := newCloudError(CodeNotFound, http.StatusNotFound, "ContainerNotFound",
				fmt.Sprintf("container %q does not exist", bucket))
			return h.finishErrorLocked("object_delete", target, cerr)
		}
		delete(b, key)
		h.finishOKLocked()
		return nil
	}

	client, err := h.createBlobClient(ep)
	if err != nil {
		return h.finishErrorLocked("object_delete", target, mapAzureError(err, "ClientInitError"))
	}

	ctx := context.Background()

	// Verify container existence first
	containerClient := client.ServiceClient().NewContainerClient(bucket)
	_, cErr := containerClient.GetProperties(ctx, nil)
	if cErr != nil {
		cMapped := mapAzureError(cErr, "ContainerNotFound")
		return h.finishErrorLocked("object_delete", target, cMapped)
	}

	_, err = client.DeleteBlob(ctx, bucket, key, nil)
	if err == nil {
		h.finishOKLocked()
		return nil
	}

	mapped := mapAzureError(err, "DeleteBlobFailed")
	// If 404 BlobNotFound when container exists: idempotent success
	if mapped.HTTPStatus == http.StatusNotFound || mapped.ProviderCode == "BlobNotFound" {
		h.finishOKLocked()
		return nil
	}

	return h.finishErrorLocked("object_delete", target, mapped)
}

// ObjectList returns a single page of objects matching prefix.
func (h *Host) ObjectList(bucket, prefix string, maxKeys int64, pageToken string) (*ObjectListPage, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if cerr := ValidateBucketName(bucket); cerr != nil {
		return nil, h.finishErrorLocked("object_list", bucket, cerr)
	}
	if cerr := ValidateListPrefix(prefix); cerr != nil {
		return nil, h.finishErrorLocked("object_list", bucket, cerr)
	}
	if cerr := ValidateMaxKeys(maxKeys); cerr != nil {
		return nil, h.finishErrorLocked("object_list", bucket, cerr)
	}

	ep, useFake, cerr := h.resolveBackendEndpointLocked("storage")
	if cerr != nil {
		return nil, h.finishErrorLocked("object_list", bucket, cerr)
	}

	if maxKeys <= 0 {
		maxKeys = 1000
	}

	if useFake {
		b, exists := h.fakeBuckets[bucket]
		if !exists {
			cerr := newCloudError(CodeNotFound, http.StatusNotFound, "ContainerNotFound",
				fmt.Sprintf("container %q does not exist", bucket))
			return nil, h.finishErrorLocked("object_list", bucket, cerr)
		}
		var matchedKeys []string
		for k := range b {
			if strings.HasPrefix(k, prefix) {
				matchedKeys = append(matchedKeys, k)
			}
		}
		sort.Strings(matchedKeys)

		startIdx := 0
		if pageToken != "" {
			for i, k := range matchedKeys {
				if k == pageToken {
					startIdx = i
					break
				}
			}
		}

		endIdx := startIdx + int(maxKeys)
		hasMore := false
		nextToken := ""
		if endIdx < len(matchedKeys) {
			hasMore = true
			nextToken = matchedKeys[endIdx]
		} else {
			endIdx = len(matchedKeys)
		}

		items := make([]*ObjectSummary, 0, endIdx-startIdx)
		for _, k := range matchedKeys[startIdx:endIdx] {
			obj := b[k]
			items = append(items, &ObjectSummary{
				Key:  k,
				Size: int64(len(obj.data)),
				ETag: obj.etag,
			})
		}
		h.finishOKLocked()
		return &ObjectListPage{
			Items:     items,
			HasMore:   hasMore,
			NextToken: nextToken,
		}, nil
	}

	client, err := h.createBlobClient(ep)
	if err != nil {
		return nil, h.finishErrorLocked("object_list", bucket, mapAzureError(err, "ClientInitError"))
	}

	ctx := context.Background()

	// Verify container existence first
	containerClient := client.ServiceClient().NewContainerClient(bucket)
	_, cErr := containerClient.GetProperties(ctx, nil)
	if cErr != nil {
		return nil, h.finishErrorLocked("object_list", bucket, mapAzureError(cErr, "ContainerNotFound"))
	}

	listOpts := &azblob.ListBlobsFlatOptions{
		Prefix:     toPtr(prefix),
		MaxResults: toPtr(int32(maxKeys)),
	}
	if pageToken != "" {
		listOpts.Marker = toPtr(pageToken)
	}

	pager := client.NewListBlobsFlatPager(bucket, listOpts)
	if !pager.More() {
		h.finishOKLocked()
		return emptyObjectListPage(), nil
	}

	page, err := pager.NextPage(ctx)
	if err != nil {
		return nil, h.finishErrorLocked("object_list", bucket, mapAzureError(err, "ListBlobsFailed"))
	}

	items := make([]*ObjectSummary, 0, len(page.Segment.BlobItems))
	for _, b := range page.Segment.BlobItems {
		key := ""
		if b.Name != nil {
			key = *b.Name
		}
		size := int64(0)
		if b.Properties != nil && b.Properties.ContentLength != nil {
			size = *b.Properties.ContentLength
		}
		etag := ""
		if b.Properties != nil && b.Properties.ETag != nil {
			etag = NormalizeETag(string(*b.Properties.ETag))
		}
		items = append(items, &ObjectSummary{
			Key:  key,
			Size: size,
			ETag: etag,
		})
	}

	hasMore := false
	nextToken := ""
	if page.NextMarker != nil && *page.NextMarker != "" {
		hasMore = true
		nextToken = *page.NextMarker
	}

	h.finishOKLocked()
	return &ObjectListPage{
		Items:     items,
		HasMore:   hasMore,
		NextToken: nextToken,
	}, nil
}

// ObjectListAll returns all object keys matching prefix in ascending lexicographical order.
func (h *Host) ObjectListAll(bucket, prefix string) ([]string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if cerr := ValidateBucketName(bucket); cerr != nil {
		return nil, h.finishErrorLocked("object_list_all", bucket, cerr)
	}
	if cerr := ValidateListPrefix(prefix); cerr != nil {
		return nil, h.finishErrorLocked("object_list_all", bucket, cerr)
	}

	ep, useFake, cerr := h.resolveBackendEndpointLocked("storage")
	if cerr != nil {
		return nil, h.finishErrorLocked("object_list_all", bucket, cerr)
	}

	if useFake {
		b, exists := h.fakeBuckets[bucket]
		if !exists {
			cerr := newCloudError(CodeNotFound, http.StatusNotFound, "ContainerNotFound",
				fmt.Sprintf("container %q does not exist", bucket))
			return nil, h.finishErrorLocked("object_list_all", bucket, cerr)
		}
		var matched []string
		for k := range b {
			if strings.HasPrefix(k, prefix) {
				matched = append(matched, k)
			}
		}
		sort.Strings(matched)
		h.finishOKLocked()
		return matched, nil
	}

	client, err := h.createBlobClient(ep)
	if err != nil {
		return nil, h.finishErrorLocked("object_list_all", bucket, mapAzureError(err, "ClientInitError"))
	}

	ctx := context.Background()

	// Verify container existence first
	containerClient := client.ServiceClient().NewContainerClient(bucket)
	_, cErr := containerClient.GetProperties(ctx, nil)
	if cErr != nil {
		return nil, h.finishErrorLocked("object_list_all", bucket, mapAzureError(cErr, "ContainerNotFound"))
	}

	pager := client.NewListBlobsFlatPager(bucket, &azblob.ListBlobsFlatOptions{
		Prefix: toPtr(prefix),
	})

	var allKeys []string
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, h.finishErrorLocked("object_list_all", bucket, mapAzureError(err, "ListBlobsFailed"))
		}
		for _, b := range page.Segment.BlobItems {
			if b.Name != nil {
				allKeys = append(allKeys, *b.Name)
			}
		}
	}

	sort.Strings(allKeys)
	h.finishOKLocked()
	return allKeys, nil
}
