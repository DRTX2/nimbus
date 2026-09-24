-- INSERT ... ON CONFLICT requires SELECT on the conflict target. The API role
-- deduplicates outbox messages by idempotency key and observations by window,
-- so it needs read access to exactly those arbitration columns. Row-level
-- security still limits every row to the authenticated tenant.
GRANT SELECT (id, tenant_id, idempotency_key) ON public.outbox_messages TO nimbus_api;

-- Observation evaluation serializes per pipeline with SELECT ... FOR UPDATE,
-- which requires UPDATE privilege. Grant only the mutable tuning columns so
-- the API can also pause pipelines and adjust thresholds.
GRANT UPDATE (name, status, consecutive_windows, backlog_threshold, age_threshold_seconds,
    incident_workflow_id, updated_at) ON public.monitored_pipelines TO nimbus_api;

-- Workers dispatch incident workflows through the same ingestion path, which
-- holds FOR SHARE on the workflow row; row locks require an UPDATE grant.
GRANT UPDATE (updated_at) ON public.workflows TO nimbus_worker;
