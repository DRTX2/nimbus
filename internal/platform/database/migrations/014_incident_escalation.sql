-- Escalation: when the workflow that should mitigate an incident fails for good, the incident is
-- marked escalated and a third workflow (triggered by nimbus.incident.escalated) pages a human.
-- It must not depend on the failed workflow: its steps were cancelled with it.
ALTER TABLE public.monitored_pipelines ADD COLUMN escalation_workflow_id uuid;
ALTER TABLE public.monitored_pipelines ADD CONSTRAINT monitored_pipelines_escalation_workflow_fkey
    FOREIGN KEY (tenant_id, escalation_workflow_id) REFERENCES public.workflows(tenant_id, id);
ALTER TABLE public.incidents ADD COLUMN escalated_at timestamptz;
