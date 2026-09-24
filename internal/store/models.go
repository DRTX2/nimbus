package store

import (
	"encoding/json"
	"time"

	"github.com/david/nimbus/internal/workflow"
)

type Workflow struct {
	ID               string              `json:"id"`
	Name             string              `json:"name"`
	Status           string              `json:"status"`
	Version          int                 `json:"version"`
	Definition       workflow.Definition `json:"definition"`
	CreatedAt        time.Time           `json:"createdAt"`
	UpdatedAt        time.Time           `json:"updatedAt"`
	ApprovalRequired bool                `json:"approvalRequired"`
}

type WorkflowDetail struct {
	Workflow
	Versions []int `json:"versions"`
}

type IngestResult struct {
	EventID     string `json:"eventId"`
	ExecutionID string `json:"executionId"`
	Duplicate   bool   `json:"duplicate"`
	ApprovalID  string `json:"approvalId,omitempty"`
}

type Principal struct {
	ID       string `json:"id"`
	TenantID string `json:"tenantId"`
	Name     string `json:"name"`
	Role     string `json:"role"`
}

// FederatedIdentity is an identity already verified by an external provider.
type FederatedIdentity struct {
	Issuer      string
	Subject     string
	DisplayName string
	TenantID    string
	ExpiresAt   time.Time
}

type Member struct {
	ID          string    `json:"id"`
	PrincipalID string    `json:"principalId"`
	Kind        string    `json:"kind,omitempty"`
	DisplayName string    `json:"displayName,omitempty"`
	Role        string    `json:"role"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type APIKey struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Prefix    string     `json:"prefix"`
	Role      string     `json:"role"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
	RevokedAt *time.Time `json:"revokedAt,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
}

type Approval struct {
	ID          string     `json:"id"`
	ExecutionID string     `json:"executionId"`
	Status      string     `json:"status"`
	RequestedBy string     `json:"requestedBy"`
	DecidedBy   *string    `json:"decidedBy,omitempty"`
	Reason      *string    `json:"reason,omitempty"`
	ExpiresAt   time.Time  `json:"expiresAt"`
	DecidedAt   *time.Time `json:"decidedAt,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
}

type Pipeline struct {
	ID                  string    `json:"id"`
	Name                string    `json:"name"`
	Mode                string    `json:"mode"`
	Region              string    `json:"region"`
	QueueARN            string    `json:"queueArn"`
	LambdaARN           string    `json:"lambdaArn"`
	DLQARN              string    `json:"dlqArn"`
	RoleARN             string    `json:"roleArn,omitempty"`
	ExternalIDRef       string    `json:"externalIdRef,omitempty"`
	IncidentWorkflowID  string    `json:"incidentWorkflowId,omitempty"`
	Status              string    `json:"status"`
	ConsecutiveWindows  int       `json:"consecutiveWindows"`
	BacklogThreshold    int64     `json:"backlogThreshold"`
	AgeThresholdSeconds int64     `json:"ageThresholdSeconds"`
	CreatedAt           time.Time `json:"createdAt"`
}

type Observation struct {
	ID                   string    `json:"id"`
	PipelineID           string    `json:"pipelineId"`
	WindowStartedAt      time.Time `json:"windowStartedAt"`
	WindowEndedAt        time.Time `json:"windowEndedAt"`
	VisibleMessages      int64     `json:"visibleMessages"`
	InFlightMessages     int64     `json:"inFlightMessages"`
	OldestMessageSeconds int64     `json:"oldestMessageSeconds"`
	DLQMessages          int64     `json:"dlqMessages"`
	LambdaInvocations    int64     `json:"lambdaInvocations"`
	LambdaErrors         int64     `json:"lambdaErrors"`
	LambdaThrottles      int64     `json:"lambdaThrottles"`
	Source               string    `json:"source"`
}

type Incident struct {
	ID             string          `json:"id"`
	PipelineID     string          `json:"pipelineId"`
	PipelineName   string          `json:"pipelineName,omitempty"`
	Detector       string          `json:"detector"`
	Status         string          `json:"status"`
	Severity       string          `json:"severity"`
	Summary        string          `json:"summary"`
	Evidence       json.RawMessage `json:"evidence"`
	OpenedAt       time.Time       `json:"openedAt"`
	AcknowledgedAt *time.Time      `json:"acknowledgedAt,omitempty"`
	RecoveredAt    *time.Time      `json:"recoveredAt,omitempty"`
	ResolvedAt     *time.Time      `json:"resolvedAt,omitempty"`
	Review         *IncidentReview `json:"review,omitempty"`
	Timeline       []TimelineEvent `json:"timeline,omitempty"`
}

