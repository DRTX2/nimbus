---
name: verify
description: Run the Nimbus checks a change needs before calling it done (Go, migrations/RLS, Gherkin behavior, API contract, dashboard, dependencies, alerts, deploy manifests). Use after edits, before commits, or when asked to verify, run all tests, or check everything.
---

# Verify a Nimbus change

Map touched files to rows (AGENTS.md change matrix) and run every command of
each row. Report failures with their output; never claim green without a run.

| Touched | Run |
| --- | --- |
| Any Go | `make verify` (fmt, vet incl. integration/acceptance tags, test, race, build, `go mod tidy -diff`) |
| Migrations, `internal/store`, RLS, grants, behavior (`test/features`) | `make test-all` (Docker: disposable Postgres on 55432, integration + Gherkin acceptance, auto teardown) |
| API handlers/types | `make openapi`, review `git diff api/openapi.json web/src/shared/api/schema.d.ts`, then `make web-verify` |
| `web/` | `make web-verify`; if a flow changed: `make dev-split && make e2e; make dev-down` |
| `go.mod`, `web/package*.json`, Dockerfile, CI | `make security` and review each new dependency |
| `deploy/observability` | `make observability` |
| `compose*.yaml`, `deploy/k8s` | `make validate-deploy` |
| Backup/restore path | `NIMBUS_RESTORE_SOURCE_URL=<superuser or BYPASSRLS url> make restore-drill` |

Run a subset against the test database:

```bash
eval "$(scripts/test-db.sh up)"
go test -tags=integration -count=1 ./internal/platform/database -run TestName
NIMBUS_ACCEPTANCE_TAGS=@some-tag go test -tags=acceptance -count=1 ./test/acceptance
scripts/test-db.sh down   # always, even after a failure
```

`NIMBUS_ACCEPTANCE_LOGS=1` shows server logs; `@wip` scenarios never run.

Gotchas:

- The Bash tool runs zsh: `status` is read-only, capture exit codes with `rc=$?`.
- Acceptance uses a scripted CloudWatch; a green run proves nothing about real AWS.
- Local only: no `terraform apply`, `kubectl apply`, AWS mutations or deploys
  without explicit approval from the user.
