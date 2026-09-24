package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/david/nimbus/internal/awsmonitor"
	"github.com/david/nimbus/internal/platform/observability"
	"github.com/david/nimbus/internal/store"
	"github.com/david/nimbus/internal/workflow"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// Version is the public API contract version reported in OpenAPI.
const Version = "0.2.0"

const maxRequestBodyBytes = 1 << 20

// Options wires the API to its collaborators. Optional fields may be nil.
type Options struct {
	Store            *store.Store
	Metrics          *observability.Metrics
	Logger           *slog.Logger
	AWSCollector     *awsmonitor.Collector
	OIDC             *OIDCVerifier
	MaxIngestionRate int
	// WebDir, when set, serves the built dashboard at /app/.
	WebDir string
}

type API struct {
	store            *store.Store
	metrics          *observability.Metrics
	logger           *slog.Logger
	awsCollector     *awsmonitor.Collector
	maxIngestionRate int
}

// New builds the public HTTP handler: middleware, OpenAPI operations, Scalar
// documentation, metrics, and the optional dashboard.
func New(options Options) http.Handler {
	router := chi.NewRouter()
	router.Use(middleware.RequestID, middleware.Recoverer, securityHeaders)
	router.Use(requestLogger(options.Logger))
	router.Use(maxBodyBytes(maxRequestBodyBytes))
	router.Use(newAuthenticator(options.Store, options.OIDC, options.Logger).middleware)
	router.Handle("/metrics", options.Metrics.Handler())
	if options.WebDir != "" {
		mountDashboard(router, options.WebDir)
	}

	api := humachi.New(router, apiConfig())
	handler := &API{
		store: options.Store, metrics: options.Metrics, logger: options.Logger,
		awsCollector: options.AWSCollector, maxIngestionRate: options.MaxIngestionRate,
	}
	handler.register(api)
	return otelhttp.NewHandler(router, "nimbus.http")
}

// OpenAPISpec renders the contract without any runtime dependencies. It is
// used to generate the checked-in snapshot consumed by the dashboard.
func OpenAPISpec() ([]byte, error) {
	api := humachi.New(chi.NewRouter(), apiConfig())
	(&API{}).register(api)
	return json.MarshalIndent(api.OpenAPI(), "", "  ")
}

func apiConfig() huma.Config {
	config := huma.DefaultConfig("Nimbus Async Pipeline Guardian API", Version)
	config.Info.Description = "Explainable AWS asynchronous-pipeline incidents backed by reliable, tenant-scoped workflow execution."
	config.DocsRenderer = huma.DocsRendererScalar
	config.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"bearerAuth": {
			Type:        "http",
			Scheme:      "bearer",
			Description: "Tenant-scoped Nimbus API key or OIDC access token.",
		},
	}
	return config
}

// NewOperational serves health and metrics for worker-only processes.
func NewOperational(repository *store.Store, metrics *observability.Metrics, logger *slog.Logger) http.Handler {
	router := chi.NewRouter()
	router.Get("/health/live", func(response http.ResponseWriter, _ *http.Request) { response.WriteHeader(http.StatusNoContent) })
	router.Get("/health/ready", func(response http.ResponseWriter, request *http.Request) {
		ctx, cancel := context.WithTimeout(request.Context(), 2*time.Second)
		defer cancel()
		if err := repository.Ping(ctx); err != nil {
			http.Error(response, "not ready", http.StatusServiceUnavailable)
			return
		}
		response.WriteHeader(http.StatusNoContent)
	})
	router.Handle("/metrics", metrics.Handler())
	return requestLogger(logger)(router)
}

