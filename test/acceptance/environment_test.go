//go:build acceptance

package acceptance

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/david/nimbus/internal/awsmonitor"
	"github.com/david/nimbus/internal/execution"
	"github.com/david/nimbus/internal/httpapi"
	"github.com/david/nimbus/internal/platform/database"
	"github.com/david/nimbus/internal/platform/observability"
	"github.com/david/nimbus/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	webhookSecret      = "acceptance-webhook-secret-0001"
	tenantIngestionCap = 30
	tenantAWSChecks    = 3
	oidcAudience       = "nimbus-acceptance"
	oidcTenantClaim    = "nimbus_tenant"
)

// environment is the system under test. In-process mode runs the real API,
// worker, and outbox publisher against PostgreSQL with the least-privilege
// runtime roles. Remote mode drives an already deployed instance over HTTP.
type environment struct {
	baseURL    string
	remote     bool
	remoteKey  string
	owner      *pgxpool.Pool
	ownerStore *store.Store
	receiver   *webhookReceiver
	idp        *identityProvider
	cloudwatch *scriptedCloudWatch
	scheduler  *awsmonitor.Scheduler
	shutdown   func()
}

func newEnvironment(ctx context.Context) (*environment, error) {
	if baseURL := strings.TrimRight(os.Getenv("NIMBUS_BASE_URL"), "/"); baseURL != "" {
		key := os.Getenv("NIMBUS_API_KEY")
		if key == "" {
			return nil, fmt.Errorf("NIMBUS_API_KEY is required with NIMBUS_BASE_URL")
		}
		return &environment{baseURL: baseURL, remote: true, remoteKey: key, shutdown: func() {}}, nil
	}
	ownerURL, apiURL, workerURL := os.Getenv("NIMBUS_TEST_DATABASE_URL"), os.Getenv("NIMBUS_TEST_API_DATABASE_URL"), os.Getenv("NIMBUS_TEST_WORKER_DATABASE_URL")
	if ownerURL == "" || apiURL == "" || workerURL == "" {
		return nil, fmt.Errorf("set NIMBUS_BASE_URL for remote runs, or NIMBUS_TEST_DATABASE_URL, NIMBUS_TEST_API_DATABASE_URL and NIMBUS_TEST_WORKER_DATABASE_URL for in-process runs")
	}
	owner, err := database.Open(ctx, ownerURL)
	if err != nil {
		return nil, err
	}
	if err := database.Migrate(ctx, owner); err != nil {
		owner.Close()
		return nil, err
	}
	apiPool, err := database.Open(ctx, apiURL)
	if err != nil {
		owner.Close()
		return nil, err
	}
	workerPool, err := database.Open(ctx, workerURL)
	if err != nil {
		owner.Close()
		apiPool.Close()
		return nil, err
	}
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	if os.Getenv("NIMBUS_ACCEPTANCE_LOGS") != "" {
		logger = slog.New(slog.NewJSONHandler(os.Stderr, nil))
	}

	idp, err := newIdentityProvider()
	if err != nil {
		return nil, err
	}
	runCtx, cancel := context.WithCancel(ctx)
	verifier, err := httpapi.NewOIDCVerifier(runCtx, httpapi.OIDCConfig{
		Issuer: idp.issuer(), Audience: oidcAudience, Algorithms: []string{"RS256"}, TenantClaim: oidcTenantClaim,
	})
	if err != nil {
		cancel()
		return nil, err
	}
	api := httptest.NewServer(httpapi.New(httpapi.Options{
		Store: store.New(apiPool), Metrics: observability.NewMetrics(false), Logger: logger,
		AWSCollector: awsmonitor.NewCollector(nil), OIDC: verifier, MaxIngestionRate: tenantIngestionCap,
		MaxAWSChecksPerHour: tenantAWSChecks,
	}))

	workerStore := store.New(workerPool)
	metrics := observability.NewMetrics(true)
	worker := execution.NewWorker(workerStore, logger, metrics,
		execution.DefaultConnectors([]string{"127.0.0.1"}, execution.NewSigner(webhookSecret)),
		execution.WorkerConfig{Count: 4, PollInterval: 50 * time.Millisecond, LeaseDuration: 45 * time.Second, MaxConcurrentSteps: 50})
	publisher := execution.NewOutboxPublisher(workerStore, logger, 50*time.Millisecond, 45*time.Second)
	// Scenarios drive the scheduler explicitly so windows stay deterministic.
	cloudwatch := &scriptedCloudWatch{calls: map[string]int{}}
	scheduler := awsmonitor.NewScheduler(workerStore, cloudwatch, logger, metrics, time.Minute, tenantAWSChecks)
	var background sync.WaitGroup
	background.Go(func() { worker.Run(runCtx) })
	background.Go(func() { publisher.Run(runCtx) })

	env := &environment{
		baseURL: api.URL, owner: owner, ownerStore: store.New(owner),
		receiver: newWebhookReceiver(), idp: idp, cloudwatch: cloudwatch, scheduler: scheduler,
	}
	env.shutdown = func() {
		cancel()
		background.Wait()
		api.Close()
		env.receiver.close()
		idp.close()
		apiPool.Close()
		workerPool.Close()
		owner.Close()
	}
	return env, nil
}

