# API Contract

`openapi.json` is **generated** from the Huma operation registrations in
`internal/httpapi`. Do not edit it by hand.

Regenerate after changing any handler input, output, or operation:

```bash
make openapi   # go run ./cmd/nimbus openapi > api/openapi.json
```

`TestOpenAPISnapshotIsCurrent` fails when the snapshot drifts from the code,
and the dashboard derives its TypeScript types from this file
(`web/src/shared/api/schema.d.ts`, `npm --prefix web run generate:api`), so a
breaking change surfaces in review as a diff here and as type errors in `web/`.
