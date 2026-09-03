-- Existing values named arbitrary process environment variables. Clear them
-- before changing the contract so deployments fail closed until an operator
-- provisions a dedicated opaque reference.
UPDATE monitored_pipelines SET external_id_env = NULL WHERE external_id_env IS NOT NULL;

ALTER TABLE monitored_pipelines RENAME COLUMN external_id_env TO external_id_ref;
ALTER TABLE monitored_pipelines ADD CONSTRAINT monitored_pipelines_external_id_ref_format
    CHECK (external_id_ref IS NULL OR external_id_ref ~ '^aws-external-id/[a-z0-9][a-z0-9._-]{0,63}$');
