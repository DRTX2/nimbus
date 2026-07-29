# Referencia De Comandos

Todos los comandos se ejecutan desde la raiz del repositorio.

## Requisitos

```bash
go version
docker version
docker compose version
make --version
curl --version
```

Versiones usadas en la validacion actual:

- Go `1.26.5`.
- Docker Engine `29.6.2`.
- Docker Compose `5.3.1`.
- OpenCode `1.18.4`.
- kubectl `1.36.2`, con Kustomize `5.8.1`.
- Terraform se valido usando la imagen `hashicorp/terraform:1.12.2`.

## Entorno Local

### Monolito modular

```bash
make dev
```

Construye la imagen e inicia Nimbus con procesos separados de API y worker, PostgreSQL,
Prometheus, Grafana y Jaeger. Compose espera la dependencia PostgreSQL; valida
el resto mediante sus endpoints.

Resultado esperado:

```bash
docker compose ps
curl --fail-with-body http://localhost:8080/health/ready
curl --fail-with-body 'http://localhost:9090/api/v1/query?query=up'
curl --fail-with-body http://localhost:16686/api/services
```

PostgreSQL tiene healthcheck de contenedor. Los demas servicios deben estar
`Up`; API, Prometheus y Jaeger se validan con los comandos anteriores.

### API y worker separados

```bash
make dev-down
make dev-split
```

Inicia la misma imagen con `NIMBUS_COMPONENT=api` y
`NIMBUS_COMPONENT=worker`.

```bash
curl --fail-with-body http://localhost:8080/health/ready
curl --fail-with-body -o /dev/null -w '%{http_code}\n' http://localhost:8081/health/ready
```

Respuesta esperada:

```text
{"status":"ok"}
204
```

### Estado y logs

```bash
docker compose -f compose.yaml -f compose.split.yaml ps
docker compose -f compose.yaml -f compose.split.yaml logs nimbus-api nimbus-worker
docker compose logs --follow nimbus
```

`make logs` corresponde al perfil monolitico. En perfil split usa el segundo
comando directamente.

### Parada

```bash
make dev-down
```

No elimina los volumenes. Para eliminar datos locales deliberadamente:

```bash
docker compose -f compose.yaml -f compose.split.yaml down --volumes --remove-orphans
```

Este ultimo comando es destructivo y no forma parte de los targets normales.

## Calidad Go

```bash
make fmt
make vet
make test
make test-race
make build
make verify
make security
```

| Comando | Validacion |
| --- | --- |
| `make fmt` | Aplica `gofmt` a todos los paquetes |
| `make vet` | Ejecuta analisis estatico de `go vet` |
| `make test` | Ejecuta pruebas unitarias |
| `make test-race` | Ejecuta pruebas con race detector |
| `make build` | Genera `bin/nimbus` con rutas reproducibles |
| `make verify` | Ejecuta formato, vet, tests, race, build, checksums y tidy diff |
| `make security` | Ejecuta `govulncheck v1.6.0` |

Resultado de seguridad esperado:

```text
No vulnerabilities found.
```

Comandos equivalentes sin Make:

```bash
gofmt -w .
go vet ./...
go test ./...
go test -race ./...
go build -trimpath -o bin/nimbus ./cmd/nimbus
go mod verify
go mod tidy -diff
go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...
```

## Docker

Validar Compose sin iniciar servicios:

```bash
docker compose config --quiet
docker compose -f compose.yaml -f compose.split.yaml config --quiet
```

Construir solo la imagen:

```bash
docker build --tag nimbus:local .
```

Inspeccionar procesos y migraciones:

```bash
docker compose -f compose.yaml -f compose.split.yaml exec -T postgres \
  psql -U nimbus -d nimbus -c 'SELECT name FROM schema_migrations ORDER BY name'
```

## Observabilidad

```bash
curl --fail-with-body http://localhost:8080/metrics
curl --fail-with-body 'http://localhost:9090/api/v1/query?query=up'
curl --fail-with-body http://localhost:16686/api/services
```

Resultados relevantes:

- Prometheus debe mostrar el target de Nimbus con valor `1`.
- Jaeger debe incluir el servicio `nimbus` despues de ejecutar peticiones HTTP.
- Grafana carga el dashboard `Nimbus Overview` desde provisioning.

## OpenAPI

```bash
curl --fail-with-body http://localhost:8080/openapi.json
curl --fail-with-body -o /dev/null -w '%{http_code}\n' http://localhost:8080/docs
```

Resultado esperado para Scalar: `200`.

Listar operaciones desde el contrato:

```bash
curl --fail-with-body --silent http://localhost:8080/openapi.json \
  | jq -r '.paths | to_entries[] | .key as $path | .value | to_entries[] | "\(.key|ascii_upcase) \($path)"'
```

## Kubernetes

Renderizar manifiestos sin aplicarlos:

```bash
kubectl kustomize deploy/k8s/base
```

Crear secretos de laboratorio y aplicar, solo contra un cluster local confirmado:

```bash
kubectl create namespace nimbus
kubectl -n nimbus create secret generic nimbus-secrets \
  --from-literal=NIMBUS_API_KEY='replace-with-a-long-random-token' \
  --from-literal=NIMBUS_DATABASE_URL='postgres://...'
kubectl apply -k deploy/k8s/base
```

No ejecutar `kubectl apply` contra un cluster compartido sin revisar contexto:

```bash
kubectl config current-context
```

## Terraform

Formato y validacion usando contenedor, sin credenciales AWS:

```bash
docker run --rm -v "$PWD/infra/terraform:/workspace" -w /workspace \
  hashicorp/terraform:1.12.2 fmt -check -recursive

docker run --rm \
  -v "$PWD/infra/terraform/environments/sandbox:/workspace" \
  -w /workspace hashicorp/terraform:1.12.2 validate
```

Plan del sandbox:

```bash
terraform -chdir=infra/terraform/environments/sandbox init
terraform -chdir=infra/terraform/environments/sandbox plan \
  -var='container_image=ghcr.io/OWNER/nimbus:SHA' \
  -var='config_parameter_name=/nimbus/sandbox/env' \
  -var='budget_email=you@example.com'
```

No se ha ejecutado `terraform apply`. Revisa costes, cuenta, region y plan antes
de crear recursos.

## OpenCode

Validar configuracion y skills:

```bash
opencode debug config
opencode debug skill
```

Comandos de proyecto disponibles tras reiniciar OpenCode:

```text
/verify
/risk-review
/release-readiness
```

Las skills se encuentran en `.opencode/skills/` y son auditables como codigo.
