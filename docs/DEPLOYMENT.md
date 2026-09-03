# Deployment Guide

## Local

`make dev` builds the local monolith with the database owner for convenience.
`make dev-split` runs API and worker independently with the non-owner
`nimbus_api` and `nimbus_worker` roles. Both profiles provision those local-only
roles on first database initialization and run the one-shot `nimbus-migrate`
component. Runtime components only verify embedded migration checksums; they
never execute DDL or bootstrap identities. The monolith is not evidence of a
hosted database boundary; use the split profile for RLS validation. Use
`docker compose ps`, `/health/ready`, and the README demo as smoke checks.
`docker compose down -v` deletes local data and is intentionally not a Make
target.

Prometheus uses profile-specific scrape configuration: the monolith profile
scrapes `nimbus:8080`, while the split override scrapes both `nimbus-api:8080`
and `nimbus-worker:8081`. Operational queue alert definitions and runbooks are
documented in [SRE_OPERATIONS.md](SRE_OPERATIONS.md).

The init script does not run again for an existing PostgreSQL volume. Provision
the two roles explicitly before upgrading such a volume; do not delete an active
volume to force initialization.

For the local Compose database, stop the application components, run the
idempotent provisioning script already mounted in PostgreSQL, then rerun the
migrator:

```bash
docker compose -f compose.yaml -f compose.split.yaml stop \
  nimbus-api nimbus-worker nimbus-migrate
docker compose exec -T postgres psql -U nimbus -d nimbus \
  < deploy/postgres/init-runtime-roles.sql
docker compose -f compose.yaml -f compose.split.yaml up nimbus-migrate
docker compose -f compose.yaml -f compose.split.yaml up -d nimbus-api nimbus-worker
```

Migration 005 intentionally fails if either runtime role is absent. Production
operators must use platform-managed passwords instead of the local script.

## AWS Sandbox

The Terraform profile creates one EC2 instance in the default VPC, IAM access
through Systems Manager, encrypted EBS, no inbound rules, IMDSv2, and an
optional monthly budget. It does not create SSH access, NAT Gateway, load
balancer, managed Kubernetes, or RDS.

This profile is for a portfolio demo, not production: PostgreSQL shares the
instance, access depends on an SSM tunnel, and a single availability zone is a
single point of failure.

Prerequisites:

1. An AWS sandbox account with MFA and a budget notification email.
2. AWS CLI authenticated with short-lived SSO credentials.
3. Terraform 1.9+ or OpenTofu 1.8+.
4. A public container image built from this repository.
5. A pre-created encrypted SSM parameter containing an env file.

Example parameter value:

```dotenv
NIMBUS_ENV=production
NIMBUS_API_KEY=replace-with-at-least-32-random-characters
POSTGRES_PASSWORD=replace-with-a-different-random-value
NIMBUS_DATABASE_URL=postgres://nimbus:URL_ENCODED_PASSWORD@postgres:5432/nimbus?sslmode=disable
```

Plan only:

```bash
terraform -chdir=infra/terraform/environments/sandbox init
terraform -chdir=infra/terraform/environments/sandbox plan \
  -var='container_image=ghcr.io/OWNER/nimbus:SHA' \
  -var='config_parameter_name=/nimbus/sandbox/env' \
  -var='budget_email=you@example.com'
```

After apply, run the `ssm_port_forward_command` output and open
`http://localhost:8080`; port 8080 has no public ingress, so the bearer token is
carried inside the authenticated SSM tunnel.

Review price and security changes before any apply. AWS pricing and Free Tier
rules vary. A public IPv4 address, EBS, traffic, logs, and taxes can incur cost
even when instance compute is eligible for credits.

## Kubernetes Lab

The base manifests expect external PostgreSQL URLs and a `nimbus-secrets`
Secret. They work for learning on kind, k3d, or k3s; they do not install a
database or ingress controller.

```bash
kubectl create namespace nimbus
kubectl -n nimbus create secret generic nimbus-secrets \
  --from-literal=NIMBUS_API_KEY='replace-me-with-a-long-random-token' \
  --from-literal=NIMBUS_API_DATABASE_URL='postgres://nimbus_api:...@postgres.example:5432/nimbus?sslmode=require' \
  --from-literal=NIMBUS_WORKER_DATABASE_URL='postgres://nimbus_worker:...@postgres.example:5432/nimbus?sslmode=require' \
  --from-literal=NIMBUS_MIGRATOR_DATABASE_URL='postgres://nimbus_owner:...@postgres.example:5432/nimbus?sslmode=require' \
  --from-literal=NIMBUS_AWS_EXTERNAL_IDS='{"aws-external-id/acme-prod":"customer-generated-value"}'
kubectl apply -k deploy/k8s/base
```

The base includes a migration Job. A controlled deployment must wait for
`job/nimbus-migrate` to complete before considering API or worker readiness;
runtime pods fail closed while migrations are missing or checksums differ. The
database platform must create non-owner `nimbus_api` and `nimbus_worker` roles
before this Job runs; migration fails if either is absent and contains no
hardcoded passwords. Change the image from the placeholder before applying. Production
needs TLS, network policies supported by the
cluster CNI, managed PostgreSQL, autoscaling evidence, disruption budgets,
backups, and a secret-store integration.

Each pod receives explicit secret keys instead of the complete Secret. The API
receives only its database URL and optional AWS external-ID map, the worker only
its database URL, and the migration Job only the owner URL and bootstrap key.
Migration 008 clears legacy environment-variable references; recreate them as
opaque `aws-external-id/...` aliases after upgrading.
