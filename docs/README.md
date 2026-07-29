# Documentacion De Nimbus

Este directorio separa producto, arquitectura, operacion y evidencia para que un
lector pueda encontrar informacion sin recorrer todo el repositorio.

| Documento | Uso |
| --- | --- |
| [`PROJECT_STATUS.es.md`](PROJECT_STATUS.es.md) | Estado real, fases terminadas, limites y roadmap pendiente |
| [`EXECUTION_GUIDE.es.md`](EXECUTION_GUIDE.es.md) | Instalacion, arranque, demos, parada y troubleshooting |
| [`COMMAND_REFERENCE.es.md`](COMMAND_REFERENCE.es.md) | Todos los comandos de desarrollo, calidad, Docker, Kubernetes, Terraform y OpenCode |
| [`API_REFERENCE.es.md`](API_REFERENCE.es.md) | Todos los endpoints con `curl`, respuesta esperada y errores frecuentes |
| [`VALIDATION_REPORT.es.md`](VALIDATION_REPORT.es.md) | Evidencia de verificaciones ejecutadas y smoke tests realizados |
| [`ARCHITECTURE.md`](ARCHITECTURE.md) | Decisiones, garantias y evolucion de arquitectura |
| [`REQUIREMENTS.md`](REQUIREMENTS.md) | Requerimientos y criterios de aceptacion |
| [`PRODUCT.md`](PRODUCT.md) | Problema, cliente objetivo, diferenciacion y modelo comercial |
| [`PHASES_4_6.md`](PHASES_4_6.md) | Demo de producto, gobernanza y separacion API/worker |
| [`DEPLOYMENT.md`](DEPLOYMENT.md) | Despliegue local, Kubernetes y sandbox AWS |
| [`SRE_OPERATIONS.md`](SRE_OPERATIONS.md) | Senales PostgreSQL, alertas beta y diagnostico inicial de colas/outbox |
| [`LEARNING_GUIDE.es.md`](LEARNING_GUIDE.es.md) | Recorrido pedagogico para estudiar el codigo |

## Fuente De Verdad

- Contrato HTTP ejecutable: <http://localhost:8080/openapi.json>.
- Documentacion interactiva: <http://localhost:8080/docs>.
- Migraciones: `internal/platform/database/migrations/`.
- Comandos canonicos: `Makefile` y `AGENTS.md`.
- Estado de seguridad: `SECURITY.md`.

Si un ejemplo manual contradice OpenAPI, prevalece OpenAPI. Si una afirmacion de
estado contradice una verificacion reciente, actualiza primero el informe de
validacion y despues este indice.
