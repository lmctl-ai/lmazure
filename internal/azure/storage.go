// SPDX-License-Identifier: MIT

package azure

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"
	"github.com/lmctl-ai/lmazure/internal/membudget"
	"github.com/lmctl-ai/lmcloud/hostkit"
)

const internalOffsetTokenPrefix = "__lmazure_ofs__:"

func stripETag(etag string) string {
	etag = strings.TrimSpace(etag)
	etag = strings.TrimPrefix(etag, "W/")
	return strings.Trim(etag, `"`)
}

func (r *remote) CreateBucket(ctx context.Context, b string) (bool, error) {
	base := hostkit.EndpointFrom(ctx)
	client, cErr := r.newBlobClient(base)
	if cErr != nil {
		return false, cErr
	}
	_, err := client.CreateContainer(ctx, b, nil)
	if err != nil {
		if hasAzureErrorCode(err, "ContainerAlreadyExists") {
			return false, nil
		}
		return false, classifyAzureError(err)
	}
	r.recordBucketCreated(base, b)
	return true, nil
}

func (r *remote) DeleteBucket(ctx context.Context, b string) error {
	base := hostkit.EndpointFrom(ctx)
	client, cErr := r.newBlobClient(base)
	if cErr != nil {
		return cErr
	}
	cClient := client.ServiceClient().NewContainerClient(b)

	// Compat probe for pre-7903c9c azuresim where ListBlobsFlat and Delete on a
	// missing container returned 200/202 instead of 404 ContainerNotFound.
	if r.compatProbe() {
		if _, err := cClient.GetProperties(ctx, nil); err != nil {
			return classifyAzureError(err)
		}
	}

	// Azure DeleteContainer deletes non-empty containers by default; pre-check
	// emptiness (MaxResults=1) to enforce lmcloud's CONFLICT requirement.
	maxOne := int32(1)
	pager := cClient.NewListBlobsFlatPager(&azblob.ListBlobsFlatOptions{
		MaxResults: &maxOne,
	})
	if pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return classifyAzureError(err)
		}
		if page.Segment != nil && len(page.Segment.BlobItems) > 0 {
			return hostkit.ProviderError(hostkit.Conflict, http.StatusConflict, "ContainerNotEmpty", "cannot delete non-empty bucket: "+b)
		}
	}

	if _, err := cClient.Delete(ctx, nil); err != nil {
		return classifyAzureError(err)
	}
	r.deleteBucketCreated(base, b)
	return nil
}

func (r *remote) HeadBucket(ctx context.Context, b string) error {
	base := hostkit.EndpointFrom(ctx)
	client, cErr := r.newBlobClient(base)
	if cErr != nil {
		return cErr
	}
	cClient := client.ServiceClient().NewContainerClient(b)
	if _, err := cClient.GetProperties(ctx, nil); err != nil {
		return classifyAzureError(err)
	}
	return nil
}

