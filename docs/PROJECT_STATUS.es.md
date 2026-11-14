# Estado Actual Del Proyecto

Fecha del estado: **2026-10-21**.

## Resumen Ejecutivo

Nimbus es una beta de portafolio funcional para automatizacion confiable de
eventos y monitorizacion explicable de pipelines AWS asincronos. No es una demo
de CRUD: persiste trabajo durable, ejecuta DAGs at-least-once, gobierna acciones
mediante approvals y puede separar API y workers usando la misma imagen.

El producto funciona localmente sin AWS. La integracion CloudWatch real es
opt-in y read-only. No se ha creado infraestructura cloud real.

## Estado Por Fase

| Fase | Estado | Entregado |
| --- | --- | --- |
| 1. Fundacion | Completa MVP | API Huma/Chi, PostgreSQL, migraciones, config, Scalar, Docker |
| 2. Runtime confiable | Completa MVP | DAG, paralelismo, leases, fencing, retries, idempotencia, SSRF guard |
| 3. Operacion | Completa MVP | Prometheus, Grafana, Jaeger/OTLP, health, CI, DevSecOps, IaC sandbox |
| 4. Producto | Beta funcional | Pipelines fixture/AWS, siete metricas, cuatro detectores, incidentes, workflow webhook |
| 5. Gobernanza | Beta funcional | API keys, RBAC, approvals, audit events, versiones, RLS schema, external-ID reference |
| 6. Escalado evolutivo | Beta funcional | Outbox, retry/fencing, componentes `all|api|worker`, Compose y Kubernetes split |
| 6b. Seguridad hosted y UI | Beta funcional | OIDC con sesiones bajo RLS, administracion de miembros, webhooks firmados con rotacion, cuotas, dashboard web, suites Gherkin |
| 7. Data/AI | Pendiente condicionado | Export gobernado, backtesting, baselines robustos, resumen AI opcional |

## Funcionalidad Disponible

### Workflows

- Contrato versionado y normalizado.
- Deteccion de ciclos y dependencias invalidas.
- Versiones inmutables y activacion explicita.
- Steps `noop` y HTTP.
- Body HTTP estatico o payload normalizado del evento.
- Timeouts, retries y backoff acotados.
- Ingestion idempotente.
- Ejecucion paralela de ramas listas.

### Producto De Reliability

- Pipeline `SQS -> Lambda -> DLQ` en modo fixture o AWS.
- CloudWatch `GetMetricData` con siete queries en un batch acotado.
- Rechazo de ventanas parciales.
- Detectores `consumer_stalled`, `lambda_throttled`, `dlq_growth` con semantica
  de DLQ no vacia, y `retry_storm`.
- Ventanas consecutivas antes de abrir o recuperar.
- Un incidente activo por pipeline/detector.
- Acknowledgement, recovery, evidencia y timeline persistido.
- Revision append-only con disposiciones acotadas, reviewer, timestamp,
  comentario opcional y audit event por clasificacion o reclasificacion.
- Reporte semanal UTC por pipeline/detector con cobertura de revision, conteos
  de disposiciones y tasa de falsos positivos sobre true/false positives.
- Outbox `incident.opened` e `incident.recovered` que inician los workflows de
  respuesta y de recuperacion del pipeline con contexto accionable (pipeline,
  detector, severidad, resumen).

### Gobernanza

- API keys almacenadas como SHA-256, con prefijo, expiracion y revocacion.
- Token completo devuelto una sola vez.
- Roles `owner`, `operator`, `approver` y `viewer`.
- Separacion de funciones: el solicitante no puede autoaprobar.
- Approval pendiente, aprobado, rechazado o expirado.
- Audit events para workflow, ingestion, pipeline, API keys y approvals.
- Principal `system` separado para automatizaciones internas.
- Politicas RLS definidas para tablas tenant-scoped.

### Operacion

- PostgreSQL como autoridad y cola durable.
- Migraciones/bootstrap como fase one-shot; API y workers validan checksums y no
  ejecutan DDL al iniciar.
- Outbox transaccional con lease, retry, fencing e idempotency key.
- API y worker escalables por separado.
- Polling como fallback de correccion, sin broker obligatorio.
- Gauges PostgreSQL de backlog ejecutable, edad, leases expirados y outbox;
  reglas Prometheus con pruebas `promtool` y alerta de frescura.
- Contenedores no root, filesystem read-only y capacidades eliminadas.
- Puertos locales ligados a `127.0.0.1`.
- Terraform sandbox sin ingress, administrado por SSM.

## Matriz De Roles

| Capacidad | Owner | Operator | Approver | Viewer |
| --- | --- | --- | --- | --- |
| Leer workflows/ejecuciones/analytics | Si | Si | Si | Si |
| Crear workflows e ingerir eventos | Si | Si | No | No |
| Crear pipelines y observaciones | Si | Si | No | No |
| Leer incidentes | Si | Si | Si | Si |
| Reconocer incidentes | Si | Si | No | No |
| Clasificar/reclasificar incidentes | Si | Si | No | No |
| Leer approvals | Si | Si | Si | Si |
| Decidir approvals | Si, salvo propios | No | Si, salvo propios | No |
| Gestionar API keys | Si | No | No | No |
| Leer audit events | Si | No | No | No |

