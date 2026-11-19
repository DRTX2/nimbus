# Referencia Practica De API

La especificacion ejecutable esta en `/openapi.json` y Scalar en `/docs`. Los
IDs y timestamps siguientes son ejemplos; no deben copiarse literalmente.

## Preparacion

```bash
export NIMBUS_URL=http://localhost:8080
export NIMBUS_TOKEN=nimbus-local-development-key
```

Headers comunes:

```bash
-H "Authorization: Bearer $NIMBUS_TOKEN" \
-H 'Content-Type: application/json'
```

Los errores siguen `application/problem+json`:

```json
{"title":"Bad Request","status":400,"detail":"invalid workflow"}
```

## Sistema

### `GET /health/live`

```bash
curl --fail-with-body "$NIMBUS_URL/health/live"
```

```json
{"status":"ok"}
```

No consulta dependencias.

### `GET /health/ready`

```bash
curl --fail-with-body "$NIMBUS_URL/health/ready"
```

```json
{"status":"ok"}
```

Devuelve `503` si PostgreSQL no esta disponible.

### OpenAPI, Scalar y metricas

```bash
curl --fail-with-body "$NIMBUS_URL/openapi.json"
curl --fail-with-body "$NIMBUS_URL/docs"
curl --fail-with-body "$NIMBUS_URL/metrics"
```

No pertenecen a `/v1`. En produccion deben protegerse mediante red interna o
gateway aunque localmente solo se publican en `127.0.0.1`.

## Workflows

### `POST /v1/workflows`

```bash
curl --fail-with-body "$NIMBUS_URL/v1/workflows" \
  -H "Authorization: Bearer $NIMBUS_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "name":"Order operations",
    "approvalRequired":false,
    "definition":{
      "schemaVersion":1,
      "eventType":"order.created",
      "steps":[
        {"id":"validate","type":"noop"},
        {"id":"notify","type":"noop","dependsOn":["validate"]},
        {"id":"audit","type":"noop","dependsOn":["validate"]}
      ]
    }
  }'
```

Respuesta `201`:

```json
{
  "id":"11111111-1111-4111-8111-111111111111",
  "name":"Order operations",
  "status":"active",
  "version":1,
  "approvalRequired":false,
  "definition":{"schemaVersion":1,"eventType":"order.created","steps":[
    {"id":"validate","type":"noop","timeoutSeconds":10,"retry":{"maxAttempts":3,"initialDelaySeconds":2,"maxDelaySeconds":60}},
    {"id":"notify","type":"noop","dependsOn":["validate"],"timeoutSeconds":10,"retry":{"maxAttempts":3,"initialDelaySeconds":2,"maxDelaySeconds":60}},
    {"id":"audit","type":"noop","dependsOn":["validate"],"timeoutSeconds":10,"retry":{"maxAttempts":3,"initialDelaySeconds":2,"maxDelaySeconds":60}}
  ]},
  "createdAt":"2026-07-29T12:00:00Z",
  "updatedAt":"2026-07-29T12:00:00Z"
}
```

La respuesta real incluye todos los steps normalizados y sus defaults. Ciclos,
dependencias inexistentes, IDs repetidos y URLs invalidas devuelven `400`.

### `GET /v1/workflows`

```bash
curl --fail-with-body "$NIMBUS_URL/v1/workflows" \
  -H "Authorization: Bearer $NIMBUS_TOKEN"
```

```json
{"items":[{"id":"11111111-1111-4111-8111-111111111111","name":"Order operations","status":"active","version":1,"definition":{"schemaVersion":1,"eventType":"order.created","steps":[{"id":"validate","type":"noop","timeoutSeconds":10,"retry":{"maxAttempts":3,"initialDelaySeconds":2,"maxDelaySeconds":60}}]},"createdAt":"2026-07-29T12:00:00Z","updatedAt":"2026-07-29T12:00:00Z","approvalRequired":false}]}
```

