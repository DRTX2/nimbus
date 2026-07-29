# Architecture

## Decisions

### Modular monolith first

Nimbus defaults to one Go process containing API and a bounded worker pool. The
same image also runs as independent `api` and `worker` components, without
introducing an internal network protocol or changing PostgreSQL authority.

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
- The HTTP connector resolves DNS itself and refuses private, loopback,
  link-local, multicast, and unspecified addresses unless an operator explicitly
  allowlists the hostname.
- Redirects are disabled to prevent SSRF validation bypass.
- Bodies, headers, retries, leases, workers, and server timeouts are bounded.

Fine-grained role authorization and RLS policies are present. Hosted production
still requires OIDC membership and dedicated non-owner DB roles that establish
`app.tenant_id` per transaction; the local schema owner bypasses RLS.

## Evolution Gates

| Change | Add only when |
| --- | --- |
| Keep API and worker split | Optional profile is implemented; use it when lifecycle or scaling independence is measured |
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
