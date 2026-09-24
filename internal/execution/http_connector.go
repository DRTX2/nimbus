package execution

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/david/nimbus/internal/store"
	"github.com/david/nimbus/internal/workflow"
)

const maxResponseBytes = 64 << 10

// Signer produces Nimbus-Signature headers. Several secrets may be active at
// once so receivers can rotate keys without dropping deliveries.
type Signer struct {
	secrets [][]byte
	now     func() time.Time
}

func NewSigner(secrets ...string) *Signer {
	signer := &Signer{now: time.Now}
	for _, secret := range secrets {
		if secret = strings.TrimSpace(secret); secret != "" {
			signer.secrets = append(signer.secrets, []byte(secret))
		}
	}
	return signer
}

func (s *Signer) enabled() bool { return s != nil && len(s.secrets) > 0 }

// Sign returns the header value `t=<unix>,v1=<hex>[,v1=<hex>...]` over
// `<unix>.<body>`. Receivers should reject timestamps outside a short window.
func (s *Signer) Sign(body []byte) (string, string) {
	timestamp := strconv.FormatInt(s.now().Unix(), 10)
	parts := []string{"t=" + timestamp}
	for _, secret := range s.secrets {
		mac := hmac.New(sha256.New, secret)
		mac.Write([]byte(timestamp + "."))
		mac.Write(body)
		parts = append(parts, "v1="+hex.EncodeToString(mac.Sum(nil)))
	}
	return strings.Join(parts, ","), timestamp
}

// HTTPConnector delivers bounded HTTP requests with SSRF protection.
type HTTPConnector struct {
	client *http.Client
	signer *Signer
}

func NewHTTPConnector(allowedHosts []string, signer *Signer) *HTTPConnector {
	return &HTTPConnector{client: secureHTTPClient(allowedHosts), signer: signer}
}

func (c *HTTPConnector) Execute(ctx context.Context, job store.Job) (json.RawMessage, error) {
	var config workflow.HTTPRequest
	if err := json.Unmarshal(job.Config, &config); err != nil {
		return nil, permanent(fmt.Errorf("decode HTTP step: %w", err))
	}
	body := []byte(config.Body)
	if config.BodyMode == "event" {
		body = job.EventPayload
	}
	request, err := http.NewRequestWithContext(ctx, config.Method, config.URL, bytes.NewReader(body))
	if err != nil {
		return nil, permanent(fmt.Errorf("build HTTP request: %w", err))
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "Nimbus-Webhook/1")
	request.Header.Set("Idempotency-Key", job.ExecutionID+":"+job.StepID)
	if c.signer.enabled() {
		signature, timestamp := c.signer.Sign(body)
		request.Header.Set("Nimbus-Signature", signature)
		request.Header.Set("Nimbus-Timestamp", timestamp)
	}
	response, err := c.client.Do(request)
	if err != nil {
		switch {
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			return nil, retryable(errors.New("HTTP request timed out"))
		case errors.Is(ctx.Err(), context.Canceled):
			return nil, retryable(errors.New("HTTP request was cancelled"))
		case errors.Is(err, errBlockedDestination):
			return nil, permanent(errBlockedDestination)
		default:
			return nil, retryable(errors.New("HTTP transport failed"))
		}
	}
	defer response.Body.Close()
	if _, err := io.Copy(io.Discard, io.LimitReader(response.Body, maxResponseBytes+1)); err != nil {
		return nil, retryable(fmt.Errorf("read HTTP response: %w", err))
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		failure := fmt.Errorf("HTTP destination returned status %d", response.StatusCode)
		if response.StatusCode == http.StatusRequestTimeout || response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500 {
			return nil, retryable(failure)
		}
		return nil, permanent(failure)
	}
	output, err := json.Marshal(map[string]any{"statusCode": response.StatusCode})
	if err != nil {
		return nil, permanent(fmt.Errorf("encode HTTP output: %w", err))
	}
	return output, nil
}

var errBlockedDestination = errors.New("destination resolves only to blocked private or local addresses")

func secureHTTPClient(allowedHosts []string) *http.Client {
	allowed := make(map[string]struct{}, len(allowedHosts))
	for _, host := range allowedHosts {
		allowed[strings.ToLower(host)] = struct{}{}
	}
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
	}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("parse destination: %w", err)
		}
		ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, fmt.Errorf("resolve destination: %w", err)
		}
		_, explicitlyAllowed := allowed[strings.ToLower(host)]
		for _, ip := range ips {
			if !explicitlyAllowed && forbiddenIP(ip) {
				continue
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		}
		return nil, errBlockedDestination
	}
	return &http.Client{
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// Non-public ranges beyond the netip predicates: "this network", carrier-grade
// NAT, and IETF protocol assignments.
var extraForbiddenPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
}

func forbiddenIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return true
	}
	for _, prefix := range extraForbiddenPrefixes {
		if prefix.Contains(ip) {
			return true
		}
	}
	return false
}
