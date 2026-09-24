package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/david/nimbus/internal/store"
)

const maxFixtureObservationAge = 24 * time.Hour

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
	huma.Register(api, securedOperation("reviewIncident", http.MethodPost, "/v1/incidents/{incidentId}/reviews", "Incidents"), a.reviewIncident)
	huma.Register(api, securedOperation("weeklyIncidentQuality", http.MethodGet, "/v1/reports/incident-quality/weekly", "Reports"), a.weeklyIncidentQuality)
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
		ExternalIDRef       string `json:"externalIdRef,omitempty" maxLength:"80" pattern:"^aws-external-id/[a-z0-9][a-z0-9._-]{0,63}$"`
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
	pipeline := store.Pipeline{Name: strings.TrimSpace(input.Body.Name), Mode: input.Body.Mode, Region: input.Body.Region, QueueARN: input.Body.QueueARN, LambdaARN: input.Body.LambdaARN, DLQARN: input.Body.DLQARN, RoleARN: input.Body.RoleARN, ExternalIDRef: input.Body.ExternalIDRef, IncidentWorkflowID: input.Body.IncidentWorkflowID, ConsecutiveWindows: input.Body.ConsecutiveWindows, BacklogThreshold: input.Body.BacklogThreshold, AgeThresholdSeconds: input.Body.AgeThresholdSeconds}
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
	observation, err := a.awsCollector.Collect(checkCtx, pipeline)
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
	if err := validateObservationWindow(input.Body.WindowStartedAt, input.Body.WindowEndedAt, time.Now().UTC()); err != nil {
		return nil, huma.Error400BadRequest(err.Error())
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

func validateObservationWindow(start, end, now time.Time) error {
	start, end = start.UTC(), end.UTC()
	if end.Sub(start) != 5*time.Minute {
		return errors.New("observation window must be exactly five minutes")
	}
	if !start.Equal(start.Truncate(5 * time.Minute)) {
		return errors.New("windowStartedAt must use a five-minute UTC boundary")
	}
	if end.After(now.Add(time.Minute)) {
		return errors.New("observation window cannot be in the future")
	}
	if end.Before(now.Add(-maxFixtureObservationAge)) {
		return errors.New("observation window must be less than 24 hours old")
	}
	return nil
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
	items, err := a.store.ListIncidents(ctx, actor.TenantID, store.IncidentFilter{})
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

type reviewIncidentInput struct {
	IncidentID string `path:"incidentId" format:"uuid"`
	Body       struct {
		Disposition string `json:"disposition" enum:"true_positive,false_positive,expected_behavior,duplicate"`
		Comment     string `json:"comment,omitempty" maxLength:"500"`
	}
}

type reviewIncidentOutput struct {
	Body store.IncidentReview
}

func (a *API) reviewIncident(ctx context.Context, input *reviewIncidentInput) (*reviewIncidentOutput, error) {
	actor, err := requirePermission(ctx, "incident:classify")
	if err != nil {
		return nil, err
	}
	review, err := a.store.ReviewIncident(ctx, actor, input.IncidentID, input.Body.Disposition, input.Body.Comment)
	if errors.Is(err, store.ErrNotFound) {
		return nil, huma.Error404NotFound("incident was not found")
	}
	if errors.Is(err, store.ErrInvalidIncidentReview) {
		return nil, huma.Error400BadRequest(err.Error())
	}
	if err != nil {
		return nil, huma.Error500InternalServerError("incident review could not be recorded")
	}
	return &reviewIncidentOutput{Body: review}, nil
}

type weeklyIncidentQualityInput struct {
	WeeksAgo int `query:"weeksAgo" minimum:"0" maximum:"52" default:"0" doc:"UTC calendar week offset: 0 is the current week, 1 is the previous week."`
}

type weeklyIncidentQualityOutput struct {
	Body struct {
		WeekStart time.Time                     `json:"weekStart"`
		WeekEnd   time.Time                     `json:"weekEnd"`
		Items     []store.WeeklyIncidentQuality `json:"items"`
	}
}

func weekBoundsUTC(now time.Time, weeksAgo int) (time.Time, time.Time) {
	now = now.UTC()
	daysSinceMonday := (int(now.Weekday()) + 6) % 7
	start := time.Date(now.Year(), now.Month(), now.Day()-daysSinceMonday, 0, 0, 0, 0, time.UTC).AddDate(0, 0, -7*weeksAgo)
	return start, start.AddDate(0, 0, 7)
}

func (a *API) weeklyIncidentQuality(ctx context.Context, input *weeklyIncidentQualityInput) (*weeklyIncidentQualityOutput, error) {
	actor, err := requirePermission(ctx, "incident:read")
	if err != nil {
		return nil, err
	}
	weekStart, weekEnd := weekBoundsUTC(time.Now(), input.WeeksAgo)
	items, err := a.store.WeeklyIncidentQuality(ctx, actor.TenantID, weekStart, weekEnd)
	if err != nil {
		return nil, huma.Error500InternalServerError("weekly incident quality report could not be loaded")
	}
	out := &weeklyIncidentQualityOutput{}
	out.Body.WeekStart = weekStart
	out.Body.WeekEnd = weekEnd
	out.Body.Items = items
	return out, nil
}
