package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/david/nimbus/internal/awsmonitor"
	"github.com/david/nimbus/internal/store"
)

func (a *API) registerIncidents(api huma.API) {
	create := securedOperation("createPipeline", http.MethodPost, "/v1/pipelines", "Reliability")
	create.DefaultStatus = http.StatusCreated
	huma.Register(api, create, a.createPipeline)
	huma.Register(api, securedOperation("listPipelines", http.MethodGet, "/v1/pipelines", "Reliability"), a.listPipelines)
	huma.Register(api, securedOperation("checkPipeline", http.MethodPost, "/v1/pipelines/{pipelineId}/checks", "Reliability"), a.checkPipeline)
	observe := securedOperation("recordObservation", http.MethodPost, "/v1/pipelines/{pipelineId}/observations", "Reliability")
	observe.DefaultStatus = http.StatusAccepted
	huma.Register(api, observe, a.recordObservation)
	huma.Register(api, securedOperation("listIncidents", http.MethodGet, "/v1/incidents", "Incidents"), a.listIncidents)
	huma.Register(api, securedOperation("acknowledgeIncident", http.MethodPost, "/v1/incidents/{incidentId}/acknowledgements", "Incidents"), a.acknowledgeIncident)
}

type createPipelineInput struct {
	Body struct {
		Name                string `json:"name" minLength:"1" maxLength:"120"`
		Mode                string `json:"mode" enum:"fixture,aws"`
		Region              string `json:"region" minLength:"3" maxLength:"32"`
		QueueARN            string `json:"queueArn" maxLength:"512"`
		LambdaARN           string `json:"lambdaArn" maxLength:"512"`
		DLQARN              string `json:"dlqArn" maxLength:"512"`
		RoleARN             string `json:"roleArn,omitempty" maxLength:"512"`
		ExternalIDEnv       string `json:"externalIdEnv,omitempty" maxLength:"128" pattern:"^[A-Z][A-Z0-9_]*$"`
		IncidentWorkflowID  string `json:"incidentWorkflowId,omitempty" format:"uuid"`
		ConsecutiveWindows  int    `json:"consecutiveWindows,omitempty" minimum:"1" maximum:"10"`
		BacklogThreshold    int64  `json:"backlogThreshold,omitempty" minimum:"1"`
		AgeThresholdSeconds int64  `json:"ageThresholdSeconds,omitempty" minimum:"1"`
	}
}
type pipelineOutput struct{ Body store.Pipeline }

func (a *API) createPipeline(ctx context.Context, input *createPipelineInput) (*pipelineOutput, error) {
	actor, err := requirePermission(ctx, "pipeline:write")
	if err != nil {
		return nil, err
	}
	pipeline := store.Pipeline{Name: strings.TrimSpace(input.Body.Name), Mode: input.Body.Mode, Region: input.Body.Region, QueueARN: input.Body.QueueARN, LambdaARN: input.Body.LambdaARN, DLQARN: input.Body.DLQARN, RoleARN: input.Body.RoleARN, ExternalIDEnv: input.Body.ExternalIDEnv, IncidentWorkflowID: input.Body.IncidentWorkflowID, ConsecutiveWindows: input.Body.ConsecutiveWindows, BacklogThreshold: input.Body.BacklogThreshold, AgeThresholdSeconds: input.Body.AgeThresholdSeconds}
	result, err := a.store.CreatePipeline(ctx, actor, pipeline)
	if err != nil {
		return nil, huma.Error400BadRequest("pipeline could not be created", err)
	}
	return &pipelineOutput{Body: result}, nil
}

type pipelinesOutput struct {
	Body struct {
		Items []store.Pipeline `json:"items"`
	}
}

func (a *API) listPipelines(ctx context.Context, _ *struct{}) (*pipelinesOutput, error) {
	actor, err := requirePermission(ctx, "pipeline:read")
	if err != nil {
		return nil, err
	}
	items, err := a.store.ListPipelines(ctx, actor.TenantID)
	if err != nil {
		return nil, huma.Error500InternalServerError("could not list pipelines")
	}
	out := &pipelinesOutput{}
	out.Body.Items = items
	return out, nil
}

type checkPipelineInput struct {
	PipelineID string `path:"pipelineId" format:"uuid"`
}

