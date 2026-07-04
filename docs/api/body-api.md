# Body API

Default endpoint: `http://127.0.0.1:6120`.

## Implemented in Stage 1

- `GET /v1/body/health`
- `GET /v1/config`
- `PUT /v1/config`
- `GET /v1/coordinator/probe`
- `POST /v1/init`
- `GET /v1/operations`
- `GET /v1/operations/:operationId`

Operations return `operationId`, `traceId`, `state`, `stage`, `progress`, `startedAt`, `completedAt`, `errorCode`, and `message`.

Errors include `errorCode`, `message`, `details.url`, `details.method`, `details.httpStatus`, `details.responseBody`, `details.configPath`, and `details.coordinatorUrl` when available.
