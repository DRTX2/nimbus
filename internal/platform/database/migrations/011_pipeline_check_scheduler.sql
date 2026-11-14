-- Workers check active AWS pipelines once per closed five-minute window.
-- next_check_at is the claim cursor: a worker advances it under
-- FOR UPDATE SKIP LOCKED before calling AWS, so replicas never check the same
-- pipeline window twice. NULL means due now.
ALTER TABLE public.monitored_pipelines ADD COLUMN next_check_at timestamptz;
CREATE INDEX monitored_pipelines_due_check_idx ON public.monitored_pipelines (tenant_id, next_check_at)
    WHERE mode = 'aws' AND status = 'active';

-- Scheduled checks evaluate observations through the same code path as the
-- API. The claim and the observation lock need UPDATE on one column only;
-- workers still cannot retune or pause pipelines.
GRANT UPDATE (next_check_at) ON public.monitored_pipelines TO nimbus_worker;
GRANT SELECT, INSERT ON public.pipeline_observations, public.incident_timeline TO nimbus_worker;
GRANT INSERT, UPDATE ON public.incidents TO nimbus_worker;

-- The shared tenant CloudWatch budget counts pipeline.check audit events.
-- Workers may count them but never read audit metadata or other actions' details.
GRANT SELECT (tenant_id, action, occurred_at) ON public.audit_events TO nimbus_worker;

-- Lists tenants with due AWS pipeline checks so the scheduler never pages
-- past them. FORCE RLS stays active for the owner, so each tenant is
-- inspected under its own scope, as in nimbus_operational_queue_metrics.
CREATE FUNCTION public.nimbus_due_check_tenants(tenant_limit integer DEFAULT 100)
RETURNS TABLE(tenant_id uuid)
LANGUAGE plpgsql
VOLATILE
SECURITY DEFINER
SET search_path = pg_catalog
AS $$
DECLARE
    tenant record;
    previous_tenant text := current_setting('app.tenant_id', true);
    bounded_limit integer := greatest(1, least(coalesce(tenant_limit, 100), 1000));
    emitted integer := 0;
BEGIN
    FOR tenant IN SELECT t.id FROM public.tenants t ORDER BY t.id LOOP
        PERFORM set_config('app.tenant_id', tenant.id::text, true);
        IF EXISTS (
            SELECT 1 FROM public.monitored_pipelines p
             WHERE p.tenant_id = tenant.id AND p.mode = 'aws' AND p.status = 'active'
               AND (p.next_check_at IS NULL OR p.next_check_at <= now())
        ) THEN
            tenant_id := tenant.id;
            RETURN NEXT;
            emitted := emitted + 1;
            EXIT WHEN emitted >= bounded_limit;
        END IF;
    END LOOP;
    PERFORM set_config('app.tenant_id', coalesce(previous_tenant, ''), true);
END;
$$;

REVOKE ALL ON FUNCTION public.nimbus_due_check_tenants(integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.nimbus_due_check_tenants(integer) TO nimbus_worker;
