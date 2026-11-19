-- Push-mode pipelines may report on shorter windows than CloudWatch's 5 minutes, like monitoring agents
-- that sample every 30-60 s. AWS pipelines stay at 300 s (enforced in the API). Existing rows keep 300.
ALTER TABLE public.monitored_pipelines
    ADD COLUMN window_seconds integer NOT NULL DEFAULT 300
    CONSTRAINT monitored_pipelines_window_seconds_check CHECK (window_seconds IN (30, 60, 300));
