package execution

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"testing"
	"time"

	"github.com/david/nimbus/internal/store"
)

func TestBackoff(t *testing.T) {
	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{attempt: 1, want: 2 * time.Second},
		{attempt: 2, want: 4 * time.Second},
		{attempt: 5, want: 30 * time.Second},
	}
	for _, test := range tests {
		if got := backoff(test.attempt, 2*time.Second, 30*time.Second); got != test.want {
			t.Fatalf("backoff(%d) = %s, want %s", test.attempt, got, test.want)
		}
	}
}

func TestForbiddenIP(t *testing.T) {
	tests := []struct {
		address string
		want    bool
	}{
		{address: "127.0.0.1", want: true},
		{address: "10.0.0.1", want: true},
		{address: "169.254.169.254", want: true},
		{address: "::1", want: true},
		{address: "::ffff:127.0.0.1", want: true},
		{address: "1.1.1.1", want: false},
	}
	for _, test := range tests {
		if got := forbiddenIP(netip.MustParseAddr(test.address)); got != test.want {
			t.Fatalf("forbiddenIP(%s) = %v, want %v", test.address, got, test.want)
		}
	}
}

func TestSecureHTTPClientIgnoresEnvironmentProxies(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://proxy.example:8080")
	t.Setenv("HTTP_PROXY", "http://proxy.example:8080")
	client := secureHTTPClient(nil)
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T", client.Transport)
	}
	if transport.Proxy != nil {
		request, err := http.NewRequest(http.MethodGet, "https://example.com", nil)
		if err != nil {
			t.Fatal(err)
		}
		proxy, _ := transport.Proxy(request)
		t.Fatalf("workflow transport uses environment proxy %v (env %q)", proxy, os.Getenv("HTTPS_PROXY"))
	}
}

func TestExecuteSignedWebhook(t *testing.T) {
	var gotSignature, gotTimestamp string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSignature = r.Header.Get("Nimbus-Signature")
		gotTimestamp = r.Header.Get("Nimbus-Timestamp")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	worker := NewWorker(nil, nil, nil, 1, 0, 0, nil, "secret123", 50)
	worker.client = server.Client() // bypass secureHTTPClient IP blocking

	config := fmt.Sprintf(`{"url": "%s", "method": "POST", "body": "test-body"}`, server.URL)
	job := store.Job{
		StepType:       "http",
		Config:         json.RawMessage(config),
		TimeoutSeconds: 5,
	}

	_, retryable, err := worker.execute(context.Background(), job)
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if retryable {
		t.Fatalf("execute should not be retryable")
	}

	if gotSignature == "" {
		t.Fatalf("expected Nimbus-Signature header")
	}
	mac := hmac.New(sha256.New, []byte("secret123"))
	mac.Write([]byte(gotTimestamp + "."))
	mac.Write([]byte("test-body"))
	expected := "t=" + gotTimestamp + ",v1=" + hex.EncodeToString(mac.Sum(nil))
	if gotSignature != expected {
		t.Fatalf("got signature %q, want %q", gotSignature, expected)
	}
}
