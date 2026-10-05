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

**Conventions**: Every task follows `docs/development/coding-conventions.md`; SQL
changes follow `docs/development/migration.md`; cross-module exposure follows
`docs/system-design/contract-purity.md`. Decisions already taken are recorded as ADRs
in `docs/decisions/` — task text cites them instead of re-arguing them.

**Layering rule that shapes this list**: Constitution I allows `domain` to import only
the standard library and `share/access`. So `domain` knows nothing about the
administrative dataset. Existence of a province or ward is checked by the use case
through the `Divisions` port, and `internal/share/administrative` owns its own sentinel
errors so it never imports a module's `domain/error`.

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

- [x] T001 Create the `internal/modules/user/` skeleton with the four layers from plan.md — `domain/{constant,error,model,repository}`, `application/{interface,implement,dto,mapper}`, `infrastructure/implement/{administrative,postgres,media,auditor}`, `presentation/{http,cli,worker,dto}` — each package with a `// Package …` comment (revive enforces this) and English comments throughout
- [x] T002 Define audit action constants (`USER_PROFILE_UPDATED`, `USER_AVATAR_SET`, `USER_AVATAR_REMOVED`, `USER_ADDRESS_CREATED`, `USER_ADDRESS_UPDATED`, `USER_ADDRESS_DELETED`, `USER_ADDRESS_DEFAULT_SET`, `USER_PROFILE_VIEWED_BY_ADMIN`) in `internal/modules/user/domain/constant/audit.go`, with a comment per constant
- [x] T003 Define sentinel errors in `internal/modules/user/domain/error/errors.go` (`ErrUserNotFound`, `ErrAddressNotFound`, `ErrInvalidPhone`, `ErrAvatarTypeUnsupported`, `ErrAvatarTooLarge`, `ErrMediaUnavailable`) matching `contracts/error-codes.md`. The three division errors (`ErrUnknownProvince`, `ErrUnknownWard`, `ErrWardProvinceMismatch`) belong to `internal/share/administrative`, **not** here, because that package is shared with order, shipping and commission and must not import a module's domain errors
- [x] T004 Define the shared package's own sentinel errors `ErrUnknownProvince`, `ErrUnknownWard` and `ErrWardProvinceMismatch` in `internal/share/administrative/errors.go`, with no import of any module
- [x] T005 Write migration `migrations/00004_user.sql` following `docs/development/migration.md`: additive only, so `ALTER TABLE users ADD COLUMN display_name, phone, avatar_public_id, avatar_secure_url, avatar_width, avatar_height` are all nullable; `CREATE TABLE addresses` with FK to `users(id)`; index `addresses (user_id) WHERE deleted_at IS NULL` for list queries; partial unique index `addresses_one_default_per_user ON addresses (user_id) WHERE is_default AND deleted_at IS NULL` per ADR-003; `COMMENT ON COLUMN` for the non-obvious columns; and both `-- +goose Up` and `-- +goose Down` sections, because the runner rejects a one-way migration
- [x] T006 Verify the bundled dataset at `internal/share/administrative/data/vn-divisions.json` (already present: 34 provinces, 3,321 wards, post-2025 two-level hierarchy per ADR-002 and research D1): every ward is nested under its province, no province or ward code is duplicated, the `_provenance` and `counts` blocks match the data, and the file is UTF-8 without BOM with LF endings per `.editorconfig`
- [x] T007 Record the dataset refresh procedure in `docs/configuration.md`: the recorded source, that a refresh regenerates the whole file from that source rather than editing it by hand, that counts are asserted by the T008 test, and that a refresh ships with a release
- [x] T008 [P] Implement `internal/share/administrative/administrative.go` with a `// Package administrative` comment: embed the JSON, load it once, expose `Provinces()` and `Wards(provinceCode)`, and return the T004 errors; add `internal/share/administrative/administrative_test.go` covering an unknown province, an unknown ward, a ward from another province, and a dataset integrity check that no code is duplicated and every ward belongs to exactly one province
- [x] T009 Add the read-only cross-module lookup contract in `internal/contracts/user.go` per `docs/system-design/contract-purity.md`: a `CustomerLookupService` interface plus its **own** DTOs (id, email, role, display name, phone, addresses ordered default-first) — never reusing a `domain/model` type, so a storage change cannot silently break the contract
- [x] T010 Define the `MediaStore` port in `internal/modules/user/application/interface/ports.go`: `Upload(ctx, bytes, targetWidth) (reference, error)` and `Remove(ctx, reference) error`, where the reference carries public id, URL, width and height (ADR-005). Name it for the capability, not the vendor
- [x] T011 Define the `Divisions` port in `internal/modules/user/application/interface/ports.go`: `Provinces(ctx)`, `Wards(ctx, provinceCode)` and `ValidateAddressDivisions(ctx, provinceCode, wardCode)` returning the shared errors from T004. This port is how `application` reaches the dataset without `domain` importing it
- [x] T012 Add media configuration to `internal/share/config/config.go` — a `MediaConfig` with Cloudinary cloud name, API key, API secret and upload folder, wired under `Config` with the `MEDIA_` prefix; validate only in the user module's composition so `cmd/migrate` and `cmd/seed` stay runnable without it

