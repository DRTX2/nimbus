# Requirements And Acceptance Criteria

## Functional MVP

| ID | Requirement | Acceptance criterion |
| --- | --- | --- |
| F-01 | Create workflow | Valid DAG returns an ID and immutable version 1 |
| F-02 | Reject invalid contract | Duplicate IDs, cycles, missing dependencies, bad URLs, and out-of-range limits return 400 |
| F-03 | Idempotent ingest | Repeated tenant/workflow/key returns the original event and execution |
| F-04 | Durable execution | Restarting a worker does not lose a committed step |
| F-05 | Parallel DAG | Multiple ready steps can be leased independently |
| F-06 | Retry | Timeout, 408, 429, and 5xx retry up to the configured bound |
| F-07 | Permanent failure | Other non-2xx responses fail without retry and fail the execution |
| F-08 | Visibility | Execution endpoint returns every created step and attempt count |
| F-09 | Operations | Health, Prometheus metrics, OTLP traces, logs, and analytics are available |
| F-10 | API contract | Scalar renders the generated OpenAPI document |

## Non-Functional MVP

- All request bodies are limited to 1 MiB and HTTP step bodies to 64 KiB.
- HTTP step timeout is 1-30 seconds and attempts are 1-10.
- Worker concurrency is 1-64 and no goroutine is created per queued database row.
- PostgreSQL is unavailable means readiness fails; liveness remains healthy.
- Logs do not contain bearer tokens, authorization headers, or event payloads.
- Container runs non-root with no capabilities and a read-only root filesystem.
- Graceful shutdown stops accepting requests and cancels worker contexts.
- Ordinary changes pass format, vet, tests, race detector, and build.

## Product Beta

| ID | Requirement | Acceptance criterion |
| --- | --- | --- |
| P-01 | Pipeline monitoring | Fixture and AWS modes model SQS, Lambda, and DLQ resources without reading messages or logs |
| P-02 | Explainable detection | Stalled consumer, throttling, non-empty DLQ, and retry storm expose numeric evidence |
| P-03 | Sustained windows | An incident opens only after the configured number of breached windows |
| P-04 | Incident lifecycle | Active incidents are unique per detector and recover after sustained healthy windows |
| P-05 | Notification workflow | `incident.opened` and `incident.recovered` outbox records idempotently ingest `nimbus.incident.opened` / `nimbus.incident.recovered` with incident context |
| G-01 | Identity | API keys are hashed, expiring, revocable, returned once, and tenant scoped |
| G-02 | Authorization | Four roles enforce permissions in every product handler |
| G-03 | Approval | Requester cannot approve their own protected execution |
| G-04 | Audit | Governed changes write structured audit events without secrets or payloads |
| G-05 | Defense in depth | Tenant tables define RLS `USING` and `WITH CHECK` policies |
| S-01 | Outbox | Domain state and availability messages commit atomically and publish at least once |
| S-02 | Split operation | One image starts as `all`, `api`, or `worker`; polling remains the correctness fallback |

## Hosted Beta Blockers

- Automated identity-provider membership lifecycle (SCIM); OIDC/JWKS sign-in is implemented.
- Dedicated migration/API/worker PostgreSQL roles plus cross-tenant RLS integration tests.
- General secret references backed by SSM or Secrets Manager; current AWS external IDs use an environment reference.
- Database privilege preventing audit `UPDATE`/`DELETE` for runtime roles.
- Receiver-side replay windows and egress policy administration (signing and rotation are implemented).
- Backup restore drill, migration rollback procedure, and incident runbooks.
- Privacy policy, retention/deletion controls, subprocessor inventory, and data export.

## Quality Strategy

Testing follows the trophy model described in [`TESTING.md`](TESTING.md):
Gherkin acceptance and end-to-end scenarios over the real system carry most of
the confidence, unit tests are reserved for security and parsing boundaries, and
a generated OpenAPI snapshot guards the contract.
