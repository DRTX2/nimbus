package execution

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"strings"
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
		{address: "100.64.0.1", want: true},
		{address: "0.1.2.3", want: true},
		{address: "::1", want: true},
		{address: "::ffff:127.0.0.1", want: true},
		{address: "fd00:ec2::254", want: true},
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

func httpJob(url string, timeout int) store.Job {
	return store.Job{
		StepType: "http", ExecutionID: "exec", StepID: "notify", TimeoutSeconds: timeout,
		Config: json.RawMessage(fmt.Sprintf(`{"url":%q,"method":"POST","body":"test-body","bodyMode":"static"}`, url)),
	}
}

// The connector error classification decides whether an external effect may be
// repeated, so every status class and transport failure is pinned here.
func TestHTTPConnectorErrorClassification(t *testing.T) {
	tests := []struct {
		status    int
		wantErr   bool
		retryable bool
	}{
		{status: http.StatusNoContent},
		{status: http.StatusBadRequest, wantErr: true},
		{status: http.StatusNotFound, wantErr: true},
		{status: http.StatusRequestTimeout, wantErr: true, retryable: true},
		{status: http.StatusTooManyRequests, wantErr: true, retryable: true},
		{status: http.StatusBadGateway, wantErr: true, retryable: true},
	}
	for _, test := range tests {
		t.Run(http.StatusText(test.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(test.status) }))
			defer server.Close()
			connector := &HTTPConnector{client: server.Client()}
			_, err := connector.Execute(context.Background(), httpJob(server.URL, 5))
			if (err != nil) != test.wantErr || IsRetryable(err) != test.retryable {
				t.Fatalf("status %d: err=%v retryable=%v", test.status, err, IsRetryable(err))
			}
		})
	}
}

func TestHTTPConnectorTimeoutIsRetryable(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { <-release }))
	defer server.Close()
	defer close(release)
	connector := &HTTPConnector{client: server.Client()}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := connector.Execute(ctx, httpJob(server.URL, 1))
	if err == nil || !IsRetryable(err) || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("timeout error = %v, retryable=%v", err, IsRetryable(err))
	}
}

func TestHTTPConnectorBlocksPrivateDestinations(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("request reached a loopback destination")
	}))
	defer server.Close()
	_, err := NewHTTPConnector(nil, nil).Execute(context.Background(), httpJob(server.URL, 5))
	if !errors.Is(err, errBlockedDestination) || IsRetryable(err) {
		t.Fatalf("private destination error = %v, retryable=%v", err, IsRetryable(err))
	}
}

func TestSignerSupportsRotation(t *testing.T) {
	var gotSignature, gotTimestamp string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSignature, gotTimestamp = r.Header.Get("Nimbus-Signature"), r.Header.Get("Nimbus-Timestamp")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	connector := &HTTPConnector{client: server.Client(), signer: NewSigner("current-secret", "previous-secret")}
	if _, err := connector.Execute(context.Background(), httpJob(server.URL, 5)); err != nil {
		t.Fatalf("execute: %v", err)
	}
	want := []string{"t=" + gotTimestamp}
	for _, secret := range []string{"current-secret", "previous-secret"} {
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(gotTimestamp + "."))
		mac.Write([]byte("test-body"))
		want = append(want, "v1="+hex.EncodeToString(mac.Sum(nil)))
	}
	if gotSignature != strings.Join(want, ",") {
		t.Fatalf("signature %q, want %q", gotSignature, strings.Join(want, ","))
	}
}

func TestUnknownStepTypeIsPermanent(t *testing.T) {
	_, err := Registry{}.execute(context.Background(), store.Job{StepType: "carrier-pigeon"})
	if err == nil || IsRetryable(err) {
		t.Fatalf("unknown connector error = %v", err)
	}
}