**Checkpoint**: Module skeleton, dataset, ports and schema exist; no behaviour yet.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Interfaces, DTOs and wiring shared by every user story.

**⚠️ CRITICAL**: No user story work begins until this phase is complete.

- [ ] T013 [P] Declare repository interfaces in `internal/modules/user/domain/repository/user.go` and `internal/modules/user/domain/repository/address.go`, embedding `share/repository.Repository[T, ID]` where it applies and adding `SetDefault(ctx, id)` and `ClearDefault(ctx, userID)` for the invariant; per coding conventions a repository MUST NOT open a transaction
- [ ] T014 [P] Define HTTP request/response payloads in `internal/modules/user/presentation/dto/dto.go`: update-profile, avatar (multipart), create/update address, address, province, ward and customer-lookup shapes, matching `contracts/openapi.yaml` and using the exact JSON field names from it. `avatar` is the only signal for "has a photo"; there is no `hasAvatar` field
- [ ] T015 [P] Map errors to HTTP in `internal/modules/user/presentation/http/errors.go`: this module's sentinel errors plus the shared division errors from T004, using the status codes in `contracts/error-codes.md`, and attaching field-level `httpx.Detail` entries for phone, province, ward and street-address failures (FR-020)
- [ ] T016 Define use-case input/output types in `internal/modules/user/application/dto/dto.go`
- [ ] T017 Declare the use-case interface in `internal/modules/user/application/interface/ports.go`: `UserService` with profile read/update, avatar set/remove, address CRUD, set-default and the administrator lookup, plus a composition `Config` carrying the avatar size ceiling
- [ ] T018 [P] Implement the single mapper in `internal/modules/user/application/mapper/mapper.go` (model ↔ dto, including the address `divisionNeedsReview` flag from research D10); this stays the only place types are converted
- [ ] T019 [P] Implement the audit adapter in `internal/modules/user/infrastructure/implement/auditor/auditor.go`, delegating to the foundation `share/audit` writer
- [ ] T020 [P] Implement the `Divisions` adapter in `internal/modules/user/infrastructure/implement/administrative/administrative.go`, delegating to `internal/share/administrative` and satisfying the T011 port, so `application` depends on an interface rather than on the shared package
- [ ] T021 [P] Add module rate limits in `internal/share/config/config.go` (avatar uploads per hour, address writes per minute) wired under the `USER_` prefix, and apply them in T022 so FR-025 does not rely on the global limit alone
- [ ] T022 Build the `/users` and `/divisions` route groups in `internal/modules/user/presentation/http/router.go` — no separate `/admin` prefix, because the administrator lookup is `GET /api/v1/users/{userId}` — enforcing authentication, ADMIN role and ownership, applying the T021 limits, and auditing privilege denials the way the auth module does. Note that chi resolves the static `/users/me` segment before `/users/{userId}`, and a `userId` that is not a UUID returns `400 VALIDATION_ERROR`
- [ ] T023 Construct the module in `cmd/api/main.go`: repositories, the T020 divisions adapter, media adapter, auditor and the router mounted under `/api/v1`, then confirm `go build ./...` succeeds

