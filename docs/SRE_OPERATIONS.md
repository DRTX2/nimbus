# Nimbus Operational Queue Signals

These signals and alerts are initial beta operating targets. They are not proven
service-level objectives: Nimbus has not yet accumulated production traffic,
capacity, or incident data sufficient to establish an SLO or error budget.

## Initial Beta Objectives

The thresholds below are candidate operational objectives for queue health and
incident response. They intentionally do not define customer availability,
monthly compliance, or an error budget. Revisit them after representative load
tests and measured production baselines; do not present them as attained SLOs.

## Signal Definitions

All seven gauges are unlabeled PostgreSQL-wide aggregates. They are refreshed only
by `worker` and `all` components every 15 seconds by default. A refresh has a
five-second deadline; on failure the exporter retains its last good values. With
multiple workers, Prometheus recording rules use `max`, because each worker reads
the same source of truth and summing would multiply the backlog.

| Signal | Definition | Initial beta alert target |
| --- | --- | --- |
| `nimbus_runnable_steps` | Queued, due steps or expired running leases with attempts remaining, whose execution is active | More than 100 for 10 minutes |
| `nimbus_oldest_runnable_step_age_seconds` | Age since the oldest due time or expired lease became runnable, clamped to zero | More than 300 seconds with runnable work for 10 minutes |
| `nimbus_expired_step_leases` | Running steps whose lease is past its expiry, including final-attempt leases awaiting reaping | More than zero for 5 minutes |
| `nimbus_publishable_outbox_messages` | Unpublished, due, unleased or expired-lease messages with attempts remaining | Oldest age over 300 seconds with publishable work for 10 minutes |
| `nimbus_oldest_publishable_outbox_age_seconds` | Age since the oldest message became publishable, clamped to zero | Used with the publishable count above |
| `nimbus_exhausted_unpublished_outbox_messages` | Unpublished messages whose attempt count reached the maximum | More than zero for 5 minutes |
| `nimbus_operational_metrics_last_success_timestamp_seconds` | Unix timestamp of the last successful aggregate refresh | Missing or older than 120 seconds for 2 minutes |

Future `available_at` retries are deliberately excluded from runnable and
publishable counts. Exhausted outbox messages are counted separately and never
included as publishable work.

## Runnable Step Backlog

First diagnosis: confirm the worker scrape is healthy, compare worker replicas
and configured concurrency, then inspect safe worker error metadata and database
connectivity. Check whether expired leases are contributing to the runnable
count before changing capacity or retry settings. Do not manually mark steps
complete: execution is at-least-once and stale lease owners must not commit.

## Outbox Publication

First diagnosis: confirm the outbox publisher loop is running, check database
connectivity, and inspect sanitized publisher errors. Distinguish delayed retries
from currently publishable messages before changing retry behavior. Publication
is at-least-once, so downstream effects must remain idempotent.

## Expired Step Leases

First diagnosis: compare lease duration with step timeout, look for worker
termination or database stalls, and confirm expired work is being reclaimed.
An expired lease does not prove a step failed; another worker may retry it.

## Exhausted Outbox

First diagnosis: inspect the affected records through an authorized operational
database procedure, classify the sanitized last error, and verify downstream
idempotency before arranging a controlled retry. Never reset attempts broadly or
publish payloads manually from logs.

## Stale Or Absent Metrics

First diagnosis: confirm at least one worker target is up and scraped, then check
worker database connectivity and the five-second aggregate query timeout. Queue
alerts are not trustworthy until a successful refresh advances the timestamp;
do not interpret absent backlog alerts as a healthy queue while this alert fires.

## Pipeline Checks

First diagnosis: split `nimbus_pipeline_checks_total` by outcome. `failed`
means CloudWatch, AssumeRole, an unconfigured external ID reference, or a
partial window; the `scheduled pipeline check failed` worker log carries the
pipeline ID and a `reason` (`configuration`, `access_denied`, `throttled`,
`timeout`, `incomplete_window`, `unavailable`) but never credentials. `budget_exhausted`
means a tenant spent `NIMBUS_MAX_AWS_CHECKS_PER_HOUR`; each active AWS pipeline
needs 12 checks per hour plus any manual checks, so raise the budget or reduce
manual polling rather than disabling the budget. Affected pipelines are blind
until checks succeed: detectors cannot open or recover incidents meanwhile.

## Releases And Rollback

Schema changes follow expand/contract so the previous binary keeps working
while a release rolls out or back:

1. Expand: a forward-only migration adds nullable columns, tables, indexes,
   functions or grants. Nothing the running binary reads is removed or renamed.
2. Deploy: run the one-shot migration job (`NIMBUS_COMPONENT=migrate`), then
   roll API and worker replicas. Runtime components refuse to start if any
   embedded migration is missing or its checksum differs.
3. Contract: drop or rename only in a later release, after no deployed binary
   reads the old shape.

Rollback is a binary rollback, never a down-migration. Before rolling back,
confirm the older image can serve the current schema:

```bash
NIMBUS_DATABASE_URL=... docker run --rm -e NIMBUS_DATABASE_URL <previous-image> verify-schema
```

The check passes when every migration the image embeds is present with the
same checksum; newer migrations from an expand step are expected and harmless.
It fails when a migration the image needs is missing or was edited, and then
that image must not serve this database. Execution is at-least-once, so in-flight
steps reclaimed after a rollback may run again; connectors rely on idempotency
keys.

## Backup And Restore Drill

`make restore-drill` (with `NIMBUS_RESTORE_SOURCE_URL`) dumps the database,
restores it into a disposable PostgreSQL after provisioning `nimbus_owner`,
`nimbus_api` and `nimbus_worker`, runs `verify-schema`, and compares exact row
counts per table. It prints the elapsed time for the dataset.

- The dump role must be a superuser or have `BYPASSRLS`: FORCE row-level
  security hides tenant rows from every other role, including the owner, and
  `pg_dump` refuses to dump partially.
- Runtime roles are cluster-wide and are not in a database dump; provision
  them before restoring or ownership, grants and policies fail to apply.
- Local evidence (2026-10-21): 20 tables and 1301 rows from the acceptance
  dataset restored and verified in 9 seconds. This says nothing about RDS
  snapshot or point-in-time recovery timings.

## Disaster Recovery

PostgreSQL is the only authority: queues, leases, outbox and incident state
all live there, so recovering the database recovers Nimbus. After a restore:

1. Provision runtime roles, restore, and run `verify-schema` with the image
   you will deploy.
2. Start workers before the API. Expired leases and unpublished outbox
   messages are reclaimed automatically; expect duplicate deliveries for work
   that was in flight at the recovery point, deduplicated by idempotency keys.
3. Scheduled CloudWatch checks resume at the next five-minute boundary;
   windows that closed while Nimbus was down are not backfilled.
4. Rotate API keys and webhook secrets if the incident involved credential
   exposure; restored `api_keys` rows still hold the old hashes.

## Validation

Run `make observability` for both Prometheus config checks and alert rule tests.
Run `make validate-deploy` for monolith and split Compose rendering plus the
Kubernetes base render. The split Prometheus config scrapes both API and worker;
the monolith config scrapes only the monolith, so neither profile contains an
intentionally absent target.

## Limitations

These aggregates provide no tenant labels or identities and therefore cannot
localize an incident to a tenant. They are polling signals, can be stale after a
database error, and do not measure end-to-end event latency, downstream success,
capacity limits, or customer-visible availability. This deliverable includes no
cloud resources and makes no claim of RDS behavior, backup/restore validation,
load validation, production SLO attainment, or proven alert thresholds.
