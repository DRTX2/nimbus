package awsmonitor

import (
	"context"
	"errors"

	"github.com/aws/smithy-go"
)

var (
	ErrNotConfigured    = errors.New("AWS connection is not configured")
	ErrIncompleteWindow = errors.New("CloudWatch returned an incomplete metric window")
)

// Failure reasons are stable, credential-free codes that tell operators what
// to fix when a CloudWatch check fails.
const (
	FailureConfiguration    = "configuration"
	FailureAccessDenied     = "access_denied"
	FailureThrottled        = "throttled"
	FailureTimeout          = "timeout"
	FailureIncompleteWindow = "incomplete_window"
	FailureUnavailable      = "unavailable"
)

var failureHints = map[string]string{
	FailureConfiguration:    "the pipeline's external ID reference is not configured on this Nimbus deployment",
	FailureAccessDenied:     "the read-only role could not be assumed or lacks cloudwatch:GetMetricData",
	FailureThrottled:        "AWS throttled the request; retry after the next window",
	FailureTimeout:          "AWS did not answer within the check timeout",
	FailureIncompleteWindow: "CloudWatch has no complete data for the queue, consumer or dead-letter queue in the last closed window",
	FailureUnavailable:      "CloudWatch could not be reached",
}

// FailureReason classifies a Collect error without exposing AWS messages,
// which can echo account identifiers or role names.
func FailureReason(err error) string {
	var apiErr smithy.APIError
	switch {
	case errors.Is(err, ErrNotConfigured):
		return FailureConfiguration
	case errors.Is(err, ErrIncompleteWindow):
		return FailureIncompleteWindow
	case errors.Is(err, context.DeadlineExceeded):
		return FailureTimeout
	case errors.As(err, &apiErr):
		switch apiErr.ErrorCode() {
		case "AccessDenied", "AccessDeniedException", "UnauthorizedOperation", "InvalidClientTokenId", "ExpiredToken", "ExpiredTokenException":
			return FailureAccessDenied
		case "Throttling", "ThrottlingException", "RequestLimitExceeded", "TooManyRequestsException":
			return FailureThrottled
		}
	}
	return FailureUnavailable
}

// FailureHint explains a failure reason in one operator-facing sentence.
func FailureHint(reason string) string {
	return failureHints[reason]
}