func (a *API) register(api huma.API) {
	huma.Register(api, huma.Operation{OperationID: "liveness", Method: http.MethodGet, Path: "/health/live", Tags: []string{"System"}},
		func(context.Context, *struct{}) (*healthOutput, error) {
			return &healthOutput{Body: healthBody{Status: "ok"}}, nil
		})
	huma.Register(api, huma.Operation{OperationID: "readiness", Method: http.MethodGet, Path: "/health/ready", Tags: []string{"System"}},
		func(ctx context.Context, _ *struct{}) (*healthOutput, error) {
			checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			if err := a.store.Ping(checkCtx); err != nil {
				a.logger.Error("readiness check failed", "error", err)
				return nil, huma.Error503ServiceUnavailable("database is not ready")
			}
			return &healthOutput{Body: healthBody{Status: "ok"}}, nil
		})

	huma.Register(api, securedOperation("getCurrentPrincipal", http.MethodGet, "/v1/me", "Identity"), a.me)
	huma.Register(api, withStatus(securedOperation("createWorkflow", http.MethodPost, "/v1/workflows", "Workflows"), http.StatusCreated), a.createWorkflow)
	huma.Register(api, securedOperation("listWorkflows", http.MethodGet, "/v1/workflows", "Workflows"), a.listWorkflows)
	huma.Register(api, securedOperation("getWorkflow", http.MethodGet, "/v1/workflows/{workflowId}", "Workflows"), a.getWorkflow)
	huma.Register(api, withStatus(securedOperation("createWorkflowVersion", http.MethodPost, "/v1/workflows/{workflowId}/versions", "Workflows"), http.StatusCreated), a.createWorkflowVersion)
	huma.Register(api, securedOperation("activateWorkflowVersion", http.MethodPost, "/v1/workflows/{workflowId}/versions/{version}/activations", "Workflows"), a.activateWorkflowVersion)
	huma.Register(api, withStatus(securedOperation("ingestEvent", http.MethodPost, "/v1/workflows/{workflowId}/events", "Events"), http.StatusAccepted), a.ingestEvent)
	huma.Register(api, securedOperation("listExecutions", http.MethodGet, "/v1/executions", "Executions"), a.listExecutions)
	huma.Register(api, securedOperation("getExecution", http.MethodGet, "/v1/executions/{executionId}", "Executions"), a.getExecution)
	huma.Register(api, securedOperation("analyticsOverview", http.MethodGet, "/v1/analytics/overview", "Analytics"), a.analytics)
	a.registerGovernance(api)
	a.registerIncidents(api)
}

func securedOperation(id, method, path, tag string) huma.Operation {
	return huma.Operation{
		OperationID: id, Method: method, Path: path, Tags: []string{tag},
		Security: []map[string][]string{{"bearerAuth": {}}},
	}
}

func withStatus(operation huma.Operation, status int) huma.Operation {
	operation.DefaultStatus = status
	return operation
}

// internalError logs the cause server-side and returns a generic problem so
// storage details never reach clients.
func (a *API) internalError(message string, err error) error {
	a.logger.Error(message, "error", err)
	return huma.Error500InternalServerError(message)
}

type healthBody struct {
	Status string `json:"status" example:"ok"`
}

type healthOutput struct {
	Body healthBody
}

type meOutput struct {
	Body struct {
		store.Principal
		Permissions []Permission `json:"permissions"`
	}
}

func (a *API) me(ctx context.Context, _ *struct{}) (*meOutput, error) {
	principal := principalFrom(ctx)
	if principal.ID == "" {
		return nil, huma.Error401Unauthorized("authentication is required")
	}
	out := &meOutput{}
	out.Body.Principal = principal
	out.Body.Permissions = permissionsFor(principal.Role)
	return out, nil
}

type createWorkflowInput struct {
	Body struct {
		Name             string              `json:"name" minLength:"1" maxLength:"120" example:"Order notification"`
		Definition       workflow.Definition `json:"definition"`
		ApprovalRequired bool                `json:"approvalRequired,omitempty"`
	}
}

type workflowOutput struct {
	Body store.Workflow
}

func (a *API) createWorkflow(ctx context.Context, input *createWorkflowInput) (*workflowOutput, error) {
	actor, err := requirePermission(ctx, PermWorkflowWrite)
	if err != nil {
		return nil, err
	}
	input.Body.Name = strings.TrimSpace(input.Body.Name)
	if input.Body.Name == "" {
		return nil, huma.Error400BadRequest("workflow name is required")
	}
	if err := input.Body.Definition.NormalizeAndValidate(); err != nil {
		return nil, huma.Error400BadRequest("invalid workflow", err)
	}
	created, err := a.store.CreateWorkflow(ctx, actor, input.Body.Name, input.Body.ApprovalRequired, input.Body.Definition)
	if err != nil {
		return nil, a.internalError("could not create workflow", err)
	}
	return &workflowOutput{Body: created}, nil
}