### `POST /v1/workflows/{workflowId}/versions`

```bash
curl --fail-with-body "$NIMBUS_URL/v1/workflows/$WORKFLOW_ID/versions" \
  -H "Authorization: Bearer $NIMBUS_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "schemaVersion":1,
    "eventType":"order.created",
    "steps":[{"id":"validate_v2","type":"noop"}]
  }'
```

Respuesta `201`: workflow con `version: 2`. Crear la version no la activa.

### `POST /v1/workflows/{workflowId}/versions/{version}/activations`

```bash
curl --fail-with-body -X POST \
  "$NIMBUS_URL/v1/workflows/$WORKFLOW_ID/versions/2/activations" \
  -H "Authorization: Bearer $NIMBUS_TOKEN"
```

```json
{"status":"active"}
```

Ejecuciones existentes conservan la version anterior.

### `POST /v1/workflows/{workflowId}/events`

```bash
curl --fail-with-body "$NIMBUS_URL/v1/workflows/$WORKFLOW_ID/events" \
  -H "Authorization: Bearer $NIMBUS_TOKEN" \
  -H 'Idempotency-Key: order-42-created' \
  -H 'Content-Type: application/json' \
  -d '{"type":"order.created","payload":{"orderId":"ord-42"}}'
```

Respuesta `202` sin approval:

```json
{"eventId":"22222222-2222-4222-8222-222222222222","executionId":"33333333-3333-4333-8333-333333333333","duplicate":false}
```

Con approval:

```json
{"eventId":"22222222-2222-4222-8222-222222222222","executionId":"33333333-3333-4333-8333-333333333333","duplicate":false,"approvalId":"44444444-4444-4444-8444-444444444444"}
```

Repetir tenant, workflow e `Idempotency-Key` devuelve los IDs originales con
`duplicate: true`.

## Ejecuciones

### `GET /v1/executions/{executionId}`

```bash
curl --fail-with-body "$NIMBUS_URL/v1/executions/$EXECUTION_ID" \
  -H "Authorization: Bearer $NIMBUS_TOKEN"
```

```json
{
  "id":"33333333-3333-4333-8333-333333333333",
  "workflowId":"11111111-1111-4111-8111-111111111111",
  "eventId":"22222222-2222-4222-8222-222222222222",
  "status":"succeeded",
  "startedAt":"2026-07-29T12:01:01Z",
  "finishedAt":"2026-07-29T12:01:02Z",
  "createdAt":"2026-07-29T12:01:00Z",
  "steps":[{"id":"99999999-9999-4999-8999-999999999999","stepId":"validate","type":"noop","status":"succeeded","attempt":1,"output":{"result":"noop"},"startedAt":"2026-07-29T12:01:01Z","finishedAt":"2026-07-29T12:01:02Z"}]
}
```

Estados posibles: `awaiting_approval`, `queued`, `running`, `succeeded`,
`failed` y `cancelled`.

## API Keys Y RBAC

### `POST /v1/api-keys`

Requiere owner.

```bash
curl --fail-with-body "$NIMBUS_URL/v1/api-keys" \
  -H "Authorization: Bearer $NIMBUS_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"name":"Portfolio approver","role":"approver","expiresInDays":30}'
```

Respuesta `201`:

```json
{
  "id":"55555555-5555-4555-8555-555555555555",
  "name":"Portfolio approver",
  "prefix":"nmb_example",
  "role":"approver",
  "expiresAt":"2026-08-28T12:00:00Z",
  "createdAt":"2026-07-29T12:00:00Z",
  "token":"nmb_PLAINTEXT_RETURNED_ONCE"
}
```

El token completo solo aparece en esta respuesta.

### `GET /v1/api-keys`

```bash
curl --fail-with-body "$NIMBUS_URL/v1/api-keys" \
  -H "Authorization: Bearer $NIMBUS_TOKEN"
```

