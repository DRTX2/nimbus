# Testing Strategy

Nimbus follows the **testing trophy**: most confidence comes from tests that run
real collaborators together (integration, system, end-to-end), backed by a
cheap static base. Unit tests exist only where a pure function guards a
boundary whose failure would be expensive or hard to see from outside.

```text
            ┌───────────────┐
            │  E2E / smoke  │  Playwright journeys + @smoke scenarios on a deployed stack
        ┌───┴───────────────┴───┐
        │  Integration/system   │  Gherkin acceptance over API + worker + PostgreSQL roles,
        │  (the widest layer)   │  Gherkin UI scenarios over the real route tree
        └───┬───────────────┬───┘
            │  Unit (few)   │  security and parsing boundaries only
        ┌───┴───────────────┴───┐
        │  Static + contract    │  go vet, tsc strict, ESLint, OpenAPI snapshot, type drift
        └───────────────────────┘
```

Every behavioral specification is written in **Gherkin**, so product, security
and engineering review the same sentences that run in CI.

## Layers

| Layer | Where | Runs against | Tags / command |
| --- | --- | --- | --- |
| Static and contract | `go vet`, `tsc`, ESLint, `TestOpenAPISnapshotIsCurrent`, `npm run check:api` | source | `make verify`, `make web-verify` |
| Unit (exceptional) | `internal/**/_test.go`, `web/src/**/format` | pure functions | `make test` |
| Database integration | `internal/platform/database/*_integration_test.go` | PostgreSQL with owner/api/worker roles | `make test-integration` |
| System acceptance | `test/features/*.feature` + `test/acceptance` (godog) | real API, worker, outbox, PostgreSQL role split, scripted webhook receiver, local OIDC issuer | `make test-acceptance` (`@integration @e2e @security @regression`) |
| UI integration | `web/src/features/**/*.feature` (vitest-cucumber) | real routes, session and components; network mocked by MSW | `npm --prefix web test` |
| Smoke | `test/features/smoke.feature` | any deployed Nimbus over HTTP | `make test-smoke` (`@smoke`) |
| End-to-end | `web/e2e/features/*.feature` (playwright-bdd) | browser against the running image | `make e2e` |
| Regression | every scenario tagged `@regression`, plus the OpenAPI snapshot | all of the above | CI on every push |

### Why unit tests are the exception

A unit test is kept only when it pins behavior that is both security-relevant
and awkward to reach through the system:

- SSRF address classification and private-destination blocking.
- Connector error classification (retry vs. permanent), required by the
  connector change matrix in `AGENTS.md`.
- Configuration fail-fast, cursor decoding, and session-cache bounds.
- Dashboard static-file serving (path traversal, cache headers).

Everything else — idempotency, DAG ordering, retries, approvals, RBAC, tenant
isolation, OIDC provisioning, incident lifecycle, quotas — is specified once in
Gherkin and verified through the running system.

## Running Locally

```bash
# Fast feedback (static, unit, race, contract)
make verify
make web-install web-verify

# System acceptance over a disposable PostgreSQL with the role split
eval "$(scripts/test-db.sh up)"
make test-integration test-acceptance
scripts/test-db.sh down

# Everything above in one command
make test-all

# Smoke and browser journeys against a running stack
make dev-split                    # NIMBUS_HTTP_PORT=18080 if 8080 is taken
make test-smoke                   # NIMBUS_BASE_URL defaults to http://localhost:8080
npx --prefix web playwright install chromium
make e2e
```

Useful switches:

- `NIMBUS_ACCEPTANCE_TAGS="@security"` runs a subset of acceptance scenarios.
- `NIMBUS_ACCEPTANCE_FORMAT=progress|pretty|junit` changes godog output.
- `NIMBUS_ACCEPTANCE_LOGS=1` streams API and worker logs during acceptance.
- `NIMBUS_E2E_BASE_URL` and `NIMBUS_E2E_API_KEY` point Playwright at another stack.

## Isolation And Determinism

- Each acceptance scenario provisions its **own tenant and owner key**, so
  scenarios run in any order and in parallel with no shared fixtures.
- Webhook destinations are scripted per scenario (`503,429,204`) and every
  delivery is recorded for signature and idempotency assertions.
- The OIDC issuer is a local discovery + JWKS server; tokens are minted per
  sign-in, including expired, untrusted-key and wrong-audience variants.
- Browser journeys seed data through the public API with run-unique names and
  fail on any console error or CSP violation.
- Waiting is always condition-based (`eventually`, Playwright auto-wait); there
  are no fixed sleeps.

## Adding A Scenario

1. Describe the behavior in the relevant `.feature` file in plain language,
   from the user's point of view. Reuse existing steps where possible.
2. Tag it: `@smoke` only if it is safe against a shared deployed tenant;
   `@security` for authorization, isolation or credential behavior;
   `@regression` for anything that must never break again.
3. Add step definitions only when no existing step expresses the intent.
4. For a bug fix, write the failing scenario first.

## Defects Found By These Suites

The acceptance and journey suites were introduced together with the current
feature set and immediately found defects that unit tests had missed:

- The worker's tenant step quota compared a UUID with the lease owner, so no
  step could ever be claimed.
- Incident recovery ran writes while a result set was open and silently lost
  its timeline and outbox entries.
- Ingestion, observations and incident-workflow dispatch lacked PostgreSQL
  privileges in the least-privilege role split.
- OIDC sign-in could not work under the API role.
- `schemaVersion` was required in the contract despite having a default.
- The dashboard fetched `/v1/pipelines/` with an empty id, field hints polluted
  accessible labels, and approvals showed stale data after navigation.
