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
