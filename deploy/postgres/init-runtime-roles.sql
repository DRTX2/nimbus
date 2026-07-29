-- Local development only. Production roles and passwords must be provisioned
-- by the database platform before Nimbus migrations run.
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'nimbus_api') THEN
        CREATE ROLE nimbus_api LOGIN PASSWORD 'nimbus-api-local';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'nimbus_worker') THEN
        CREATE ROLE nimbus_worker LOGIN PASSWORD 'nimbus-worker-local';
    END IF;
END $$;
