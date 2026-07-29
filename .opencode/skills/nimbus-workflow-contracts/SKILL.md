---
name: nimbus-workflow-contracts
description: Use ONLY when creating or changing Nimbus workflow definitions, DAG validation, schemas, triggers, steps, retries, timeouts, or execution plans.
---

# Nimbus Workflow Contracts

- Keep the pipeline `decode -> validate -> normalize -> persist`; never execute during validation.
- Reject unknown fields, duplicate step IDs, missing dependencies, self-dependencies, and cycles.
- Apply defaults before hashing or persisting a definition.
- Keep published versions immutable and require explicit versions for incompatible changes.
- Define retry, timeout, cancellation, and failed-dependency behavior without ambiguity.
- Preserve deterministic plans: map iteration order must not affect behavior.
- Add table-driven tests for valid and invalid DAGs, boundaries, and determinism.
- Never embed credentials in a workflow definition; use secret references in future connectors.