```json
{"items":[{"id":"55555555-5555-4555-8555-555555555555","name":"Portfolio approver","prefix":"nmb_example","role":"approver","createdAt":"2026-07-29T12:00:00Z"}]}
```

Nunca devuelve el secreto.

### `DELETE /v1/api-keys/{apiKeyId}`

```bash
curl --fail-with-body -X DELETE \
  "$NIMBUS_URL/v1/api-keys/$API_KEY_ID" \
  -H "Authorization: Bearer $NIMBUS_TOKEN"
```

Respuesta `204` sin body. La key revocada devuelve `401` y no se reactiva tras
reiniciar Nimbus.

## Approvals

### `GET /v1/approvals`

```bash
curl --fail-with-body "$NIMBUS_URL/v1/approvals" \
  -H "Authorization: Bearer $NIMBUS_TOKEN"
```

```json
{"items":[{"id":"44444444-4444-4444-8444-444444444444","executionId":"33333333-3333-4333-8333-333333333333","status":"pending","requestedBy":"PRINCIPAL_ID","expiresAt":"2026-07-30T12:00:00Z","createdAt":"2026-07-29T12:00:00Z"}]}
```

### `POST /v1/approvals/{approvalId}/decisions`

Usa una key `approver` diferente del solicitante:

```bash
curl --fail-with-body "$NIMBUS_URL/v1/approvals/$APPROVAL_ID/decisions" \
  -H "Authorization: Bearer $APPROVER_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"decision":"approved","comment":"Evidence and blast radius reviewed"}'
```

```json
{"id":"44444444-4444-4444-8444-444444444444","executionId":"33333333-3333-4333-8333-333333333333","status":"approved","requestedBy":"REQUESTER_ID","decidedBy":"APPROVER_ID","expiresAt":"2026-07-30T12:00:00Z","decidedAt":"2026-07-29T12:05:00Z","createdAt":"2026-07-29T12:00:00Z"}
```

Decisiones: `approved` o `rejected`. Autoaprobacion: `403`. Solicitud expirada o
decision contradictoria: `409`.

## Pipelines E Incidentes

### `POST /v1/pipelines`

```bash
curl --fail-with-body "$NIMBUS_URL/v1/pipelines" \
  -H "Authorization: Bearer $NIMBUS_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "name":"Orders async pipeline",
    "mode":"fixture",
    "region":"us-east-1",
    "queueArn":"arn:aws:sqs:us-east-1:123456789012:orders",
    "lambdaArn":"arn:aws:lambda:us-east-1:123456789012:function:orders-consumer",
    "dlqArn":"arn:aws:sqs:us-east-1:123456789012:orders-dlq",
    "incidentWorkflowId":"WORKFLOW_ID",
    "recoveryWorkflowId":"RECOVERY_WORKFLOW_ID",
    "consecutiveWindows":2,
    "backlogThreshold":100,
    "ageThresholdSeconds":300
  }'
```

`incidentWorkflowId` recibe `nimbus.incident.opened` y `recoveryWorkflowId`
recibe `nimbus.incident.recovered`; ambos son opcionales. `escalationWorkflowId`
(opcional) recibe `nimbus.incident.escalated` cuando la ejecucion de mitigacion
(la disparada por `nimbus.incident.opened`) falla definitivamente: el incidente
queda con `escalatedAt`, la linea de tiempo registra `escalated` (con el paso y el
error) y el payload incluye `reason`. Se escala una sola vez por incidente y nunca
si ya se recupero. Ese workflow debe solo notificar: no puede depender de lo que fallo. Si la version activa
del workflow escucha otro `eventType`, la API responde `400`. El payload incluye
`incidentId`, `event`, `pipelineId`, `pipelineName`, `detector`, `severity`,
`summary` y `occurredAt` (RFC 3339, cuando ocurrio la transicion: `openedAt` o
`recoveredAt`; los pasos se ejecutan en orden y con reintentos, asi que la hora de
entrega puede ser posterior), nunca credenciales ni metricas crudas del cliente.

