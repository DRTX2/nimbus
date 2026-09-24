# Nimbus

Nimbus is an open-source guardian for revenue-sensitive AWS asynchronous
pipelines. It turns sustained SQS, Lambda, and DLQ symptoms into explainable,
deduplicated incidents without reading queue payloads or application logs. A
durable event automation engine preserves the evidence needed to answer:
**what happened, why did Nimbus alert, and what ran next?**

The repository is intentionally a modular monolith. PostgreSQL is the source of
truth and durable queue; Redis, NATS, microservices, and managed Kubernetes are
added only when measured constraints justify them.

## Why This Product

Lean AWS teams often discover failed billing, order, import, or notification
pipelines after customers report them. Existing alarms expose isolated metrics,
while engineers still correlate queue age, consumer activity, errors, throttles,
and DLQ growth by hand. Nimbus targets that gap with read-only collection,
deterministic evidence, durable incident state, and a governed response engine.

The commercial wedge is **AWS Async Pipeline Guardian**: detect and explain a
stalled consumer, retry storm, throttling, or DLQ arrival, then track response
and recovery. General workflow execution is enabling infrastructure, not the
market category. See [`docs/PRODUCT.md`](docs/PRODUCT.md).

## Current Vertical

- Create, version, activate, and validate immutable workflow DAGs.
- Accept a tenant-scoped event with an `Idempotency-Key`.
- Create the event, execution, and root steps in one PostgreSQL transaction.
- Claim work concurrently with leases and `FOR UPDATE SKIP LOCKED`.
- Execute `noop` or bounded HTTP steps in parallel as dependencies become ready.
- Retry transient failures with exponential backoff.
- Reject stale worker commits after a lease is lost.
- Expose execution state, 24-hour operational analytics, Prometheus metrics,
  OTLP traces, health probes, OpenAPI, and Scalar documentation.
- Register fixture or AWS SQS/Lambda/DLQ pipelines and evaluate four explainable detectors.
- Deduplicate incidents, retain a timeline, acknowledge them, and recover them automatically.
- Dispatch incident workflows through a transactional outbox with bounded polling.
- Issue and revoke tenant API keys with `owner`, `operator`, `approver`, or `viewer` roles.
- Pause protected executions until a different principal approves or rejects them.
- Deploy the same image as `all`, `api`, or `worker` without introducing a broker.
- Sign in with API keys or OIDC (discovery, JWKS, algorithm allowlist, tenant claim)
  under the same row-level-security boundary; owners manage member roles.
- Operate everything from a same-origin dashboard at `/app/`: incident response
  with the failing pipeline stage highlighted, workflows, runs, approvals, access
  and detection-quality reporting.

Execution is **at-least-once**. HTTP destinations receive a stable
`Idempotency-Key`, but the destination must honor it to prevent duplicate side
effects after a network or process failure.

## Architecture

```text
Client / alert / webhook
          |
          v
  Go API + Scalar docs
          |
          v
   PostgreSQL transaction
   event + execution + steps
          |
          v
 bounded worker pool -----> HTTP connector
 leases / retry / DAG       SSRF guard + timeout
          |
          +----> Prometheus / OTLP / JSON logs
```

Code is grouped by business capability rather than framework layers:

```text
cmd/nimbus                    process composition and lifecycle
internal/workflow             workflow contract and DAG validation
internal/store                tenant-scoped PostgreSQL operations
internal/execution            worker runtime and connectors
internal/detector             explainable pipeline detection rules
internal/awsmonitor           bounded, read-only CloudWatch adapter
internal/httpapi              Huma/Chi API, auth (API key + OIDC), RBAC, dashboard hosting
internal/platform             database and observability
api/openapi.json              generated contract snapshot (see api/README.md)
web                           React + TypeScript dashboard (typed from the contract)
test/features                 Gherkin acceptance specifications (godog)
deploy                        Docker observability and Kubernetes
infra/terraform               low-cost AWS sandbox profile
```

Detailed decisions and guarantees live in [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md).

## Quick Start

Requirements: Go 1.26+, Docker Engine, Docker Compose, Make, and curl.

```bash
make dev
```

Services:

| Service | URL | Credentials |
| --- | --- | --- |
| Dashboard | <http://localhost:8080/app/> | API bearer token below |
| Scalar API docs | <http://localhost:8080/docs> | API bearer token below |
| Grafana | <http://localhost:3000> | `admin` / `admin` for local use |
| Prometheus | <http://localhost:9090> | none, local use only |
| Jaeger | <http://localhost:16686> | none, local use only |

The local bearer token is `nimbus-local-development-key`. It is deliberately
rejected when `NIMBUS_ENV=production`.

### Run The Demo

Create a DAG where two tasks run in parallel after `validate`:

```bash
curl --fail-with-body http://localhost:8080/v1/workflows \
  -H 'Authorization: Bearer nimbus-local-development-key' \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "Order operations",
    "definition": {
      "schemaVersion": 1,
      "eventType": "order.created",
      "steps": [
        {"id": "validate", "type": "noop"},
        {"id": "notify", "type": "noop", "dependsOn": ["validate"]},
        {"id": "audit", "type": "noop", "dependsOn": ["validate"]}
      ]
    }
  }'
```

