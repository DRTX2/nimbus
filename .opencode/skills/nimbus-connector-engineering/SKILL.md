---
name: nimbus-connector-engineering
description: Use ONLY when adding or changing a Nimbus cloud, Kubernetes, database, queue, HTTP API, authentication, pagination, or provider connector.
---

# Nimbus Connector Engineering

- Define small interfaces from the core consumer, not wrappers around an entire SDK.
- Separate configuration, credentials, client construction, and operation execution.
- Propagate contexts, deadlines, correlation IDs, and stable idempotency keys.
- Classify errors as retryable, permanent, authentication, authorization, or throttling.
- Retry only safe or idempotent operations and honor `Retry-After`.
- Handle pagination and response size limits explicitly.
- Block SSRF destinations and redact tokens, DSNs, headers, and sensitive payloads.
- Require contract tests with a fake or emulator; live-provider tests stay opt-in.
- Justify every SDK by maintenance, license, advisories, and transitive cost.