Respuesta `201`:

```json
{"id":"66666666-6666-4666-8666-666666666666","name":"Orders async pipeline","mode":"fixture","region":"us-east-1","queueArn":"arn:aws:sqs:us-east-1:123456789012:orders","lambdaArn":"arn:aws:lambda:us-east-1:123456789012:function:orders-consumer","dlqArn":"arn:aws:sqs:us-east-1:123456789012:orders-dlq","incidentWorkflowId":"11111111-1111-4111-8111-111111111111","status":"active","consecutiveWindows":2,"backlogThreshold":100,"ageThresholdSeconds":300,"createdAt":"2026-07-29T12:00:00Z"}
```

Modo `aws` exige `roleArn` y `externalIdRef`. La referencia usa el formato
`aws-external-id/NOMBRE` y referencia una entrada de `NIMBUS_AWS_EXTERNAL_IDS`;
no contiene el secreto ni puede leer otras variables del proceso. Una referencia
desconocida falla antes de llamar AWS.

### `GET /v1/pipelines`

```bash
curl --fail-with-body "$NIMBUS_URL/v1/pipelines" \
  -H "Authorization: Bearer $NIMBUS_TOKEN"
```

```json
{"items":[{"id":"66666666-6666-4666-8666-666666666666","name":"Orders async pipeline","mode":"fixture","region":"us-east-1","queueArn":"arn:aws:sqs:us-east-1:123456789012:orders","lambdaArn":"arn:aws:lambda:us-east-1:123456789012:function:orders-consumer","dlqArn":"arn:aws:sqs:us-east-1:123456789012:orders-dlq","incidentWorkflowId":"11111111-1111-4111-8111-111111111111","status":"active","consecutiveWindows":2,"backlogThreshold":100,"ageThresholdSeconds":300,"createdAt":"2026-07-29T12:00:00Z"}]}
```

### `POST /v1/pipelines/{pipelineId}/observations`

Solo para `fixture`:

```bash
curl --fail-with-body "$NIMBUS_URL/v1/pipelines/$PIPELINE_ID/observations" \
  -H "Authorization: Bearer $NIMBUS_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "windowStartedAt":"2026-07-29T12:00:00Z",
    "windowEndedAt":"2026-07-29T12:05:00Z",
    "visibleMessages":200,
    "inFlightMessages":0,
    "oldestMessageSeconds":600,
    "dlqMessages":0,
    "lambdaInvocations":0,
    "lambdaErrors":0,
    "lambdaThrottles":0
  }'
```

La ventana debe durar exactamente cinco minutos, comenzar en un limite UTC de
cinco minutos, no estar en el futuro y tener menos de 24 horas. Solo ventanas
realmente contiguas cuentan como una falla sostenida.

Primera ventana, respuesta `202`:

```json
{"incidents":[]}
```

Segunda ventana consecutiva:

```json
{"incidents":[{"id":"77777777-7777-4777-8777-777777777777","pipelineId":"66666666-6666-4666-8666-666666666666","detector":"consumer_stalled","status":"open","severity":"critical","summary":"Queue backlog is aging while the Lambda consumer has no invocations","evidence":{},"openedAt":"2026-07-29T12:10:00Z"}]}
```

Repetir una ventana reutiliza la observacion persistida; no cambia la evidencia.

### `POST /v1/pipelines/{pipelineId}/checks`

Solo para `aws`:

```bash
curl --fail-with-body -X POST "$NIMBUS_URL/v1/pipelines/$PIPELINE_ID/checks" \
  -H "Authorization: Bearer $NIMBUS_TOKEN"
```