// provisionTenant creates an isolated tenant with an owner API key so each
// scenario starts from a clean, independent state.
func (e *environment) provisionTenant(ctx context.Context) (tenantID, ownerToken string, err error) {
	if e.remote {
		return "", e.remoteKey, nil
	}
	tenantID = randomUUID()
	ownerToken = "nimbus-acceptance-" + randomHex(16)
	if err := e.ownerStore.BootstrapTenant(ctx, tenantID, "Acceptance "+tenantID[:8]); err != nil {
		return "", "", err
	}
	if err := e.ownerStore.BootstrapOwner(ctx, tenantID, "Acceptance Owner", ownerToken); err != nil {
		return "", "", err
	}
	return tenantID, ownerToken, nil
}

func randomHex(bytes int) string {
	value := make([]byte, bytes)
	if _, err := rand.Read(value); err != nil {
		panic(err)
	}
	return hex.EncodeToString(value)
}

func randomUUID() string {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		panic(err)
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	h := hex.EncodeToString(value)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

// webhookReceiver is a scripted HTTP destination. Each hook replies with a
// configured status sequence and records every delivery.
type webhookReceiver struct {
	server     *httptest.Server
	mu         sync.Mutex
	scripts    map[string][]int
	deliveries map[string][]delivery
}

type delivery struct {
	header http.Header
	body   []byte
}

func newWebhookReceiver() *webhookReceiver {
	receiver := &webhookReceiver{scripts: map[string][]int{}, deliveries: map[string][]delivery{}}
	receiver.server = httptest.NewServer(http.HandlerFunc(receiver.serve))
	return receiver
}

func (r *webhookReceiver) serve(response http.ResponseWriter, request *http.Request) {
	name := strings.TrimPrefix(request.URL.Path, "/hooks/")
	body, _ := io.ReadAll(io.LimitReader(request.Body, 1<<20))
	r.mu.Lock()
	r.deliveries[name] = append(r.deliveries[name], delivery{header: request.Header.Clone(), body: body})
	status := http.StatusNoContent
	if script := r.scripts[name]; len(script) > 0 {
		status = script[0]
		if len(script) > 1 {
			r.scripts[name] = script[1:]
		}
	}
	r.mu.Unlock()
	response.WriteHeader(status)
}

func (r *webhookReceiver) url(name string) string { return r.server.URL + "/hooks/" + name }

func (r *webhookReceiver) script(name string, statuses []int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.scripts[name] = statuses
}

func (r *webhookReceiver) received(name string) []delivery {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]delivery(nil), r.deliveries[name]...)
}

func (r *webhookReceiver) close() { r.server.Close() }

// scriptedCloudWatch stands in for CloudWatch during scheduled checks. It
// returns the scenario's metric profile for the window that closed last.
type scriptedCloudWatch struct {
	mu      sync.Mutex
	profile map[string]int64
	calls   map[string]int
}

func (c *scriptedCloudWatch) report(profile map[string]int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.profile = profile
}

func (c *scriptedCloudWatch) callsFor(pipelineID string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls[pipelineID]
}

func (c *scriptedCloudWatch) Collect(_ context.Context, pipeline store.Pipeline) (store.Observation, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls[pipeline.ID]++
	end := time.Now().UTC().Truncate(5 * time.Minute)
	return store.Observation{
		PipelineID: pipeline.ID, WindowStartedAt: end.Add(-5 * time.Minute), WindowEndedAt: end,
		VisibleMessages: c.profile["visibleMessages"], InFlightMessages: c.profile["inFlightMessages"],
		OldestMessageSeconds: c.profile["oldestMessageSeconds"], DLQMessages: c.profile["dlqMessages"],
		LambdaInvocations: c.profile["lambdaInvocations"], LambdaErrors: c.profile["lambdaErrors"],
		LambdaThrottles: c.profile["lambdaThrottles"], Source: "cloudwatch",
	}, nil
}
