ALTER TABLE workflows ADD COLUMN approval_required boolean NOT NULL DEFAULT false;

ALTER TABLE executions DROP CONSTRAINT executions_status_check;
ALTER TABLE executions ADD CONSTRAINT executions_status_check
    CHECK (status IN ('awaiting_approval', 'queued', 'running', 'succeeded', 'failed', 'cancelled'));

CREATE TABLE principals (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    kind text NOT NULL CHECK (kind IN ('api_key', 'oidc', 'system')),
    issuer text,
    subject text,
    display_name text NOT NULL CHECK (length(display_name) BETWEEN 1 AND 120),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX principals_federated_identity_idx ON principals (issuer, subject)
    WHERE issuer IS NOT NULL AND subject IS NOT NULL;

CREATE TABLE memberships (
    id uuid NOT NULL DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    principal_id uuid NOT NULL REFERENCES principals(id),
    role text NOT NULL CHECK (role IN ('owner', 'operator', 'approver', 'viewer')),
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'revoked')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, id),
    UNIQUE (tenant_id, principal_id)
);

CREATE TABLE api_keys (
    id uuid NOT NULL DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    principal_id uuid NOT NULL REFERENCES principals(id),
    name text NOT NULL CHECK (length(name) BETWEEN 1 AND 120),
    prefix text NOT NULL,
    secret_hash bytea NOT NULL UNIQUE,
    expires_at timestamptz,
    last_used_at timestamptz,
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, id),
    FOREIGN KEY (tenant_id, principal_id) REFERENCES memberships(tenant_id, principal_id)
);

CREATE TABLE audit_events (
    id uuid NOT NULL DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    actor_id uuid,
    action text NOT NULL,
    resource_type text NOT NULL,
    resource_id text NOT NULL,
    result text NOT NULL CHECK (result IN ('succeeded', 'denied', 'failed')),
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    occurred_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, id)
);
CREATE INDEX audit_events_tenant_time_idx ON audit_events (tenant_id, occurred_at DESC);

CREATE TABLE approval_requests (
    id uuid NOT NULL DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    execution_id uuid NOT NULL,
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected', 'expired')),
    requested_by uuid NOT NULL REFERENCES principals(id),
    decided_by uuid REFERENCES principals(id),
    reason text CHECK (length(reason) <= 500),
    decision_comment text CHECK (length(decision_comment) <= 500),
    expires_at timestamptz NOT NULL DEFAULT now() + interval '24 hours',
    decided_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, id),
    UNIQUE (tenant_id, execution_id),
    FOREIGN KEY (tenant_id, execution_id) REFERENCES executions(tenant_id, id) ON DELETE CASCADE
);

CREATE TABLE monitored_pipelines (
    id uuid NOT NULL DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    name text NOT NULL CHECK (length(name) BETWEEN 1 AND 120),
    mode text NOT NULL CHECK (mode IN ('fixture', 'aws')),
    region text NOT NULL CHECK (length(region) BETWEEN 3 AND 32),
    queue_arn text NOT NULL CHECK (length(queue_arn) <= 512),
    lambda_arn text NOT NULL CHECK (length(lambda_arn) <= 512),
    dlq_arn text NOT NULL CHECK (length(dlq_arn) <= 512),
    role_arn text CHECK (length(role_arn) <= 512),
    external_id_env text CHECK (length(external_id_env) <= 128),
    incident_workflow_id uuid,
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'inactive')),
    consecutive_windows integer NOT NULL DEFAULT 2 CHECK (consecutive_windows BETWEEN 1 AND 10),
    backlog_threshold bigint NOT NULL DEFAULT 100 CHECK (backlog_threshold >= 0),
    age_threshold_seconds bigint NOT NULL DEFAULT 300 CHECK (age_threshold_seconds >= 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, id),
    UNIQUE (tenant_id, queue_arn, lambda_arn),
    FOREIGN KEY (tenant_id, incident_workflow_id) REFERENCES workflows(tenant_id, id)
);

