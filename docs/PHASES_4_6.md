# Product, Governance, And Scale Demo

This guide demonstrates phases 4-6 without AWS credentials or a message broker.
Use the bootstrap owner token from the README.

## Phase 4: Reliability Product

Create a workflow that receives normalized incident data. Replace the URL with a
public test endpoint you control; private and metadata addresses are blocked.

```json
{
  "name": "Incident notification",
  "definition": {
    "schemaVersion": 1,
    "eventType": "nimbus.incident.opened",
    "steps": [{
      "id": "notify",
      "type": "http",
      "http": {
        "method": "POST",
        "url": "https://httpbin.org/status/204",
        "bodyMode": "event"
      }
    }]
  }
}
```

Create a fixture pipeline through `POST /v1/pipelines` and set its returned
workflow ID as `incidentWorkflowId`:

```json
{
  "name": "Orders async pipeline",
  "mode": "fixture",
  "region": "us-east-1",
  "queueArn": "arn:aws:sqs:us-east-1:123456789012:orders",
  "lambdaArn": "arn:aws:lambda:us-east-1:123456789012:function:orders-consumer",
  "dlqArn": "arn:aws:sqs:us-east-1:123456789012:orders-dlq",
  "incidentWorkflowId": "WORKFLOW_ID",
  "consecutiveWindows": 2,
  "backlogThreshold": 100,
  "ageThresholdSeconds": 300
}
```

Send two consecutive five-minute observations to
`POST /v1/pipelines/{id}/observations`. A stalled-consumer example has
`visibleMessages: 200`, `oldestMessageSeconds: 600`, and
`lambdaInvocations: 0`. The second window opens exactly one incident and writes
`incident.opened` to the outbox. Repeating a window cannot create another active
incident. Two healthy windows recover it.

For real AWS mode, register `mode: aws`, a read-only `roleArn`, and an optional
opaque `externalIdRef` such as `aws-external-id/acme-prod`. The API resolves it
only from the operator-managed `NIMBUS_AWS_EXTERNAL_IDS` JSON map. Then call
`POST /v1/pipelines/{id}/checks`. Nimbus uses one bounded CloudWatch
`GetMetricData` request for seven metrics. It never calls `ReceiveMessage` or
CloudWatch Logs. Customer-side least-privilege Terraform is in
`infra/terraform/modules/aws-pipeline-readonly`.

## Phase 5: Governance

Create an approver key with `POST /v1/api-keys`:

```json
{"name":"Portfolio approver","role":"approver","expiresInDays":30}
```

The plaintext key appears once. Create a workflow with
`"approvalRequired": true`, then ingest an event using an operator or owner key.
The execution remains `awaiting_approval` and has no runnable steps. Use the
different approver token at `POST /v1/approvals/{id}/decisions`:

```json
{"decision":"approved","comment":"Evidence and blast radius reviewed"}
```

Approval creates root steps and outbox work atomically. Self-approval returns
`403`; expired or contradictory decisions return `409`. Review evidence via
`GET /v1/audit-events`. API keys can be listed and revoked without restart.

Workflow changes use `POST /v1/workflows/{id}/versions` and explicit activation
at `POST /v1/workflows/{id}/versions/{version}/activations`; running executions
remain bound to their original immutable version.

## Phase 6: Operational Split

The normal local command remains simple:

```bash
make dev
```

To prove independent lifecycle and scaling:

```bash
make dev-down
make dev-split
```

- API: <http://localhost:8080>
- Worker operational health/metrics: <http://localhost:8081/health/ready>

Stop only `nimbus-worker`, ingest an event, and verify the API still returns
`202`. Start the worker again and it drains PostgreSQL. Outbox records use
leases, retries, fencing and bounded polling. PostgreSQL remains the source of
correctness; a future broker or `LISTEN/NOTIFY` may reduce latency without owning work.

## Explicit Boundaries

- Execution and outbox delivery are at-least-once.
- The CloudWatch check is operator-triggered in this beta; automatic schedules
  should be added only after polling-cost validation.
- RLS policies are present, but hosted production must provision separate
  non-owner migration, API, and worker database roles.
- OIDC/JWKS and signed webhook secrets remain hosted-service release gates.