**Checkpoint**: Foundation ready — user stories can now begin.

---

## Phase 3: User Story 1 - View and update own profile (Priority: P1) 🎯 MVP

**Goal**: A signed-in customer can read their profile and change display name and phone.

**Independent Test**: Sign in, `GET /api/v1/users/me` shows the account, `PATCH` updates
the name and phone, the phone comes back normalised, an invalid phone is rejected with a
field detail, and a second customer cannot read the first customer's profile.

### Tests for User Story 1 (write first — they MUST fail)

- [ ] T024 [P] [US1] Phone normalisation tests in `internal/modules/user/domain/model/phone_test.go`: accepts `0912345678`, `0912 345 678`, `+84912345678`; rejects `12345`, letters, and 9 or 11 digits; asserts the stored form is always ten digits
- [ ] T025 [P] [US1] Profile model tests in `internal/modules/user/domain/model/profile_test.go`: trimming, clearing display name and phone, rejecting an invalid phone, and the rule that avatar fields are all null or all populated
- [ ] T026 [P] [US1] Use-case tests in `internal/modules/user/application/implement/profile_test.go` using in-memory fakes: read returns the profile, partial update leaves the omitted field untouched, an empty string clears the field, an invalid phone returns `ErrInvalidPhone` and persists nothing
- [ ] T027 [P] [US1] HTTP tests in `internal/modules/user/presentation/http/http_test.go`: `401` with no token and `401` with an expired token, `200` for the owner, `400` with `details[].field == "phone"` for a bad phone, an audit event for every successful update, and — for FR-021 — a `200` response with a null avatar when the customer has no photo, proving a media outage cannot fail the read. There is deliberately no `404` case here: the profile is the `users` row and FR-024 removes account deletion, so the row cannot go missing

### Implementation for User Story 1

- [ ] T028 [US1] Implement the phone value object in `internal/modules/user/domain/model/phone.go` (trim, strip separators, `+84` → `0`, require ten digits) as a pure domain function so CLI and future workers cannot bypass it
- [ ] T029 [US1] Implement the profile entity and update rules in `internal/modules/user/domain/model/profile.go`, with no knowledge of the administrative dataset
- [ ] T030 [US1] Implement the profile repository adapter in `internal/modules/user/infrastructure/implement/postgres/user.go`, embedding `share/repository.Base`, reading and writing only the six profile columns and binding every value as a query parameter
- [ ] T031 [US1] Implement profile read and update use cases in `internal/modules/user/application/implement/profile.go`, taking the account from the session context and never from input (research D5), recording `USER_PROFILE_UPDATED`
- [ ] T032 [US1] Implement the `GET` and `PATCH /api/v1/users/me` handlers in `internal/modules/user/presentation/http/handler.go`
- [ ] T033 [US1] Add the profile integration test in `internal/modules/user/presentation/http/http_integration_test.go` behind the `integration` tag: update → read back → confirm normalisation, confirm the audit row, and confirm the read still succeeds with a null avatar when no media is configured

**Checkpoint**: US1 works standalone — a customer has a maintained profile.

---

## Phase 4: User Story 2 - Manage shipping addresses (Priority: P2)

**Goal**: A customer can keep several addresses, choose one per order, and exactly one is default.

**Independent Test**: Sign in, create two addresses, confirm the first became default,
mark the second default, edit one, hide one, then confirm the list, the total and the
single-default invariant.

