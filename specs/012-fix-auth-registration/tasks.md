---
description: "Task list for OTP-Gated Registration (No Account Before Verification)"
---

# Tasks: OTP-Gated Registration (No Account Before Verification)

**Input**: Design documents from `/specs/012-fix-auth-registration/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md

**Tests**: Included. The constitution mandates test-first coverage for state transitions
(positive + negative), and the spec's SC/edge cases require unit + integration tests for
the registration lifecycle, OTP attempts, replacement, idempotency and concurrency.

**Organization**: Tasks are grouped by user story. Because this is one coherent flow, the
stories are coupled: **US1 + US2 form the MVP** (a registration that creates no account
until the code is confirmed), and US2/U3/U4/U5 build on US1's temporary-registration store.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (US1–US5)
- Exact file paths are included in each task

## Path Conventions

- Single Go module; module code under `internal/modules/auth/`, platform additions under
  `internal/share/`, executables under `cmd/`, migrations under `migrations/`.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Configuration the whole flow reads

- [X] T001 Add `PendingRegistrationTTL time.Duration` (`env:"PENDING_REGISTRATION_TTL" envDefault:"30m"`) to `AuthConfig` and validate it `> 0`; change the `OTPTTL` default to `5m` and the `OTPMaxAttempts` default to `5` in `internal/share/config/config.go`
- [X] T002 [P] Update the default expectations in `internal/share/config/config_test.go` (`OTP_MAX_ATTEMPTS` 3→5, add `PENDING_REGISTRATION_TTL`)
- [X] T003 [P] Update `.env.example`: set `OTP_TTL=5m`, `OTP_MAX_ATTEMPTS=5`, and add `PENDING_REGISTRATION_TTL=30m`

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The temporary-registration model, ports, DTOs and Redis adapter shared by every story

**⚠️ CRITICAL**: No user story work can begin until this phase is complete

- [X] T004 Create the `PendingRegistration` model (`RegistrationID`, `Email`, `Username *string`, `DisplayName *string`, `PasswordHash`, `CreatedAt`, `ExpiresAt`) and `NewRegistrationID() (string, error)` (32 bytes `crypto/rand`, `base64.RawURLEncoding`) in `internal/modules/auth/domain/model/registration.go`
- [X] T005 [P] Write unit tests for the reference generator (unpredictable/length/decodable) in `internal/modules/auth/domain/model/registration_test.go`
- [X] T006 [P] Add `ErrRegistrationNotFound = errors.New("registration not found")` in `internal/modules/auth/domain/error/errors.go`
- [X] T007 [P] Add `CodeRegistrationNotFound = "AUTH_REGISTRATION_NOT_FOUND"` in `internal/modules/auth/domain/constant/codes.go`; add `AuditRegistrationStarted`, `AuditRegistrationCancelled`, `AuditRegistrationFailed` (a code was rejected) and `AuditRegistrationResent` (a code was replaced) in `internal/modules/auth/domain/constant/audit.go` (reuse `AuditEmailVerified` for a verified code and `AuditRegisterDeliveryFailed` for a failed delivery) — FR-020 needs all six events
- [X] T008 Define the `RegistrationStore` port (`Save`, `ByID`, `Delete`, `IssueOTP`, `CanResend`, `VerifyOTP`, `InvalidateOTP`, `DisarmCooldown`, all keyed by `registrationID string`) in `internal/modules/auth/application/interface/ports.go`
- [X] T009 [P] Add the use-case input/output DTOs (`StartRegistrationInput`, `VerifyRegistrationInput{RegistrationID, OTP}`, `RegistrationIDInput`, `RegistrationStartedOutput{RegistrationID, Status, OTPExpiresIn, ResendAvailableIn}`) in `internal/modules/auth/application/dto/dto.go`, and the HTTP DTOs (`StartRegistrationRequest` = current `RegisterRequest`, `VerifyRegistrationRequest`, `RegistrationIDRequest`, `RegistrationStartedResponse`) in `internal/modules/auth/presentation/dto/dto.go`
- [X] T010 Implement the Redis `RegistrationStore` in `internal/modules/auth/infrastructure/implement/redis/registration.go` per `data-model.md` (keys `auth:reg:{id}`, `auth:reg:email:{email}`, `auth:reg:otp:{id}`, `auth:reg:otp:attempts:{id}`, `auth:reg:otp:blocked:{id}`, `auth:reg:otp:sent:{id}`); `Save` performs the atomic same-email replacement via a Lua script through `cache.Client().Eval`
- [X] T011 [P] Write miniredis unit tests for the store (save/load/replace/delete; OTP issue/verify/attempts/block/cooldown) in `internal/modules/auth/infrastructure/implement/redis/registration_test.go`
- [X] T012 Add `Registration appinterface.RegistrationStore` to `Service` and `PendingRegistrationTTL`, `OTPResendCooldown` to `implement.Config` in `internal/modules/auth/application/implement/service.go`
- [X] T013 Wire the store and new TTLs in `cmd/api/main.go` (`authredis.NewRegistrationStore(redisCache, token.Hasher{}, cfg.Auth.OTPMaxAttempts, cfg.Auth.OTPTTL, cfg.Auth.OTPBlockTTL, cfg.Auth.OTPResendCooldown, cfg.Auth.PendingRegistrationTTL)`)
- [X] T014 [P] Add a fake `RegistrationStore` to `internal/modules/auth/application/implement/fakes_test.go` for use-case tests

**Checkpoint**: Model, port, adapter and wiring ready — user stories can begin

---

## Phase 3: User Story 1 - Registration holds no permanent account until verified (Priority: P1) 🎯 MVP

**Goal**: `POST /auth/register` validates, stores a temporary registration, emails a code, and returns the reference — creating no account.

**Independent Test**: Register once and confirm no `users` row exists and the response carries `registrationId` + timings; a taken email/username returns 409; a second registration for the same email replaces the first.

### Tests for User Story 1

- [X] T015 [P] [US1] Write failing unit tests for `StartRegistration` in `internal/modules/auth/application/implement/registration_test.go`: valid start stores temp + sends code and creates no account; duplicate email → `ErrEmailTaken`; duplicate username → `ErrUsernameTaken`; second start for the same email replaces the previous; email normalized; delivery failure keeps temp, disarms cooldown and returns `ErrVerificationDeliveryFailed`

### Implementation for User Story 1

- [X] T016 [US1] Implement `StartRegistration(ctx context.Context, in dto.StartRegistrationInput) (dto.RegistrationStartedOutput, error)` in `internal/modules/auth/application/implement/registration.go`: validate password policy, normalize email/username, `Users.ByEmail` → `ErrEmailTaken`, username availability check, generate reference, hash password, `Registration.Save`, generate+hash OTP, `Registration.IssueOTP`, send with the existing retry/classification helpers; on delivery failure keep the temp registration, disarm cooldown, audit `AuditRegisterDeliveryFailed`, return `ErrVerificationDeliveryFailed`; on success audit `AuditRegistrationStarted` and return the reference + `otpExpiresIn` + `resendAvailableIn`
- [X] T017 [US1] Replace the `Register` handler with `StartRegistration` returning `RegistrationStartedResponse` (`202`) with Swagger annotations in `internal/modules/auth/presentation/http/handler.go`
- [X] T018 [US1] Keep `POST /register` in `internal/modules/auth/presentation/http/router.go` (flow rate limit)
- [X] T019 [US1] Map `ErrRegistrationNotFound` → `AUTH_REGISTRATION_NOT_FOUND` (400, generic) in `internal/modules/auth/presentation/http/errors.go`, keeping the existing `AUTH_EMAIL_TAKEN`/`AUTH_USERNAME_TAKEN` (409) mappings
- [X] T020 [US1] Update the `AuthService` interface in `internal/modules/auth/application/interface/ports.go`: add `StartRegistration`, remove `Register`

**Checkpoint**: Registration starts with no durable account; independently testable

---

## Phase 4: User Story 2 - Confirming the code creates the account exactly once (Priority: P1) 🎯 MVP

**Goal**: `POST /auth/register/verify-otp` confirms the code, creates exactly one active account, discards the temp registration, and is idempotent on replay.

**Independent Test**: Register → verify with the correct code creates an active account usable for `/auth/login`; replaying the same confirmation creates no second account; wrong/expired/unknown references create none.

### Tests for User Story 2

- [X] T021 [P] [US2] Write failing unit tests for `VerifyRegistrationOTP` in `internal/modules/auth/application/implement/registration_test.go`: correct code creates an `active` CUSTOMER account and marks the temp `completed` (code + email pointer removed, record retained); a **repeated** confirmation of the same reference returns success and creates no duplicate; wrong code → `ErrOTPInvalid` and the temp is kept; unknown reference → `ErrRegistrationNotFound`; expired code → `ErrOTPExpired`; a database error keeps the temp and returns the error

### Implementation for User Story 2

- [X] T022 [US2] Implement `VerifyRegistrationOTP(ctx context.Context, in dto.VerifyRegistrationInput) error` in `internal/modules/auth/application/implement/registration.go`: `Registration.ByID` (→ `ErrRegistrationNotFound`), `Registration.VerifyOTP`, insert the account with `Status: active` and `Role: CUSTOMER`; on success remove the code and the email pointer and **mark the registration `completed`** (retain the record until TTL); a repeated confirmation of a completed reference returns success without creating a second account (FR-010/FR-011). Audit `AuditEmailVerified` on success and `AuditRegistrationFailed` when the code is rejected. **Idempotency vs username conflict (FR-011/FR-023)**: on a unique-violation insert error, call `Users.ByEmail(reg.Email)` — if found, treat as idempotent success (mark completed, audit verified); if not found the violation is the username → return `ErrUsernameTaken` (409) and keep the temp; any other DB error keeps the temp and is returned
- [X] T023 [US2] Add the `VerifyRegistrationOTP` handler with Swagger annotations in `internal/modules/auth/presentation/http/handler.go` and add `POST /register/verify-otp`, removing `POST /verify-email`, in `internal/modules/auth/presentation/http/router.go`
- [X] T024 [US2] Remove the obsolete `VerifyEmail` use case (delete `internal/modules/auth/application/implement/verify_email.go`) and remove `VerifyEmail` from the `AuthService` interface in `internal/modules/auth/application/interface/ports.go`
- [X] T025 [P] [US2] Write an integration test in `internal/modules/auth/presentation/http/http_integration_test.go`: start registration → verify → account is `active` → `/auth/login` succeeds; a **repeated** verify of the same reference returns `200` success and creates no duplicate (FR-011)

**Checkpoint**: US1 + US2 together are the MVP — a working, verification-gated registration

---

## Phase 5: User Story 3 - Requesting a fresh code (Priority: P2)

**Goal**: `POST /auth/register/resend-otp` replaces the code after the cooldown and resets the attempt budget.

**Independent Test**: Resend before the cooldown → `429 AUTH_RESEND_COOLDOWN`; after it → `202` and the previous code no longer verifies.

### Tests for User Story 3

- [X] T026 [P] [US3] Write failing unit tests for `ResendRegistrationOTP` in `internal/modules/auth/application/implement/registration_test.go`: within cooldown → `ErrResendCooldown`; after cooldown → new code issued, old code invalid, attempt counter reset; unknown reference → generic not-found

### Implementation for User Story 3

- [X] T027 [US3] Implement `ResendRegistrationOTP(ctx context.Context, in dto.RegistrationIDInput) error` in `internal/modules/auth/application/implement/registration.go` (load, `CanResend`, generate+hash new code, `IssueOTP`, send with retry, disarm cooldown on failure, audit `AuditRegistrationResent`)
- [X] T028 [US3] Add the resend handler with Swagger annotations in `internal/modules/auth/presentation/http/handler.go`; add `POST /register/resend-otp` (inside the flow rate-limit group, `AUTH_FLOW_RATE_PER_MINUTE`, FR-018) and remove `POST /resend-verification` in `internal/modules/auth/presentation/http/router.go`
- [X] T029 [US3] Remove the obsolete `ResendVerification` use case and update the `AuthService` interface in `internal/modules/auth/application/interface/ports.go`
- [X] T030 [P] [US3] Write an integration test in `internal/modules/auth/presentation/http/http_integration_test.go`: cooldown refusal then successful resend that invalidates the old code

**Checkpoint**: Resend works and respects the cooldown

---

## Phase 6: User Story 5 - The flow survives partial failures and abuse (Priority: P2)

**Goal**: A transient account-store failure at confirmation does not lose the registration, retries converge on one account, and code guessing is bounded.

**Independent Test**: Force an insert failure after a correct code → temp kept → retry succeeds once; concurrent confirmations → one account; wrong codes past the limit are refused until a resend.

### Tests for User Story 5

- [X] T031 [P] [US5] Write failing tests in `internal/modules/auth/application/implement/registration_test.go`: a failing `Users.Create` keeps the temp registration and returns the error; retrying afterwards creates exactly one account; wrong codes beyond `OTP_MAX_ATTEMPTS` → `ErrOTPTooManyAttempts`; two concurrent `VerifyRegistrationOTP` calls for one reference produce one account
- [X] T032 [P] [US5] Write an integration concurrency test in `internal/modules/auth/presentation/http/http_integration_test.go` that fires two simultaneous verify requests for one reference and asserts exactly one `users` row

### Implementation for User Story 5

- [X] T033 [US5] Harden `VerifyRegistrationOTP` in `internal/modules/auth/application/implement/registration.go` so a non-conflict insert error leaves the registration and code intact (rely on the `users` unique constraints for the single-account guarantee), and confirm the attempt-limit path returns `ErrOTPTooManyAttempts`
- [X] T034 [US5] Confirm the existing sign-in/refresh/logout/password-reset/password-change tests still pass (update fixtures only where the removed endpoints were referenced); fix any regression

**Checkpoint**: Transient failures and concurrency cannot lose or duplicate accounts

---

## Phase 7: User Story 4 - Abandoning or cancelling a registration (Priority: P3)

**Goal**: Leaving the screen is harmless (temp data lives to TTL); `POST /auth/register/cancel` removes it on demand.

**Independent Test**: Abandoning a registration leaves the temp data until TTL; cancel makes the reference and code unusable and is idempotent.

### Tests for User Story 4

- [X] T035 [P] [US4] Write failing unit tests for `CancelRegistration` in `internal/modules/auth/application/implement/registration_test.go`: cancel removes the temp registration and invalidates the code; cancelling an unknown/expired reference is a generic no-op; the temp record is untouched by anything but cancel/verify/expiry

### Implementation for User Story 4

- [X] T036 [US4] Implement `CancelRegistration(ctx context.Context, in dto.RegistrationIDInput) error` in `internal/modules/auth/application/implement/registration.go` (`Registration.Delete`, audit `AuditRegistrationCancelled`, idempotent/generic)
- [X] T037 [US4] Add the cancel handler with Swagger annotations in `internal/modules/auth/presentation/http/handler.go`; add `POST /register/cancel` (inside the flow rate-limit group, `AUTH_FLOW_RATE_PER_MINUTE`, FR-018) in `internal/modules/auth/presentation/http/router.go`; add `CancelRegistration` to the `AuthService` interface in `internal/modules/auth/application/interface/ports.go`
- [X] T038 [P] [US4] Write an integration test in `internal/modules/auth/presentation/http/http_integration_test.go`: cancel makes verify/resend return the generic not-found result

**Checkpoint**: All user stories are independently functional

---

## Phase 8: Polish & Cross-Cutting Concerns

**Purpose**: Contract docs, cleanup, gates, and the environment reset requested by the user

- [X] T039 Remove the now-unused old registration code and tests: delete `internal/modules/auth/application/implement/register.go`, delete `internal/modules/auth/infrastructure/implement/redis/otp.go` + `otp_test.go` (and the OTP parts of `store_test.go`) once `RegistrationStore` is the only consumer; drop the obsolete `OTPStore` interface and the `VerifyEmailRequest`/`EmailRequest` DTOs; update `internal/modules/auth/application/implement/auth_test.go`, `register_test.go` and `fakes_test.go` to the new surface
- [X] T040 [P] Update `internal/modules/auth/README.md` (and any module flow notes) to describe the deferred-account flow and the four endpoints
- [X] T041 [P] Update `docs/api-reference.md`: replace the `verify-email`/`resend-verification` entries with the four new endpoints (method, path, auth, rate limit, request/response + example, error codes), state the `409 AUTH_EMAIL_TAKEN` behaviour, and call out the removed endpoints in the change log (Constitution VIII)
- [X] T042 [P] Update `docs/configuration.md` env table: `OTP_TTL` default `15m→5m`, `OTP_MAX_ATTEMPTS` default `3→5`, and add `PENDING_REGISTRATION_TTL` (`30m`)

### Swagger annotations *(bắt buộc — feature có endpoint)*

- [X] T043 Ensure all four handlers in `internal/modules/auth/presentation/http/handler.go` carry `@Summary`, `@Tags`, `@Param`, `@Success`, `@Failure`, `@Router`, then run `make swagger` to regenerate `docs/swagger/`; `make swagger-check` must pass (Constitution VIII)

### Frontend Integration Guide *(bắt buộc — hiến pháp §Governance)*

- [X] T044 **Đồng bộ guide** ở phase cuối sau khi mã nguồn ổn định: đọc lại `specs/012-fix-auth-registration/frontend-guide.md` và đối chiếu với mã nguồn **có đếm số** — mọi trường JSON của DTO, mọi giá trị enum, mọi route, mọi mã HTTP. Sửa mọi chỗ lệch; ghi rõ phần nào chưa kiểm được và vì sao

### Environment reset & validation *(yêu cầu của dev)*

- [X] T045 **Reset Docker**: `make down` rồi `make up` (dùng `docker compose down -v` nếu cần xoá volume), chạy lại `make migrate-up` + seed, đảm bảo stack sạch khởi động và Redis/PostgreSQL rỗng đúng như mong đợi
- [X] T046 Validate `specs/012-fix-auth-registration/quickstart.md` end-to-end trên stack vừa reset (bảy kịch bản: start/verify/resend/cancel/replacement/attempt-limit/sign-in), và kiểm tra tường minh SC-006: không có password/code dạng plaintext trong log và trong Redis
- [X] T047 Run the full gate `make check` (format + tidy + vet + lint + unit + integration + build + swagger-check)
- [X] T048 Write `specs/012-fix-auth-registration/deferred.md` for any out-of-scope debt found (what it is, why out of scope, what would unblock it) — do not leave unchecked tasks here

---

## Phase 9: Convergence

**Lý do**: `/speckit-converge` phát hiện FR-001 ("validate … email format") chưa được thoả — `StartRegistration` chỉ normalize email, không kiểm định dạng. FR-001 là MUST nên orchestrator bổ sung task và thực thi (quyết định §9, ghi trong báo cáo).

- [X] T049 [US1] Thêm kiểm định dạng email phía server cho `StartRegistration` (FR-001): thêm `model.ValidateEmail` (dùng `net/mail.ParseAddress` + kiểm tra local/domain) và sentinel `ErrInvalidEmail`; map `ErrInvalidEmail` → `VALIDATION_ERROR` (400) trong `internal/modules/auth/presentation/http/errors.go`; unit test trong `internal/modules/auth/application/implement/registration_test.go`; cập nhật `docs/api-reference.md` §3.1 và `specs/012-fix-auth-registration/contracts/registration-error-codes.md` để nêu `VALIDATION_ERROR` trên `register`

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: no dependencies — start immediately
- **Foundational (Phase 2)**: depends on Setup; **blocks all user stories**
- **US1 (Phase 3)**: after Foundational
- **US2 (Phase 4)**: after US1 (verify needs the temp registration that `StartRegistration` writes) — **MVP is US1 + US2 together**
- **US3 (Phase 5)**: after US1 (resend needs a temp registration)
- **US5 (Phase 6)**: after US2 (hardens verify)
- **US4 (Phase 7)**: after US1 (cancel needs a temp registration)
- **Polish (Phase 8)**: after all desired stories

### Story Dependencies

- US1 → US2, US3, US4, US5 (all need the temporary-registration store and `StartRegistration`)
- US3, US4, US5 are independent of each other once US1 (and US2 for US5) is done

### Within Each Story

- Tests are written and FAIL before implementation (constitution IV)
- Model/port before use case; use case before handler/route
- Core implementation before integration tests

### Parallel Opportunities

- T002, T003 in Setup; T005–T009, T011, T014 in Foundational
- All `[P]` test tasks within a story
- After Foundational, US3 and US4 can proceed in parallel once US1 lands

---

## Parallel Example: Foundational

```text
Task: T005 Unit tests for the reference generator in domain/model/registration_test.go
Task: T006 ErrRegistrationNotFound in domain/error/errors.go
Task: T007 Constants in domain/constant/{codes,audit}.go
Task: T009 DTOs in application/dto/dto.go and presentation/dto/dto.go
Task: T011 miniredis tests for the store in infrastructure/implement/redis/registration_test.go
Task: T014 Fake RegistrationStore in application/implement/fakes_test.go
```

---

## Implementation Strategy

### MVP First (US1 + US2)

1. Complete Phase 1: Setup
2. Complete Phase 2: Foundational
3. Complete Phase 3 (US1) then Phase 4 (US2)
4. **STOP and VALIDATE**: register creates no account; verify creates exactly one; login works
5. US3, US5, US4 add resend, resilience and cancel incrementally

### Incremental Delivery

1. Setup + Foundational → foundation ready
2. US1 + US2 → deploy/demo the gated registration (MVP)
3. US3 → resend; US5 → resilience; US4 → cancel
4. Phase 8 → contract docs, cleanup, Docker reset, quickstart, full gate

---

## Notes

- [P] tasks = different files, no dependencies
- [Story] label maps each task to its user story for traceability
- No migration is required (accounts are inserted `active`; existing constraints suffice)
- Commit only when the dev explicitly authorizes it; the dev asked to reset Docker, update
  the frontend guide and the API doc as part of finishing this feature
