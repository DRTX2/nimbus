---
name: nimbus-privileged-action-safety
description: Use ONLY when Nimbus mutates cloud resources, Kubernetes, repositories, secrets, databases, queues, filesystems, IAM, or executes commands.
---

# Nimbus Privileged Action Safety

- Classify an action as read-only, mutating, destructive, or privileged.
- Require explicit approval for destructive actions and privilege changes.
- Prefer structured APIs. Never concatenate untrusted input into `sh -c` or SQL.
- Provide a plan or dry-run where supported.
- Restrict credentials and targets by tenant, account, region, project, and namespace.
- Bound affected resources, rows, duration, and concurrency.
- Audit actor, action, target, decision, and result without exposing secrets.
- Do not claim rollback or atomicity unless the provider guarantees it.
- Add negative authorization and blast-radius tests.
