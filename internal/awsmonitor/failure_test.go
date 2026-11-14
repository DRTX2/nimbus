package awsmonitor

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/aws/smithy-go"
	"github.com/david/nimbus/internal/store"
)

func TestFailureReasonClassifiesWithoutLeakingAWSMessages(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		want string
	}{
		"unconfigured reference": {fmt.Errorf("collect: %w", ErrNotConfigured), FailureConfiguration},
		"incomplete window":      {fmt.Errorf("%w: missing dlq", ErrIncompleteWindow), FailureIncompleteWindow},
		"timeout":                {fmt.Errorf("read: %w", context.DeadlineExceeded), FailureTimeout},
		"assume role denied":     {&smithy.GenericAPIError{Code: "AccessDenied", Message: "arn:aws:iam::123456789012:role/x"}, FailureAccessDenied},
		"cloudwatch denied":      {fmt.Errorf("read: %w", &smithy.GenericAPIError{Code: "AccessDeniedException"}), FailureAccessDenied},
		"expired token":          {&smithy.GenericAPIError{Code: "ExpiredToken"}, FailureAccessDenied},
		"throttled":              {&smithy.GenericAPIError{Code: "ThrottlingException"}, FailureThrottled},
		"unknown API error":      {&smithy.GenericAPIError{Code: "InternalServiceFault"}, FailureUnavailable},
		"network":                {errors.New("dial tcp: connection refused"), FailureUnavailable},
	} {
		t.Run(name, func(t *testing.T) {
			reason := FailureReason(tc.err)
			if reason != tc.want {
				t.Fatalf("FailureReason() = %q, want %q", reason, tc.want)
			}
			if hint := FailureHint(reason); hint == "" || strings.Contains(hint, "123456789012") {
				t.Fatalf("FailureHint(%q) = %q, want a credential-free hint", reason, hint)
			}
		})
	}
}

func TestCollectorReportsUnconfiguredReferenceAsConfiguration(t *testing.T) {
	_, err := NewCollector(nil).Collect(context.Background(), store.Pipeline{
		ID: "p", Mode: "aws", RoleARN: "arn:aws:iam::123456789012:role/read", ExternalIDRef: "aws-external-id/missing",
	})
	if got := FailureReason(err); got != FailureConfiguration {
		t.Fatalf("FailureReason() = %q, want %q (err %v)", got, FailureConfiguration, err)
	}
}
