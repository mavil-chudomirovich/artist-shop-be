---

description: "Task list for feature 003-user-profile"
---

# Tasks: Customer Profile & Shipping Addresses

**Input**: Design documents from `/specs/003-user-profile/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md

**Tests**: Included. The constitution (Principle IV) mandates test-first coverage for
state transitions, and this feature has two test-first obligations — the "at most one
default address" invariant and the ownership/authorization boundaries. Tests in each
story phase MUST be written first and MUST fail before the matching implementation.

**Organization**: Tasks are grouped by user story so each story can be implemented
and validated independently.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (US1–US4)
- Every task names exact file paths under `internal/`, `migrations/` or `docs/`

## Path Conventions

This is a Go web service inside a modular monolith. Paths follow `plan.md`:

- `internal/modules/user/{domain,application,infrastructure,presentation}`
- `internal/share/administrative` for the shared administrative dataset
- `internal/contracts/user.go` for the cross-module read-only lookup
- `migrations/NNNNN_<module>_<description>.sql` for schema changes

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Module skeleton, shared reference data and ports that every story needs.

- [ ] T001 Create the `internal/modules/user/` skeleton with the four layers from plan.md — `domain/{constant,error,model,repository}`, `application/{interface,implement,dto,mapper}`, `infrastructure/implement/{postgres,media,auditor}`, `presentation/{http,cli,worker,dto}` — each with a `doc.go` package comment
- [ ] T002 Define audit action constants (`USER_PROFILE_UPDATED`, `USER_AVATAR_SET`, `USER_AVATAR_REMOVED`, `USER_ADDRESS_CREATED`, `USER_ADDRESS_UPDATED`, `USER_ADDRESS_DELETED`, `USER_ADDRESS_DEFAULT_SET`, `USER_PROFILE_VIEWED_BY_ADMIN`) in `internal/modules/user/domain/constant/audit.go`
- [ ] T003 Define sentinel errors matching `contracts/user-error-codes.md` (`ErrUserNotFound`, `ErrAddressNotFound`, `ErrInvalidPhone`, `ErrUnknownProvince`, `ErrUnknownWard`, `ErrWardProvinceMismatch`, `ErrAvatarTypeUnsupported`, `ErrAvatarTooLarge`, `ErrMediaUnavailable`) in `internal/modules/user/domain/error/errors.go`
- [ ] T004 Write migration `migrations/00004_user.sql` per `data-model.md` §8: `ALTER TABLE users` adding `display_name`, `phone`, `avatar_public_id`, `avatar_url`, `avatar_width`, `avatar_height`; `CREATE TABLE addresses` with FK to `users(id)`; index `addresses (user_id) WHERE deleted_at IS NULL`; partial unique index `addresses_one_default_per_user ON addresses (user_id) WHERE is_default AND deleted_at IS NULL`; provide both `-- +goose Up` and `-- +goose Down`
- [ ] T005 [P] Add the embedded official Vietnamese administrative dataset at `internal/share/administrative/data/vn-divisions.json` (provinces and their wards, each with a stable `code` and a display `name`), per research D1
- [ ] T006 [P] Implement `internal/share/administrative/administrative.go`: load the embedded JSON once, expose `Provinces()` and `Wards(provinceCode)`, plus `ValidateProvince(code)` / `ValidateWard(provinceCode, wardCode)` returning the unknown/mismatch errors; add unit tests in `internal/share/administrative/administrative_test.go` covering an unknown province, an unknown ward and a ward from another province
- [ ] T007 Add the read-only cross-module customer lookup contract in `internal/contracts/user.go`: `CustomerLookupService` interface plus its DTOs (id, email, role, display name, phone, addresses), so modules 07/10/16 reuse it instead of reading this module's tables
- [ ] T008 Define the `MediaStore` port in `internal/modules/user/application/interface/ports.go`: `Upload(ctx, bytes, targetWidth) (reference, error)` and `Remove(ctx, reference) error`, where the reference carries public id, URL, width and height (research D6)
- [ ] T009 Add media configuration to `internal/share/config/config.go` — a `MediaConfig` with Cloudinary cloud name, API key, API secret and upload folder, wired under `Config` with the `MEDIA_` prefix; validate only in the user module's composition, so `cmd/migrate` and `cmd/seed` stay runnable without it

**Checkpoint**: Module skeleton, dataset, ports and schema exist; no behaviour yet.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Interfaces, DTOs and wiring shared by every user story.

**⚠️ CRITICAL**: No user story work begins until this phase is complete.

- [ ] T010 [P] Declare repository interfaces in `internal/modules/user/domain/repository/user.go` and `internal/modules/user/domain/repository/address.go`, embedding `share/repository.Repository[T, ID]` where it applies and adding `SetDefault(ctx, id)` and `ClearDefault(ctx, userID)` for the invariant
- [ ] T011 [P] Define HTTP request/response payloads in `internal/modules/user/presentation/dto/dto.go`: update-profile, avatar (multipart), create/update address, address, province, ward and customer-lookup shapes, matching `contracts/openapi.yaml`
- [ ] T012 [P] Map domain errors to HTTP in `internal/modules/user/presentation/http/errors.go` using the status codes from `contracts/user-error-codes.md`, and attach field-level `httpx.Detail` entries for phone, province, ward and street-address failures (FR-020)
- [ ] T013 Define use-case input/output types in `internal/modules/user/application/dto/dto.go`
- [ ] T014 Declare the use-case interface in `internal/modules/user/application/interface/ports.go`: `UserService` with profile read/update, avatar set/remove, address CRUD, set-default and the administrator lookup, plus a composition `Config` carrying TTL-free limits such as the avatar size ceiling
- [ ] T015 [P] Implement the single mapper in `internal/modules/user/application/mapper/mapper.go` (model ↔ dto, including the address `divisionNeedsReview` flag from research D10)
- [ ] T016 [P] Implement the audit adapter in `internal/modules/user/infrastructure/implement/auditor/auditor.go`, delegating to the foundation `share/audit` writer
- [ ] T017 Build the `/users`, `/divisions` and administrator route groups in `internal/modules/user/presentation/http/router.go`, enforcing authentication, ADMIN role and ownership, and auditing privilege denials the way the auth module does
- [ ] T018 Construct the module in `cmd/api/main.go`: repositories, media adapter, auditor and the router mounted under `/api/v1`, and confirm `go build ./...` succeeds

**Checkpoint**: Foundation ready — user stories can now begin.

---

## Phase 3: User Story 1 - View and update own profile (Priority: P1) 🎯 MVP

**Goal**: A signed-in customer can read their profile and change display name and phone.

**Independent Test**: Sign in, `GET /api/v1/users/me` shows the account, `PATCH` updates
the name and phone, the phone comes back normalised, an invalid phone is rejected with a
field detail, and a second customer cannot read the first customer's profile.

### Tests for User Story 1 (write first — they MUST fail)

- [ ] T019 [P] [US1] Phone normalisation tests in `internal/modules/user/domain/model/phone_test.go`: accepts `0912345678`, `0912 345 678`, `+84912345678`; rejects `12345`, letters, and 9 or 11 digits; asserts the stored form is always ten digits
- [ ] T020 [P] [US1] Profile model tests in `internal/modules/user/domain/model/profile_test.go`: trimming, clearing display name and phone, rejecting an invalid phone, and the rule that avatar fields are all null or all populated
- [ ] T021 [P] [US1] Use-case tests in `internal/modules/user/application/implement/profile_test.go` using in-memory fakes: read returns the profile, partial update leaves the omitted field untouched, an empty string clears the field, an invalid phone returns `ErrInvalidPhone` and persists nothing
- [ ] T022 [P] [US1] HTTP tests in `internal/modules/user/presentation/http/http_test.go`: `401` without a token, `200` for the owner, `404` for a non-existent profile, `400` with `details[].field == "phone"` for a bad phone, and an audit event for every successful update

### Implementation for User Story 1

- [ ] T023 [US1] Implement the phone value object in `internal/modules/user/domain/model/phone.go` (trim, strip separators, `+84` → `0`, require ten digits)
- [ ] T024 [US1] Implement the profile entity and update rules in `internal/modules/user/domain/model/profile.go`
- [ ] T025 [US1] Implement the profile repository adapter in `internal/modules/user/infrastructure/implement/postgres/user.go`, embedding `share/repository.Base` and reading only the profile columns
- [ ] T026 [US1] Implement profile read and update use cases in `internal/modules/user/application/implement/profile.go`, deriving the account from the session context and recording `USER_PROFILE_UPDATED`
- [ ] T027 [US1] Implement the `GET` and `PATCH /api/v1/users/me` handlers in `internal/modules/user/presentation/http/handler.go`
- [ ] T028 [US1] Add the profile integration test in `internal/modules/user/presentation/http/http_integration_test.go` behind the `integration` tag: update → read back → confirm normalisation and the audit row

**Checkpoint**: US1 works standalone — a customer has a maintained profile.

---

## Phase 4: User Story 2 - Manage shipping addresses (Priority: P2)

**Goal**: A customer can keep several addresses, choose one per order, and exactly one is default.

**Independent Test**: Sign in, create two addresses, confirm the first became default,
mark the second default, edit one, hide one, then confirm the list, the total and the
single-default invariant.

### Tests for User Story 2 (write first — they MUST fail)

- [ ] T029 [P] [US2] Address model tests in `internal/modules/user/domain/model/address_test.go`: create, edit preserving `isDefault`, mark default, hide, reject a ward from another province, and reject a hidden address becoming default
- [ ] T030 [P] [US2] Use-case tests in `internal/modules/user/application/implement/address_test.go`: first address becomes default, a second address does not, setting a default clears the previous one atomically, hiding the last default leaves zero defaults, and every operation is scoped to the session account
- [ ] T031 [P] [US2] Repository invariant test in `internal/modules/user/infrastructure/implement/postgres/address_integration_test.go`: writing a second default row directly (bypassing the use case) is rejected by the partial unique index, and paginated listing returns a stable order with a total
- [ ] T032 [P] [US2] HTTP tests in `internal/modules/user/presentation/http/address_test.go`: `404 USER_ADDRESS_NOT_FOUND` for another customer's address id, `400 USER_WARD_PROVINCE_MISMATCH`, `400 USER_UNKNOWN_PROVINCE`, `204` on hide, and `404` when touching a hidden address

### Implementation for User Story 2

- [ ] T033 [US2] Implement the address entity and default-flag transitions in `internal/modules/user/domain/model/address.go`
- [ ] T034 [US2] Implement the address repository adapter in `internal/modules/user/infrastructure/implement/postgres/address.go`, including `SetDefault` as a single transaction through the `UnitOfWork` port and filters for `deleted_at IS NULL`
- [ ] T035 [US2] Implement address use cases in `internal/modules/user/application/implement/address.go`, wiring domain validation to `share/administrative` and recording the four address audit actions
- [ ] T036 [US2] Implement the address handlers in `internal/modules/user/presentation/http/handler.go`: list with pagination, create, edit, hide, set default
- [ ] T037 [US2] Implement the cascading-select endpoints in `internal/modules/user/presentation/http/handler.go`: `GET /api/v1/divisions/provinces` and `GET /api/v1/divisions/provinces/{provinceCode}/wards`
- [ ] T038 [US2] Add the address integration test in `internal/modules/user/presentation/http/http_integration_test.go`: create two, flip the default, hide one, confirm one default remains and the audit trail exists

**Checkpoint**: US1 and US2 both work independently; a customer can prepare delivery for an order.

---

## Phase 5: User Story 3 - Upload and remove a profile photo (Priority: P3)

**Goal**: A customer can attach, replace and remove an avatar, with server-side validation.

**Independent Test**: Upload a valid image and see it on the profile, upload a non-image
and a too-large file and see both rejected with the previous avatar intact, then remove it.

### Tests for User Story 3 (write first — they MUST fail)

- [ ] T039 [P] [US3] Avatar validation tests in `internal/modules/user/domain/model/avatar_test.go`: only JPEG, PNG and WebP accepted by content sniffing, the 2 MB ceiling enforced, and the all-or-nothing field rule
- [ ] T040 [P] [US3] Use-case tests in `internal/modules/user/application/implement/avatar_test.go` with a fake `MediaStore`: successful upload stores the reference, a rejected upload leaves the previous avatar untouched, and a media failure returns `ErrMediaUnavailable` without changing the profile

### Implementation for User Story 3

- [ ] T041 [US3] Implement the avatar reference value object in `internal/modules/user/domain/model/avatar.go`
- [ ] T042 [US3] Implement the Cloudinary adapter in `internal/modules/user/infrastructure/implement/media/cloudinary.go`, uploading the original bytes with a 512 px width transformation and mapping provider failures to `ErrMediaUnavailable`
- [ ] T043 [US3] Implement avatar set and remove use cases in `internal/modules/user/application/implement/profile.go`, recording `USER_AVATAR_SET` and `USER_AVATAR_REMOVED`
- [ ] T044 [US3] Implement the multipart handlers in `internal/modules/user/presentation/http/handler.go` with a route-specific body ceiling above the global `MAX_BODY_BYTES`, sniffing the payload instead of trusting filename or client content type
- [ ] T045 [US3] Add the avatar integration test in `internal/modules/user/presentation/http/http_integration_test.go` behind the `integration` tag: upload, replace, rejected upload keeps the old avatar, remove

**Checkpoint**: US1–US3 work independently — the customer profile is complete.

---

## Phase 6: User Story 4 - Read-only customer lookup for operators (Priority: P4)

**Goal**: An administrator can read a customer's contact details and addresses for order or commission handling, and every read is audited.

**Independent Test**: Sign in as a customer and get `403`; sign in as admin and get the
contact details and address list; confirm an audit row exists for the read.

### Tests for User Story 4 (write first — they MUST fail)

- [ ] T046 [P] [US4] HTTP authorization tests in `internal/modules/user/presentation/http/admin_test.go`: customer token → `403`, admin token → `200`, unknown account → `404 USER_NOT_FOUND`, and the audit action recorded for each successful read

### Implementation for User Story 4

- [ ] T047 [US4] Implement the lookup use case in `internal/modules/user/application/implement/admin_lookup.go` and make the module satisfy `internal/contracts.CustomerLookupService`
- [ ] T048 [US4] Implement the ADMIN-guarded route in `internal/modules/user/presentation/http/handler.go`, offering no write path for customer data
- [ ] T049 [US4] Add the audit assertion to the integration test in `internal/modules/user/presentation/http/http_integration_test.go`: an admin read writes `USER_PROFILE_VIEWED_BY_ADMIN`

**Checkpoint**: All four stories work; later modules have a stable contract to reuse.

---

## Phase 7: Polish & Cross-Cutting Concerns

- [ ] T050 [P] Update `docs/api-reference.md` with the twelve new endpoints, the summary table row, the new error codes in §1.4 and a change-log entry — required by Constitution VIII in the same change
- [ ] T051 [P] Add the media variables to `.env.example` and document them in `docs/configuration.md`, including the failure mode that a missing media configuration disables avatar upload
- [ ] T052 [P] Update `docs/modules/02-user.md` to mark the feature implemented and record the dataset refresh policy
- [ ] T053 [P] Verify in `internal/share/logging/redact_test.go` that media credentials and provider responses cannot leak into logs
- [ ] T054 Run the `quickstart.md` validation end-to-end (`make up-tools` plus the curl scenarios) and record the observed outcomes
- [ ] T055 Run `make check` (gofmt, tidy, vet with both build tags, golangci-lint, unit, integration, build) across `internal/modules/user/`, `internal/share/administrative/`, `internal/contracts/` and `migrations/00004_user.sql`, and clear every finding
- [ ] T056 Security review pass over `internal/modules/user/presentation/http/handler.go`, `internal/modules/user/presentation/http/router.go` and `internal/modules/user/infrastructure/implement/media/cloudinary.go`: confirm no route accepts an owner id, uploads are validated by content, the default-address invariant holds under concurrent writes, and no media credential is logged

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: no dependencies — start immediately
- **Foundational (Phase 2)**: depends on Phase 1 — **blocks all user stories**
- **User Stories (Phases 3–6)**: all depend on Phase 2; US1 → US2 → US3 → US4 in priority
  order, though they touch different files and can overlap once the foundation lands
- **Polish (Phase 7)**: depends on the stories being implemented

### User Story Dependencies

- **US1 (P1)**: needs only the foundation. Delivers the MVP.
- **US2 (P2)**: needs the foundation; reuses the session and audit wiring from US1 but
  is independently testable.
- **US3 (P3)**: needs the profile entity from US1 (it attaches a reference to it) and
  the `MediaStore` port from Phase 1.
- **US4 (P4)**: needs the foundation plus both profile and address readers; delivers no
  user-visible value on its own.

### Critical Path

T001 → T004 → T010 → T014 → T018 → T026 → T027 → (US1 complete) → T035 → T036 →
T044 → T048

The default-address invariant additionally requires T004 before T034, and its test T031
is the one that must fail first.

### Within Each User Story

1. Tests first, and they must fail before the implementation
2. Domain model → repository adapter → use case → handler
3. Integration test last, proving the story works against real infrastructure

### Parallel Opportunities

- Phase 1: T005/T006 (dataset) and T002/T003 (constants and errors) can run together
- Phase 2: T010, T011, T012, T015, T016 touch separate files and can run together
- Phase 3: T019–T022 (four test files) can run together, then T023/T024 together
- Phase 4: T029–T032 can run together
- Phase 5: T039/T040 together
- Phase 7: T050, T051, T052, T053 are documentation and can run in parallel

### Parallel Example: Phase 4

```bash
# All US2 tests written first, in parallel — they fail against the empty module
Task: "Address model tests in internal/modules/user/domain/model/address_test.go"
Task: "Use-case tests in internal/modules/user/application/implement/address_test.go"
Task: "Repository invariant test in internal/modules/user/infrastructure/implement/postgres/address_integration_test.go"
Task: "HTTP tests in internal/modules/user/presentation/http/address_test.go"

