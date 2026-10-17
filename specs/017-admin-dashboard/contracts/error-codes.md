# Contract: Error codes — Admin Dashboard Overview

This endpoint adds **no new machine-readable error code**. It answers with the shared
foundation catalogue only; `contracts/openapi.yaml` says where each appears.

## Codes reused

| Code | HTTP | Meaning | Retryable |
|---|---|---|---|
| `VALIDATION_ERROR` | 400 | `days` is not 7 or 30, or `lowStockThreshold` is outside 0..10000. The response's `details[].field` is `days` or `lowStockThreshold`; the issue names the accepted range | No — fix the request |
| `UNAUTHENTICATED` | 401 | No access token, or the token is invalid/expired | No — sign in |
| `FORBIDDEN` | 403 | The session is valid but not ADMIN. The foundation's auth hook records `AUTH_PRIVILEGE_DENIED` | No — use an administrator session |
| `RATE_LIMITED` | 429 | The shared request limit was exceeded; `Retry-After` is present | Yes — after the stated delay |
| `INTERNAL_ERROR` | 500 | A read or assembly failed unexpectedly; no storage detail or PII is exposed | Yes — if the failure persists, investigate |

No `404` is possible: the route is static and has no resource identifier.

## Deliberately absent codes

| Situation | Why there is no code |
|---|---|
| No data yet (no orders, no customers, no low stock) | This is a successful dashboard with zero/empty sections, not an error. `revenueTrend` is still zero-filled to its full length. |
| A recent order's customer account was deleted | The order remains; `customer` is `null`. This is a valid read result, not a not-found response. |
| A legacy order line has no resolvable category | It is grouped under `"Khác"`; the dashboard does not lose revenue and does not return an error. |
| Previous month revenue/count is zero | `growthPercent` is `null`; this is a valid comparison result, not a validation failure. |

## Audit action (not an error code)

A successful view records one `ADMIN_DASHBOARD_VIEWED` event in `audit_logs` with
outcome `SUCCESS`, actor from the administrator session, target type `dashboard`, target
id `overview`, and metadata `{days, lowStockThreshold, recentOrderCount}`. It contains
no customer name, email, order content or other PII. A role denial is recorded by the
foundation as `AUTH_PRIVILEGE_DENIED`, not by this module.

## Validation detail shape

```json
{
  "error": {
    "code": "VALIDATION_ERROR",
    "message": "Request validation failed",
    "details": [
      { "field": "days", "issue": "must be 7 or 30" }
    ],
    "requestId": "..."
  }
}
```

`details` carries the member name, never a storage/table/query detail. The message is
the shared foundation message; clients branch on `code` and `details[].field`.
