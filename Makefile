.PHONY: fmt vet test test-race build verify security dev dev-split dev-down logs clean

fmt:
	gofmt -w .

vet:
	go vet ./...

test:
	go test ./...

test-race:
	go test -race ./...

build:
	go build -trimpath -o bin/nimbus ./cmd/nimbus

verify: fmt vet test test-race build
	go mod verify
	go mod tidy -diff

security:
	go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...

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
