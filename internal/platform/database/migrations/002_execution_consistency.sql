ALTER TABLE workflow_versions
    ADD CONSTRAINT workflow_versions_tenant_workflow_id_unique
    UNIQUE (tenant_id, workflow_id, id);

ALTER TABLE events
    ADD CONSTRAINT events_tenant_workflow_id_unique
    UNIQUE (tenant_id, workflow_id, id);

ALTER TABLE executions
    DROP CONSTRAINT executions_tenant_id_workflow_version_id_fkey,
    DROP CONSTRAINT executions_tenant_id_event_id_fkey,
    ADD CONSTRAINT executions_workflow_version_consistency_fkey
        FOREIGN KEY (tenant_id, workflow_id, workflow_version_id)
        REFERENCES workflow_versions (tenant_id, workflow_id, id),
    ADD CONSTRAINT executions_event_consistency_fkey
        FOREIGN KEY (tenant_id, workflow_id, event_id)
        REFERENCES events (tenant_id, workflow_id, id);