Respuesta: el mismo formato `{"incidents":[]}`. Devuelve `502` si credenciales,
AssumeRole o la ventana completa de siete metricas no estan disponibles; el
`detail` incluye un codigo estable y una pista sin datos de AWS:
`configuration`, `access_denied`, `throttled`, `timeout`, `incomplete_window` o
`unavailable`.
Cada intento consume el presupuesto horario del tenant
(`NIMBUS_MAX_AWS_CHECKS_PER_HOUR`, por defecto 60), aunque AWS falle; al
agotarse devuelve `429` sin llamar a CloudWatch y cada intento queda en audit
como `pipeline.check`.

Con `NIMBUS_AWS_SCHEDULER_ENABLED=true`, los workers chequean cada pipeline
`aws` activo una vez por ventana cerrada de cinco minutos, con jitter de hasta
dos minutos y el mismo presupuesto por tenant, sin necesidad de llamar a este
endpoint.

### `GET /v1/incidents`

```bash
curl --fail-with-body "$NIMBUS_URL/v1/incidents" \
  -H "Authorization: Bearer $NIMBUS_TOKEN"
```

```json
{"items":[{"id":"77777777-7777-4777-8777-777777777777","pipelineId":"66666666-6666-4666-8666-666666666666","detector":"consumer_stalled","status":"open","severity":"critical","summary":"Queue backlog is aging while the Lambda consumer has no invocations","evidence":{},"openedAt":"2026-07-29T12:10:00Z"}]}
```

### `POST /v1/incidents/{incidentId}/acknowledgements`

```bash
curl --fail-with-body -X POST \
  "$NIMBUS_URL/v1/incidents/$INCIDENT_ID/acknowledgements" \
  -H "Authorization: Bearer $NIMBUS_TOKEN"
```

```json
{"status":"acknowledged"}
```

Solo un incidente `open` puede reconocerse. Ventanas saludables sostenidas lo
cambian a `recovered`.

### `POST /v1/incidents/{incidentId}/reviews`

Requiere rol `owner` u `operator`. `approver` y `viewer` solo pueden leer el
incidente y no pueden clasificarlo.

```bash
curl --fail-with-body "$NIMBUS_URL/v1/incidents/$INCIDENT_ID/reviews" \
  -H "Authorization: Bearer $NIMBUS_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"disposition":"false_positive","comment":"Deploy planificado durante la ventana"}'
```

```json
{"id":"99999999-9999-4999-8999-999999999999","disposition":"false_positive","reviewerPrincipalId":"REVIEWER_ID","comment":"Deploy planificado durante la ventana","reviewedAt":"2026-07-29T13:00:00Z"}
```

Las disposiciones permitidas son `true_positive`, `false_positive`,
`expected_behavior` y `duplicate`. No cambian `status`: acknowledgement,
recovery y resolution siguen siendo estados del ciclo de vida independientes.
El comentario es opcional y admite como maximo 500 caracteres. Cada nueva
clasificacion crea una revision y un audit event; reclasificar no sobrescribe el
historial. `GET /v1/incidents` muestra la ultima revision en el campo `review`.

### `GET /v1/reports/incident-quality/weekly`

Disponible para todos los roles con lectura de incidentes. `weeksAgo` es un
entero entre `0` y `52`; `0` selecciona la semana UTC actual. Las semanas son
intervalos semiabiertos `[lunes 00:00 UTC, lunes siguiente 00:00 UTC)` basados en
`openedAt`.

```bash
curl --fail-with-body \
  "$NIMBUS_URL/v1/reports/incident-quality/weekly?weeksAgo=1" \
  -H "Authorization: Bearer $NIMBUS_TOKEN"
```

```json
{"weekStart":"2026-07-20T00:00:00Z","weekEnd":"2026-07-27T00:00:00Z","items":[{"pipelineId":"66666666-6666-4666-8666-666666666666","pipelineName":"Orders async pipeline","detector":"consumer_stalled","total":5,"reviewed":4,"unreviewed":1,"falsePositives":1,"truePositives":1,"expectedBehavior":1,"duplicates":1,"falsePositiveRate":0.5}]}
```