# Then the domain work in parallel, before adapters and handlers
Task: "Implement the address entity in internal/modules/user/domain/model/address.go"
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 1 Setup
2. Complete Phase 2 Foundational — **critical, blocks everything**
3. Complete Phase 3 (US1)
4. **STOP and VALIDATE**: profile read/update works and is authorization-tested
5. Ship if the profile alone is useful; it already unblocks order and commission data

### Incremental Delivery

1. Setup + Foundational → foundation ready
2. US1 → validate → deliver (MVP: a customer can maintain their profile)
3. US2 → validate → deliver (addresses; unblocks ordering)
4. US3 → validate → deliver (avatar)
5. US4 → validate → deliver (operator lookup; unblocks order/commission work)
6. Polish → full documentation and quality gate

### Parallel Team Strategy

After Phase 2, the four stories touch disjoint files and can be split across
developers: one on US1+US3 (same aggregate), one on US2, one on US4.

---

## Notes

- [P] means different files and no dependency on incomplete tasks
- [Story] maps each task to its user story for traceability
- Test tasks are mandatory here (Constitution IV): the default-address invariant is a
  state transition, and every endpoint needs an authorization boundary test
- Migration `00004_user.sql` must be reversible; the runner requires Up and Down
- Do not start US2 or later before Phase 2 is complete
- Commit after each task or logical group; update this file as tasks are marked `[X]`