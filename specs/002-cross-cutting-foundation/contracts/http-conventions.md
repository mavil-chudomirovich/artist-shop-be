# HTTP Conventions

Binding contract for every HTTP endpoint in the backend. Later modules MUST
follow it. Base path: `/api/v1` (versioned). Operational endpoints (`/healthz`,
`/readyz`) sit outside the versioned path.

## Correlation

- Clients MAY send `X-Request-Id` (a UUID). If absent or malformed, the server
  generates one.
- The effective correlation ID is returned in the `X-Request-Id` response header
  and included in error bodies and logs.

## Success envelope

```json
{
  "data": { " ...": "payload" },
  "meta": { "requestId": "9f1c...", "timestamp": "2026-09-18T10:00:00Z" }
}
```

- `data`: the payload; may be `null`, an object, or an array.
- `meta`: request metadata. `requestId` and `timestamp` are always present;
  pagination metadata (`page`, `pageSize`, `total`) is added to list responses.

### List responses

```json
{
  "data": [ { " ...": "item" } ],
  "meta": { "requestId": "...", "timestamp": "...", "page": 1, "pageSize": 20, "total": 137 }
}
```

## Error envelope

```json
{
  "error": {
    "code": "VALIDATION_ERROR",
    "message": "Human-readable, safe summary",
    "details": [ { "field": "email", "issue": "must be a valid email" } ],
    "requestId": "9f1c..."
  }
}
```

- `code`: stable, machine-readable value from `error-codes.md` (never changes
  meaning).
- `message`: safe, non-technical summary; MUST NOT reveal internals.
- `details`: optional array of field-level or contextual issues.
- `requestId`: matches `X-Request-Id`.
- No stack traces, SQL, or secret values are ever returned.

## Status mapping

| HTTP status | When |
|-------------|------|
| 200 / 201 | Success (200 read/update, 201 create) |
| 204 | Success with no body (deletes) |
| 400 | Malformed syntax or invalid input |
| 401 | Missing/invalid credentials (module auth) |
| 403 | Authenticated but not permitted |
| 404 | Resource not found |
| 405 | Method not allowed for the route |
| 409 | State/conflict violation |
| 413 | Request body too large |
| 415 | Unsupported media type |
| 429 | Rate limit exceeded |
| 500 | Unexpected internal failure (recovered panic included) |
| 503 | Dependency/schema not ready |

An error response body is returned for every non-2xx status that carries a body;
`204` and `HEAD` responses carry none.

## Content type

- Requests and responses are `application/json; charset=utf-8`.
- Only JSON request bodies are accepted; other media types yield `415`.

## Versioning

- All business endpoints live under `/api/v1`.
- Backward-incompatible changes require a new version segment; existing versions
  remain until explicitly retired.
