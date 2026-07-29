-- Runtime roles must be provisioned by the deployment platform before this
-- migration. A normal database owner can grant privileges but need not have
-- CREATEROLE.
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'nimbus_api') THEN
        RAISE EXCEPTION 'required runtime role nimbus_api is absent; provision runtime roles before migrating';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'nimbus_worker') THEN
        RAISE EXCEPTION 'required runtime role nimbus_worker is absent; provision runtime roles before migrating';
    END IF;
END $$;

-- This owner-only mirror breaks the pre-tenant authentication/RLS cycle. Runtime
-- roles receive no table privileges; a security-definer trigger keeps it atomic
-- with api_keys changes.
CREATE TABLE public.api_key_scopes (
    secret_hash bytea PRIMARY KEY,
    tenant_id uuid NOT NULL,
    expires_at timestamptz,
    revoked_at timestamptz
);
REVOKE ALL ON public.api_key_scopes FROM PUBLIC;

CREATE FUNCTION public.nimbus_sync_api_key_scope()
RETURNS trigger
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        DELETE FROM public.api_key_scopes WHERE secret_hash = OLD.secret_hash;
        RETURN OLD;
    END IF;

    INSERT INTO public.api_key_scopes (secret_hash, tenant_id, expires_at, revoked_at)
    VALUES (NEW.secret_hash, NEW.tenant_id, NEW.expires_at, NEW.revoked_at)
    ON CONFLICT (secret_hash) DO UPDATE
        SET tenant_id = EXCLUDED.tenant_id,
            expires_at = EXCLUDED.expires_at,
            revoked_at = EXCLUDED.revoked_at;
    RETURN NEW;
END;
$$;
REVOKE ALL ON FUNCTION public.nimbus_sync_api_key_scope() FROM PUBLIC;

CREATE TRIGGER api_keys_scope_sync
AFTER INSERT OR UPDATE OF secret_hash, tenant_id, expires_at, revoked_at OR DELETE
ON public.api_keys
FOR EACH ROW EXECUTE FUNCTION public.nimbus_sync_api_key_scope();

INSERT INTO public.api_key_scopes (secret_hash, tenant_id, expires_at, revoked_at)
SELECT secret_hash, tenant_id, expires_at, revoked_at FROM public.api_keys;

CREATE FUNCTION public.nimbus_api_tenant()
RETURNS uuid
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog
AS $$
    SELECT tenant_id
      FROM public.api_key_scopes
     WHERE secret_hash = decode(nullif(current_setting('app.api_key_hash', true), ''), 'hex')
       AND revoked_at IS NULL
       AND (expires_at IS NULL OR expires_at > now());
$$;
REVOKE ALL ON FUNCTION public.nimbus_api_tenant() FROM PUBLIC;

DO $$
DECLARE table_name text;
BEGIN
    FOREACH table_name IN ARRAY ARRAY[
        'workflows','workflow_versions','events','executions','step_runs','memberships',
        'api_keys','audit_events','approval_requests','monitored_pipelines',
        'pipeline_observations','incidents','incident_timeline','outbox_messages'
    ] LOOP
        EXECUTE format('ALTER TABLE public.%I FORCE ROW LEVEL SECURITY', table_name);
        EXECUTE format('DROP POLICY tenant_isolation ON public.%I', table_name);
        EXECUTE format(
            'CREATE POLICY tenant_api ON public.%I TO nimbus_api USING (tenant_id = public.nimbus_api_tenant()) WITH CHECK (tenant_id = public.nimbus_api_tenant())',
            table_name);
        EXECUTE format(
            'CREATE POLICY tenant_worker ON public.%I TO nimbus_worker USING (tenant_id = nullif(current_setting(''app.tenant_id'', true), '''')::uuid) WITH CHECK (tenant_id = nullif(current_setting(''app.tenant_id'', true), '''')::uuid)',
            table_name);
        EXECUTE format(
            'CREATE POLICY tenant_owner ON public.%I USING (current_user NOT IN (''nimbus_api'', ''nimbus_worker'') AND tenant_id = nullif(current_setting(''app.tenant_id'', true), '''')::uuid) WITH CHECK (current_user NOT IN (''nimbus_api'', ''nimbus_worker'') AND tenant_id = nullif(current_setting(''app.tenant_id'', true), '''')::uuid)',
            table_name);
    END LOOP;
END $$;

