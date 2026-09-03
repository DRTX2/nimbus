package store

import (
	"strings"
	"testing"
	"time"
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

func TestContiguousDetectorWindows(t *testing.T) {
	start := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	windows := []Observation{
		{WindowStartedAt: start.Add(5 * time.Minute), WindowEndedAt: start.Add(10 * time.Minute)},
		{WindowStartedAt: start, WindowEndedAt: start.Add(5 * time.Minute)},
	}
	if !contiguousDetectorWindows(windows) {
		t.Fatal("contiguous five-minute windows were rejected")
	}

	windows[1].WindowEndedAt = start.Add(4 * time.Minute)
	if contiguousDetectorWindows(windows) {
		t.Fatal("a gap between detector windows was accepted")
	}

	windows[1].WindowEndedAt = start.Add(5 * time.Minute)
	windows[1].WindowStartedAt = start.Add(-time.Minute)
	if contiguousDetectorWindows(windows) {
		t.Fatal("an oversized or unaligned detector window was accepted")
	}
}

func TestValidatePipelineCredentialBoundary(t *testing.T) {
	valid := Pipeline{Mode: "aws", RoleARN: "arn:aws:iam::123456789012:role/nimbus", ExternalIDRef: "aws-external-id/acme-prod"}
	if err := validatePipeline(valid); err != nil {
		t.Fatalf("valid AWS pipeline rejected: %v", err)
	}
	for _, pipeline := range []Pipeline{
		{Mode: "aws", RoleARN: valid.RoleARN},
		{Mode: "aws", RoleARN: valid.RoleARN, ExternalIDRef: "NIMBUS_API_KEY"},
		{Mode: "fixture", ExternalIDRef: valid.ExternalIDRef},
	} {
		if err := validatePipeline(pipeline); err == nil {
			t.Fatalf("invalid pipeline accepted: %+v", pipeline)
		}
	}
}
