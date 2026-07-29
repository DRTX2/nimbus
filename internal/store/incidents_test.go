package store

import (
	"strings"
	"testing"
)

func TestValidateIncidentReview(t *testing.T) {
	for _, disposition := range []string{"true_positive", "false_positive", "expected_behavior", "duplicate"} {
		if err := validateIncidentReview(disposition, "bounded comment"); err != nil {
			t.Fatalf("valid disposition %q rejected: %v", disposition, err)
		}
	}
	if err := validateIncidentReview("resolved", ""); err == nil {
		t.Fatal("lifecycle status accepted as a disposition")
	}
	if err := validateIncidentReview("true_positive", strings.Repeat("x", maxIncidentReviewCommentLength+1)); err == nil {
		t.Fatal("oversized comment accepted")
	}
	if err := validateIncidentReview("true_positive", strings.Repeat("á", maxIncidentReviewCommentLength)); err != nil {
		t.Fatalf("bounded UTF-8 comment rejected: %v", err)
	}
}
