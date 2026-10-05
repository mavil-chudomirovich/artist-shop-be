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
      the standard library and `share/access`. Repository interfaces live in
      `domain/repository`; external media port in `application/interface`; one
      mapper in `application/mapper`; shared administrative data in
      `internal/share/administrative`. Cross-module exposure is a read-only
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
      in this same change with the eleven new endpoints, the summary table and a
      change-log row.
- [x] **Container & Local Development Workflow**: no new Dockerfile or compose
      service. The dataset ships inside the binary via `go:embed`, so no volume
      mount and no new environment variable is required for local development.

**Gate result**: PASS. No violations, no Complexity Tracking entries required.

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
│   └── user-error-codes.md  # module error codes added to the catalogue
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
│   │   │   └── ports.go                #   use cases, MediaStore port, UnitOfWork
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
│   │       ├── postgres/
│   │       │   ├── user.go              #   profile adapter (embeds share/repository.Base)
│   │       │   └── address.go           #   address adapter, default-flag transaction
│   │       ├── media/
│   │       │   └── cloudinary.go       #   MediaStore adapter
│   │       └── auditor/
│   │           └── auditor.go           #   audit adapter over share/audit
│   └── presentation/
│       ├── http/
│       │   ├── handler.go
│       │   ├── router.go               #   mounted at /users, /admin, /divisions
│       │   ├── errors.go               #   domain error → HTTP mapping
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

## Complexity Tracking

> No constitution violations. No entries required.