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
	"github.com/david/nimbus/internal/platform/observability"
	"github.com/david/nimbus/internal/store"
	"github.com/david/nimbus/internal/workflow"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

type principalContextKey struct{}

type API struct {
	store   *store.Store
	metrics *observability.Metrics
	logger  *slog.Logger
}

func New(store *store.Store, metrics *observability.Metrics, logger *slog.Logger) http.Handler {
	router := chi.NewRouter()
	router.Use(middleware.RequestID, middleware.Recoverer)
	router.Use(requestLogger(logger))
	router.Use(maxBodyBytes(1 << 20))
	router.Use(authenticate(store, logger))
	router.Handle("/metrics", metrics.Handler())

	config := huma.DefaultConfig("Nimbus Event Automation API", "0.1.0")
	config.Info.Description = "Reliable, tenant-scoped event workflow execution with at-least-once semantics."
	config.DocsRenderer = huma.DocsRendererScalar
	config.DocsRendererConfig = map[string]any{"theme": "deepSpace", "hideModels": false}
	api := humachi.New(router, config)
	api.OpenAPI().Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"bearerAuth": {
			Type:        "http",
			Scheme:      "bearer",
			Description: "Tenant-scoped Nimbus API token. The local development token is documented in the README.",
		},
	}
	handler := &API{store: store, metrics: metrics, logger: logger}
	handler.register(api)

	return otelhttp.NewHandler(router, "nimbus.http")
}

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

	createWorkflowOperation := securedOperation("createWorkflow", http.MethodPost, "/v1/workflows", "Workflows")
	createWorkflowOperation.DefaultStatus = http.StatusCreated
	huma.Register(api, createWorkflowOperation, a.createWorkflow)
	huma.Register(api, securedOperation("listWorkflows", http.MethodGet, "/v1/workflows", "Workflows"), a.listWorkflows)
	createVersion := securedOperation("createWorkflowVersion", http.MethodPost, "/v1/workflows/{workflowId}/versions", "Workflows")
	createVersion.DefaultStatus = http.StatusCreated
	huma.Register(api, createVersion, a.createWorkflowVersion)
	huma.Register(api, securedOperation("activateWorkflowVersion", http.MethodPost, "/v1/workflows/{workflowId}/versions/{version}/activations", "Workflows"), a.activateWorkflowVersion)
	ingestEventOperation := securedOperation("ingestEvent", http.MethodPost, "/v1/workflows/{workflowId}/events", "Events")
	ingestEventOperation.DefaultStatus = http.StatusAccepted
	huma.Register(api, ingestEventOperation, a.ingestEvent)
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

type healthBody struct {
	Status string `json:"status" example:"ok"`
}

type healthOutput struct {
	Body healthBody
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
	input.Body.Name = strings.TrimSpace(input.Body.Name)
	if input.Body.Name == "" {
		return nil, huma.Error400BadRequest("workflow name is required")
	}
	if err := input.Body.Definition.NormalizeAndValidate(); err != nil {
		return nil, huma.Error400BadRequest("invalid workflow", err)
	}
	actor, err := requirePermission(ctx, "workflow:write")
	if err != nil {
		return nil, err
	}
	created, err := a.store.CreateWorkflow(ctx, actor, input.Body.Name, input.Body.ApprovalRequired, input.Body.Definition)
	if err != nil {
		a.logger.Error("create workflow failed", "error", err)
		return nil, huma.Error500InternalServerError("could not create workflow")
	}
	return &workflowOutput{Body: created}, nil
}

type workflowsOutput struct {
	Body struct {
		Items []store.Workflow `json:"items"`
	}
}

func (a *API) listWorkflows(ctx context.Context, _ *struct{}) (*workflowsOutput, error) {
	actor, err := requirePermission(ctx, "workflow:read")
	if err != nil {
		return nil, err
	}
	items, err := a.store.ListWorkflows(ctx, actor.TenantID)
	if err != nil {
		a.logger.Error("list workflows failed", "error", err)
		return nil, huma.Error500InternalServerError("could not list workflows")
	}
	output := &workflowsOutput{}
	output.Body.Items = items
	return output, nil
}

type createWorkflowVersionInput struct {
	WorkflowID string `path:"workflowId" format:"uuid"`
	Body       workflow.Definition
}