func (r *remote) ListBuckets(ctx context.Context, token string) ([]string, string, error) {
	base := hostkit.EndpointFrom(ctx)
	client, cErr := r.newBlobClient(base)
	if cErr != nil {
		return nil, "", cErr
	}
	opts := &azblob.ListContainersOptions{}
	if token != "" {
		opts.Marker = &token
	}
	pager := client.NewListContainersPager(opts)
	if !pager.More() {
		return []string{}, "", nil
	}
	page, err := pager.NextPage(ctx)
	if err != nil {
		return nil, "", classifyAzureError(err)
	}
	names := make([]string, 0, len(page.ContainerItems))
	for _, item := range page.ContainerItems {
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

func (r *remote) PutObject(ctx context.Context, b, k string, body []byte, ct string, meta map[string]string) (hostkit.ObjectMeta, error) {
	if err := membudget.Require(int64(len(body))); err != nil {
		return hostkit.ObjectMeta{}, err
	}
	base := hostkit.EndpointFrom(ctx)
	client, cErr := r.newBlobClient(base)
	if cErr != nil {
		return hostkit.ObjectMeta{}, cErr
	}
	bClient := client.ServiceClient().NewContainerClient(b).NewBlockBlobClient(k)

	azMeta := make(map[string]*string, len(meta))
	cleanMeta := make(map[string]string, len(meta))
	for mk, mv := range meta {
		lower := strings.ToLower(mk)
		valCopy := mv
		azMeta[lower] = &valCopy
		cleanMeta[lower] = mv
	}

	uploadOpts := &azblob.UploadBufferOptions{
		BlockSize: 16 * 1024 * 1024, // Single-shot PUT for all <=10 MiB lmcloud payloads
		HTTPHeaders: &blob.HTTPHeaders{
			BlobContentType: &ct,
		},
	}
	if len(azMeta) > 0 {
		uploadOpts.Metadata = azMeta
	}

	resp, err := bClient.UploadBuffer(ctx, body, uploadOpts)
	if err != nil {
		return hostkit.ObjectMeta{}, classifyAzureError(err)
	}

	etag := ""
	if resp.ETag != nil {
		etag = stripETag(string(*resp.ETag))
	}

	return hostkit.ObjectMeta{
		Exists:      true,
		Bucket:      b,
		Key:         k,
		Size:        int64(len(body)),
		ETag:        etag,
		ContentType: ct,
		Metadata:    cleanMeta,
	}, nil
}

func (r *remote) GetObject(ctx context.Context, b, k string, limit int64) ([]byte, error) {
	base := hostkit.EndpointFrom(ctx)
	client, cErr := r.newBlobClient(base)
	if cErr != nil {
		return nil, cErr
	}
	bClient := client.ServiceClient().NewContainerClient(b).NewBlockBlobClient(k)
	resp, err := bClient.DownloadStream(ctx, nil)
	if err != nil {
		return nil, classifyAzureError(err)
	}
	defer resp.Body.Close()
	if resp.ContentLength != nil && *resp.ContentLength > 0 {
		if *resp.ContentLength > limit {
			return nil, hostkit.ProviderError(hostkit.Internal, 0, "", "object body exceeds limit; refusing to truncate")
		}
		if err := membudget.Require(*resp.ContentLength); err != nil {
			return nil, err
		}
	}
	data, readErr := hostkit.ReadBounded(resp.Body, limit)
	if readErr != nil {
		return nil, readErr
	}
	if err := membudget.Require(int64(len(data))); err != nil {
		return nil, err
	}
	return data, nil
}

func (r *remote) HeadObject(ctx context.Context, b, k string) (hostkit.ObjectMeta, error) {
	base := hostkit.EndpointFrom(ctx)
	client, cErr := r.newBlobClient(base)
	if cErr != nil {
		return hostkit.ObjectMeta{}, cErr
	}
	cClient := client.ServiceClient().NewContainerClient(b)
	bClient := cClient.NewBlockBlobClient(k)

	props, err := bClient.GetProperties(ctx, nil)
	if err != nil {
		if isContainerNotFoundError(err) {
			return hostkit.ObjectMeta{}, classifyAzureError(err)
		}
		if isBlobNotFoundError(err) || isStatus(err, http.StatusNotFound) {
			if r.compatProbe() {
				if _, pErr := cClient.GetProperties(ctx, nil); pErr != nil {
					return hostkit.ObjectMeta{}, classifyAzureError(pErr)
				}
			}
			return hostkit.ObjectMeta{}, classifyAzureError(err).WithAbsent(hostkit.AbsentObject)
		}
		return hostkit.ObjectMeta{}, classifyAzureError(err)
	}

	size := int64(0)
	if props.ContentLength != nil {
		size = *props.ContentLength
	}
	etag := ""
	if props.ETag != nil {
		etag = stripETag(string(*props.ETag))
	}
	ct := "application/octet-stream"
	if props.ContentType != nil && *props.ContentType != "" {
		ct = *props.ContentType
	}
	meta := make(map[string]string, len(props.Metadata))
	for mk, mv := range props.Metadata {
		if mv != nil {
			meta[strings.ToLower(mk)] = *mv
		}
	}

	return hostkit.ObjectMeta{
		Exists:      true,
		Bucket:      b,
		Key:         k,
		Size:        size,
		ETag:        etag,
		ContentType: ct,
		Metadata:    meta,
	}, nil
}

func (r *remote) DeleteObject(ctx context.Context, b, k string) error {
	base := hostkit.EndpointFrom(ctx)
	client, cErr := r.newBlobClient(base)
	if cErr != nil {
		return cErr
	}
	cClient := client.ServiceClient().NewContainerClient(b)
	bClient := cClient.NewBlockBlobClient(k)

	if _, err := bClient.Delete(ctx, nil); err != nil {
		if isContainerNotFoundError(err) {
			return classifyAzureError(err)
		}
		if isBlobNotFoundError(err) || isStatus(err, http.StatusNotFound) {
			if r.compatProbe() {
				if _, pErr := cClient.GetProperties(ctx, nil); pErr != nil {
					return classifyAzureError(pErr)
				}
			}
			return classifyAzureError(err).WithAbsent(hostkit.AbsentObject)
		}
		return classifyAzureError(err)
	}
	return nil
}

func (r *remote) ListObjects(ctx context.Context, b, prefix string, max int, token string) (hostkit.ObjectPage, error) {
	base := hostkit.EndpointFrom(ctx)
	client, cErr := r.newBlobClient(base)
	if cErr != nil {
		return hostkit.ObjectPage{}, cErr
	}
	cClient := client.ServiceClient().NewContainerClient(b)

	// Compat probe for pre-7903c9c azuresim where ListBlobsFlat on a missing
	// container returned 200 OK <Blobs/> instead of 404 ContainerNotFound.
	if r.compatProbe() {
		if _, err := cClient.GetProperties(ctx, nil); err != nil {
			return hostkit.ObjectPage{}, classifyAzureError(err)
		}
	}

	syntheticOffset := -1
	if strings.HasPrefix(token, internalOffsetTokenPrefix) {
		rawOfs := strings.TrimPrefix(token, internalOffsetTokenPrefix)
		ofs, err := strconv.Atoi(rawOfs)
		if err != nil || ofs < 0 {
			return hostkit.ObjectPage{}, hostkit.Preflightf("invalid page_token")
		}
		syntheticOffset = ofs
	}

	maxResults := int32(max)
	listOpts := &azblob.ListBlobsFlatOptions{
		MaxResults: &maxResults,
	}
	if prefix != "" {
		listOpts.Prefix = &prefix
	}
	if token != "" && syntheticOffset < 0 {
		listOpts.Marker = &token
	}

	pager := cClient.NewListBlobsFlatPager(listOpts)
	var allItems []hostkit.ObjectSummary
	nextToken := ""

	if pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return hostkit.ObjectPage{}, classifyAzureError(err)
		}
		if page.Segment != nil {
			for _, item := range page.Segment.BlobItems {
				if item == nil || item.Name == nil {
					continue
				}
				size := int64(0)
				etag := ""
				if item.Properties != nil {
					if item.Properties.ContentLength != nil {
						size = *item.Properties.ContentLength
					}
					if item.Properties.ETag != nil {
						etag = stripETag(string(*item.Properties.ETag))
					}
				}
				allItems = append(allItems, hostkit.ObjectSummary{
					Key:  *item.Name,
					Size: size,
					ETag: etag,
				})
			}
		}
		if page.NextMarker != nil && *page.NextMarker != "" {
			nextToken = *page.NextMarker
		}
	}

	sort.Slice(allItems, func(i, j int) bool {
		return allItems[i].Key < allItems[j].Key
	})

	// azuresim returns all matching blobs in one segment regardless of
	// maxresults; apply client-side pagination when the response exceeds max.
	if syntheticOffset >= 0 {
		if syntheticOffset >= len(allItems) {
			return hostkit.ObjectPage{Items: []hostkit.ObjectSummary{}}, nil
		}
		end := syntheticOffset + max
		if end < len(allItems) {
			nextToken = internalOffsetTokenPrefix + strconv.Itoa(end)
			allItems = allItems[syntheticOffset:end]
		} else {
			nextToken = ""
			allItems = allItems[syntheticOffset:]
		}
	} else if len(allItems) > max && nextToken == "" {
		nextToken = internalOffsetTokenPrefix + strconv.Itoa(max)
		allItems = allItems[:max]
	}

	if token != "" && nextToken == token {
		return hostkit.ObjectPage{}, hostkit.ProviderError(hostkit.Internal, 0, "StalledPagination", "pagination stalled: continuation token repeated")
	}
	if allItems == nil {
		allItems = []hostkit.ObjectSummary{}
	}
	return hostkit.ObjectPage{
		Items:     allItems,
		NextToken: nextToken,
	}, nil
}
