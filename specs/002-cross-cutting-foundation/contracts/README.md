# Contracts: Cross-Cutting Foundation

Interface contracts this feature exposes to API consumers and operators.

| File | Purpose |
|------|---------|
| [http-conventions.md](./http-conventions.md) | Response envelope, error format, headers, versioning, status mapping |
| [error-codes.md](./error-codes.md) | Stable error-code catalogue shared by all modules |
| [openapi.yaml](./openapi.yaml) | Operational endpoints (liveness, readiness) |

Business endpoints are added by later modules and MUST follow
`http-conventions.md` and reuse `error-codes.md`.
