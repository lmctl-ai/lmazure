// SPDX-License-Identifier: MIT

package azure

import (
	"context"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/lmctl-ai/lmcloud/hostkit"
)

func errLiveNotImplemented(where string) *hostkit.CloudError {
	return hostkit.ProviderError(
		hostkit.Unauthenticated,
		0,
		"LiveCredentialsNotImplemented",
		"live Azure credentials (DefaultAzureCredential) are not wired yet ("+where+"); point an endpoint at azuresim on loopback or leave endpoints unset for the offline fake",
	)
}

// Classify maps an HTTP status and Azure error code onto the 8 shared
// hostkit.Code values (lmcloud design 02 §4 & 07 §5).
func Classify(status int, providerCode, msg string) *hostkit.CloudError {
	code := ClassifyCode(status, providerCode, msg)
	if msg == "" {
		msg = providerCode
	}
	if msg == "" {
		msg = http.StatusText(status)
	}
	ce := hostkit.ProviderError(code, status, providerCode, msg)
	switch providerCode {
	case "ContainerNotFound":
		return ce.WithAbsent(hostkit.AbsentBucket)
	case "BlobNotFound":
		return ce.WithAbsent(hostkit.AbsentObject)
	case "QueueNotFound":
		return ce.WithAbsent(hostkit.AbsentQueue)
	case "MessageNotFound":
		return ce.WithAbsent(hostkit.AbsentMessage)
	}
	return ce
}

// ClassifyCode maps an HTTP status and Azure error code string onto hostkit.Code.
func ClassifyCode(status int, providerCode, msg string) hostkit.Code {
	switch providerCode {
	case "ContainerNotFound", "BlobNotFound", "QueueNotFound", "MessageNotFound", "ResourceNotFound", "ParentNotFound":
		return hostkit.NotFound
	case "ContainerAlreadyExists", "ContainerBeingDeleted", "QueueAlreadyExists", "QueueBeingDeleted", "BlobAlreadyExists", "LeaseAlreadyPresent", "LeaseIdMismatchWithBlobOperation", "LeaseIdMismatchWithContainerOperation":
		return hostkit.Conflict
	case "ConditionNotMet", "TargetConditionNotMet", "SourceConditionNotMet":
		return hostkit.PreconditionFailed
	case "PopReceiptMismatch", "InvalidMarker", "InvalidBlobOrBlock", "InvalidMetadata", "InvalidHeaderValue", "InvalidQueryParameterValue", "OutOfRangeQueryParameterValue", "InvalidXmlDocument", "InvalidInput", "RequestBodyTooLarge", "MessageTooLarge":
		return hostkit.InvalidArgument
	case "AuthenticationFailed", "InvalidAuthenticationInfo", "NoAuthenticationInformation":
		return hostkit.Unauthenticated
	case "AuthorizationFailure", "AuthorizationPermissionMismatch", "AuthorizationResourceTypeMismatch", "AuthorizationServiceMismatch", "AuthorizationSourceIPMismatch", "AuthorizationProtocolMismatch", "InsufficientAccountPermissions", "AccountIsDisabled":
		return hostkit.PermissionDenied
	case "ServerBusy", "OperationTimedOut":
		return hostkit.Internal
	}

	switch status {
	case http.StatusBadRequest, http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity:
		return hostkit.InvalidArgument
	case http.StatusUnauthorized:
		return hostkit.Unauthenticated
	case http.StatusForbidden:
		lower := strings.ToLower(providerCode + " " + msg)
		if strings.Contains(lower, "authenticationfailed") ||
			strings.Contains(lower, "invalidauthenticationinfo") ||
			strings.Contains(lower, "noauthenticationinformation") ||
			strings.Contains(lower, "mac signature") ||
			strings.Contains(lower, "failed to authenticate") {
			return hostkit.Unauthenticated
		}
		return hostkit.PermissionDenied
	case http.StatusNotFound:
		return hostkit.NotFound
	case http.StatusConflict:
		return hostkit.Conflict
	case http.StatusPreconditionFailed:
		return hostkit.PreconditionFailed
	default:
		return hostkit.Internal
	}
}

type azureXMLError struct {
	Code    string `xml:"Code"`
	Message string `xml:"Message"`
}

func extractAzureError(respErr *azcore.ResponseError) (int, string, string) {
	status := respErr.StatusCode
	code := strings.TrimSpace(respErr.ErrorCode)
	msg := strings.TrimSpace(respErr.Error())

	if respErr.RawResponse != nil {
		if code == "" {
			code = strings.TrimSpace(respErr.RawResponse.Header.Get("x-ms-error-code"))
		}
		if respErr.RawResponse.Body != nil {
			if body, err := io.ReadAll(io.LimitReader(respErr.RawResponse.Body, 8192)); err == nil && len(body) > 0 {
				var parsed azureXMLError
				if xml.Unmarshal(body, &parsed) == nil {
					if code == "" && parsed.Code != "" {
						code = strings.TrimSpace(parsed.Code)
					}
					if parsed.Message != "" {
						msg = strings.TrimSpace(parsed.Message)
					}
				}
			}
		}
	}
	return status, code, msg
}

func classifyAzureError(err error) *hostkit.CloudError {
	if err == nil {
		return nil
	}
	var ce *hostkit.CloudError
	if errors.As(err, &ce) {
		return ce
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return hostkit.ProviderError(hostkit.Internal, 0, "RequestTimeout", err.Error())
	}
	var respErr *azcore.ResponseError
	if errors.As(err, &respErr) {
		status, code, msg := extractAzureError(respErr)
		return Classify(status, code, msg)
	}
	lower := strings.ToLower(err.Error())
	if strings.Contains(lower, "timeout") || strings.Contains(lower, "deadline exceeded") {
		return hostkit.ProviderError(hostkit.Internal, 0, "RequestTimeout", err.Error())
	}
	return hostkit.ProviderError(hostkit.Internal, 0, "TransportError", err.Error())
}

func hasAzureErrorCode(err error, targets ...string) bool {
	var respErr *azcore.ResponseError
	if !errors.As(err, &respErr) {
		return false
	}
	_, code, _ := extractAzureError(respErr)
	for _, t := range targets {
		if strings.EqualFold(code, t) {
			return true
		}
	}
	return false
}

func isStatus(err error, code int) bool {
	var respErr *azcore.ResponseError
	return errors.As(err, &respErr) && respErr.StatusCode == code
}

func isContainerNotFoundError(err error) bool {
	return hasAzureErrorCode(err, "ContainerNotFound")
}

func isBlobNotFoundError(err error) bool {
	return hasAzureErrorCode(err, "BlobNotFound")
}

func isQueueNotFoundError(err error) bool {
	return hasAzureErrorCode(err, "QueueNotFound")
}
