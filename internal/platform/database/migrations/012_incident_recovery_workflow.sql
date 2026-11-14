-- Pipelines may dispatch a second workflow when an incident recovers, so
-- responders hear "all clear" through the same signed delivery path.
ALTER TABLE public.monitored_pipelines ADD COLUMN recovery_workflow_id uuid;
ALTER TABLE public.monitored_pipelines ADD CONSTRAINT monitored_pipelines_recovery_workflow_fkey
    FOREIGN KEY (tenant_id, recovery_workflow_id) REFERENCES public.workflows(tenant_id, id);
GRANT UPDATE (recovery_workflow_id) ON public.monitored_pipelines TO nimbus_api;
