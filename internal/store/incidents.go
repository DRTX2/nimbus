package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/david/nimbus/internal/detector"
	"github.com/jackc/pgx/v5"
)

func (s *Store) CreatePipeline(ctx context.Context, actor Principal, pipeline Pipeline) (Pipeline, error) {
	if pipeline.Mode != "fixture" && pipeline.Mode != "aws" {
		return Pipeline{}, errors.New("pipeline mode must be fixture or aws")
	}
	if pipeline.Mode == "aws" && strings.TrimSpace(pipeline.RoleARN) == "" {
		return Pipeline{}, errors.New("roleArn is required for AWS pipelines")
	}
	if pipeline.ConsecutiveWindows == 0 {
		pipeline.ConsecutiveWindows = 2
	}
	if pipeline.BacklogThreshold == 0 {
		pipeline.BacklogThreshold = 100
	}
	if pipeline.AgeThresholdSeconds == 0 {
		pipeline.AgeThresholdSeconds = 300
	}
	tx, err := s.beginTenant(ctx, actor.TenantID)
	if err != nil {
		return Pipeline{}, fmt.Errorf("begin pipeline creation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	err = tx.QueryRow(ctx, `
		INSERT INTO monitored_pipelines (tenant_id, name, mode, region, queue_arn, lambda_arn, dlq_arn, role_arn, external_id_env,
			incident_workflow_id, consecutive_windows, backlog_threshold, age_threshold_seconds)
		VALUES ($1,$2,$3,$4,$5,$6,$7,NULLIF($8,''),NULLIF($9,''),NULLIF($10,'')::uuid,$11,$12,$13)
		RETURNING id, status, created_at`, actor.TenantID, pipeline.Name, pipeline.Mode, pipeline.Region,
		pipeline.QueueARN, pipeline.LambdaARN, pipeline.DLQARN, pipeline.RoleARN, pipeline.ExternalIDEnv, pipeline.IncidentWorkflowID,
		pipeline.ConsecutiveWindows, pipeline.BacklogThreshold, pipeline.AgeThresholdSeconds).
		Scan(&pipeline.ID, &pipeline.Status, &pipeline.CreatedAt)
	if err != nil {
		return Pipeline{}, fmt.Errorf("create pipeline: %w", err)
	}
	if err := insertAudit(ctx, tx, actor, "pipeline.create", "pipeline", pipeline.ID, "succeeded", map[string]any{"mode": pipeline.Mode}); err != nil {
		return Pipeline{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Pipeline{}, fmt.Errorf("commit pipeline: %w", err)
	}
	return pipeline, nil
}

func (s *Store) ListPipelines(ctx context.Context, tenantID string) ([]Pipeline, error) {
	tx, err := s.beginTenant(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("begin pipeline list: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `
		SELECT id,name,mode,region,queue_arn,lambda_arn,dlq_arn,COALESCE(role_arn,''),COALESCE(external_id_env,''),COALESCE(incident_workflow_id::text,''),
			status,consecutive_windows,backlog_threshold,age_threshold_seconds,created_at
		FROM monitored_pipelines WHERE tenant_id=$1 ORDER BY created_at DESC`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list pipelines: %w", err)
	}
	defer rows.Close()
	result := make([]Pipeline, 0)
	for rows.Next() {
		var item Pipeline
		if err := rows.Scan(&item.ID, &item.Name, &item.Mode, &item.Region, &item.QueueARN, &item.LambdaARN, &item.DLQARN, &item.RoleARN, &item.ExternalIDEnv,
			&item.IncidentWorkflowID, &item.Status, &item.ConsecutiveWindows, &item.BacklogThreshold, &item.AgeThresholdSeconds, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan pipeline: %w", err)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit pipeline list: %w", err)
	}
	return result, nil
}

func (s *Store) GetPipeline(ctx context.Context, tenantID, id string) (Pipeline, error) {
	tx, err := s.beginTenant(ctx, tenantID)
	if err != nil {
		return Pipeline{}, fmt.Errorf("begin get pipeline: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var item Pipeline
	err = tx.QueryRow(ctx, `SELECT id,name,mode,region,queue_arn,lambda_arn,dlq_arn,COALESCE(role_arn,''),COALESCE(external_id_env,''),
		COALESCE(incident_workflow_id::text,''),status,consecutive_windows,backlog_threshold,age_threshold_seconds,created_at
		FROM monitored_pipelines WHERE tenant_id=$1 AND id=$2`, tenantID, id).Scan(&item.ID, &item.Name, &item.Mode, &item.Region,
		&item.QueueARN, &item.LambdaARN, &item.DLQARN, &item.RoleARN, &item.ExternalIDEnv, &item.IncidentWorkflowID, &item.Status, &item.ConsecutiveWindows,
		&item.BacklogThreshold, &item.AgeThresholdSeconds, &item.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Pipeline{}, ErrNotFound
	}
	if err != nil {
		return Pipeline{}, fmt.Errorf("get pipeline: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Pipeline{}, fmt.Errorf("commit get pipeline: %w", err)
	}
	return item, nil
}

func (s *Store) RecordObservation(ctx context.Context, actor Principal, observation Observation) ([]Incident, error) {
	tx, err := s.beginTenant(ctx, actor.TenantID)
	if err != nil {
		return nil, fmt.Errorf("begin observation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var pipeline Pipeline
	err = tx.QueryRow(ctx, `
		SELECT id, mode, status, consecutive_windows, backlog_threshold, age_threshold_seconds
		FROM monitored_pipelines WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, actor.TenantID, observation.PipelineID).
		Scan(&pipeline.ID, &pipeline.Mode, &pipeline.Status, &pipeline.ConsecutiveWindows, &pipeline.BacklogThreshold, &pipeline.AgeThresholdSeconds)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load pipeline: %w", err)
	}
	if pipeline.Mode != "fixture" && observation.Source == "fixture" {
		return nil, ErrForbidden
	}
	err = tx.QueryRow(ctx, `WITH inserted AS (
		INSERT INTO pipeline_observations (tenant_id,pipeline_id,window_started_at,window_ended_at,visible_messages,
			in_flight_messages,oldest_message_seconds,dlq_messages,lambda_invocations,lambda_errors,lambda_throttles,source)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		ON CONFLICT (tenant_id,pipeline_id,window_started_at) DO NOTHING
		RETURNING id,window_ended_at,visible_messages,in_flight_messages,oldest_message_seconds,dlq_messages,lambda_invocations,lambda_errors,lambda_throttles,source)
		SELECT * FROM inserted UNION ALL
		SELECT id,window_ended_at,visible_messages,in_flight_messages,oldest_message_seconds,dlq_messages,lambda_invocations,lambda_errors,lambda_throttles,source
		FROM pipeline_observations WHERE tenant_id=$1 AND pipeline_id=$2 AND window_started_at=$3 LIMIT 1`, actor.TenantID, observation.PipelineID, observation.WindowStartedAt, observation.WindowEndedAt,
		observation.VisibleMessages, observation.InFlightMessages, observation.OldestMessageSeconds, observation.DLQMessages,
		observation.LambdaInvocations, observation.LambdaErrors, observation.LambdaThrottles, observation.Source).Scan(&observation.ID,
		&observation.WindowEndedAt, &observation.VisibleMessages, &observation.InFlightMessages, &observation.OldestMessageSeconds,
		&observation.DLQMessages, &observation.LambdaInvocations, &observation.LambdaErrors, &observation.LambdaThrottles, &observation.Source)
	if err != nil {
		return nil, fmt.Errorf("insert observation: %w", err)
	}

	rows, err := tx.Query(ctx, `
		SELECT visible_messages,in_flight_messages,oldest_message_seconds,dlq_messages,lambda_invocations,lambda_errors,lambda_throttles
		FROM pipeline_observations WHERE tenant_id=$1 AND pipeline_id=$2 ORDER BY window_started_at DESC LIMIT $3`,
		actor.TenantID, observation.PipelineID, pipeline.ConsecutiveWindows)
	if err != nil {
		return nil, fmt.Errorf("load detector windows: %w", err)
	}
	windows := make([]Observation, 0, pipeline.ConsecutiveWindows)
	for rows.Next() {
		var item Observation
		if err := rows.Scan(&item.VisibleMessages, &item.InFlightMessages, &item.OldestMessageSeconds, &item.DLQMessages,
			&item.LambdaInvocations, &item.LambdaErrors, &item.LambdaThrottles); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan detector window: %w", err)
		}
		windows = append(windows, item)
	}
	rows.Close()
	if len(windows) < pipeline.ConsecutiveWindows {
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit observation: %w", err)
		}
		return []Incident{}, nil
	}

	counts := map[string]int{}
	latest := map[string]detector.Decision{}
	for _, window := range windows {
		for _, decision := range detector.Evaluate(detector.Observation{
			VisibleMessages: window.VisibleMessages, OldestMessageSeconds: window.OldestMessageSeconds,
			DLQMessages: window.DLQMessages, LambdaInvocations: window.LambdaInvocations,
			LambdaErrors: window.LambdaErrors, LambdaThrottles: window.LambdaThrottles,
		}, detector.Thresholds{Backlog: pipeline.BacklogThreshold, Age: pipeline.AgeThresholdSeconds}) {
			latest[decision.Detector] = decision
			if decision.Breached {
				counts[decision.Detector]++
			}
		}
	}
	evidence, _ := json.Marshal(observation)
	changed := make([]Incident, 0)
	for name, decision := range latest {
		if counts[name] == pipeline.ConsecutiveWindows {
			fingerprintBytes := sha256.Sum256([]byte(actor.TenantID + ":" + observation.PipelineID + ":" + name))
			fingerprint := hex.EncodeToString(fingerprintBytes[:])
			var incident Incident
			err := tx.QueryRow(ctx, `
				INSERT INTO incidents (tenant_id,pipeline_id,detector,fingerprint,severity,summary,evidence)
				VALUES ($1,$2,$3,$4,$5,$6,$7)
				ON CONFLICT (tenant_id,pipeline_id,detector) WHERE status IN ('open','acknowledged')
				DO UPDATE SET evidence=EXCLUDED.evidence,updated_at=now()
				RETURNING id,pipeline_id,detector,status,severity,summary,evidence,opened_at,acknowledged_at,recovered_at,resolved_at`,
				actor.TenantID, observation.PipelineID, name, fingerprint, decision.Severity, decision.Summary, evidence).
				Scan(&incident.ID, &incident.PipelineID, &incident.Detector, &incident.Status, &incident.Severity, &incident.Summary, &incident.Evidence,
					&incident.OpenedAt, &incident.AcknowledgedAt, &incident.RecoveredAt, &incident.ResolvedAt)
			if err != nil {
				return nil, fmt.Errorf("open incident: %w", err)
			}
			var timelineExists bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM incident_timeline WHERE tenant_id=$1 AND incident_id=$2 AND event_type='opened')`, actor.TenantID, incident.ID).Scan(&timelineExists); err != nil {
				return nil, err
			}
			if !timelineExists {
				_, err = tx.Exec(ctx, `INSERT INTO incident_timeline (tenant_id,incident_id,event_type,actor_id,details) VALUES ($1,$2,'opened',$3,$4)`, actor.TenantID, incident.ID, actor.ID, evidence)
				if err != nil {
					return nil, fmt.Errorf("append incident timeline: %w", err)
				}
				if err := insertOutbox(ctx, tx, actor.TenantID, "incident.opened", "incident", incident.ID, "incident:"+incident.ID+":opened", map[string]any{"incidentId": incident.ID}); err != nil {
					return nil, err
				}
			}
			changed = append(changed, incident)
		} else if counts[name] == 0 {
			rows, err := tx.Query(ctx, `UPDATE incidents SET status='recovered',recovered_at=now(),updated_at=now()
				WHERE tenant_id=$1 AND pipeline_id=$2 AND detector=$3 AND status IN ('open','acknowledged') RETURNING id`, actor.TenantID, observation.PipelineID, name)
			if err != nil {
				return nil, fmt.Errorf("recover incident: %w", err)
			}
			for rows.Next() {
				var id string
				if err := rows.Scan(&id); err != nil {
					rows.Close()
					return nil, err
				}
				_, _ = tx.Exec(ctx, `INSERT INTO incident_timeline (tenant_id,incident_id,event_type,actor_id) VALUES ($1,$2,'recovered',$3)`, actor.TenantID, id, actor.ID)
				_ = insertOutbox(ctx, tx, actor.TenantID, "incident.recovered", "incident", id, "incident:"+id+":recovered", map[string]any{"incidentId": id})
			}
			rows.Close()
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit observation evaluation: %w", err)
	}
	return changed, nil
}

func (s *Store) ListIncidents(ctx context.Context, tenantID string) ([]Incident, error) {
	tx, err := s.beginTenant(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("begin incident list: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `SELECT i.id,i.pipeline_id,i.detector,i.status,i.severity,i.summary,i.evidence,i.opened_at,
		i.acknowledged_at,i.recovered_at,i.resolved_at,r.id,r.disposition,r.reviewer_principal_id,r.comment,r.reviewed_at
		FROM incidents i
		LEFT JOIN LATERAL (
			SELECT id,disposition,reviewer_principal_id,comment,reviewed_at
			FROM incident_reviews
			WHERE tenant_id=i.tenant_id AND incident_id=i.id
			ORDER BY revision DESC LIMIT 1
		) r ON true
		WHERE i.tenant_id=$1 ORDER BY i.opened_at DESC,i.id LIMIT 100`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list incidents: %w", err)
	}
	defer rows.Close()
	result := make([]Incident, 0)
	for rows.Next() {
		var item Incident
		var reviewID, disposition, reviewerID *string
		var comment *string
		var reviewedAt *time.Time
		if err := rows.Scan(&item.ID, &item.PipelineID, &item.Detector, &item.Status, &item.Severity, &item.Summary, &item.Evidence,
			&item.OpenedAt, &item.AcknowledgedAt, &item.RecoveredAt, &item.ResolvedAt, &reviewID, &disposition, &reviewerID, &comment, &reviewedAt); err != nil {
			return nil, err
		}
		if reviewID != nil {
			item.Review = &IncidentReview{ID: *reviewID, Disposition: *disposition, ReviewerPrincipalID: *reviewerID, Comment: comment, ReviewedAt: *reviewedAt}
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit incident list: %w", err)
	}
	return result, nil
}

const maxIncidentReviewCommentLength = 500

var ErrInvalidIncidentReview = errors.New("invalid incident review")

func validateIncidentReview(disposition, comment string) error {
	switch disposition {
	case "true_positive", "false_positive", "expected_behavior", "duplicate":
	default:
		return fmt.Errorf("%w: invalid disposition", ErrInvalidIncidentReview)
	}
	if len([]rune(comment)) > maxIncidentReviewCommentLength {
		return fmt.Errorf("%w: comment must be at most %d characters", ErrInvalidIncidentReview, maxIncidentReviewCommentLength)
	}
	return nil
}

// ReviewIncident appends a classification so reclassifications retain history.
func (s *Store) ReviewIncident(ctx context.Context, actor Principal, incidentID, disposition, comment string) (IncidentReview, error) {
	comment = strings.TrimSpace(comment)
	if err := validateIncidentReview(disposition, comment); err != nil {
		return IncidentReview{}, err
	}
	tx, err := s.beginTenant(ctx, actor.TenantID)
	if err != nil {
		return IncidentReview{}, fmt.Errorf("begin incident review: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT true FROM incidents WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, actor.TenantID, incidentID).Scan(&exists); errors.Is(err, pgx.ErrNoRows) {
		return IncidentReview{}, ErrNotFound
	} else if err != nil {
		return IncidentReview{}, fmt.Errorf("load incident for review: %w", err)
	}
	var previous *IncidentReview
	var latest IncidentReview
	var latestComment *string
	err = tx.QueryRow(ctx, `SELECT id,disposition,reviewer_principal_id,comment,reviewed_at
		FROM incident_reviews WHERE tenant_id=$1 AND incident_id=$2
		ORDER BY revision DESC LIMIT 1`, actor.TenantID, incidentID).
		Scan(&latest.ID, &latest.Disposition, &latest.ReviewerPrincipalID, &latestComment, &latest.ReviewedAt)
	if err == nil {
		latest.Comment = latestComment
		previous = &latest
		if latest.Disposition == disposition && latest.ReviewerPrincipalID == actor.ID && nullableStringEquals(latest.Comment, comment) {
			if err := tx.Commit(ctx); err != nil {
				return IncidentReview{}, fmt.Errorf("commit idempotent incident review: %w", err)
			}
			return latest, nil
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return IncidentReview{}, fmt.Errorf("load previous incident review: %w", err)
	}
	var review IncidentReview
	var storedComment *string
	if err := tx.QueryRow(ctx, `INSERT INTO incident_reviews
		(tenant_id,incident_id,revision,disposition,reviewer_principal_id,comment)
		SELECT $1,$2,COALESCE(max(revision),0)+1,$3,$4,NULLIF($5,'')
		FROM incident_reviews WHERE tenant_id=$1 AND incident_id=$2
		RETURNING id,disposition,reviewer_principal_id,comment,reviewed_at`, actor.TenantID, incidentID, disposition, actor.ID, comment).
		Scan(&review.ID, &review.Disposition, &review.ReviewerPrincipalID, &storedComment, &review.ReviewedAt); err != nil {
		return IncidentReview{}, fmt.Errorf("insert incident review: %w", err)
	}
	review.Comment = storedComment
	action := "incident.review"
	metadata := map[string]any{"disposition": disposition}
	if previous != nil && previous.Disposition != disposition {
		action = "incident.reclassify"
		metadata["previousDisposition"] = previous.Disposition
	}
	if err := insertAudit(ctx, tx, actor, action, "incident", incidentID, "succeeded", metadata); err != nil {
		return IncidentReview{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return IncidentReview{}, fmt.Errorf("commit incident review: %w", err)
	}
	return review, nil
}

func nullableStringEquals(value *string, expected string) bool {
	if value == nil {
		return expected == ""
	}
	return *value == expected
}

func (s *Store) WeeklyIncidentQuality(ctx context.Context, tenantID string, weekStart, weekEnd time.Time) ([]WeeklyIncidentQuality, error) {
	tx, err := s.beginTenant(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("begin weekly incident quality report: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `SELECT i.pipeline_id,p.name,i.detector,
		count(*) AS total,
		count(r.id) AS reviewed,
		count(*)-count(r.id) AS unreviewed,
		count(*) FILTER (WHERE r.disposition='false_positive') AS false_positives,
		count(*) FILTER (WHERE r.disposition='true_positive') AS true_positives,
		count(*) FILTER (WHERE r.disposition='expected_behavior') AS expected_behavior,
		count(*) FILTER (WHERE r.disposition='duplicate') AS duplicates,
		CASE WHEN count(*) FILTER (WHERE r.disposition IN ('false_positive','true_positive')) = 0 THEN 0
		ELSE count(*) FILTER (WHERE r.disposition='false_positive')::double precision /
			count(*) FILTER (WHERE r.disposition IN ('false_positive','true_positive')) END AS false_positive_rate
		FROM incidents i
		JOIN monitored_pipelines p ON p.tenant_id=i.tenant_id AND p.id=i.pipeline_id
		LEFT JOIN LATERAL (
			SELECT id,disposition FROM incident_reviews
			WHERE tenant_id=i.tenant_id AND incident_id=i.id
			ORDER BY revision DESC LIMIT 1
		) r ON true
		WHERE i.tenant_id=$1 AND i.opened_at >= $2 AND i.opened_at < $3
		GROUP BY i.pipeline_id,p.name,i.detector
		ORDER BY p.name,i.pipeline_id,i.detector`, tenantID, weekStart, weekEnd)
	if err != nil {
		return nil, fmt.Errorf("query weekly incident quality report: %w", err)
	}
	defer rows.Close()
	items := make([]WeeklyIncidentQuality, 0)
	for rows.Next() {
		var item WeeklyIncidentQuality
		if err := rows.Scan(&item.PipelineID, &item.PipelineName, &item.Detector, &item.Total, &item.Reviewed, &item.Unreviewed,
			&item.FalsePositives, &item.TruePositives, &item.ExpectedBehavior, &item.Duplicates, &item.FalsePositiveRate); err != nil {
			return nil, fmt.Errorf("scan weekly incident quality report: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate weekly incident quality report: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit weekly incident quality report: %w", err)
	}
	return items, nil
}

func (s *Store) AcknowledgeIncident(ctx context.Context, actor Principal, id string) error {
	tx, err := s.beginTenant(ctx, actor.TenantID)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	command, err := tx.Exec(ctx, `UPDATE incidents SET status='acknowledged',acknowledged_at=now(),updated_at=now() WHERE tenant_id=$1 AND id=$2 AND status='open'`, actor.TenantID, id)
	if err != nil {
		return fmt.Errorf("acknowledge incident: %w", err)
	}
	if command.RowsAffected() == 0 {
		return ErrNotFound
	}
	_, err = tx.Exec(ctx, `INSERT INTO incident_timeline (tenant_id,incident_id,event_type,actor_id) VALUES ($1,$2,'acknowledged',$3)`, actor.TenantID, id, actor.ID)
	if err != nil {
		return err
	}
	if err := insertAudit(ctx, tx, actor, "incident.acknowledge", "incident", id, "succeeded", map[string]any{}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