## Estado Del Entorno

Perfil activo al emitir este documento: **split**.

| Componente | URL | Estado validado |
| --- | --- | --- |
| API/Scalar | <http://localhost:8080/docs> | Healthy |
| Worker operations | <http://localhost:8081/health/ready> | `204` |
| PostgreSQL | Red interna Compose | Healthy |
| Prometheus | <http://localhost:9090> | Healthy |
| Grafana | <http://localhost:3000> | Healthy |
| Jaeger | <http://localhost:16686> | Healthy |

Existen datos locales de smoke test en el volumen PostgreSQL. No son secretos ni
datos de clientes. El token temporal de approver usado durante validacion fue
revocado.

## Limites Conocidos

- La entrega externa es at-least-once; el receptor debe respetar
  `Idempotency-Key`.
- No hay renovacion de lease; el timeout maximo del step debe permanecer menor
  que el lease.
- El scheduler CloudWatch es opt-in (`NIMBUS_AWS_SCHEDULER_ENABLED`); solo se
  valido con un CloudWatch simulado, no contra una cuenta AWS real.
- No hay descubrimiento automatico de topologias AWS.
- RLS se fuerza con roles locales no propietarios para API y worker y contexto
  tenant transaccional. Produccion debe provisionar esos roles antes de migrar.
- OIDC/JWKS implementado (discovery, allowlist de algoritmos, claim de tenant,
  sesiones efimeras bajo RLS); no hay SCIM ni sincronizacion automatica de grupos.
- No hay proveedor general de secretos. Los webhooks salientes se firman con
  HMAC-SHA256 (`Nimbus-Signature`, rotacion por multiples secretos activos).
- Analytics actual es operacional de 24 horas, no un BI historico.
- El reporte de calidad cubre como maximo 52 semanas hacia atras y no sustituye
  entrevistas, resultados operativos de clientes ni validacion comercial.
- El dashboard `/app/` cubre respuesta a incidentes, workflows, ejecuciones,
  approvals, acceso y calidad; Scalar sigue disponible para el contrato.
- No hay despliegue AWS real, dominio, TLS publico ni SLA demostrado.
- No se han realizado entrevistas con clientes ni existen design partners
  reales; esas cifras siguen siendo objetivos de validacion.

## Fases Pendientes

### Prioridad 1: Hosted Security

- OIDC/JWKS con issuer, audience, algoritmos y cache acotados. (COMPLETADO)
- Webhooks firmados con rotacion de secreto. (COMPLETADO; la ventana antireplay la aplica el receptor)
- Cuotas de ingestion y concurrencia por tenant. (COMPLETADO, incluido presupuesto horario de checks CloudWatch;
  la cuota de ingestion se serializa por tenant con advisory lock y no se excede con peticiones concurrentes)
- Secret references en AWS SSM/Secrets Manager.

### Prioridad 2: Validacion Comercial

- Entrevistar cinco equipos y operar con tres design partners.
- Usar el reporte semanal implementado para medir calidad durante pruebas reales
  y acordar umbrales de aceptacion con participantes.
- Scheduler CloudWatch con presupuesto de llamadas y jitter. (COMPLETADO;
  ventanas cerradas de cinco minutos, claims en PostgreSQL, alerta
  `NimbusPipelineChecksNotObserving`)
- Descubrimiento read-only de event source mappings.
- Notificaciones Slack o webhook generico como nuevos `Connector`.
- Dashboard de incidentes y approvals. (COMPLETADO)

### Prioridad 3: Produccion Y SRE

- RDS, TLS, backups y restore drills. (Restore drill local COMPLETADO:
  `make restore-drill` verifica checksums y conteos; falta RDS/PITR real y TLS)
- Migraciones como job separado con estrategia expand/contract. (COMPLETADO;
  job one-shot, `nimbus verify-schema` para rollbacks, ver `SRE_OPERATIONS.md`)
- Validar con carga los umbrales iniciales de backlog/outbox y convertirlos en
  SLOs con error budgets basados en evidencia.
- Pruebas E2E y de aceptacion Gherkin (COMPLETADO, ver `docs/TESTING.md`: 71 escenarios de
  aceptacion y 15 journeys Playwright, incluido el enrutamiento de recuperacion); falta carga sostenida.
- SBOM SPDX del repositorio en CI (COMPLETADO, artifact `sbom` del workflow
  Security); falta provenance de release y publicacion por SHA.
- Runbooks de incidentes, rollback y disaster recovery. (COMPLETADO en
  `SRE_OPERATIONS.md`; DR sin ejercicio en nube real)

### Fase 7: Data, BI Y AI

- Retencion y clasificacion de datos por tenant.
- Export allowlisted a S3/Parquet.
- Consulta local con DuckDB y opcional Athena.
- Backtesting de detectores con datasets versionados.
- Baselines explicables por mediana/MAD y hora del dia.
- Resumen AI opcional, con evidencia citada y fuera del camino critico.

No se deben introducir Kafka, Redis, NATS, EKS o un data lake solo para marcar
tecnologias en el CV. Cada incorporacion requiere una metrica o necesidad de
producto documentada.
