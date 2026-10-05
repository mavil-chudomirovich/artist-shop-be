# Implementation Plan: Customer Profile & Shipping Addresses

**Branch**: `003-user-profile` | **Date**: 2026-10-05 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/003-user-profile/spec.md`

## Summary

Extend the customer account with a profile (display name, phone, avatar) and a
multi-address book whose entries point at the official Vietnamese administrative
dataset (province → ward, two levels, chosen with cascading selects). Delivers
profile CRUD, avatar upload through an external media service, address CRUD with
an "at most one default per account" guarantee, and a read-only administrator
lookup that later order/commission modules reuse.

Technical approach: a new `internal/modules/user` module following the existing
four-layer layout, extending the `users` table created by the auth module, adding
an `addresses` table protected by a partial unique index, and reading the
administrative dataset from an embedded JSON file rather than duplicating it in the
database.

## Technical Context

**Language/Version**: Go 1.26

**Primary Dependencies**: chi v5 (router), pgx v5 (PostgreSQL), caarlos0/env v11
(config), google/uuid, golang.org/x/crypto (already in module graph). New: none
required — image resizing is delegated to the media provider rather than adding an
imaging library to the service (see research.md D6).

**Storage**: PostgreSQL 16 (existing `users` table extended, new `addresses`
table), plus reference administrative data bundled as embedded JSON. No new
storage engine.

**Testing**: Go standard `testing`. Unit tests with in-memory fakes; PostgreSQL
repository tests behind the `integration` build tag with testcontainers (the
existing `internal/share/testsupport` helper). HTTP handler tests use a stub
service; one end-to-end HTTP flow test exercises the module against real
infrastructure.

**Target Platform**: Linux server (Docker), Go 1.26+ toolchain; developers may run
the service directly on Windows/macOS.

**Project Type**: web-service (REST API), modular monolith

**Performance Goals**: profile read/write p95 < 200 ms; address list p95 < 200 ms;
dataset lookups served from memory so they cost no database round trip; avatar
upload end-to-end p95 < 3 s for a 2 MB image.

**Constraints**: existing foundation pipeline (rate limit, body limit 1 MiB default
— raised per-request for avatar upload via `MAX_BODY_BYTES`-class override),
JWT sessions issued by the auth module, response envelope and error catalogue
owned by `internal/share/httpx`, all money/inventory rules untouched by this
feature.

**Scale/Scope**: MVP retail platform; profile and address data for up to ~10^5
accounts, each with a handful of addresses; ~700 wards and ~35 provinces in the
reference dataset.

## Constitution Check

*GATE: evaluated before Phase 0 research and re-checked after Phase 1 design.*

- [x] **I. Modular Monolith & Clean Architecture**: new module `internal/modules/user`
      with exactly `domain`, `application`, `infrastructure`, `presentation`.
      Dependencies stay `presentation → application → domain` and
      `infrastructure → domain` + `application/interface`. `domain` imports only
      the standard library and `share/access`, so it holds **no** knowledge of the
      administrative dataset: `domain/model/address.go` validates structure only
      (codes non-empty, names captured) and the province/ward existence check runs in
      `application` through a `Divisions` port. Repository interfaces live in
      `domain/repository`; the `Divisions` and `MediaStore` ports live in
      `application/interface`; one mapper in `application/mapper`; the dataset itself in
      `internal/share/administrative`, whose sentinel errors it owns rather than
      importing any module's `domain/error`. Cross-module exposure is a read-only
      contract in `internal/contracts`, not a table shared with other modules.
- [x] **II. Transactional Integrity for Money & Inventory**: not applicable — this
      feature touches no stock, payment, order total or commission stage. No money
      value is stored.
- [x] **III. Explicit State Machines & Domain Invariants**: the address default
      flag and the avatar attachment are lifecycle changes, so transitions go
      through domain functions (not ad-hoc column writes) and each has a positive
      and a negative test.
- [x] **IV. Test-First for Critical Logic**: the "at most one default address"
      invariant and the ownership checks are written as failing tests before the
      implementation. Every endpoint gets an authentication/authorization boundary
      test (guest / customer / other customer / admin).
- [x] **V. Security, Privacy & Least Privilege**: the account id always comes from
      the session, never from the request, so one customer cannot reach another
      customer's data. Uploads are validated by inspecting the uploaded bytes, not
      the file name or client-declared content type. Administrator reads are
      read-only, audited, and never expose password material. Rate limits cover
      the write-heavy endpoints. No secret is committed; media credentials come
      from the environment.
- [x] **VI. Observability & Auditability**: every profile, avatar and address
      change is recorded in `audit_logs` with actor, action, target and timestamp,
      including administrator reads of customer data. Structured logs carry the
      correlation ID and redact secrets via `share/logging`.
- [x] **VII. Simplicity & Incremental Delivery**: REST under `/api/v1` only. No
      WebSocket, no message broker, no caching layer. The administrative dataset is
      embedded rather than synchronised from an external API at runtime.
- [x] **VIII. API Documentation as a Contract**: `docs/api-reference.md` is updated
      in this same change with the twelve new endpoints, each in the six-part format
      including its rate limit, the §1.5 rate-limit table rows for the two new module
      limits, the summary table and a change-log row.
- [x] **Container & Local Development Workflow**: no new Dockerfile or compose
      service. The dataset ships inside the binary via `go:embed`, so no volume
      mount and no new environment variable is required for local development.

**Gate result**: PASS. The only deviation from the API constraint on pagination is
recorded in Complexity Tracking below.

## Project Structure

### Documentation (this feature)

```text
specs/003-user-profile/
├── spec.md              # feature specification (input)
├── plan.md              # this file
├── research.md          # Phase 0 output — decisions and rejected alternatives
├── data-model.md        # Phase 1 output — entities, constraints, transitions
├── quickstart.md        # Phase 1 output — runnable validation guide
├── contracts/
│   ├── README.md        # endpoint summary and conventions
│   ├── openapi.yaml     # endpoint definitions and schemas
│   └── error-codes.md   # module error codes added to the catalogue
├── checklists/
│   └── requirements.md  # specification quality checklist
└── tasks.md             # Phase 2 output (/speckit.tasks — not created here)
```

### Source Code (repository root)

```text
internal/
├── contracts/                          # cross-module read-only customer lookup
│   └── user.go                         #   interface + DTO reused by later modules
├── modules/user/                       # NEW module 02
│   ├── domain/
│   │   ├── constant/
│   │   │   └── audit.go                #   audit action names
│   │   ├── error/
│   │   │   └── errors.go               #   USER_* sentinel errors
│   │   ├── model/
│   │   │   ├── profile.go              #   Profile entity + update rules
│   │   │   ├── address.go              #   Address entity + default-flag rules
│   │   │   ├── phone.go                #   Phone value object + normalisation
│   │   │   └── avatar.go               #   Avatar reference value object
│   │   └── repository/
│   │       ├── user.go                 #   profile repository interface
│   │       └── address.go              #   address repository interface
│   ├── application/
│   │   ├── interface/
│   │   │   └── ports.go                #   use cases, Divisions + MediaStore ports, UnitOfWork
│   │   ├── implement/
│   │   │   ├── service.go              #   composition of use cases
│   │   │   ├── profile.go              #   get/update profile, avatar add/remove
│   │   │   ├── address.go              #   address CRUD + set default
│   │   │   └── admin_lookup.go         #   read-only administrator lookup
│   │   ├── dto/
│   │   │   └── dto.go                  #   use-case input/output
│   │   └── mapper/
│   │       └── mapper.go               #   model ↔ dto
│   ├── infrastructure/
│   │   └── implement/
│   │       ├── administrative/
│   │       │   └── administrative.go    #   Divisions adapter over share/administrative
│   │       ├── postgres/
│   │       │   ├── user.go              #   profile adapter (embeds share/repository.Base)
│   │       │   └── address.go           #   address adapter, default-flag transaction
│   │       ├── media/
│   │       │   └── cloudinary.go        #   MediaStore adapter
│   │       └── auditor/
│   │           └── auditor.go           #   audit adapter over share/audit
│   └── presentation/
│       ├── http/
│       │   ├── handler.go
│       │   ├── router.go               #   mounted at /users and /divisions only
│       │   ├── errors.go               #   domain + administrative error → HTTP mapping
│       │   └── *_test.go
│       ├── dto/
│       │   └── dto.go                  #   HTTP request/response payloads
│       ├── worker/
│       │   └── doc.go                  #   no worker required by this feature
│       └── cli/
│           └── doc.go
└── share/
    └── administrative/                  # NEW shared reference data
        ├── administrative.go           #   Province/Ward lookup + validation
        ├── data/
        │   └── vn-divisions.json       #   embedded official dataset
        └── administrative_test.go