type IncidentReview struct {
	ID                  string    `json:"id"`
	Disposition         string    `json:"disposition"`
	ReviewerPrincipalID string    `json:"reviewerPrincipalId"`
	Comment             *string   `json:"comment,omitempty"`
	ReviewedAt          time.Time `json:"reviewedAt"`
}

type WeeklyIncidentQuality struct {
	PipelineID        string  `json:"pipelineId"`
	PipelineName      string  `json:"pipelineName"`
	Detector          string  `json:"detector"`
	Total             int64   `json:"total"`
	Reviewed          int64   `json:"reviewed"`
	Unreviewed        int64   `json:"unreviewed"`
	FalsePositives    int64   `json:"falsePositives"`
	TruePositives     int64   `json:"truePositives"`
	ExpectedBehavior  int64   `json:"expectedBehavior"`
	Duplicates        int64   `json:"duplicates"`
	FalsePositiveRate float64 `json:"falsePositiveRate"`
}

type TimelineEvent struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	ActorID   *string         `json:"actorId,omitempty"`
	Details   json.RawMessage `json:"details"`
	CreatedAt time.Time       `json:"createdAt"`
}

type AuditEvent struct {
	ID           string          `json:"id"`
	ActorID      *string         `json:"actorId,omitempty"`
	Action       string          `json:"action"`
	ResourceType string          `json:"resourceType"`
	ResourceID   string          `json:"resourceId"`
	Result       string          `json:"result"`
	Metadata     json.RawMessage `json:"metadata"`
	OccurredAt   time.Time       `json:"occurredAt"`
}

type OutboxMessage struct {
	ID          string
	TenantID    string
	Topic       string
	AggregateID string
	Payload     json.RawMessage
	Attempt     int
	MaxAttempts int
	LeaseOwner  string
}

type Execution struct {
	ID         string     `json:"id"`
	WorkflowID string     `json:"workflowId"`
	Version    int        `json:"workflowVersion"`
	EventID    string     `json:"eventId"`
	Status     string     `json:"status"`
	Error      *string    `json:"error,omitempty"`
	StartedAt  *time.Time `json:"startedAt,omitempty"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
	Steps      []StepRun  `json:"steps"`
}

type StepRun struct {
	ID         string          `json:"id"`
	StepID     string          `json:"stepId"`
	Type       string          `json:"type"`
	Status     string          `json:"status"`
	Attempt    int             `json:"attempt"`
	Output     json.RawMessage `json:"output,omitempty"`
	Error      *string         `json:"error,omitempty"`
	StartedAt  *time.Time      `json:"startedAt,omitempty"`
	FinishedAt *time.Time      `json:"finishedAt,omitempty"`
}

type Job struct {
	ID                  string
	TenantID            string
	ExecutionID         string
	StepID              string
	StepType            string
	Config              json.RawMessage
	Attempt             int
	MaxAttempts         int
	InitialDelaySeconds int
	MaxDelaySeconds     int
	TimeoutSeconds      int
	LeaseOwner          string
	EventPayload        json.RawMessage
}

type Analytics struct {
	Events24h           int64   `json:"events24h"`
	Executions24h       int64   `json:"executions24h"`
	Succeeded24h        int64   `json:"succeeded24h"`
	Failed24h           int64   `json:"failed24h"`
	SuccessRate         float64 `json:"successRate"`
	QueuedSteps         int64   `json:"queuedSteps"`
	OldestQueuedSeconds int64   `json:"oldestQueuedSeconds"`
}

type ExecutionSummary struct {
	ID           string     `json:"id"`
	WorkflowID   string     `json:"workflowId"`
	WorkflowName string     `json:"workflowName"`
	Status       string     `json:"status"`
	Error        *string    `json:"error,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
	StartedAt    *time.Time `json:"startedAt,omitempty"`
	FinishedAt   *time.Time `json:"finishedAt,omitempty"`
}

type ExecutionFilter struct {
	Status     string
	WorkflowID string
}

type ExecutionPage struct {
	Items      []ExecutionSummary `json:"items"`
	NextCursor string             `json:"nextCursor,omitempty"`
}

type IncidentFilter struct {
	Status string
}
