ALTER TABLE monitored_pipelines
    ADD COLUMN IF NOT EXISTS external_id_env text CHECK (length(external_id_env) <= 128);