type workflowsOutput struct {
	Body struct {
		Items []store.Workflow `json:"items"`
	}
}

func (a *API) listWorkflows(ctx context.Context, _ *struct{}) (*workflowsOutput, error) {
	actor, err := requirePermission(ctx, PermWorkflowRead)
	if err != nil {
		return nil, err
	}
	items, err := a.store.ListWorkflows(ctx, actor.TenantID)
	if err != nil {
		return nil, a.internalError("could not list workflows", err)
	}
	output := &workflowsOutput{}
	output.Body.Items = items
	return output, nil
}

type workflowPathInput struct {
	WorkflowID string `path:"workflowId" format:"uuid"`
}

type workflowDetailOutput struct {
	Body store.WorkflowDetail
}

func (a *API) getWorkflow(ctx context.Context, input *workflowPathInput) (*workflowDetailOutput, error) {
	actor, err := requirePermission(ctx, PermWorkflowRead)
	if err != nil {
		return nil, err
	}
	result, err := a.store.GetWorkflow(ctx, actor.TenantID, input.WorkflowID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, huma.Error404NotFound("workflow was not found")
	}
	if err != nil {
		return nil, a.internalError("could not load workflow", err)
	}
	return &workflowDetailOutput{Body: result}, nil
}

type createWorkflowVersionInput struct {
	WorkflowID string `path:"workflowId" format:"uuid"`
	Body       workflow.Definition
}

func (a *API) createWorkflowVersion(ctx context.Context, input *createWorkflowVersionInput) (*workflowOutput, error) {
	actor, err := requirePermission(ctx, PermWorkflowWrite)
	if err != nil {
		return nil, err
	}
	if err := input.Body.NormalizeAndValidate(); err != nil {
		return nil, huma.Error400BadRequest("invalid workflow", err)
	}
	result, err := a.store.CreateWorkflowVersion(ctx, actor, input.WorkflowID, input.Body)
	if errors.Is(err, store.ErrNotFound) {
		return nil, huma.Error404NotFound("workflow was not found")
	}
	if err != nil {
		return nil, a.internalError("could not create workflow version", err)
	}
	return &workflowOutput{Body: result}, nil
}

type activateWorkflowVersionInput struct {
	WorkflowID string `path:"workflowId" format:"uuid"`
	Version    int    `path:"version" minimum:"1"`
}

type statusOutput struct {
	Body struct {
		Status string `json:"status"`
	}
}

func newStatusOutput(status string) *statusOutput {
	out := &statusOutput{}
	out.Body.Status = status
	return out
}

func (a *API) activateWorkflowVersion(ctx context.Context, input *activateWorkflowVersionInput) (*statusOutput, error) {
	actor, err := requirePermission(ctx, PermWorkflowWrite)
	if err != nil {
		return nil, err
	}
	err = a.store.ActivateWorkflowVersion(ctx, actor, input.WorkflowID, input.Version)
	if errors.Is(err, store.ErrNotFound) {
		return nil, huma.Error404NotFound("workflow version was not found")
	}
	if err != nil {
		return nil, a.internalError("workflow version could not be activated", err)
	}
	return newStatusOutput("active"), nil
}

type ingestEventInput struct {
	WorkflowID     string `path:"workflowId" format:"uuid"`
	IdempotencyKey string `header:"Idempotency-Key" required:"true" minLength:"1" maxLength:"200"`
	Body           struct {
		Type    string         `json:"type" minLength:"1" maxLength:"100" example:"order.created"`
		Payload map[string]any `json:"payload"`
	}
}

type ingestEventOutput struct {
	Status int
	Body   store.IngestResult
}

