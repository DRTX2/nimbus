-- OIDC principals authenticate through a verified JWT in the API process. The
-- API role has no privileges on principals or memberships, so a fixed
-- search-path SECURITY DEFINER function provisions the identity and issues a
-- short-lived opaque session. The API binds RLS to the session hash exactly as
-- it does for API keys; the database never trusts a caller-provided tenant
-- setting for API access.
CREATE TABLE public.principal_sessions (
    secret_hash bytea PRIMARY KEY CHECK (length(secret_hash) = 32),
    tenant_id uuid NOT NULL REFERENCES public.tenants(id) ON DELETE CASCADE,
    principal_id uuid NOT NULL REFERENCES public.principals(id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX principal_sessions_principal_expiry_idx ON public.principal_sessions (principal_id, expires_at);
REVOKE ALL ON public.principal_sessions FROM PUBLIC;

CREATE OR REPLACE FUNCTION public.nimbus_api_tenant()
RETURNS uuid
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog
AS $$
    WITH presented AS (
        SELECT decode(nullif(current_setting('app.api_key_hash', true), ''), 'hex') AS secret_hash
    )
    SELECT tenant_id FROM (
        SELECT s.tenant_id
          FROM public.api_key_scopes s, presented p
         WHERE s.secret_hash = p.secret_hash
           AND s.revoked_at IS NULL
           AND (s.expires_at IS NULL OR s.expires_at > now())
        UNION ALL
        SELECT o.tenant_id
          FROM public.principal_sessions o, presented p
         WHERE o.secret_hash = p.secret_hash
           AND o.expires_at > now()
    ) scopes
    LIMIT 1;
$$;
REVOKE ALL ON FUNCTION public.nimbus_api_tenant() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.nimbus_api_tenant() TO nimbus_api;

CREATE FUNCTION public.nimbus_authenticate_oidc(
    oidc_issuer text,
    oidc_subject text,
    oidc_display_name text,
    requested_tenant uuid,
    session_hash bytea,
    session_expires_at timestamptz
)
RETURNS TABLE(principal_id uuid, tenant_id uuid, display_name text, membership_role text)
LANGUAGE plpgsql
VOLATILE
SECURITY DEFINER
SET search_path = pg_catalog
AS $$
DECLARE
    resolved_principal uuid;
    resolved_role text;
    resolved_status text;
    provisioned_rows integer := 0;
BEGIN
    IF oidc_issuer IS NULL OR length(oidc_issuer) NOT BETWEEN 1 AND 512
       OR oidc_subject IS NULL OR length(oidc_subject) NOT BETWEEN 1 AND 255
       OR session_hash IS NULL OR length(session_hash) <> 32
       OR session_expires_at IS NULL OR session_expires_at <= now() THEN
        RETURN;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM public.tenants t WHERE t.id = requested_tenant) THEN
        RETURN;
    END IF;

    -- Tenant tables force RLS for their owner too; scope this transaction.
    PERFORM set_config('app.tenant_id', requested_tenant::text, true);

    INSERT INTO public.principals (kind, issuer, subject, display_name)
    VALUES ('oidc', oidc_issuer, oidc_subject,
            left(coalesce(nullif(btrim(oidc_display_name), ''), oidc_subject), 120))
    ON CONFLICT (issuer, subject) WHERE issuer IS NOT NULL AND subject IS NOT NULL
    DO UPDATE SET display_name = EXCLUDED.display_name
    RETURNING id INTO resolved_principal;

    INSERT INTO public.memberships (tenant_id, principal_id, role)
    VALUES (requested_tenant, resolved_principal, 'viewer')
    ON CONFLICT ON CONSTRAINT memberships_tenant_id_principal_id_key DO NOTHING;
    GET DIAGNOSTICS provisioned_rows = ROW_COUNT;

    SELECT m.role, m.status INTO resolved_role, resolved_status
      FROM public.memberships m
     WHERE m.tenant_id = requested_tenant AND m.principal_id = resolved_principal;
    IF resolved_status IS DISTINCT FROM 'active' THEN
        RETURN;
    END IF;

    IF provisioned_rows > 0 THEN
        INSERT INTO public.audit_events (tenant_id, actor_id, action, resource_type, resource_id, result, metadata)
        VALUES (requested_tenant, resolved_principal, 'oidc.provision', 'principal',
                resolved_principal::text, 'succeeded', jsonb_build_object('role', 'viewer'));
    END IF;

    DELETE FROM public.principal_sessions s
     WHERE s.principal_id = resolved_principal AND s.expires_at <= now();
    INSERT INTO public.principal_sessions (secret_hash, tenant_id, principal_id, expires_at)
    VALUES (session_hash, requested_tenant, resolved_principal,
            least(session_expires_at, now() + interval '1 hour'));

    RETURN QUERY
    SELECT p.id, requested_tenant, p.display_name, resolved_role
      FROM public.principals p
     WHERE p.id = resolved_principal;
END;
$$;
REVOKE ALL ON FUNCTION public.nimbus_authenticate_oidc(text, text, text, uuid, bytea, timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.nimbus_authenticate_oidc(text, text, text, uuid, bytea, timestamptz) TO nimbus_api;

-- Revoking a membership immediately ends any federated session for it.
CREATE FUNCTION public.nimbus_revoke_membership_sessions()
RETURNS trigger
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog
AS $$
BEGIN
    IF NEW.status <> 'active' OR NEW.role IS DISTINCT FROM OLD.role THEN
        DELETE FROM public.principal_sessions s
         WHERE s.tenant_id = NEW.tenant_id AND s.principal_id = NEW.principal_id;
    END IF;
    RETURN NEW;
END;
$$;
REVOKE ALL ON FUNCTION public.nimbus_revoke_membership_sessions() FROM PUBLIC;

CREATE TRIGGER memberships_session_revocation
AFTER UPDATE OF status, role ON public.memberships
FOR EACH ROW EXECUTE FUNCTION public.nimbus_revoke_membership_sessions();

-- Membership administration by owners through the API role.
GRANT UPDATE (role, status, updated_at) ON public.memberships TO nimbus_api;

-- Workers never read federated sessions; they may reclaim expired rows.
CREATE FUNCTION public.nimbus_purge_expired_sessions(batch_limit integer DEFAULT 1000)
RETURNS bigint
LANGUAGE sql
VOLATILE
SECURITY DEFINER
SET search_path = pg_catalog
AS $$
    WITH doomed AS (
        SELECT secret_hash FROM public.principal_sessions
         WHERE expires_at <= now()
         ORDER BY expires_at
         LIMIT greatest(1, least(coalesce(batch_limit, 1000), 10000))
    ), deleted AS (
        DELETE FROM public.principal_sessions s USING doomed d
         WHERE s.secret_hash = d.secret_hash
        RETURNING 1
    )
    SELECT count(*) FROM deleted;
$$;
REVOKE ALL ON FUNCTION public.nimbus_purge_expired_sessions(integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.nimbus_purge_expired_sessions(integer) TO nimbus_worker;

-- Principal names for the authenticated API tenant only. The API role keeps no
-- direct privilege on the global principals table.
CREATE FUNCTION public.nimbus_tenant_principals()
RETURNS TABLE(principal_id uuid, kind text, display_name text)
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog
AS $$
    SELECT p.id, p.kind, p.display_name
      FROM public.principals p
     WHERE EXISTS (
        SELECT 1 FROM public.memberships m
         WHERE m.principal_id = p.id
           AND m.tenant_id = public.nimbus_api_tenant()
     );
$$;
REVOKE ALL ON FUNCTION public.nimbus_tenant_principals() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.nimbus_tenant_principals() TO nimbus_api;