### Tests for User Story 2 (write first — they MUST fail)

- [ ] T034 [P] [US2] Address model tests in `internal/modules/user/domain/model/address_test.go`: create, edit preserving `isDefault`, mark default, hide, reject structurally invalid input, and reject a hidden address becoming default. A rejected edit must leave the previous default in place
- [ ] T035 [P] [US2] Use-case tests in `internal/modules/user/application/implement/address_test.go` with a fake `Divisions` port: first address becomes default, a second address does not, setting a default clears the previous one atomically, hiding the last default leaves zero defaults, an unknown province or ward is rejected without persisting, a ward from another province is rejected, every operation is scoped to the session account, and a stored ward code that has left the dataset still reads back with `divisionNeedsReview: true` and its captured names (research D10)
- [ ] T036 [P] [US2] Repository invariant test in `internal/modules/user/infrastructure/implement/postgres/address_integration_test.go`: writing a second default row directly, bypassing the use case, is rejected by the partial unique index (ADR-003), and paginated listing returns a stable order with a total
- [ ] T037 [P] [US2] HTTP tests in `internal/modules/user/presentation/http/address_test.go`: `404 USER_ADDRESS_NOT_FOUND` for another customer's address id, `400 USER_WARD_PROVINCE_MISMATCH`, `400 USER_UNKNOWN_PROVINCE`, `204` on hide, and `404` when touching a hidden address

### Implementation for User Story 2

- [ ] T038 [US2] Implement the address entity and default-flag transitions in `internal/modules/user/domain/model/address.go`, with an explicit transition function rather than an ad-hoc flag write, validating structure only
- [ ] T039 [US2] Implement the address repository adapter in `internal/modules/user/infrastructure/implement/postgres/address.go`, including `SetDefault` as a single transaction through the `UnitOfWork` port, filters for `deleted_at IS NULL` (ADR-004), and captured province/ward names for history
- [ ] T040 [US2] Implement address use cases in `internal/modules/user/application/implement/address.go`, validating divisions through the T011 `Divisions` port and recording the four address audit actions
- [ ] T041 [US2] Implement the address handlers in `internal/modules/user/presentation/http/handler.go`: list with pagination (page ≥ 1, pageSize ≤ 100), create, edit, hide, set default
- [ ] T042 [US2] Implement the cascading-select endpoints in `internal/modules/user/presentation/http/handler.go`: `GET /api/v1/divisions/provinces` and `GET /api/v1/divisions/provinces/{provinceCode}/wards`, both unpaginated per the Complexity Tracking entry, ward list scoped to one province
- [ ] T043 [US2] Add the address integration test in `internal/modules/user/presentation/http/http_integration_test.go`: create two, flip the default, hide one, confirm one default remains and the audit trail exists, — for SC-007 — confirm the hidden row and its captured province/ward/street text survive unchanged, and — for FR-023 — confirm that a disabled account keeps its addresses retrievable and no longer accepts new ones

**Checkpoint**: US1 and US2 both work independently; a customer can prepare delivery for an order.

---

## Phase 5: User Story 3 - Upload and remove a profile photo (Priority: P3)

**Goal**: A customer can attach, replace and remove an avatar, with server-side validation.

**Independent Test**: Upload a valid image and see it on the profile, upload a non-image
and a too-large file and see both rejected with the previous avatar intact, then remove it.

### Tests for User Story 3 (write first — they MUST fail)

