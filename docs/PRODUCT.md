# Product Brief

## Product Thesis

Nimbus helps small engineering teams turn alerts and webhooks into safe,
reliable, and auditable operational workflows. The customer buys shorter
incident response and less manual toil, not a generic collection of cloud tools.

## Initial Customer Profile

- Startup or scale-up with 3-20 engineers and no dedicated SRE team.
- One to three AWS accounts using Lambda, SQS, EventBridge, and PostgreSQL.
- On-call engineers currently execute runbooks from chat, CI, or local scripts.
- Platform lead, CTO, or engineering manager owns the buying decision.

## First Commercial Package

**AWS asynchronous pipeline guardian**:

1. Assume a customer-managed, read-only IAM role using an external ID.
2. Discover an `SQS -> Lambda -> DLQ` relationship.
3. Read bounded CloudWatch metrics, never queue payloads or application logs.
4. Detect a stalled consumer, retry storm, throttling, or messages present in the DLQ.
5. Create one deduplicated incident with evidence, likely cause, and runbook.
6. Notify a generic webhook and track recovery.
7. Offer an approval-gated diagnostic/remediation workflow in a later release.

This wedge is narrower and cheaper than competing with Datadog, Temporal, or
Zapier as complete platforms.

## Differentiation

- Explainable correlation instead of another threshold alarm.
- Agentless, read-only onboarding and no ingestion of customer payloads.
- Every automation has a durable timeline and explicit at-least-once semantics.
- Cost and blast-radius budgets per tenant.
- Open core: local execution is free; managed connectors and governance are paid.

## Success Metrics

| Metric | MVP target |
| --- | --- |
| Time to first detected incident | Under 15 minutes |
| Detection after sustained fault | Under 20 minutes |
| Duplicate incident rate | Under 2% |
| False alert rate | Under 1 per pipeline/week |
| Workflow completion rate | Above 99% excluding destination failures |
| Design partners | 3 teams before adding another cloud |

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

## Business Model Hypothesis

- Community: self-hosted core, one tenant, standard connectors.
- Team: managed control plane, multiple accounts, notifications, 30-day history.
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
