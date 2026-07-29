# Deployment Guide

## Local

`make dev` builds and starts PostgreSQL, runs the one-shot `nimbus-migrate`
component, and starts Nimbus, Prometheus, Grafana, and Jaeger. API and worker
components only verify the embedded migration checksums; they never execute DDL
or bootstrap identities. Use `docker compose ps`, `/health/ready`, and the demo
in the README as smoke checks. `docker compose down -v` deletes local data and
is intentionally not a Make target.

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

The base manifests expect an external PostgreSQL URL and a `nimbus-secrets`
Secret. They work for learning on kind, k3d, or k3s; they do not install a
database or ingress controller.

```bash
kubectl create namespace nimbus
kubectl -n nimbus create secret generic nimbus-secrets \
  --from-literal=NIMBUS_API_KEY='replace-me-with-a-long-random-token' \
  --from-literal=NIMBUS_DATABASE_URL='postgres://...'
kubectl apply -k deploy/k8s/base
```

The base includes a migration Job. A controlled deployment must wait for
`job/nimbus-migrate` to complete before considering API or worker readiness;
runtime pods fail closed while migrations are missing or checksums differ.
Change the image from the placeholder before applying. Production needs separate
migrator/runtime database credentials, TLS, network policies supported by the
cluster CNI, managed PostgreSQL, autoscaling evidence, disruption budgets,
backups, and a secret-store integration.