- [ ] T044 [P] [US3] Avatar validation tests in `internal/modules/user/domain/model/avatar_test.go`: only JPEG, PNG and WebP accepted by content sniffing, the 2 MB ceiling enforced, the all-or-nothing field rule, and a stored reference whose reported width never exceeds 512 px (FR-015)
- [ ] T045 [P] [US3] Use-case tests in `internal/modules/user/application/implement/avatar_test.go` with a fake `MediaStore`: successful upload stores the reference, a rejected upload leaves the previous avatar untouched, and a media failure returns `ErrMediaUnavailable` without changing the profile
- [ ] T046 [P] [US3] Rate-limit test in `internal/modules/user/presentation/http/avatar_test.go`: exceeding the configured upload rate returns `429` with `Retry-After` while the existing avatar keeps working (FR-025, SC-013)

### Implementation for User Story 3

- [ ] T047 [US3] Implement the avatar reference value object in `internal/modules/user/domain/model/avatar.go`
- [ ] T048 [US3] Implement the Cloudinary adapter in `internal/modules/user/infrastructure/implement/media/cloudinary.go`, uploading the original bytes with a 512 px width transformation (ADR-005) and mapping provider failures to `ErrMediaUnavailable` without leaking provider detail
- [ ] T049 [US3] Implement avatar set and remove use cases in `internal/modules/user/application/implement/profile.go`, recording `USER_AVATAR_SET` and `USER_AVATAR_REMOVED`, and releasing the previous reference when replacing
- [ ] T050 [US3] Implement the multipart handlers in `internal/modules/user/presentation/http/handler.go` with a route-specific body ceiling above the global `MAX_BODY_BYTES`, sniffing the payload instead of trusting the filename or the client-declared content type, and reading with a hard ceiling
- [ ] T051 [US3] Add the avatar integration test in `internal/modules/user/presentation/http/http_integration_test.go` behind the `integration` tag: upload, replace, rejected upload keeps the old avatar, remove

**Checkpoint**: US1–US3 work independently — the customer profile is complete.

---

## Phase 6: User Story 4 - Read-only customer lookup for operators (Priority: P4)

**Goal**: An administrator can read a customer's contact details and addresses for order or commission handling, and every read is audited.

**Independent Test**: Sign in as a customer and get `403`; sign in as admin and get the
contact details and address list; confirm an audit row exists for the read.

### Tests for User Story 4 (write first — they MUST fail)

- [ ] T052 [P] [US4] HTTP authorization tests in `internal/modules/user/presentation/http/admin_test.go`: customer token → `403`, admin token → `200`, unknown account → `404 USER_NOT_FOUND`, a non-UUID `userId` → `400 VALIDATION_ERROR`, and the audit action recorded for each successful read

### Implementation for User Story 4

- [ ] T053 [US4] Implement the lookup use case in `internal/modules/user/application/implement/admin_lookup.go` and make the module satisfy `internal/contracts.CustomerLookupService`, returning addresses default-first per FR-007d
- [ ] T054 [US4] Implement the ADMIN-guarded `GET /api/v1/users/{userId}` route in `internal/modules/user/presentation/http/handler.go`, offering no write path for customer data
- [ ] T055 [US4] Add the audit assertion to the integration test in `internal/modules/user/presentation/http/http_integration_test.go`: an admin read writes `USER_PROFILE_VIEWED_BY_ADMIN`

**Checkpoint**: All four stories work; later modules have a stable contract to reuse.

---

## Phase 7: Polish & Cross-Cutting Concerns