func (a *API) checkPipeline(ctx context.Context, input *checkPipelineInput) (*observationOutput, error) {
	actor, err := requirePermission(ctx, "pipeline:write")
	if err != nil {
		return nil, err
	}
	pipeline, err := a.store.GetPipeline(ctx, actor.TenantID, input.PipelineID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, huma.Error404NotFound("pipeline was not found")
	}
	if err != nil {
		return nil, huma.Error500InternalServerError("pipeline could not be loaded")
	}
	checkCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	observation, err := awsmonitor.Collect(checkCtx, pipeline)
	if err != nil {
		a.logger.Warn("AWS pipeline check failed", "pipeline_id", pipeline.ID, "error", err)
		return nil, huma.Error502BadGateway("CloudWatch metrics could not be collected")
	}
	incidents, err := a.store.RecordObservation(ctx, actor, observation)
	if err != nil {
		return nil, huma.Error500InternalServerError("observation could not be evaluated")
	}
	out := &observationOutput{}
	out.Body.Incidents = incidents
	return out, nil
}

type observationInput struct {
	PipelineID string `path:"pipelineId" format:"uuid"`
	Body       struct {
		WindowStartedAt      time.Time `json:"windowStartedAt"`
		WindowEndedAt        time.Time `json:"windowEndedAt"`
		VisibleMessages      int64     `json:"visibleMessages" minimum:"0"`
		InFlightMessages     int64     `json:"inFlightMessages" minimum:"0"`
		OldestMessageSeconds int64     `json:"oldestMessageSeconds" minimum:"0"`
		DLQMessages          int64     `json:"dlqMessages" minimum:"0"`
		LambdaInvocations    int64     `json:"lambdaInvocations" minimum:"0"`
		LambdaErrors         int64     `json:"lambdaErrors" minimum:"0"`
		LambdaThrottles      int64     `json:"lambdaThrottles" minimum:"0"`
	}
}
type observationOutput struct {
	Body struct {
		Incidents []store.Incident `json:"incidents"`
	}
}

func (a *API) recordObservation(ctx context.Context, input *observationInput) (*observationOutput, error) {
	actor, err := requirePermission(ctx, "pipeline:write")
	if err != nil {
		return nil, err
	}
	if !input.Body.WindowEndedAt.After(input.Body.WindowStartedAt) {
		return nil, huma.Error400BadRequest("windowEndedAt must be after windowStartedAt")
	}
	observation := store.Observation{PipelineID: input.PipelineID, WindowStartedAt: input.Body.WindowStartedAt, WindowEndedAt: input.Body.WindowEndedAt, VisibleMessages: input.Body.VisibleMessages, InFlightMessages: input.Body.InFlightMessages, OldestMessageSeconds: input.Body.OldestMessageSeconds, DLQMessages: input.Body.DLQMessages, LambdaInvocations: input.Body.LambdaInvocations, LambdaErrors: input.Body.LambdaErrors, LambdaThrottles: input.Body.LambdaThrottles, Source: "fixture"}
	incidents, err := a.store.RecordObservation(ctx, actor, observation)
	if errors.Is(err, store.ErrNotFound) {
		return nil, huma.Error404NotFound("pipeline was not found")
	}
	if errors.Is(err, store.ErrForbidden) {
		return nil, huma.Error403Forbidden("fixture observations are disabled for AWS pipelines")
	}
	if err != nil {
		return nil, huma.Error500InternalServerError("observation could not be evaluated")
	}
	out := &observationOutput{}
	out.Body.Incidents = incidents
	return out, nil
}

type incidentsOutput struct {
	Body struct {
		Items []store.Incident `json:"items"`
	}
}

func (a *API) listIncidents(ctx context.Context, _ *struct{}) (*incidentsOutput, error) {
	actor, err := requirePermission(ctx, "incident:read")
	if err != nil {
		return nil, err
	}
	items, err := a.store.ListIncidents(ctx, actor.TenantID)
	if err != nil {
		return nil, huma.Error500InternalServerError("could not list incidents")
	}
	out := &incidentsOutput{}
	out.Body.Items = items
	return out, nil
}

type acknowledgeInput struct {
	IncidentID string `path:"incidentId" format:"uuid"`
}
type acknowledgeOutput struct {
	Body struct {
		Status string `json:"status"`
	}
}

func (a *API) acknowledgeIncident(ctx context.Context, input *acknowledgeInput) (*acknowledgeOutput, error) {
	actor, err := requirePermission(ctx, "incident:ack")
	if err != nil {
		return nil, err
	}
	if err := a.store.AcknowledgeIncident(ctx, actor, input.IncidentID); errors.Is(err, store.ErrNotFound) {
		return nil, huma.Error404NotFound("open incident was not found")
	} else if err != nil {
		return nil, huma.Error500InternalServerError("incident could not be acknowledged")
	}
	out := &acknowledgeOutput{}
	out.Body.Status = "acknowledged"
	return out, nil
}
