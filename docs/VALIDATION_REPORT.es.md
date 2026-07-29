# Informe De Validacion

Fecha: **2026-07-29**. Entorno: Linux amd64, workspace local.

Este informe separa comandos realmente ejecutados de validaciones pendientes.
Los resultados no constituyen un SLA ni una certificacion de produccion.

Ultima repeticion de `make verify` y `make security`: correcta despues de crear
esta documentacion.

## Toolchain

| Herramienta | Resultado observado |
| --- | --- |
| `go version` | `go1.26.5-X:nodwarf5 linux/amd64` |
| Docker Server | `29.6.2` |
| Docker Compose | `5.3.1` |
| Git | `2.55.0` |
| OpenCode | `1.18.4` |
| kubectl | `1.36.2` |
| Kustomize | `5.8.1` |

## Verificacion Canonica

Comando ejecutado:

```bash
make verify
```

Subcomandos y resultado:

| Comando | Resultado |
| --- | --- |
| `gofmt -w .` | Correcto |
| `go vet ./...` | Correcto, sin diagnosticos |
| `go test ./...` | Correcto |
| `go test -race ./...` | Correcto, sin races detectadas |
| `go build -trimpath -o bin/nimbus ./cmd/nimbus` | Correcto |
| `go mod verify` | `all modules verified` |
| `go mod tidy -diff` | Sin diferencias |

Paquetes con pruebas directas:

- `internal/workflow`: DAG, ciclos, dependencias y defaults HTTP.
- `internal/execution`: backoff y clasificacion de IPs bloqueadas.
- `internal/httpapi`: matriz basica de roles.
- `internal/detector`: decisiones deterministas.
- `internal/awsmonitor`: batch de siete queries y mapeo CloudWatch.

## Seguridad De Dependencias

```bash
make security
```

Resultado:

```text
No vulnerabilities found.
```

Durante el desarrollo se detecto `GO-2026-6061` en
`google.golang.org/grpc v1.81.1`. Se actualizo a `v1.82.1` y el escaneo posterior
quedo limpio.

CI configurado, no ejecutado localmente como GitHub Actions:

- CodeQL.
- Trivy filesystem scan.
- Dependency Review.
- Dependabot.
- Build de imagen sin push.

## Docker Compose

Validaciones ejecutadas:

```bash
docker compose config --quiet
docker compose -f compose.yaml -f compose.split.yaml config --quiet
make dev
make dev-split
```

Resultados:

- Perfil monolitico construyo y sus endpoints respondieron correctamente.
- Perfil split construyo API y worker de forma independiente.
- API readiness devolvio JSON `status: ok`.
- Worker readiness devolvio HTTP `204`.
- PostgreSQL quedo `healthy`; Prometheus, Grafana y Jaeger quedaron `Up` y sus
  endpoints se validaron por separado.
- Los puertos publicados usan `127.0.0.1`.

Se encontro y corrigio durante la validacion una sintaxis `tmpfs` de Compose que
interpretaba `noexec` como una ruta separada.

## Migraciones

Consulta ejecutada:

```sql
SELECT name FROM schema_migrations ORDER BY name;
```

Resultado:

```text
001_initial.sql
002_execution_consistency.sql
003_product_governance_scale.sql
004_aws_external_id_reference.sql
```

Tambien se verifico la creacion del principal interno:

```text
kind=system, display_name=Nimbus automation
```

## Smoke Test De Workflow

Flujo ejecutado:

1. Crear DAG con root `validate`.
2. Crear `notify` y `audit` dependientes.
3. Ingerir `order.created`.
4. Consultar ejecucion.
5. Repetir la misma idempotency key.

Resultados observados:

- Root ejecutado una vez.
- Dependientes ejecutados por workers diferentes en la misma ventana temporal.
- Estado final `succeeded`.
- Cada step tuvo `attempt: 1`.
- Repeticion devolvio `duplicate: true` con los IDs originales.