CREATE TABLE pipeline_observations (
    id uuid NOT NULL DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    pipeline_id uuid NOT NULL,
    window_started_at timestamptz NOT NULL,
    window_ended_at timestamptz NOT NULL,
    visible_messages bigint NOT NULL CHECK (visible_messages >= 0),
    in_flight_messages bigint NOT NULL CHECK (in_flight_messages >= 0),
    oldest_message_seconds bigint NOT NULL CHECK (oldest_message_seconds >= 0),
    dlq_messages bigint NOT NULL CHECK (dlq_messages >= 0),
    lambda_invocations bigint NOT NULL CHECK (lambda_invocations >= 0),
    lambda_errors bigint NOT NULL CHECK (lambda_errors >= 0),
    lambda_throttles bigint NOT NULL CHECK (lambda_throttles >= 0),
    source text NOT NULL CHECK (source IN ('fixture', 'cloudwatch')),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, id),
    UNIQUE (tenant_id, pipeline_id, window_started_at),
    FOREIGN KEY (tenant_id, pipeline_id) REFERENCES monitored_pipelines(tenant_id, id) ON DELETE CASCADE,
    CHECK (window_ended_at > window_started_at)
);

CREATE TABLE incidents (
    id uuid NOT NULL DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    pipeline_id uuid NOT NULL,
    detector text NOT NULL CHECK (detector IN ('consumer_stalled', 'lambda_throttled', 'dlq_growth', 'retry_storm')),
    status text NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'acknowledged', 'recovered', 'resolved')),
    fingerprint text NOT NULL,
    severity text NOT NULL CHECK (severity IN ('warning', 'critical')),
    summary text NOT NULL CHECK (length(summary) <= 500),
    evidence jsonb NOT NULL,
    opened_at timestamptz NOT NULL DEFAULT now(),
    acknowledged_at timestamptz,
    recovered_at timestamptz,
    resolved_at timestamptz,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, id),
    FOREIGN KEY (tenant_id, pipeline_id) REFERENCES monitored_pipelines(tenant_id, id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX incidents_one_active_idx ON incidents (tenant_id, pipeline_id, detector)
    WHERE status IN ('open', 'acknowledged');
CREATE INDEX incidents_tenant_opened_idx ON incidents (tenant_id, opened_at DESC);

CREATE TABLE incident_timeline (
    id uuid NOT NULL DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    incident_id uuid NOT NULL,
    event_type text NOT NULL,
    actor_id uuid,
    details jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, id),
    FOREIGN KEY (tenant_id, incident_id) REFERENCES incidents(tenant_id, id) ON DELETE CASCADE
);

CREATE TABLE outbox_messages (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    topic text NOT NULL,
    aggregate_type text NOT NULL,
    aggregate_id text NOT NULL,
    payload jsonb NOT NULL DEFAULT '{}'::jsonb,
    idempotency_key text NOT NULL UNIQUE,
    available_at timestamptz NOT NULL DEFAULT now(),
    attempt integer NOT NULL DEFAULT 0,
    max_attempts integer NOT NULL DEFAULT 10 CHECK (max_attempts BETWEEN 1 AND 20),
    lease_owner text,
    lease_until timestamptz,
    published_at timestamptz,
    last_error text,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX outbox_claim_idx ON outbox_messages (available_at, created_at) WHERE published_at IS NULL;

ALTER TABLE workflows ENABLE ROW LEVEL SECURITY;
ALTER TABLE workflow_versions ENABLE ROW LEVEL SECURITY;
ALTER TABLE events ENABLE ROW LEVEL SECURITY;
ALTER TABLE executions ENABLE ROW LEVEL SECURITY;
ALTER TABLE step_runs ENABLE ROW LEVEL SECURITY;
ALTER TABLE memberships ENABLE ROW LEVEL SECURITY;
ALTER TABLE api_keys ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE approval_requests ENABLE ROW LEVEL SECURITY;
ALTER TABLE monitored_pipelines ENABLE ROW LEVEL SECURITY;
ALTER TABLE pipeline_observations ENABLE ROW LEVEL SECURITY;
ALTER TABLE incidents ENABLE ROW LEVEL SECURITY;
ALTER TABLE incident_timeline ENABLE ROW LEVEL SECURITY;
ALTER TABLE outbox_messages ENABLE ROW LEVEL SECURITY;

DO $$
DECLARE table_name text;
BEGIN
    FOREACH table_name IN ARRAY ARRAY['workflows','workflow_versions','events','executions','step_runs','memberships','api_keys','audit_events','approval_requests','monitored_pipelines','pipeline_observations','incidents','incident_timeline','outbox_messages']
    LOOP
        EXECUTE format('CREATE POLICY tenant_isolation ON %I USING (tenant_id = nullif(current_setting(''app.tenant_id'', true), '''')::uuid) WITH CHECK (tenant_id = nullif(current_setting(''app.tenant_id'', true), '''')::uuid)', table_name);
    END LOOP;
END $$;
