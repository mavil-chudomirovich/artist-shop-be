# Contracts: Customer Profile & Shipping Addresses

Endpoints under `/api/v1/users`, `/api/v1/divisions` and the administrator lookup
follow the foundation HTTP conventions
(`specs/002-cross-cutting-foundation/contracts/http-conventions.md`) and reuse the
shared response envelope (`data` + `meta`) and error format from
`internal/share/httpx`.

| File | Purpose |
|------|---------|
| [openapi.yaml](./openapi.yaml) | Endpoint definitions, request/response schemas |
| [user-error-codes.md](./user-error-codes.md) | Module-specific error codes added to the catalogue |

## Endpoint summary

| Method | Path | Auth | Purpose |
|--------|------|------|---------|
| GET | `/api/v1/users/me` | access token | Read own profile |
| PATCH | `/api/v1/users/me` | access token | Update display name and/or phone |
| POST | `/api/v1/users/me/avatar` | access token | Upload or replace the avatar |
| DELETE | `/api/v1/users/me/avatar` | access token | Remove the avatar |
| GET | `/api/v1/users/me/addresses` | access token | List own addresses (paginated) |
| POST | `/api/v1/users/me/addresses` | access token | Create an address |
| PATCH | `/api/v1/users/me/addresses/{addressId}` | access token | Edit an address |
| DELETE | `/api/v1/users/me/addresses/{addressId}` | access token | Hide an address |
| POST | `/api/v1/users/me/addresses/{addressId}/default` | access token | Mark an address as default |
| GET | `/api/v1/divisions/provinces` | access token | List provinces for the select |
| GET | `/api/v1/divisions/provinces/{provinceCode}/wards` | access token | List wards of one province |
| GET | `/api/v1/users/{userId}` | ADMIN | Read-only customer lookup for operators |

## Conventions this feature relies on

- **The account is taken from the session.** No self-service route accepts a user
  id; the identifier always comes from the caller's token. A request that supplies
  another account's id is therefore impossible by construction rather than by
  validation (research D5).
- **Field-level validation errors** use `error.details[]` with `field` and `issue`,
  so a client can highlight the exact input (FR-020).
- **Pagination** uses `?page=&pageSize=` with `page` starting at 1, a default
  `pageSize` of 20 and a maximum of 100. The response carries
  `meta.page`, `meta.pageSize` and `meta.total`.
- **Every request needs `Content-Type: application/json`** except the avatar upload,
  which uses `multipart/form-data`.
- **Address writes never accept an owner id**, and the default flag is set through
  its own route rather than as a field, so it is always an explicit, audited action.

## Client flow for the cascading address selects

1. `GET /api/v1/divisions/provinces` → list of `{ code, name }`.
2. `GET /api/v1/divisions/provinces/{provinceCode}/wards` → wards of that province
   only. The client renders the second select from this response; it never needs
   the ward list up front.
3. `POST /api/v1/users/me/addresses` sends the chosen `provinceCode` and `wardCode`
   plus the captured names. A ward that does not belong to the province is rejected
   with `USER_WARD_PROVINCE_MISMATCH`, so a tampered client cannot create an
   impossible address.