- [ ] T056 [P] Update `docs/api-reference.md`: add the twelve new endpoints in the six-part format used by the existing entries (info table **including its rate limit** · Request · Response · Errors · notes), add the `USER_*` codes to §1.4, add the two new module limits from T021 to §1.5, extend the §4 summary table, add a change-log row, and correct the constitution version cited in its header. Required by Constitution VIII in the same change
- [ ] T057 [P] Add the media and rate-limit variables to `.env.example` and document them in `docs/configuration.md`, including the failure mode that a missing media configuration disables avatar upload
- [ ] T058 [P] Update `docs/modules/02-user.md` to mark the feature implemented, record the dataset refresh policy, and state the `users` ownership split: auth owns `id`, `email`, `password_hash`, `role`, `status` and timestamps; this module owns the six profile columns and is their only writer
- [ ] T059 [P] Verify in `internal/share/logging/redact_test.go` that media credentials and provider responses cannot leak into logs
- [ ] T060 [P] Check that every new file is UTF-8 without BOM with LF endings per `.editorconfig` and `docs/development/code-hygiene.md`, including `internal/share/administrative/data/vn-divisions.json`
- [ ] T061 [P] Write an ADR at `docs/decisions/NNN-<name>.md` for any long-lived decision taken during implementation that ADR-002 to ADR-005 does not already cover, following `docs/decisions/template.md`; skip this task only when nothing new came up
- [ ] T062 Run the `quickstart.md` validation end-to-end (`make up-tools` plus the curl scenarios) **including the four mandatory checks from `docs/development/api-testing.md`**: 401 without a token, 403 plus an audit row for the wrong role, cross-account access refused with 404, and a correct response envelope
- [ ] T063 Run `make check` — the close-out gate — and clear every finding across `internal/modules/user/`, `internal/share/administrative/`, `internal/contracts/` and `migrations/00004_user.sql`; confirm `git status` shows no stray files before staging
- [ ] T064 Security review pass over `internal/modules/user/presentation/http/handler.go`, `internal/modules/user/presentation/http/router.go` and `internal/modules/user/infrastructure/implement/media/cloudinary.go`: confirm no route accepts an owner id, uploads are validated by content, the default-address invariant holds under concurrent writes, and no media credential is logged

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
- **US2 (P2)**: needs the foundation plus the `Divisions` port; reuses the session and
  audit wiring from US1 but is independently testable.
- **US3 (P3)**: needs the profile entity from US1 (it attaches a reference to it) and
  the `MediaStore` port from Phase 1.
- **US4 (P4)**: needs the foundation plus both profile and address readers; delivers no
  user-visible value on its own.

### Critical Path

T001 → T005 → T013 → T017 → T023 → T031 → T032 → (US1 complete) → T040 → T041 → T050 → T054

The default-address invariant additionally requires T005 before T039, and its test T036
is the one that must fail first.

### Within Each User Story

1. Tests first, and they must fail before the implementation
2. Domain model → repository adapter → use case → handler
3. Integration test last, proving the story works against real infrastructure

### Parallel Opportunities

- Phase 1: T002/T003/T004 (constants and errors) and T006/T007 (dataset verification and
  documentation) can run together
- Phase 2: T013, T014, T015, T018, T019, T020, T021 touch separate files and can run
  together
- Phase 3: T024–T027 (four test files) can run together, then T028/T029 together
- Phase 4: T034–T037 can run together
- Phase 5: T044–T046 can run together
- Phase 7: T056–T061 are documentation and verification and can run in parallel

### Parallel Example: Phase 4

```bash
# All US2 tests written first, in parallel — they fail against the empty module
Task: "Address model tests in internal/modules/user/domain/model/address_test.go"
Task: "Use-case tests in internal/modules/user/application/implement/address_test.go"
Task: "Repository invariant test in internal/modules/user/infrastructure/implement/postgres/address_integration_test.go"
Task: "HTTP tests in internal/modules/user/presentation/http/address_test.go"

# Then the domain work, before adapters and handlers
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
6. Polish → full documentation and the close-out gate

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
- Gates come from `AGENTS.md` §3: a single task needs `make lint` + `make test`; an
  endpoint change additionally needs `make test-integration`; before pushing or opening a
  PR run the close-out gate `make check` (T063)
- SC-001 and SC-010 are human-time outcomes and are verified manually through
  `quickstart.md` (T062); the p95 figures in `plan.md` are informational targets, not
  buildable criteria
- Commit after each task or logical group using Conventional Commits, and only stage the
  files belonging to that change
- Decisions that outlive this feature belong in `docs/decisions/` (T061); decisions scoped
  to this feature belong in `research.md`