## Smoke Test HTTP

Se creo un step `POST https://httpbin.org/status/204`.

Resultado:

Campos relevantes de la respuesta completa:

```json
{"status":"succeeded","steps":[{"id":"f065ea74-f0d6-4b09-8081-25dda8558d8d","stepId":"send","type":"http","status":"succeeded","attempt":1,"output":{"statusCode":204},"startedAt":"2026-07-29T03:56:28Z","finishedAt":"2026-07-29T03:56:29Z"}]}
```

Esto valida DNS, TLS, timeout, body acotado y persistencia del resultado. No
demuestra disponibilidad futura de `httpbin.org`.

## Smoke Test De Incidentes

Datos usados:

- Pipeline fixture `SQS -> Lambda -> DLQ`.
- `consecutiveWindows: 2`.
- `backlogThreshold: 100`.
- `ageThresholdSeconds: 300`.
- Dos ventanas con backlog mayor a 200, edad mayor a 600 e invocaciones cero.

Resultados:

- Primera ventana: `incidents: []`.
- Segunda ventana: un unico `consumer_stalled`, severidad `critical`.
- Outbox `incident.opened`: publicado.
- Workflow `nimbus.incident.opened`: ejecucion `succeeded`.
- Step webhook con `bodyMode: event`: respuesta `204`.

## Smoke Test De Gobernanza

Flujo ejecutado:

1. Crear key temporal `approver` con expiracion de 30 dias.
2. Crear workflow `approvalRequired: true`.
3. Ingerir evento con owner.
4. Intentar autoaprobar con owner solicitante.
5. Aprobar con principal diferente.
6. Consultar ejecucion y auditoria.
7. Revocar la key temporal.

Resultados:

- Ingestion devolvio `approvalId`.
- Ejecucion permanecio sin steps ejecutables hasta la decision.
- Autoaprobacion devolvio `403`.
- Approver distinto devolvio `approved`.
- Ejecucion final `succeeded`.
- Audit log registro `approval.decide`.
- Key temporal revocada con `204`.

No se incluye el token generado en este documento.

## Observabilidad

Validaciones ejecutadas:

```bash
curl http://localhost:9090/api/v1/query?query=up
curl http://localhost:16686/api/services
```

Resultados:

- Prometheus reporto Nimbus `up=1`.
- Jaeger incluyo el servicio `nimbus`.
- Logs JSON incluyeron request ID, ejecucion, step y attempts sin authorization headers.

## Kubernetes

```bash
kubectl kustomize deploy/k8s/base >/dev/null
```

Resultado: correcto. Renderiza Deployments separados para API y worker, Service
solo para API, probes, recursos y security contexts. No se aplico a ningun cluster.

## Terraform

```bash
docker run --rm -v "$PWD/infra/terraform:/workspace" -w /workspace \
  hashicorp/terraform:1.12.2 fmt -check -recursive
```

Resultado: correcto.

El sandbox fue inicializado con backend deshabilitado y `terraform validate`
termino con `Success! The configuration is valid.`. No se ejecuto plan con una
cuenta real ni `terraform apply`.

## OpenCode

```bash
opencode debug config
opencode debug skill
```

Resultados:

- Configuracion valida.
- Las cinco skills Nimbus fueron descubiertas.
- Los comandos de verificacion, riesgo y release estan instalados.

## Validaciones Pendientes

- Tests de integracion PostgreSQL automatizados en CI.
- Pruebas negativas RLS con roles no propietarios.
- Test E2E automatizado en lugar de smoke manual.
- Load tests y recuperacion de backlog medidos.
- Restore drill de PostgreSQL.
- Prueba real AssumeRole/CloudWatch opt-in.
- Ejecucion de GitHub Actions en un repositorio remoto.
- Terraform plan/apply en una cuenta sandbox con budget aprobado.
- Despliegue Kubernetes real y pruebas de NetworkPolicy/TLS.
