.PHONY: fmt vet test test-race test-db test-integration test-acceptance test-smoke openapi web-install web-verify web-build e2e test-all build verify security observability validate-deploy dev dev-split dev-down logs clean

fmt:
	gofmt -w cmd internal test

vet:
	go vet ./...
	go vet -tags=integration,acceptance ./...

test:
	go test ./...

test-race:
	go test -race ./...

# Disposable PostgreSQL with the owner/api/worker role split.
test-db:
	scripts/test-db.sh up

test-integration:
	go test -tags=integration -count=1 ./internal/platform/database

# Gherkin scenarios (test/features) against the real API, worker and database.
# In-process: requires NIMBUS_TEST_*_DATABASE_URL (see `eval "$$(scripts/test-db.sh env)"`).
test-acceptance:
	go test -tags=acceptance -count=1 ./test/acceptance

# @smoke scenarios against a running deployment: NIMBUS_BASE_URL and NIMBUS_API_KEY.
test-smoke:
	NIMBUS_BASE_URL=$${NIMBUS_BASE_URL:-http://localhost:8080} NIMBUS_API_KEY=$${NIMBUS_API_KEY:-nimbus-local-development-key} \
		go test -tags=acceptance -count=1 ./test/acceptance

openapi:
	go run ./cmd/nimbus openapi > api/openapi.json
	npm --prefix web run generate:api

web-install:
	npm --prefix web ci

# Static checks, contract drift, and Gherkin integration scenarios for the dashboard.
web-verify:
	npm --prefix web run check:api
	npm --prefix web run typecheck
	npm --prefix web run lint
	npm --prefix web test

web-build:
	npm --prefix web run build

# Browser journeys (web/e2e/features) against a running stack, e.g. `make dev-split`.
e2e:
	NIMBUS_E2E_BASE_URL=$${NIMBUS_E2E_BASE_URL:-http://127.0.0.1:$${NIMBUS_HTTP_PORT:-8080}} npm --prefix web run e2e

# The whole testing trophy: static + unit, contract, integration, acceptance.
test-all: verify web-verify
	eval "$$(scripts/test-db.sh up)" && $(MAKE) test-integration test-acceptance; status=$$?; scripts/test-db.sh down; exit $$status

build:
	go build -trimpath -o bin/nimbus ./cmd/nimbus

verify: fmt vet test test-race build
	go mod verify
	go mod tidy -diff

security:
	go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...

observability:
	docker run --rm --entrypoint promtool -v "$(CURDIR)/deploy/observability:/etc/prometheus:ro" prom/prometheus:v3.5.0@sha256:63805ebb8d2b3920190daf1cb14a60871b16fd38bed42b857a3182bc621f4996 check config /etc/prometheus/prometheus.yml
	docker run --rm --entrypoint promtool -v "$(CURDIR)/deploy/observability:/etc/prometheus:ro" prom/prometheus:v3.5.0@sha256:63805ebb8d2b3920190daf1cb14a60871b16fd38bed42b857a3182bc621f4996 check config /etc/prometheus/prometheus-split.yml
	docker run --rm --entrypoint promtool -v "$(CURDIR)/deploy/observability:/etc/prometheus:ro" prom/prometheus:v3.5.0@sha256:63805ebb8d2b3920190daf1cb14a60871b16fd38bed42b857a3182bc621f4996 test rules /etc/prometheus/nimbus.rules.test.yml

validate-deploy:
	docker compose config --quiet
	docker compose -f compose.yaml -f compose.split.yaml config --quiet
	kubectl kustomize deploy/k8s/base >/dev/null

dev:
	docker compose up --build --detach --wait --remove-orphans

dev-split:
	docker compose -f compose.yaml -f compose.split.yaml up --build --detach --wait

dev-down:
	docker compose -f compose.yaml -f compose.split.yaml down --remove-orphans

logs:
	docker compose logs --follow nimbus

clean:
	rm -rf bin coverage.out
