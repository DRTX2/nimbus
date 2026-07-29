CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE tenants (
    id uuid PRIMARY KEY,
    name text NOT NULL CHECK (length(name) BETWEEN 1 AND 120),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE workflows (
    id uuid NOT NULL DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    name text NOT NULL CHECK (length(name) BETWEEN 1 AND 120),
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'inactive')),
    active_version integer NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, id)
);

CREATE TABLE workflow_versions (
    id uuid NOT NULL DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    workflow_id uuid NOT NULL,
    version integer NOT NULL CHECK (version > 0),
    definition jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, id),
    UNIQUE (tenant_id, workflow_id, version),
    FOREIGN KEY (tenant_id, workflow_id) REFERENCES workflows(tenant_id, id) ON DELETE CASCADE
);

CREATE TABLE events (
    id uuid NOT NULL DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    workflow_id uuid NOT NULL,
    external_id text NOT NULL CHECK (length(external_id) BETWEEN 1 AND 200),
    event_type text NOT NULL,
    payload jsonb NOT NULL,
    received_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, id),
    UNIQUE (tenant_id, workflow_id, external_id),
    FOREIGN KEY (tenant_id, workflow_id) REFERENCES workflows(tenant_id, id)
);

CREATE TABLE executions (
    id uuid NOT NULL DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    workflow_id uuid NOT NULL,
    workflow_version_id uuid NOT NULL,
    event_id uuid NOT NULL,
    status text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'cancelled')),
    error text,
    started_at timestamptz,
    finished_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, id),
    UNIQUE (tenant_id, workflow_version_id, event_id),
    FOREIGN KEY (tenant_id, workflow_id) REFERENCES workflows(tenant_id, id),
    FOREIGN KEY (tenant_id, workflow_version_id) REFERENCES workflow_versions(tenant_id, id),
    FOREIGN KEY (tenant_id, event_id) REFERENCES events(tenant_id, id)
);

CREATE TABLE step_runs (
    id uuid NOT NULL DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    execution_id uuid NOT NULL,
    step_id text NOT NULL,
    step_type text NOT NULL CHECK (step_type IN ('noop', 'http')),
    config jsonb,
    depends_on jsonb NOT NULL DEFAULT '[]'::jsonb,
    status text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'cancelled')),
    attempt integer NOT NULL DEFAULT 0,
    max_attempts integer NOT NULL CHECK (max_attempts BETWEEN 1 AND 10),
    initial_delay_seconds integer NOT NULL,
    max_delay_seconds integer NOT NULL,
    timeout_seconds integer NOT NULL CHECK (timeout_seconds BETWEEN 1 AND 30),
    available_at timestamptz NOT NULL DEFAULT now(),
    lease_owner text,
    lease_until timestamptz,
    output jsonb,
    error text,
    started_at timestamptz,
    finished_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, id),
    UNIQUE (tenant_id, execution_id, step_id),
    FOREIGN KEY (tenant_id, execution_id) REFERENCES executions(tenant_id, id) ON DELETE CASCADE
);

CREATE INDEX step_runs_claim_idx ON step_runs (available_at, created_at)
    WHERE status = 'queued';
CREATE INDEX step_runs_expired_lease_idx ON step_runs (lease_until)
    WHERE status = 'running';
CREATE INDEX executions_tenant_created_idx ON executions (tenant_id, created_at DESC);
CREATE INDEX events_tenant_received_idx ON events (tenant_id, received_at DESC);