func (a *API) ingestEvent(ctx context.Context, input *ingestEventInput) (*ingestEventOutput, error) {
	actor, err := requirePermission(ctx, PermEventWrite)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.IdempotencyKey) == "" {
		return nil, huma.Error400BadRequest("Idempotency-Key is required")
	}
	if input.Body.Payload == nil {
		input.Body.Payload = map[string]any{}
	}
	payload, err := json.Marshal(input.Body.Payload)
	if err != nil {
		return nil, huma.Error400BadRequest("payload is not valid JSON", err)
	}
	result, err := a.store.IngestEvent(ctx, actor, input.WorkflowID, input.IdempotencyKey, input.Body.Type, payload, a.maxIngestionRate)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return nil, huma.Error404NotFound("workflow was not found or is inactive")
	case errors.Is(err, store.ErrEventTypeMismatch):
		return nil, huma.Error400BadRequest("event type does not match the workflow trigger")
	case errors.Is(err, store.ErrRateLimitExceeded):
		return nil, huma.Error429TooManyRequests("tenant ingestion rate limit exceeded")
	case err != nil:
		return nil, a.internalError("event could not be accepted", err)
	}
	if result.Duplicate {
		a.metrics.EventsDuplicate.Inc()
	} else {
		a.metrics.EventsAccepted.Inc()
	}
	return &ingestEventOutput{Status: http.StatusAccepted, Body: result}, nil
}

type listExecutionsInput struct {
	Status     string `query:"status" enum:"awaiting_approval,queued,running,succeeded,failed,cancelled" required:"false"`
	WorkflowID string `query:"workflowId" format:"uuid" required:"false"`
	Limit      int    `query:"limit" minimum:"1" maximum:"100" default:"50"`
	Cursor     string `query:"cursor" maxLength:"256" doc:"Opaque cursor from a previous page's nextCursor."`
}

type executionsOutput struct {
	Body store.ExecutionPage
}

func (a *API) listExecutions(ctx context.Context, input *listExecutionsInput) (*executionsOutput, error) {
	actor, err := requirePermission(ctx, PermExecutionRead)
	if err != nil {
		return nil, err
	}
	page, err := a.store.ListExecutions(ctx, actor.TenantID,
		store.ExecutionFilter{Status: input.Status, WorkflowID: input.WorkflowID},
		store.Page{Limit: input.Limit, Cursor: input.Cursor})
	if errors.Is(err, store.ErrInvalidInput) {
		return nil, huma.Error400BadRequest("cursor is invalid")
	}
	if err != nil {
		return nil, a.internalError("could not list executions", err)
	}
	return &executionsOutput{Body: page}, nil
}

type executionInput struct {
	ExecutionID string `path:"executionId" format:"uuid"`
}

type executionOutput struct {
	Body store.Execution
}

func (a *API) getExecution(ctx context.Context, input *executionInput) (*executionOutput, error) {
	actor, err := requirePermission(ctx, PermExecutionRead)
	if err != nil {
		return nil, err
	}
	result, err := a.store.GetExecution(ctx, actor.TenantID, input.ExecutionID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, huma.Error404NotFound("execution was not found")
	}
	if err != nil {
		return nil, a.internalError("could not load execution", err)
	}
	return &executionOutput{Body: result}, nil
}

type analyticsOutput struct {
	Body store.Analytics
}

func (a *API) analytics(ctx context.Context, _ *struct{}) (*analyticsOutput, error) {
	actor, err := requirePermission(ctx, PermAnalyticsRead)
	if err != nil {
		return nil, err
	}
	result, err := a.store.Analytics(ctx, actor.TenantID)
	if err != nil {
		return nil, a.internalError("could not load analytics", err)
	}
	return &analyticsOutput{Body: result}, nil
}

func maxBodyBytes(limit int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			request.Body = http.MaxBytesReader(response, request.Body, limit)
			next.ServeHTTP(response, request)
		})
	}
}

func requestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			started := time.Now()
			recorder := middleware.NewWrapResponseWriter(response, request.ProtoMajor)
			next.ServeHTTP(recorder, request)
			logger.Info("HTTP request", "method", request.Method, "path", request.URL.Path, "status", recorder.Status(),
				"request_id", middleware.GetReqID(request.Context()), "duration_ms", time.Since(started).Milliseconds())
		})
	}
}
