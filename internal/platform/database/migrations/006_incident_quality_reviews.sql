CREATE TABLE public.incident_reviews (
    id uuid NOT NULL DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    incident_id uuid NOT NULL,
    revision integer NOT NULL CHECK (revision > 0),
    disposition text NOT NULL CHECK (disposition IN ('true_positive', 'false_positive', 'expected_behavior', 'duplicate')),
    reviewer_principal_id uuid NOT NULL,
    comment text CHECK (length(comment) <= 500),
    reviewed_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, id),
    UNIQUE (tenant_id, incident_id, revision),
    FOREIGN KEY (tenant_id, incident_id) REFERENCES public.incidents(tenant_id, id) ON DELETE CASCADE,
    FOREIGN KEY (tenant_id, reviewer_principal_id) REFERENCES public.memberships(tenant_id, principal_id)
);

COMMENT ON COLUMN public.incident_reviews.disposition IS
    'Review classification: true_positive, false_positive, expected_behavior, or duplicate. Independent of incident lifecycle status.';

ALTER TABLE public.incident_reviews ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.incident_reviews FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_api ON public.incident_reviews TO nimbus_api
    USING (tenant_id = public.nimbus_api_tenant())
    WITH CHECK (tenant_id = public.nimbus_api_tenant());
CREATE POLICY tenant_owner ON public.incident_reviews
    USING (current_user NOT IN ('nimbus_api', 'nimbus_worker') AND tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (current_user NOT IN ('nimbus_api', 'nimbus_worker') AND tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT ON public.incident_reviews TO nimbus_api;
