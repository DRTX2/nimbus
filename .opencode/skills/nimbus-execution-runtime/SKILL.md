---
name: nimbus-execution-runtime
description: Use ONLY when changing Nimbus workers, queues, step transitions, leases, retries, cancellation, concurrency, checkpointing, or recovery behavior.
---

# Nimbus Execution Runtime

- Assume at-least-once delivery and document its consequences.
- Persist state before acknowledging work; use stable idempotency keys for external effects.
- Use leases and prevent expired workers from committing stale results.
- Never reopen terminal states accidentally.
- Propagate cancellation through `context.Context` and distinguish it from an execution failure.
- Use bounded exponential backoff with jitter and classify retryable errors explicitly.
- Bound worker count and all internal queues.
- Inject clocks or randomness when deterministic tests need them.
- Test duplicate delivery, restart recovery, lease expiry, exhausted retries, and cancellation.
- Run `go test -race ./...` for concurrency changes.
