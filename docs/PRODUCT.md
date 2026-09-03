# Product Strategy

## Product Thesis

Nimbus helps lean AWS engineering teams detect and explain failures in
revenue-sensitive asynchronous pipelines before customers report them. The
customer buys earlier detection, faster diagnosis, and evidence of recovery.
The durable workflow engine is supporting infrastructure, not the headline.

Nimbus does not compete as a generic workflow builder, observability platform,
or autonomous remediation agent.

## Initial Customer Profile

- B2B SaaS company with 8-40 engineers and no dedicated SRE team, or one platform
  engineer supporting the whole organization.
- One to three AWS accounts and 5-50 production `SQS -> Lambda -> DLQ` pipelines.
- Revenue-sensitive workloads such as billing, orders, imports, notifications,
  or partner integrations.
- CloudWatch alarms exist but are noisy, fragmented, or poor at diagnosis.
- The Head of Engineering, CTO, or platform lead buys; the on-call engineer uses
  the product.

## Primary Job To Be Done

When a revenue-sensitive SQS/Lambda pipeline degrades, tell the on-call engineer
whether the consumer stalled, Lambda is throttling or failing, or work reached
the DLQ. Show the bounded numeric evidence and track recovery without reading
customer messages, so the team can act before support tickets arrive.

## First Commercial Package

**AWS asynchronous pipeline guardian**:

1. Assume a customer-managed, read-only IAM role using an external ID.
2. Discover an `SQS -> Lambda -> DLQ` relationship.
3. Read bounded CloudWatch metrics, never queue payloads or application logs.
4. Detect a stalled consumer, retry storm, throttling, or messages present in the DLQ.
5. Create one deduplicated incident with evidence, likely cause, and runbook.
6. Notify a generic webhook and track recovery.
7. Preserve acknowledgement, classification, and recovery in one timeline.
8. Offer approval-gated read-only diagnostics after detection is trusted.

This wedge is narrower and cheaper than competing with Datadog, Temporal, or
Zapier as complete platforms.

## Differentiation

- Explainable correlation instead of another threshold alarm.
- Agentless, read-only onboarding and no ingestion of customer payloads.
- Every automation has a durable timeline and explicit at-least-once semantics.
- Cost and blast-radius budgets per tenant.
- Open core: local execution is free; managed connectors and governance are paid.

## Essential Customer Journey

1. Deploy a customer-controlled, read-only AWS role with an external ID.
2. Discover and select an SQS/Lambda/DLQ topology instead of entering ARNs.
3. Preview metric availability, permissions, and expected API cost.
4. Accept recommended thresholds and a five-minute monitoring policy.
5. Receive an incident containing pipeline identity, detector, evidence, likely
   checks, and a link to the incident.
6. Acknowledge, investigate, and classify the incident from one console.
7. Receive a recovery notification and review the durable timeline.
8. Review weekly detection quality and operational value.

## Success Metrics

| Metric | MVP target |
| --- | --- |
| Time to first detected incident | Under 15 minutes |
| Detection after sustained fault | Under 20 minutes |
| Duplicate incident rate | Under 2% |
| False alert rate | Under 1 per pipeline/week |
| Workflow completion rate | Above 99% excluding destination failures |
| Design partners | 3 teams before adding another cloud |

Additional activation metrics are time to valid AWS connection, time to first
monitored pipeline, percentage of incidents acknowledged and classified, and
incidents found before a customer escalation.

These are validation targets, not achieved results. Nimbus has not conducted
customer interviews or enrolled design partners. The incident review API and
weekly report provide an internal measurement loop so future trials can record
evidence instead of inferring commercial validation from product telemetry.

## Incident Quality Review Loop

1. An owner or operator classifies an incident as `true_positive`,
   `false_positive`, `expected_behavior`, or `duplicate`, with an optional
   bounded comment.
