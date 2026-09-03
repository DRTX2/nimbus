# Guia De Ejecucion Y Operacion

## 1. Primera Ejecucion

```bash
make dev
```

Comprueba:

```bash
curl --fail-with-body http://localhost:8080/health/live
curl --fail-with-body http://localhost:8080/health/ready
```

Abre Scalar en <http://localhost:8080/docs>. Usa el token local solo en
desarrollo:

```text
nimbus-local-development-key
```

Nimbus rechaza este token si `NIMBUS_ENV=production`.

## 2. Demo Minima De Workflow

1. Crea un workflow desde Scalar o con el ejemplo de `API_REFERENCE.es.md`.
2. Guarda el `id` retornado.
3. Ingiere un evento con un `Idempotency-Key` unico.
4. Consulta `executionId` hasta obtener `succeeded` o `failed`.
5. Repite exactamente la ingestion y comprueba `duplicate: true`.

Esta demo prueba validacion DAG, transaccion de ingestion, workers, leases,
dependencias, outbox e idempotencia.

## 3. Demo De Producto

Ejecuta en este orden:

1. Crea un workflow `nimbus.incident.opened` con step HTTP `bodyMode: event`.
2. Crea un pipeline `fixture` con `incidentWorkflowId`.
3. Envia una ventana degradada: no debe abrir incidente si se requieren dos.
4. Envia la segunda ventana degradada: debe abrir un unico incidente.
5. Consulta `/v1/incidents`.
6. Envia dos ventanas saludables: el incidente debe pasar a `recovered`.

Los cuerpos completos estan en `PHASES_4_6.md` y `API_REFERENCE.es.md`.

## 4. Demo De Gobernanza

1. Crea una API key con rol `approver` y guarda el token mostrado una vez.
2. Crea un workflow con `approvalRequired: true`.
3. Ingiere un evento con el owner u operator.
4. Comprueba que la ejecucion esta `awaiting_approval` y no tiene steps.
5. Intenta aprobar con el solicitante: respuesta esperada `403`.
6. Aprueba con la key `approver`: respuesta `200`.
7. Comprueba que la ejecucion termina.
8. Revoca la key temporal y verifica que deja de autenticar.
9. Consulta `/v1/audit-events`.

## 5. Demo De Resiliencia Operativa

Inicia el perfil separado:

```bash
make dev-down
make dev-split
```

Deten solo el worker:

```bash
docker compose -f compose.yaml -f compose.split.yaml stop nimbus-worker
```

Ingiere un evento. El API debe responder `202` porque PostgreSQL confirma el
trabajo antes de ejecutarlo. Reinicia el worker:

```bash
docker compose -f compose.yaml -f compose.split.yaml start nimbus-worker
```

La ejecucion debe salir del backlog. Perder un ciclo de polling no pierde datos.

## 6. AWS Read-Only

El modo AWS usa la cadena estandar de credenciales, opcionalmente asume un rol y
solo llama `cloudwatch:GetMetricData`.

1. Despliega el modulo `infra/terraform/modules/aws-pipeline-readonly` en la
   cuenta observada.
2. Configura un external ID aleatorio fuera del payload del workflow.
3. Agrega el valor al mapa JSON exclusivo del API, por ejemplo
   `NIMBUS_AWS_EXTERNAL_IDS={"aws-external-id/acme-prod":"valor-aleatorio"}`.
4. Registra el pipeline con `externalIdRef: aws-external-id/acme-prod`.
5. Ejecuta `POST /v1/pipelines/{id}/checks`.

Nimbus rechaza la ventana si CloudWatch no devuelve las siete series completas.
No consulta mensajes SQS ni CloudWatch Logs. La referencia nunca puede nombrar
una variable del proceso: solo resuelve entradas del mapa dedicado y falla de
forma cerrada si la entrada no existe. La consulta usa el periodo UTC completo
anterior para tolerar el retraso normal de publicacion de metricas.

## 7. Troubleshooting

### Readiness `503`

```bash
docker compose logs postgres
docker compose logs nimbus
```

Revisa `NIMBUS_DATABASE_URL` y el health check de PostgreSQL.

### API `401`

- Usa `Authorization: Bearer TOKEN`.
- Comprueba expiracion o revocacion en `/v1/api-keys` con una key owner.
- Una key revocada no se reactiva al reiniciar.

### API `403`

El token es valido pero el rol no posee el permiso. Consulta la matriz en
`PROJECT_STATUS.es.md`. La autoaprobacion tambien devuelve `403`.

### Approval `409`

La solicitud ya fue decidida, expiro o la ejecucion ya no esta esperando.

### Pipeline fixture `403`

No se pueden insertar observaciones fixture en un pipeline `aws`.

### Check AWS `502`

Revisa credenciales base, `roleArn`, external ID, region y disponibilidad de las
siete metricas. Nimbus no convierte datos parciales en ceros.

### HTTP step bloqueado

Destinos privados, loopback, link-local y metadata cloud estan bloqueados. Una
allowlist local se configura con `NIMBUS_OUTBOUND_ALLOWED_HOSTS`, pero no debe
usarse para ampliar egress sin revision de riesgo.

### Repetir desde cero

```bash
make dev-down
docker compose -f compose.yaml -f compose.split.yaml down --volumes --remove-orphans
make dev
```

Esto elimina todos los datos locales. No usar contra entornos compartidos.