El reporte agrupa por pipeline y detector, ordenado por nombre de pipeline, ID y
detector. Usa solo la clasificacion mas reciente de cada incidente. La tasa es
`false_positive / (false_positive + true_positive)`; no incluye incidentes sin
revision, `expected_behavior` ni `duplicate` en el denominador. Si no hay
clasificaciones accionables la tasa es `0`.

## Analitica Y Auditoria

### `GET /v1/analytics/overview`

```bash
curl --fail-with-body "$NIMBUS_URL/v1/analytics/overview" \
  -H "Authorization: Bearer $NIMBUS_TOKEN"
```

```json
{"events24h":3,"executions24h":3,"succeeded24h":3,"failed24h":0,"successRate":1,"queuedSteps":0,"oldestQueuedSeconds":0}
```

### `GET /v1/audit-events`

Requiere owner.

```bash
curl --fail-with-body "$NIMBUS_URL/v1/audit-events" \
  -H "Authorization: Bearer $NIMBUS_TOKEN"
```

```json
{"items":[{"id":"88888888-8888-4888-8888-888888888888","actorId":"APPROVER_ID","action":"approval.decide","resourceType":"approval","resourceId":"44444444-4444-4444-8444-444444444444","result":"succeeded","metadata":{"decision":"approved"},"occurredAt":"2026-07-29T12:05:00Z"}]}
```

## Matriz De Errores

| Codigo | Significado habitual |
| --- | --- |
| `400` | Contrato, DAG, evento, rol, decision o ventana invalidos |
| `401` | Token ausente, desconocido, expirado o revocado |
| `403` | Rol insuficiente, autoaprobacion o fixture sobre pipeline AWS |
| `404` | Recurso no encontrado dentro del tenant autenticado |
| `409` | Approval expirado o ya decidido de otra forma |
| `413` | Body mayor de 1 MiB |
| `502` | CloudWatch/AssumeRole no produjo una ventana valida |
| `503` | PostgreSQL no esta listo |

No se deben registrar tokens, authorization headers, payloads completos ni
respuestas sensibles al investigar estos errores.

## Endpoints Agregados En 0.2.0

El contrato completo y versionado esta en `api/openapi.json`. Resumen:

| Metodo y ruta | Permiso | Uso |
| --- | --- | --- |
| `GET /v1/me` | autenticado | Principal actual, rol y lista de permisos efectivos |
| `GET /v1/workflows/{workflowId}` | `workflow:read` | Workflow con definicion activa y numeros de version |
| `GET /v1/executions?status=&workflowId=&limit=&cursor=` | `execution:read` | Ejecuciones paginadas por cursor opaco (`nextCursor`) |
| `GET /v1/pipelines/{pipelineId}` | `pipeline:read` | Configuracion de un pipeline |
| `GET /v1/incidents?status=active` | `incident:read` | Filtro por estado; `active` = open o acknowledged |
| `GET /v1/incidents/{incidentId}` | `incident:read` | Incidente con ultima revision y timeline completo |
| `POST /v1/incidents/{incidentId}/resolutions` | `incident:resolve` | Cierra el incidente (idempotente) y publica `incident.resolved` |
| `GET /v1/members` | `identity:read` | Miembros del tenant con tipo de credencial |
| `PATCH /v1/members/{memberId}` | `identity:write` | Cambia rol o suspende; no se permite sobre uno mismo |

Paginacion de ejecuciones:

```bash
curl --fail-with-body "http://localhost:8080/v1/executions?limit=2" \
  -H 'Authorization: Bearer nimbus-local-development-key'
# Usar el valor nextCursor devuelto:
curl --fail-with-body "http://localhost:8080/v1/executions?limit=2&cursor=NEXT_CURSOR" \
  -H 'Authorization: Bearer nimbus-local-development-key'
```

Inicio de sesion OIDC: enviar el access token del proveedor como
`Authorization: Bearer <jwt>`. El primer acceso crea una membresia `viewer`; un
owner la promueve con `PATCH /v1/members/{memberId}`.
