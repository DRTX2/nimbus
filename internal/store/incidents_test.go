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
	if !contiguousDetectorWindows(windows, 5*time.Minute) {
		t.Fatal("contiguous five-minute windows were rejected")
	}

	windows[1].WindowEndedAt = start.Add(4 * time.Minute)
	if contiguousDetectorWindows(windows, 5*time.Minute) {
		t.Fatal("a gap between detector windows was accepted")
	}

	windows[1].WindowEndedAt = start.Add(5 * time.Minute)
	windows[1].WindowStartedAt = start.Add(-time.Minute)
	if contiguousDetectorWindows(windows, 5*time.Minute) {
		t.Fatal("an oversized or unaligned detector window was accepted")
	}
}

func TestDetectorWindowsFollowThePipelineWindow(t *testing.T) {
	start := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	short := []Observation{
		{WindowStartedAt: start.Add(30 * time.Second), WindowEndedAt: start.Add(60 * time.Second)},
		{WindowStartedAt: start, WindowEndedAt: start.Add(30 * time.Second)},
	}
	if !contiguousDetectorWindows(short, 30*time.Second) {
		t.Fatal("contiguous 30-second windows were rejected for a 30-second pipeline")
	}
	if contiguousDetectorWindows(short, 5*time.Minute) {
		t.Fatal("30-second windows were accepted for a 5-minute pipeline")
	}
	if err := validateWindowShape(start.Add(10*time.Second), start.Add(40*time.Second), 30*time.Second); err == nil {
		t.Fatal("a window off the 30-second boundary was accepted")
	}
	if err := validateWindowShape(start, start.Add(time.Minute), 30*time.Second); err == nil {
		t.Fatal("a window longer than the pipeline window was accepted")
	}
}

func TestValidatePipelineWindow(t *testing.T) {
	for _, seconds := range []int{30, 60, 300} {
		if err := validatePipeline(Pipeline{Name: "orders", Mode: "fixture", WindowSeconds: seconds}); err != nil {
			t.Fatalf("%d-second push pipeline rejected: %v", seconds, err)
		}
	}
	if err := validatePipeline(Pipeline{Name: "orders", Mode: "fixture", WindowSeconds: 45}); err == nil {
		t.Fatal("unsupported window accepted")
	}
	aws := Pipeline{Name: "billing", Mode: "aws", RoleARN: "arn:aws:iam::123456789012:role/nimbus", ExternalIDRef: "aws-external-id/acme-prod", WindowSeconds: 60}
	if err := validatePipeline(aws); err == nil {
		t.Fatal("an AWS pipeline with a window CloudWatch cannot serve was accepted")
	}
}

func TestValidatePipelineCredentialBoundary(t *testing.T) {
	valid := Pipeline{Name: "billing", Mode: "aws", RoleARN: "arn:aws:iam::123456789012:role/nimbus", ExternalIDRef: "aws-external-id/acme-prod"}
	if err := validatePipeline(valid); err != nil {
		t.Fatalf("valid AWS pipeline rejected: %v", err)
	}
	for _, pipeline := range []Pipeline{
		{Name: "billing", Mode: "aws", RoleARN: valid.RoleARN},
		{Name: "billing", Mode: "aws", RoleARN: valid.RoleARN, ExternalIDRef: "NIMBUS_API_KEY"},
		{Name: "billing", Mode: "fixture", ExternalIDRef: valid.ExternalIDRef},
		{Mode: "fixture"},
	} {
		if err := validatePipeline(pipeline); err == nil {
			t.Fatalf("invalid pipeline accepted: %+v", pipeline)
		}
	}
}