func (a *API) createWorkflowVersion(ctx context.Context, input *createWorkflowVersionInput) (*workflowOutput, error) {
	actor, err := requirePermission(ctx, "workflow:write")
	if err != nil {
		return nil, err
	}
	if err := input.Body.NormalizeAndValidate(); err != nil {
		return nil, huma.Error400BadRequest("invalid workflow", err)
	}
	result, err := a.store.CreateWorkflowVersion(ctx, actor, input.WorkflowID, input.Body)
	if err != nil {
		return nil, huma.Error500InternalServerError("could not create workflow version")
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

func (a *API) activateWorkflowVersion(ctx context.Context, input *activateWorkflowVersionInput) (*statusOutput, error) {
	actor, err := requirePermission(ctx, "workflow:write")
	if err != nil {
		return nil, err
	}
	err = a.store.ActivateWorkflowVersion(ctx, actor, input.WorkflowID, input.Version)
	if errors.Is(err, store.ErrNotFound) {
		return nil, huma.Error404NotFound("workflow version was not found")
	}
	if err != nil {
		return nil, huma.Error500InternalServerError("workflow version could not be activated")
	}
	out := &statusOutput{}
	out.Body.Status = "active"
	return out, nil
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
	if strings.TrimSpace(input.IdempotencyKey) == "" {
		return nil, huma.Error400BadRequest("Idempotency-Key is required")
	}
	payload, err := json.Marshal(input.Body.Payload)
	if err != nil {
		return nil, huma.Error400BadRequest("payload is not valid JSON", err)
	}
	actor, permissionErr := requirePermission(ctx, "event:write")
	if permissionErr != nil {
		return nil, permissionErr
	}
	result, err := a.store.IngestEvent(ctx, actor, input.WorkflowID, input.IdempotencyKey, input.Body.Type, payload)
	if errors.Is(err, store.ErrNotFound) {
		return nil, huma.Error404NotFound("workflow was not found or is inactive")
	}
	if errors.Is(err, store.ErrEventTypeMismatch) {
		return nil, huma.Error400BadRequest("event type does not match the workflow trigger")
	}
	if err != nil {
		a.logger.Error("event ingestion failed", "error", err)
		return nil, huma.Error500InternalServerError("event could not be accepted")
	}
	if result.Duplicate {
		a.metrics.EventsDuplicate.Inc()
	} else {
		a.metrics.EventsAccepted.Inc()
	}
	return &ingestEventOutput{Status: http.StatusAccepted, Body: result}, nil
}

type executionInput struct {
	ExecutionID string `path:"executionId" format:"uuid"`
}

type executionOutput struct {
	Body store.Execution
}

func (a *API) getExecution(ctx context.Context, input *executionInput) (*executionOutput, error) {
	actor, err := requirePermission(ctx, "execution:read")
	if err != nil {
		return nil, err
	}
	result, err := a.store.GetExecution(ctx, actor.TenantID, input.ExecutionID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, huma.Error404NotFound("execution was not found")
	}
	if err != nil {
		a.logger.Error("load execution failed", "error", err)
		return nil, huma.Error500InternalServerError("could not load execution")
	}
	return &executionOutput{Body: result}, nil
}

type analyticsOutput struct {
	Body store.Analytics
}

func (a *API) analytics(ctx context.Context, _ *struct{}) (*analyticsOutput, error) {
	actor, err := requirePermission(ctx, "analytics:read")
	if err != nil {
		return nil, err
	}
	result, err := a.store.Analytics(ctx, actor.TenantID)
	if err != nil {
		a.logger.Error("load analytics failed", "error", err)
		return nil, huma.Error500InternalServerError("could not load analytics")
	}
	return &analyticsOutput{Body: result}, nil
}

func authenticate(repository *store.Store, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			if !strings.HasPrefix(request.URL.Path, "/v1/") {
				next.ServeHTTP(response, request)
				return
			}
			token, validAuthorization := bearerToken(request.Header.Get("Authorization"))
			if !validAuthorization {
				response.Header().Set("Content-Type", "application/problem+json")
				response.WriteHeader(http.StatusUnauthorized)
				_, _ = response.Write([]byte(`{"title":"Unauthorized","status":401,"detail":"a valid bearer token is required"}`))
				return
			}
			principal, capability, err := repository.AuthenticateAPIKey(request.Context(), token)
			if err != nil {
				if err != nil && !errors.Is(err, store.ErrForbidden) {
					logger.Error("authentication failed", "error", err)
				}
				response.Header().Set("Content-Type", "application/problem+json")
				response.WriteHeader(http.StatusUnauthorized)
				_, _ = response.Write([]byte(`{"title":"Unauthorized","status":401,"detail":"a valid bearer token is required"}`))
				return
			}
			ctx := context.WithValue(request.Context(), principalContextKey{}, principal)
			ctx = store.WithTenantCapability(ctx, capability)
			next.ServeHTTP(response, request.WithContext(ctx))
		})
	}
}

func bearerToken(value string) (string, bool) {
	parts := strings.Fields(value)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
		return "", false
	}
	return parts[1], true
}

func principalFrom(ctx context.Context) store.Principal {
	principal, _ := ctx.Value(principalContextKey{}).(store.Principal)
	return principal
}

func tenantFrom(ctx context.Context) string { return principalFrom(ctx).TenantID }

func requirePermission(ctx context.Context, permission string) (store.Principal, error) {
	principal := principalFrom(ctx)
	if principal.ID == "" || !roleAllows(principal.Role, permission) {
		return store.Principal{}, huma.Error403Forbidden("the authenticated principal lacks the required permission")
	}
	return principal, nil
}

func roleAllows(role, permission string) bool {
	if role == "owner" {
		return true
	}
	allowed := map[string]map[string]bool{
		"operator": {"workflow:read": true, "workflow:write": true, "event:write": true, "execution:read": true, "analytics:read": true, "pipeline:read": true, "pipeline:write": true, "incident:read": true, "incident:ack": true, "approval:read": true},
		"approver": {"workflow:read": true, "execution:read": true, "incident:read": true, "approval:read": true, "approval:decide": true, "analytics:read": true},
		"viewer":   {"workflow:read": true, "execution:read": true, "analytics:read": true, "pipeline:read": true, "incident:read": true, "approval:read": true},
	}
	return allowed[role][permission]
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
			next.ServeHTTP(response, request)
			logger.Info("HTTP request", "method", request.Method, "path", request.URL.Path,
				"request_id", middleware.GetReqID(request.Context()), "duration_ms", time.Since(started).Milliseconds())
		})
	}
}