2. Nimbus retains each classification and audit event; the latest review is the
   current disposition and remains independent from lifecycle status. Exact
   retries by the same reviewer are idempotent and do not inflate history.
3. The weekly UTC report groups incidents by pipeline and detector, exposing
   reviewed and unreviewed coverage plus disposition counts.
4. False-positive rate is calculated only from reviewed `false_positive` and
   `true_positive` outcomes. Expected behavior, duplicates, and unreviewed
   incidents are reported but excluded from that denominator.

This loop supports detector-quality evaluation. It does not demonstrate demand,
willingness to pay, operational outcomes at a customer, or commercial traction.

## Delivery Roadmap

### Now: complete one sellable loop

- Enroll three design partners through concierge onboarding.
- Add a budgeted scheduler using closed, non-overlapping five-minute windows,
  jitter, freshness checks, and tenant call quotas.
- Add topology discovery and connection validation.
- Deliver actionable opened and recovered notifications, beginning with signed
  webhooks and one polished chat integration selected by partners.
- Add a focused console for connection, pipeline health, incident inbox/detail,
  acknowledgement, recovery, and quality review.
- Complete hosted boundaries: managed secret references, OIDC, quotas, retention,
  backup restore evidence, and end-to-end tests.

### Next: deepen diagnosis

- Read-only event-source mapping state, concurrency configuration, queue
  attributes, and recent deployment context.
- Detector backtesting and recommended investigation steps.
- Multi-account and environment grouping when pilots require it.
- One narrowly scoped, approval-gated diagnostic only after repeated demand.

### Later: expand only with evidence

- A second AWS topology requested by at least three paying customers.
- Enterprise identity, retention, audit export, and private networking.
- Adaptive baselines after enough classified production history exists.
- Mutating remediation only with preview, dry run, idempotency, approval, and
  explicit blast-radius limits.

## Explicit Non-Goals

- A Zapier/n8n-style connector marketplace or visual general-purpose canvas.
- Temporal-like SDKs, arbitrary code execution, or a new workflow language.
- Multi-cloud support before the AWS wedge has three successful partners.
- Broad log ingestion, on-call scheduling, or replacing observability tools.
- Autonomous remediation before deterministic diagnosis is trusted.
- Kafka, Redis, microservices, or partitioning without measured pressure.
- AI in the detection or execution critical path.

## Business Model Hypothesis

- Design partner: paid, isolated AWS pilot with concierge calibration.
- Community: self-hosted core, one tenant, deterministic detectors.
- Team: managed control plane, multiple accounts, notifications, 30-day history,
  priced by monitored pipeline rather than event volume.
- Governance: SSO, approvals, access reviews, retention policies, audit export.
- Enterprise later: private networking, custom retention, compliance evidence,
  support, and organization-wide policy.

Pricing should be validated against interviews. A starting hypothesis is a base
fee plus monitored pipelines, not event volume, because polling cost and value
track resources more closely than raw events.

## Data And AI Strategy

AI is not in the critical execution path. Rules remain deterministic and
explainable.

- Phase A: PostgreSQL operational aggregates and downloadable CSV/JSON reports.
- Phase B: redact and export approved fields to S3 as Parquet; query with DuckDB
  locally or Athena on demand.
- Phase C: backtest detectors and use robust median/MAD baselines by hour/day.
- Phase D: optional LLM-generated incident summary grounded only in collected
  evidence, with citations and an explicit confidence indicator.

Governance requirements include data classification, tenant retention policy,
field allowlists, lineage from event to report, deletion workflows, encryption,
and audit evidence. Raw payloads are never training data by default.

## Product Risks

- False positives destroy trust: use sustained windows, deduplication, and replay.
- Cloud cost can exceed customer value: enforce resource and API call budgets.
- Remediation increases blast radius: ship diagnostics before mutations and
  require approval, scope limits, and dry-run support.
- Scope can become a resume-driven technology zoo: every phase has an explicit
  user or scale gate.
