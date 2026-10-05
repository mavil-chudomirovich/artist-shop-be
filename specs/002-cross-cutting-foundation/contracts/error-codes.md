# Error Code Catalogue

Stable machine-readable codes shared by all modules. Each code has exactly one
HTTP status. Codes are never reused with a different meaning; add new codes
rather than changing existing ones.

## Common codes (foundation)

| Code | HTTP | Meaning |
|------|------|---------|
| `VALIDATION_ERROR` | 400 | One or more fields failed validation; see `details`. |
| `MALFORMED_REQUEST` | 400 | Body/parameters could not be parsed. |
| `UNAUTHENTICATED` | 401 | Missing or invalid credentials (implemented by module auth). |
| `FORBIDDEN` | 403 | Authenticated but not permitted. |
| `NOT_FOUND` | 404 | The requested resource does not exist. |
| `METHOD_NOT_ALLOWED` | 405 | Route exists but not for this method. |
| `CONFLICT` | 409 | Request conflicts with current state (e.g., duplicate). |
| `PAYLOAD_TOO_LARGE` | 413 | Request body exceeds the configured limit. |
| `UNSUPPORTED_MEDIA_TYPE` | 415 | Request body is not JSON. |
| `RATE_LIMITED` | 429 | Too many requests; retry later. |
| `INTERNAL_ERROR` | 500 | Unexpected internal failure; no details exposed. |
| `SERVICE_UNAVAILABLE` | 503 | A dependency or the schema is not ready. |

## Namespacing for future modules

Modules add their own codes with a module prefix, e.g.:

- `AUTH_INVALID_CREDENTIALS` (401), `AUTH_EMAIL_TAKEN` (409),
  `AUTH_TOKEN_EXPIRED` (401)
- `ORDER_INVALID_STATE` (409), `PRODUCT_NOT_AVAILABLE` (409)

Codes are registered centrally so duplicates are detected at build/test time.
