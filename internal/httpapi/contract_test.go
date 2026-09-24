package httpapi

import (
	"bytes"
	"os"
	"testing"
)

// The checked-in contract is consumed by the dashboard and external clients;
// any drift must be an explicit, reviewed change.
func TestOpenAPISnapshotIsCurrent(t *testing.T) {
	want, err := OpenAPISpec()
	if err != nil {
		t.Fatalf("render OpenAPI: %v", err)
	}
	got, err := os.ReadFile("../../api/openapi.json")
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	if !bytes.Equal(bytes.TrimSpace(got), bytes.TrimSpace(want)) {
		t.Fatal("api/openapi.json is stale; run `make openapi` and review the diff")
	}
}