Copy the returned workflow `id`, then ingest an event:

```bash
curl --fail-with-body http://localhost:8080/v1/workflows/WORKFLOW_ID/events \
  -H 'Authorization: Bearer nimbus-local-development-key' \
  -H 'Idempotency-Key: portfolio-demo-001' \
  -H 'Content-Type: application/json' \
  -d '{"type":"order.created","payload":{"orderId":"ord-42"}}'
```

Copy `executionId` and inspect the execution:

```bash
curl --fail-with-body http://localhost:8080/v1/executions/EXECUTION_ID \
  -H 'Authorization: Bearer nimbus-local-development-key'
```

Send the same event again with the same key. Nimbus returns the original IDs and
`"duplicate": true` without creating another execution.

For the reliability-product demo, follow [`docs/PHASES_4_6.md`](docs/PHASES_4_6.md).
It creates an incident workflow, a fixture pipeline, two degraded windows, an
incident, a second approver API key, and demonstrates split API/worker operation.

Stop containers without deleting their volumes:

```bash
make dev-down
```

## Quality And Security

```bash
make verify          # static, unit, race, contract snapshot
make web-verify      # dashboard types, lint, Gherkin UI scenarios
make test-all        # plus PostgreSQL integration and Gherkin acceptance
make e2e             # browser journeys against a running stack
make security
```

Testing follows a trophy model: most confidence comes from Gherkin acceptance,
integration and end-to-end scenarios that run the real API, worker and
PostgreSQL role split; unit tests are reserved for security and parsing
boundaries. See [`docs/TESTING.md`](docs/TESTING.md).

The canonical verification runs formatting, vet, unit tests, the race detector,
a reproducible build, module checksum verification, and a tidy diff check.
`make security` runs the pinned Go vulnerability scanner. CI also runs CodeQL,
Trivy, dependency review, and builds the hardened, non-root container image.

Security controls implemented today:

- Hashed, persisted, expiring and revocable API keys; tenant and role derive from authentication.
- Permission checks for identity, workflows, pipelines, incidents, approvals, audit, and reads.
- Tenant predicates in every application query.
- Forced PostgreSQL RLS on every tenant-scoped table, with API policy bound to
  the authenticated key capability and separate non-owner API/worker roles in
  split and Kubernetes profiles.
- Approval separation of duties and append-only audit-event API.
- 1 MiB request limit, bounded worker count, response size, retries, and timeouts.
- SSRF protection for HTTP steps, no redirects, and cloud metadata/private IP blocking.
- No authorization headers, secrets, or event payloads in logs.
- Read-only container filesystem, dropped Linux capabilities, and non-root user.
- GitHub Actions pinned by commit, Dependabot, CodeQL, and least-privilege cloud guidance.

This is a production-oriented portfolio beta, not yet a hosted production security boundary.
OIDC/JWKS, signed webhooks with key rotation, and per-tenant ingestion and
concurrency quotas are implemented. Encrypted general-purpose secret references
and restore evidence remain blockers. See
[`SECURITY.md`](SECURITY.md).

## Delivery Profiles

- **Local:** Docker Compose with PostgreSQL, Grafana, Prometheus, and Jaeger.
- **AWS sandbox:** one SSM-managed EC2 instance in the default VPC, no SSH or API
  ingress, no NAT Gateway, encrypted EBS, SSM port forwarding, and an optional budget alarm.
- **Kubernetes lab:** hardened manifests for kind, k3d, or a single-node k3s lab.
- **Split local:** `make dev-split` runs API and worker as independent processes.
- **Production target:** ECS or EKS plus RDS only after user and load validation.

AWS Free Tier eligibility varies by account creation date and region. Public
IPv4, EBS, logs, data transfer, and taxes may cost money. Terraform is never
applied automatically. Read [`docs/DEPLOYMENT.md`](docs/DEPLOYMENT.md) first.

## Roadmap

| Phase | Outcome | Status |
| --- | --- | --- |
| 1. Foundation | API, PostgreSQL, auth context, OpenAPI/Scalar, Docker | Implemented |
| 2. Reliable runtime | DAG, parallel steps, leases, retry, idempotency, SSRF guard | Implemented MVP |
| 3. Operations | Analytics API, metrics, traces, dashboards, CI, IaC profiles | Implemented MVP |
| 4. Product validation | Fixture/CloudWatch detectors, incident workflow, timeline | Implemented beta |
| 5. Governance | RBAC/API keys, approvals, RLS policies, audit, external-ID reference | Implemented beta |
| 6. Scale when measured | Transactional outbox, bounded polling, split API/workers | Implemented beta |
| 6b. Hosted security + UI | OIDC sessions under RLS, member admin, signed webhooks, quotas, dashboard, Gherkin suites | Implemented beta |
| 7. Data/AI | Parquet export, backtesting, explainable anomaly detection, optional summaries | Conditional |

See [`docs/LEARNING_GUIDE.es.md`](docs/LEARNING_GUIDE.es.md) for a guided code
tour in Spanish and [`docs/REQUIREMENTS.md`](docs/REQUIREMENTS.md) for acceptance criteria.
The complete documentation index is [`docs/README.md`](docs/README.md).

## License

MIT. See [`LICENSE`](LICENSE).
