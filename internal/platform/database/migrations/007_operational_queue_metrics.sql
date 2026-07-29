CREATE FUNCTION public.nimbus_operational_queue_metrics()
RETURNS TABLE(
    runnable_steps bigint,
    oldest_runnable_step_age_seconds double precision,
    expired_step_leases bigint,
    publishable_outbox_messages bigint,
    oldest_publishable_outbox_age_seconds double precision,
    exhausted_unpublished_outbox_messages bigint
)
LANGUAGE plpgsql
VOLATILE
SECURITY DEFINER
SET search_path = pg_catalog
AS $$
DECLARE
    tenant record;
    previous_tenant text := current_setting('app.tenant_id', true);
    observed_at timestamptz := statement_timestamp();
    tenant_count bigint;
    tenant_age double precision;
BEGIN
    runnable_steps := 0;
    oldest_runnable_step_age_seconds := 0;
    expired_step_leases := 0;
    publishable_outbox_messages := 0;
    oldest_publishable_outbox_age_seconds := 0;
    exhausted_unpublished_outbox_messages := 0;

    -- FORCE RLS remains active for the function owner. Aggregate one tenant at
    -- a time so the function does not weaken the tenant policies it summarizes.
    FOR tenant IN SELECT id FROM public.tenants LOOP
        PERFORM set_config('app.tenant_id', tenant.id::text, true);

        SELECT count(*), coalesce(greatest(0, extract(epoch FROM observed_at - min(
                   CASE WHEN s.status = 'queued' THEN s.available_at ELSE s.lease_until END))), 0)
          INTO tenant_count, tenant_age
          FROM public.step_runs s
          JOIN public.executions e ON e.tenant_id = s.tenant_id AND e.id = s.execution_id
         WHERE ((s.status = 'queued' AND s.available_at <= observed_at)
             OR (s.status = 'running' AND s.lease_until < observed_at))
           AND s.tenant_id = tenant.id
           AND s.attempt < s.max_attempts
           AND e.status IN ('queued', 'running');
        runnable_steps := runnable_steps + tenant_count;
        oldest_runnable_step_age_seconds := greatest(oldest_runnable_step_age_seconds, tenant_age);

        SELECT count(*) INTO tenant_count FROM public.step_runs s
         WHERE s.tenant_id = tenant.id
           AND s.status = 'running' AND s.lease_until < observed_at;
        expired_step_leases := expired_step_leases + tenant_count;

        SELECT count(*), coalesce(greatest(0, extract(epoch FROM observed_at - min(
                   CASE WHEN o.lease_until IS NULL THEN o.available_at
                        ELSE greatest(o.available_at, o.lease_until) END))), 0)
          INTO tenant_count, tenant_age
          FROM public.outbox_messages o
         WHERE o.published_at IS NULL
           AND o.tenant_id = tenant.id
           AND o.attempt < o.max_attempts
           AND o.available_at <= observed_at
           AND (o.lease_until IS NULL OR o.lease_until < observed_at);
        publishable_outbox_messages := publishable_outbox_messages + tenant_count;
        oldest_publishable_outbox_age_seconds := greatest(oldest_publishable_outbox_age_seconds, tenant_age);

        SELECT count(*) INTO tenant_count FROM public.outbox_messages o
         WHERE o.tenant_id = tenant.id
           AND o.published_at IS NULL AND o.attempt >= o.max_attempts;
        exhausted_unpublished_outbox_messages := exhausted_unpublished_outbox_messages + tenant_count;
    END LOOP;

    PERFORM set_config('app.tenant_id', coalesce(previous_tenant, ''), true);
    RETURN NEXT;
END;
$$;

REVOKE ALL ON FUNCTION public.nimbus_operational_queue_metrics() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.nimbus_operational_queue_metrics() TO nimbus_worker;