CREATE FUNCTION public.nimbus_authenticate_api_key(key_hash bytea)
RETURNS TABLE(principal_id uuid, tenant_id uuid, display_name text, membership_role text)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog
AS $$
DECLARE authenticated_tenant uuid;
BEGIN
    SELECT s.tenant_id INTO authenticated_tenant
      FROM public.api_key_scopes s
     WHERE s.secret_hash = key_hash
       AND s.revoked_at IS NULL
       AND (s.expires_at IS NULL OR s.expires_at > now());
    IF authenticated_tenant IS NULL THEN
        RETURN;
    END IF;

    PERFORM set_config('app.tenant_id', authenticated_tenant::text, true);
    UPDATE public.api_keys k SET last_used_at = now()
     WHERE k.tenant_id = authenticated_tenant AND k.secret_hash = key_hash;

    RETURN QUERY
    SELECT p.id, m.tenant_id, p.display_name, m.role
      FROM public.api_keys k
      JOIN public.principals p ON p.id = k.principal_id
      JOIN public.memberships m ON m.tenant_id = k.tenant_id AND m.principal_id = p.id
     WHERE k.tenant_id = authenticated_tenant
       AND k.secret_hash = key_hash
       AND m.status = 'active';
END;
$$;

CREATE FUNCTION public.nimbus_create_api_principal(principal_name text)
RETURNS uuid
LANGUAGE sql
VOLATILE
SECURITY DEFINER
SET search_path = pg_catalog
AS $$
    INSERT INTO public.principals (kind, display_name)
    VALUES ('api_key', principal_name)
    RETURNING id;
$$;

CREATE FUNCTION public.nimbus_system_principal(requested_tenant uuid)
RETURNS TABLE(principal_id uuid, display_name text, membership_role text)
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog
AS $$
    SELECT p.id, p.display_name, m.role
      FROM public.memberships m
      JOIN public.principals p ON p.id = m.principal_id
     WHERE requested_tenant = nullif(current_setting('app.tenant_id', true), '')::uuid
       AND m.tenant_id = requested_tenant
       AND m.status = 'active'
       AND p.kind = 'system'
     LIMIT 1;
$$;

CREATE FUNCTION public.nimbus_worker_tenants(after_tenant uuid DEFAULT NULL, tenant_limit integer DEFAULT 100)
RETURNS TABLE(tenant_id uuid)
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog
AS $$
    SELECT id
      FROM public.tenants
     ORDER BY CASE WHEN after_tenant IS NULL OR id > after_tenant THEN 0 ELSE 1 END, id
     LIMIT greatest(1, least(coalesce(tenant_limit, 100), 1000));
$$;

CREATE FUNCTION public.nimbus_verify_migration(migration_name text, migration_checksum text)
RETURNS boolean
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog
AS $$
    SELECT EXISTS (
        SELECT 1 FROM public.schema_migrations
         WHERE name = migration_name AND checksum = migration_checksum
    );
$$;

REVOKE ALL ON FUNCTION public.nimbus_authenticate_api_key(bytea) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.nimbus_create_api_principal(text) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.nimbus_system_principal(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.nimbus_worker_tenants(uuid, integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.nimbus_verify_migration(text, text) FROM PUBLIC;
REVOKE ALL ON public.schema_migrations FROM PUBLIC;

GRANT USAGE ON SCHEMA public TO nimbus_api;
GRANT SELECT, INSERT, UPDATE ON public.workflows TO nimbus_api;
GRANT SELECT, INSERT ON public.workflow_versions, public.events, public.step_runs,
    public.memberships, public.pipeline_observations, public.incident_timeline TO nimbus_api;
GRANT SELECT, INSERT, UPDATE ON public.executions, public.api_keys,
    public.approval_requests, public.incidents TO nimbus_api;
GRANT SELECT, INSERT ON public.audit_events TO nimbus_api;
GRANT SELECT, INSERT ON public.monitored_pipelines TO nimbus_api;
GRANT INSERT ON public.outbox_messages TO nimbus_api;
GRANT EXECUTE ON FUNCTION public.nimbus_api_tenant() TO nimbus_api;
GRANT EXECUTE ON FUNCTION public.nimbus_authenticate_api_key(bytea) TO nimbus_api;
GRANT EXECUTE ON FUNCTION public.nimbus_create_api_principal(text) TO nimbus_api;
GRANT EXECUTE ON FUNCTION public.nimbus_verify_migration(text, text) TO nimbus_api;

GRANT USAGE ON SCHEMA public TO nimbus_worker;
GRANT SELECT ON public.workflows, public.workflow_versions, public.events, public.executions,
    public.step_runs, public.memberships, public.approval_requests,
    public.monitored_pipelines, public.incidents, public.outbox_messages TO nimbus_worker;
GRANT INSERT ON public.events, public.executions, public.step_runs, public.approval_requests,
    public.outbox_messages, public.audit_events TO nimbus_worker;
GRANT UPDATE ON public.executions, public.step_runs, public.outbox_messages TO nimbus_worker;
GRANT EXECUTE ON FUNCTION public.nimbus_system_principal(uuid) TO nimbus_worker;
GRANT EXECUTE ON FUNCTION public.nimbus_worker_tenants(uuid, integer) TO nimbus_worker;
GRANT EXECUTE ON FUNCTION public.nimbus_verify_migration(text, text) TO nimbus_worker;
