# Nimbus Engineering Guide

Nimbus is a Go platform for reliable event automation. It starts as a modular
monolith and evolves only when measured scale or product validation requires it.

## Invariants

- Prefer the Go standard library and small, maintained dependencies.
- Pass `context.Context` as the first argument; never store it in a struct.
- Wrap errors with `%w`. Do not panic for operational failures.
- Treat execution as at-least-once. External effects require idempotency.
- Keep PostgreSQL as the source of truth. Caches and brokers are not authorities.
- Never log credentials, authorization headers, full event payloads, or secrets.
- Enforce tenant scope in every query and derive the tenant from authentication.
- Bound request bodies, responses, timeouts, goroutines, retries, and queues.
- Do not add a service, abstraction, dependency, or cloud resource without a current need.
- Do not run destructive infrastructure or cloud operations without explicit approval.

## Canonical Checks

```bash
make fmt
make vet
make test
make test-race
make build
make verify
make security
```

Integration tests and cloud deployments are opt-in. Generated files must state
their source and reproducible generation command.

## Change Matrix

| Change | Minimum validation |
| --- | --- |
| Ordinary Go | format, vet, unit tests |
| Runtime/concurrency | unit tests and race detector |
| Workflow contract | fixtures, cycle checks, determinism tests |
| Connector | unit tests, timeout and error classification tests |
| Dependency/CI/image | supply-chain review and vulnerability scan |
| Privileged action | authorization and negative safety tests |
