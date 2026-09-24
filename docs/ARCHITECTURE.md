# Architecture

## Decisions

### Modular monolith first

Nimbus remains one Go codebase and binary, while the default deployment runs
independent `api` and `worker` components so each can use a least-privilege
database role. This does not introduce an internal network protocol or change
PostgreSQL authority.

### PostgreSQL is the authority

An ingestion transaction writes the event, execution, and root step runs. A
worker claims one row with `FOR UPDATE SKIP LOCKED`, a lease owner, and expiry.
The transaction is closed before external I/O. Redis or NATS may later reduce
poll latency, but they will not own execution state.

### At-least-once execution

A process can fail after an HTTP destination accepted a request but before the
step result commits. Nimbus therefore cannot promise exactly-once behavior over
arbitrary HTTP. It sends `Idempotency-Key: <execution-id>:<step-id>`, retries only
classified transient failures, and documents the destination requirement.

### Immutable workflow versions

Executions reference a concrete workflow version. The API creates new immutable
versions and activates them explicitly without changing existing executions.

### Migrations are a release phase

The `migrate` component is the only process that applies embedded migrations and
bootstraps the initial local identity. It serializes concurrent migrators with a
PostgreSQL advisory lock and records a SHA-256 checksum for every migration.
API and worker processes verify that exact history at startup and fail closed;
they do not execute DDL or read `schema_migrations` directly. Verification uses
a boolean, fixed-search-path `SECURITY DEFINER` function.

## Ingestion Sequence

```text
API              PostgreSQL                         Worker
 |                   |                                |
 | validate DAG      |                                |
 | BEGIN             |                                |
 | insert event ---->| unique idempotency key         |
 | insert execution->|                                |
 | insert roots ---->|                                |
 | COMMIT            |                                |
 | 202               |<-- claim + lease --------------|
 |                   |                                | external I/O
 |                   |<-- result + ready dependents ---|
```

The uniqueness key `(tenant_id, workflow_id, external_id)` returns the original
event and execution for duplicate ingestion.

## State Model

Execution states include `awaiting_approval`, `queued`, `running`, `succeeded`,
`failed` and `cancelled`. Approval moves an execution from `awaiting_approval`
to `queued`; rejection or expiry cancels it. Step states are `queued`, `running`,
`succeeded`, `failed` and `cancelled`, with `running -> queued` for retry or lease
reclaim. Terminal states are not reopened.

Dependent steps are inserted only after every declared dependency succeeded.
Independent ready steps can be claimed by different workers concurrently.

## Security Boundaries

- The authentication middleware places the tenant in context. Request bodies
  and path parameters never choose a tenant.
- Every store method requires a tenant ID and every tenant table query filters it.
- Tenant-facing store transactions use transaction-local settings only. API
  requests carry an opaque API-key hash capability from authentication; API RLS
  resolves that capability through an owner-only scope mirror and does not trust
  `app.tenant_id`. Worker and owner policies use `app.tenant_id` separately.
- API-key lookup is the only pre-tenant API operation. Fixed-search-path
  `SECURITY DEFINER` functions resolve the owner-only scope mirror without
  granting runtime access to API-key hashes.
- Workers enumerate at most 100 tenant IDs through a worker-only function and
  rotate the cursor before claiming steps or outbox rows inside one tenant.
- The HTTP connector resolves DNS itself and refuses private, loopback,
  link-local, multicast, and unspecified addresses unless an operator explicitly
  allowlists the hostname.
- Redirects are disabled to prevent SSRF validation bypass.
- Bodies, headers, retries, leases, workers, and server timeouts are bounded.

The deployment platform must create `nimbus_api` and `nimbus_worker` before the
owner runs migrations. Migration 005 fails clearly when either is absent, then
grants them without requiring `CREATEROLE`, creating passwords, or granting
`BYPASSRLS`. Compose does this only for a fresh local PostgreSQL volume with
documented local-only credentials. The split profile uses those roles; the
convenience monolith still uses the local owner and is not a hosted isolation
boundary.

### Federated identity

OIDC access tokens are verified in the API process (discovery, JWKS, algorithm
allowlist, issuer, audience, expiry, optional tenant claim). The API then calls
`nimbus_authenticate_oidc`, a fixed search-path `SECURITY DEFINER` function
that provisions the principal as a viewer on first sight and issues an opaque
session whose hash lives in an owner-only table. `nimbus_api_tenant()` accepts
either an API-key hash or a live session hash, so both credential kinds share
one RLS boundary. Membership changes delete sessions by trigger; workers purge
expired sessions in bounded batches.

## Extension Points

- **Connectors.** Step types implement `execution.Connector` and register in a
  `Registry`. Failures are classified with `StepError`; unclassified errors are
  permanent, so a new connector cannot accidentally multiply external effects.
- **Contract-first clients.** `api/openapi.json` is generated from the handlers
  and guarded by a test; the dashboard derives its types from it, so a breaking
  API change fails type checking in `web/`.
- **Pagination.** List endpoints use opaque keyset cursors, so ordering keys can
  change without breaking clients.
- **Identity providers.** OIDC configuration is data (issuer, audience,
  algorithms, tenant claim); a second provider would add a verifier, not a new
  authorization path.

## Dashboard

`web/` is a React + TypeScript single-page application built by Vite and
served by the API at `/app/` (set `NIMBUS_WEB_DIR`; the container image does
this). Being same-origin, it needs no CORS and runs under a strict CSP. It is
organized by capability (`src/features/*`) over a shared typed client
(`src/shared/api`), session (`src/shared/auth`) and UI kit (`src/shared/ui`).
Permissions come from `GET /v1/me`; the client never re-implements RBAC.

## Evolution Gates

| Change | Add only when |
| --- | --- |
| Separate API/worker services | Already required for database-role isolation; scale them independently only with measured need |
| NATS JetStream | Polling latency or fan-out is a measured bottleneck; use transactional outbox |
| Redis | Distributed rate limiting is required; never store authoritative execution state |
| Table partitioning | Retention and query plans show material degradation |
| Kubernetes | Multiple workloads/team policy justify orchestration overhead |
| Analytical store | PostgreSQL rollups cannot meet report latency or retention cost |
| Multi-region | Paying customers require regional recovery objectives |

## Target Reliability

Initial objectives for a hosted beta:

- API availability SLO: 99.9% monthly.
- Accepted event durability: committed to PostgreSQL before `202`.
- Detection-to-queue latency p95: under 2 seconds locally.
- Recovery point objective: 5 minutes with managed PostgreSQL backups.
- Recovery time objective: 60 minutes until restore drills prove better.

These are targets, not claims; dashboards and restore evidence must support them.
