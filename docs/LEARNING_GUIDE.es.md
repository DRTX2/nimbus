# Guia de aprendizaje

Nimbus esta organizado para que puedas estudiarlo desde conceptos conocidos de
Spring Boot, .NET o NestJS hacia patrones idiomaticos de Go.

## Recorrido recomendado

1. Lee `cmd/nimbus/main.go`: equivale al composition root. Configura recursos,
   controla el ciclo de vida y hace graceful shutdown sin un contenedor DI.
2. Lee `internal/httpapi/api.go`: Huma registra handlers tipados y genera
   OpenAPI. Chi aporta routing y middleware. El `context.Context` lleva
   cancelacion, trazas y tenant.
3. Lee `internal/workflow/model.go`: la definicion se normaliza antes de ser
   persistida. La deteccion DFS evita DAGs ciclicos.
4. Lee `internal/store/workflows.go`: observa la transaccion atomica de evento,
   ejecucion y pasos raiz, y la restriccion de idempotencia.
5. Lee `internal/store/executions.go`: `SKIP LOCKED` permite varios workers sin
   que dos reclamen la misma fila. El lease recupera trabajo abandonado.
6. Lee `internal/execution/worker.go`: un numero fijo de goroutines hace polling,
   aplica timeout, clasifica errores y agenda retries.
7. Revisa `internal/platform/observability`: metricas con cardinalidad acotada y
   exportacion de trazas por OTLP.

## Equivalencias mentales

| Ecosistema previo | Nimbus/Go |
| --- | --- |
| Controller | Handler tipado de Huma |
| Dependency injection container | Constructores y wiring explicito en `main` |
| HostedService / scheduled worker | Goroutines controladas por contexto |
| ORM repository | SQL explicito con pgx |
| Annotation validation | Tags y validacion de dominio explicita |
| CancellationToken | `context.Context` |
| Exception middleware | Errores Huma y wrapping con `%w` |

## Experimentos seguros

1. Agrega un test que demuestre que un DAG con dependencia inexistente falla.
2. Deten `nimbus` despues de ingerir un evento y levantalo otra vez; el lease
   permite recuperar un paso abandonado.
3. Crea un step HTTP hacia un endpoint que responda 500 y observa el backoff.
4. Envia dos veces el mismo `Idempotency-Key` y comprueba que solo existe una
   ejecucion.
5. Sube `NIMBUS_WORKER_COUNT` y usa el race detector antes de comparar throughput.

## Siguientes incrementos pedagogicos

- Fase 4A: endpoint para crear una nueva version inmutable y activarla.
- Fase 4B: estudia la tabla outbox y el publicador ya implementados; evalua
  `LISTEN/NOTIFY` o NATS solo si las metricas justifican reducir el polling.
- Fase 4C: autenticacion OIDC, memberships y pruebas negativas por tenant.
- Fase 4D: detector AWS read-only para SQS/Lambda/DLQ.
- Fase 4E: exportacion gobernada a Parquet y backtesting con DuckDB.

No implementes todas a la vez. Cada incremento debe tener una historia de
usuario, criterio de aceptacion, medicion y prueba de recuperacion.
