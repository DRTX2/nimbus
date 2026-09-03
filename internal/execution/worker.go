package execution

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/david/nimbus/internal/platform/observability"
	"github.com/david/nimbus/internal/store"
	"github.com/david/nimbus/internal/workflow"
)

const maxResponseBytes = 64 << 10

type Worker struct {
	store         *store.Store
	logger        *slog.Logger
	metrics       *observability.Metrics
	client        *http.Client
	count         int
	pollInterval  time.Duration
	leaseDuration time.Duration
}

func NewWorker(store *store.Store, logger *slog.Logger, metrics *observability.Metrics, count int, pollInterval, leaseDuration time.Duration, allowedHosts []string) *Worker {
	return &Worker{
		store: store, logger: logger, metrics: metrics, count: count,
		pollInterval: pollInterval, leaseDuration: leaseDuration,
		client: secureHTTPClient(allowedHosts),
	}
}

func (w *Worker) Run(ctx context.Context) {
	var group sync.WaitGroup
	for index := 0; index < w.count; index++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			w.loop(ctx, workerID(index))
		}(index)
	}
	group.Wait()
}

func (w *Worker) loop(ctx context.Context, owner string) {
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()
	for {
		worked, err := w.runOne(ctx, owner)
		if err != nil && !errors.Is(err, context.Canceled) {
			w.logger.Error("worker iteration failed", "worker_id", owner, "error", err)
		}
		if worked {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w *Worker) runOne(ctx context.Context, owner string) (bool, error) {
	job, found, err := w.store.ClaimStep(ctx, owner, w.leaseDuration)
	if err != nil || !found {
		return false, err
	}
	started := time.Now()
	output, retryable, err := w.execute(ctx, job)
	w.metrics.StepDuration.WithLabelValues(job.StepType).Observe(time.Since(started).Seconds())
	if err == nil {
		w.metrics.StepsCompleted.WithLabelValues(job.StepType, "succeeded").Inc()
		if err := w.store.CompleteStep(ctx, job, output); err != nil {
			return true, fmt.Errorf("complete job %s: %w", job.ID, err)
		}
		w.logger.Info("step succeeded", "execution_id", job.ExecutionID, "step_id", job.StepID, "attempt", job.Attempt)
		return true, nil
	}

	w.metrics.StepsCompleted.WithLabelValues(job.StepType, "failed").Inc()
	message := safeError(err)
	var retryAt *time.Time
	if retryable && job.Attempt < job.MaxAttempts {
		delay := backoff(job.Attempt, time.Duration(job.InitialDelaySeconds)*time.Second, time.Duration(job.MaxDelaySeconds)*time.Second)
		next := time.Now().Add(delay)
		retryAt = &next
	}
	if persistErr := w.store.FailStep(ctx, job, message, retryAt); persistErr != nil {
		return true, fmt.Errorf("persist job %s failure: %w", job.ID, persistErr)
	}
	w.logger.Warn("step failed", "execution_id", job.ExecutionID, "step_id", job.StepID,
		"attempt", job.Attempt, "retryable", retryAt != nil, "error", message)
	return true, nil
}

func (w *Worker) execute(parent context.Context, job store.Job) (json.RawMessage, bool, error) {
	ctx, cancel := context.WithTimeout(parent, time.Duration(job.TimeoutSeconds)*time.Second)
	defer cancel()

	switch job.StepType {
	case "noop":
		return json.RawMessage(`{"result":"noop"}`), false, nil
	case "http":
		var config workflow.HTTPRequest
		if err := json.Unmarshal(job.Config, &config); err != nil {
			return nil, false, fmt.Errorf("decode HTTP step: %w", err)
		}
		body := []byte(config.Body)
		if config.BodyMode == "event" {
			body = job.EventPayload
		}
		request, err := http.NewRequestWithContext(ctx, config.Method, config.URL, bytes.NewReader(body))
		if err != nil {
			return nil, false, fmt.Errorf("build HTTP request: %w", err)
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Idempotency-Key", job.ExecutionID+":"+job.StepID)
		response, err := w.client.Do(request)
		if err != nil {
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return nil, true, errors.New("HTTP request timed out")
			}
			if errors.Is(ctx.Err(), context.Canceled) {
				return nil, true, errors.New("HTTP request was cancelled")
			}
			return nil, true, errors.New("HTTP transport failed")
		}
		defer response.Body.Close()
		if _, err := io.Copy(io.Discard, io.LimitReader(response.Body, maxResponseBytes+1)); err != nil {
			return nil, true, fmt.Errorf("read HTTP response: %w", err)
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			retryable := response.StatusCode == http.StatusRequestTimeout || response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500
			return nil, retryable, fmt.Errorf("HTTP destination returned status %d", response.StatusCode)
		}
		output, _ := json.Marshal(map[string]any{"statusCode": response.StatusCode})
		return output, false, nil
	default:
		return nil, false, fmt.Errorf("unsupported step type %q", job.StepType)
	}
}

func backoff(attempt int, initial, maximum time.Duration) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := float64(initial) * math.Pow(2, float64(attempt-1))
	if delay > float64(maximum) {
		return maximum
	}
	return time.Duration(delay)
}

func workerID(index int) string {
	random := make([]byte, 4)
	if _, err := rand.Read(random); err != nil {
		return "worker-" + strconv.Itoa(index)
	}
	return "worker-" + strconv.Itoa(index) + "-" + hex.EncodeToString(random)
}

func safeError(err error) string {
	message := err.Error()
	if len(message) > 500 {
		return message[:500]
	}
	return message
}

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
		return nil, errors.New("destination resolves only to blocked private or local addresses")
	}
	return &http.Client{
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func forbiddenIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	return !ip.IsValid() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified()
}