migrations/
└── 00004_user.sql                      # extends users, creates addresses

cmd/api/main.go                          # composition root: mount the user module
```

**Structure Decision**: keep the module layout identical to `internal/modules/auth`
(four layers, one mapper, repository adapters embedding `share/repository.Base`)
so the project has exactly one module shape. The administrative dataset lives in
`internal/share/administrative` rather than inside the user module because order,
shipping and commission modules read it too, and the constitution requires code used
by more than one module to live in `internal/share/`. The dataset is embedded with
`go:embed` instead of being loaded into PostgreSQL: it is small, read-only, and
keeping it in the binary removes an entire class of synchronisation bugs (see
research.md D1). `internal/contracts/user.go` carries the read-only administrator
lookup so later modules depend on an interface rather than on the user module's
tables.

**Why the dataset is not validated in `domain`**: Constitution I allows `domain` to
import only the standard library and `share/access`, so `domain/model/address.go`
cannot depend on `share/administrative`. The dataset package also cannot import this
module's `domain/error`, because order, shipping and commission will read the same
dataset without knowing anything about the user module. The split that satisfies
both rules: `domain` validates structure only, `application/implement` calls the
`Divisions` port declared in `application/interface`, `infrastructure/implement/
administrative` adapts `share/administrative` to that port, and the shared package
owns its own sentinel errors which `presentation/http/errors.go` maps to the
`USER_UNKNOWN_PROVINCE` / `USER_UNKNOWN_WARD` / `USER_WARD_PROVINCE_MISMATCH` codes.

**Routing note**: the module mounts only `/users` and `/divisions`. The administrator
lookup is `GET /api/v1/users/{userId}`, not a separate `/admin` prefix, because that
single path is what the spec, contracts and research define. chi resolves the static
`/users/me` segment before the `/users/{userId}` parameter, and a `userId` that is not
a UUID returns `400 VALIDATION_ERROR`.

## Complexity Tracking
| `GET /api/v1/divisions/*` handlers call the `Divisions` port (`application/interface`) directly from `presentation/http` instead of a `UserService` use case | Both endpoints are pure reference-data lookups: a province list and a ward list, with no business rule, no audit-worthy state change and no rate limit beyond the global one. A pass-through use case would forward arguments and return results unchanged | Putting them behind a use case would add a method, a DTO pair and an indirection that carries no decision, and would make the two reference endpoints depend on the customer-profile use case that T023 finally wires. The port still lives in the `application` layer, so the layering rule is respected: `presentation → application` |


| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| `GET /api/v1/divisions/provinces` and `GET /api/v1/divisions/provinces/{provinceCode}/wards` return an unpaginated array, while the API constraint says every list endpoint supports pagination | Both are read-only **reference data** bounded by the administrative dataset: roughly 35 provinces and at most a few hundred wards per province, embedded in the binary. The whole set is already in memory and one request returns it in single-digit milliseconds | Paginating it would add `page`/`pageSize` parameters and a `meta` block to two endpoints whose full result is smaller than any page size a client would request. The address list, which **is** customer-generated and unbounded, is paginated per FR-018 |
| Avatar width limit is verified through the media provider's returned dimensions rather than by decoding the image in Go | Adds no dependency, and the provider is already the system of record for the stored asset | Decoding in Go would add an imaging dependency to the binary for a dimension the provider already reports (ADR-005) |

**Performance goals** (`p95 < 200 ms` for profile and address reads, `< 3 s` for a 2 MB
avatar upload) are informational targets recorded in Technical Context, not buildable
acceptance criteria; this feature ships no benchmark harness. The buildable criteria are
SC-004, SC-007, SC-009, SC-011 and SC-013, which assert invariant correctness rather
than timing, each mapped to a task. Three criteria are verified manually or after
launch instead: SC-001 and SC-010 are human-time outcomes exercised through
`quickstart.md`, and SC-006 ("no more than 5% of valid updates are rejected for reasons
other than the customer's own input") is a post-launch operational metric with no
buildable work attached to it.
