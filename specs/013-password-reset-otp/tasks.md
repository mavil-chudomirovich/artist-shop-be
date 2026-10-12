---
description: "Task list for Password Reset via Emailed Code"
---

# Tasks: Password Reset via Emailed Code

**Input**: Design documents from `/specs/013-password-reset-otp/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md

**Tests**: Included. The constitution mandates test-first coverage for state transitions
(positive + negative), and the spec's SC/edge cases require unit + integration tests for the
code lifecycle, token issuance/superseding, and the no-session reset.

**Organization**: Tasks are grouped by user story. The stories form one flow: **US1 + US2 +
US3 are the core** (request code → verify → reset), and US4 hardens abuse/failure handling.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (US1–US4)
- Exact file paths are included in each task

## Path Conventions

- Single Go module; module code under `internal/modules/auth/`, executables under `cmd/`.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Small shared addition used by the whole flow

- [X] T001 Add the reset audit constants in `internal/modules/auth/domain/constant/audit.go` — `AuditPasswordResetCodeVerified = "AUTH_PASSWORD_RESET_CODE_VERIFIED"`, `AuditPasswordResetCodeFailed = "AUTH_PASSWORD_RESET_CODE_FAILED"`, `AuditPasswordResetDeliveryFailed = "AUTH_PASSWORD_RESET_DELIVERY_FAILED"` (reuse `AuditPasswordResetReq` for a request and `AuditPasswordReset` for completion) — FR-018 needs all five events

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The reset-code port, DTOs, Redis adapter and wiring shared by every story

**⚠️ CRITICAL**: No user story work can begin until this phase is complete

- [X] T002 Define the `PasswordResetOTPStore` port in `internal/modules/auth/application/interface/ports.go` (`Issue(ctx, email, otpHash)`, `CanResend(ctx, email)`, `Verify(ctx, email, otp)`, `Invalidate(ctx, email)`, `DisarmCooldown(ctx, email)`; all keyed by normalized email)
- [X] T003 [P] Add the use-case DTOs (`VerifyResetOTPInput{Email, OTP}`, `ResetTokenOutput{ResetToken string, ExpiresIn int}`) in `internal/modules/auth/application/dto/dto.go`, and the HTTP DTOs (`VerifyResetOTPRequest{Email, OTP}`, `ResetTokenResponse{ResetToken, ExpiresIn}`) in `internal/modules/auth/presentation/dto/dto.go`
- [X] T004 Implement the Redis `PasswordResetOTPStore` in `internal/modules/auth/infrastructure/implement/redis/reset_otp.go` per `data-model.md` (keys `auth:reset:otp:{email}`, `:attempts:`, `:blocked:`, `:sent:`; Argon2id hash via `PasswordHasher`; TTLs from the constructor; block at `maxAttempts`)
- [X] T005 [P] Write miniredis unit tests for the store (issue/verify, attempts→block, cooldown + disarm, expiry, replacement) in `internal/modules/auth/infrastructure/implement/redis/reset_otp_test.go`
- [X] T006 Add `ResetOTP appinterface.PasswordResetOTPStore` to `Service` in `internal/modules/auth/application/implement/service.go`, and wire `NewPasswordResetOTPStore(redisCache, token.Hasher{}, cfg.Auth.OTPMaxAttempts, cfg.Auth.OTPTTL, cfg.Auth.OTPBlockTTL, cfg.Auth.OTPResendCooldown)` in `cmd/api/main.go`
- [X] T007 [P] Add a fake `PasswordResetOTPStore` to `internal/modules/auth/application/implement/fakes_test.go` for use-case tests

**Checkpoint**: Port, adapter and wiring ready — user stories can begin

---

## Phase 3: User Story 1 - Request a reset code (Priority: P1) 🎯 MVP

**Goal**: `POST /auth/password/forgot` emails a one-time code (not a token) with a generic response.

**Independent Test**: Request for a registered address → generic `202` + a code by email; for an unregistered address → the same `202` and no email.

### Tests for User Story 1

- [X] T008 [P] [US1] Write failing unit tests for `ForgotPassword` in `internal/modules/auth/application/implement/password_test.go`: registered → code hash stored, email sent, cooldown armed; unregistered → generic no-op, nothing stored/sent; within cooldown → `ErrResendCooldown`; delivery failure → `ErrVerificationDeliveryFailed` and cooldown disarmed

### Implementation for User Story 1

- [X] T009 [US1] Rewrite `ForgotPassword` in `internal/modules/auth/application/implement/password.go`: validate the email (`model.ValidateEmail`), normalize, `Users.ByEmail` (unknown → return nil, generic), `ResetOTP.CanResend`, generate+hash the code with `generateOTP`+`Hasher`, `ResetOTP.Issue`, send an email whose body is the code (no token) via `sendWithRetry`; on failure disarm the cooldown, audit `AuditPasswordResetDeliveryFailed`, and return `ErrVerificationDeliveryFailed`; on success audit `AuditPasswordResetReq`
- [X] T010 [US1] Update the `ForgotPassword` handler message and Swagger annotations in `internal/modules/auth/presentation/http/handler.go` (route and `202` unchanged); ensure `ErrEmailInvalid`/validation maps in `internal/modules/auth/presentation/http/errors.go`
- [X] T011 [P] [US1] Write an integration test in `internal/modules/auth/presentation/http/http_integration_test.go`: registered address returns generic `202` and the fake/inbox receives a code; unregistered returns the same `202` and sends nothing

**Checkpoint**: A code can be requested; the email no longer carries a token

---

## Phase 4: User Story 2 - Verify the code and receive a reset token (Priority: P1) 🎯 MVP

**Goal**: `POST /auth/password/forgot/verify-otp` exchanges a correct code for a single-use reset token.

**Independent Test**: Request a code, submit the correct code → a reset token is returned and the code is dead; a wrong code returns none.

### Tests for User Story 2

- [X] T012 [P] [US2] Write failing unit tests for `VerifyResetOTP` (append to `password_test.go`): correct code → a reset token is created for the account and the code is invalidated; wrong → `ErrOTPInvalid` and no token; expired → `ErrOTPExpired`; at the attempt limit → `ErrOTPTooManyAttempts`; unregistered email (no active code) → `ErrOTPExpired` and no token

### Implementation for User Story 2

- [X] T013 [US2] Implement `VerifyResetOTP(ctx, dto.VerifyResetOTPInput) (dto.ResetTokenOutput, error)` in `internal/modules/auth/application/implement/password.go`: validate+normalize the email, `ResetOTP.Verify`, `Users.ByEmail` (not found → `ErrOTPExpired`; a defensive branch that is unreachable because a code only exists for a registered address), generate the opaque token via `RefreshTokens.Generate`, create a `model.ResetRequest` (`ExpiresAt = now + Config.PasswordResetTTL`) inside `Tx.WithinTx` through `Resets.Create`, then `ResetOTP.Invalidate`; audit `AuditPasswordResetCodeVerified` on success and `AuditPasswordResetCodeFailed` when the code is rejected; return `{ResetToken, ExpiresIn: PasswordResetTTL seconds}`
- [X] T014 [US2] Add the `VerifyResetOTP` handler with Swagger annotations in `internal/modules/auth/presentation/http/handler.go` and add `POST /password/forgot/verify-otp` (inside the flow rate-limit group) in `internal/modules/auth/presentation/http/router.go`
- [X] T015 [US2] Add `VerifyResetOTP` to the `AuthService` interface in `internal/modules/auth/application/interface/ports.go`
- [X] T016 [P] [US2] Write an integration test in `http_integration_test.go`: request → read code → verify-otp → `200` with `resetToken` + `expiresIn`; the code no longer verifies afterwards

**Checkpoint**: US1 + US2 together issue a code-verified reset token

---

## Phase 5: User Story 3 - Set the new password and return to sign-in (Priority: P2)

**Goal**: `POST /auth/password/reset` changes the password and returns success **without** a session.

**Independent Test**: Reset with a valid token → success with no token pair; old password fails, new password signs in; the token cannot be reused.

### Tests for User Story 3

- [X] T017 [P] [US3] Update/write failing unit tests for `ResetPassword` in `password_test.go`: valid token → password changed, token consumed, all sessions revoked, prior access tokens blacklisted, **no session returned**; used/expired/unknown token → `ErrResetInvalid`; weak password → `ErrWeakPassword` and the token stays usable

### Implementation for User Story 3

- [X] T018 [US3] Change `ResetPassword` in `internal/modules/auth/application/implement/password.go` to return `error` (drop the issued session): keep the policy check, token validation, `Users.UpdatePassword`, `Resets.Consume`, `Sessions.RevokeAllForUser` and `Blacklist.RevokeUserBefore` in one transaction; audit `AuditPasswordReset`; remove the `issueSession` call
- [X] T019 [US3] Update the `ResetPassword` signature in the `AuthService` interface (`internal/modules/auth/application/interface/ports.go`) to `error`
- [X] T020 [US3] Update the `ResetPassword` handler in `internal/modules/auth/presentation/http/handler.go` to write `httpdto.MessageResponse` at `200` (no token pair) with updated Swagger annotations
- [X] T021 [US3] Update the existing tests that assumed `ResetPassword` returned a session (`internal/modules/auth/application/implement/auth_test.go`, `internal/modules/auth/presentation/http/http_test.go`, `http_integration_test.go`) so the build and tests stay green
- [X] T022 [P] [US3] Write an integration test in `http_integration_test.go`: full flow request→verify→reset returns `200` with no tokens; `/auth/login` with the new password succeeds and with the old password fails; reusing the token returns `400 AUTH_RESET_INVALID`

**Checkpoint**: The full flow works end-to-end and ends at sign-in

---

## Phase 6: User Story 4 - Abuse and interruption handling (Priority: P3)

**Goal**: Request/verify are rate-limited and cooldown-gated; delivery failure is retryable; superseded tokens are refused.

**Independent Test**: Exceed the request limit → throttled; force a delivery failure → clear retryable result with the cooldown freed; issue a newer token → the older one is refused.

### Tests for User Story 4

- [X] T023 [P] [US4] Write failing tests in `password_test.go` + `internal/modules/auth/presentation/http/http_test.go`: the request cooldown gates repeats; the flow rate limit returns `429 RATE_LIMITED` on `/password/forgot` and `/password/forgot/verify-otp`; a newer reset supersedes an earlier token (the earlier one is refused)
- [X] T024 [P] [US4] Write an integration test in `http_integration_test.go` for the cooldown, the rate limit, and token superseding

### Implementation for User Story 4

- [X] T025 [US4] Harden the flow per the tests (depends on T009/T014, which add the cooldown/rate-limit paths): ensure the request endpoint's cooldown/rate-limit responses are correct and a superseded/used token is refused by the existing `ResetRepository.Create` invalidation + `used_at` check; fix any regression
- [X] T026 [US4] Confirm the existing sign-in/registration/password-change tests still pass; update fixtures only where needed

**Checkpoint**: The flow is throttled and interruption-safe

---

## Phase 7: Polish & Cross-Cutting Concerns

**Purpose**: Contract docs, cleanup, environment reset, and the full gate

- [X] T027 [P] Update `internal/modules/auth/README.md` to describe the code-verified reset flow and the three endpoints
- [X] T028 [P] Update `docs/api-reference.md`: rewrite §3.8 (`/password/forgot` now emails a code and returns `AUTH_RESEND_COOLDOWN` again), add the `verify-otp` endpoint, rewrite §3.9 (`/password/reset` no longer returns a session), update the summary table and the change log (Constitution VIII)
- [X] T029 [P] Review `docs/development/api-testing.md` and `docs/getting-started.md` for the reset flow and correct any reference to the old token-email / auto-login behaviour

### Swagger annotations *(bắt buộc — feature có endpoint)*

- [X] T030 Ensure all three handlers in `internal/modules/auth/presentation/http/handler.go` carry `@Summary`, `@Tags`, `@Param`, `@Success`, `@Failure`, `@Router`, then run `make swagger` to regenerate `docs/swagger/`; `make swagger-check` must pass (Constitution VIII)

### Frontend Integration Guide *(bắt buộc — hiến pháp §Governance)*

- [X] T031 **Đồng bộ guide** sau khi mã nguồn ổn định: đọc lại `specs/013-password-reset-otp/frontend-guide.md` và đối chiếu với mã nguồn **có đếm số** — mọi trường JSON của DTO, mọi giá trị enum, mọi route, mọi mã HTTP. Sửa mọi chỗ lệch; ghi rõ phần nào chưa kiểm được và vì sao

### Environment reset & validation

- [X] T032 **Reset Docker**: `make down` rồi `make up-tools` (dùng Mailpit cho email), chạy lại `make migrate-up-docker`, đảm bảo stack sạch khởi động
- [X] T033 Validate `specs/013-password-reset-otp/quickstart.md` end-to-end trên stack vừa reset (request → code → verify → reset → login bằng mk mới; mk cũ fail; token dùng lại bị từ chối), và kiểm tra thủ công SC-006 (không plaintext password/code/token trong log và Redis) — ghi rõ đây là kiểm tra thủ công, không phải test tự động
- [X] T034 Run the full gate `make check` (format + tidy + vet + lint + unit + integration + build + swagger-check)
- [X] T035 Write `specs/013-password-reset-otp/deferred.md` for any out-of-scope debt found (what it is, why out of scope, what would unblock it) — do not leave unchecked tasks here

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: no dependencies
- **Foundational (Phase 2)**: depends on Setup; **blocks all user stories**
- **US1 (Phase 3)**: after Foundational
- **US2 (Phase 4)**: after Foundational (verify needs the store; logically after US1 to have a code)
- **US3 (Phase 5)**: after US2 (reset needs a token); **core = US1 + US2 + US3**
- **US4 (Phase 6)**: after US1/US2
- **Polish (Phase 7)**: after all desired stories

### Within Each Story

- Tests are written and FAIL before implementation (constitution IV)
- Port/adapter before use case; use case before handler/route
- Core implementation before integration tests

### Parallel Opportunities

- T003, T005, T007 in Foundational
- All `[P]` test tasks within a story

---

## Parallel Example: Foundational

```text
Task: T003 DTOs in application/dto/dto.go and presentation/dto/dto.go
Task: T005 miniredis tests for the store in infrastructure/implement/redis/reset_otp_test.go
Task: T007 Fake PasswordResetOTPStore in application/implement/fakes_test.go
```

---

## Implementation Strategy

### MVP First (US1 + US2 + US3)

1. Complete Phase 1 and Phase 2
2. Complete US1, then US2, then US3 → the whole reset flow works end-to-end
3. US4 hardens abuse/failure handling
4. Phase 7 → docs, swagger, Docker reset, quickstart, full gate

### Notes

- [P] tasks = different files, no dependencies
- No migration and no new config keys are needed (reset token table and `OTP_*`/`PASSWORD_RESET_TTL` already exist)
- Commit only when the dev explicitly authorizes it

---

## Phase 8: Convergence

**Lý do**: `/speckit-converge` đối chiếu mã nguồn với `spec.md`/`plan.md`/`tasks.md` và chỉ còn **một** gap.
Edge case của spec — "Two resets race for the same account: at most one succeeds; the other is
refused" — và quyết định D11 của `research.md` ("hai lời gọi `verify-otp` đồng thời cho cùng một
tài khoản chỉ để lại tối đa một reset token dùng được") **chưa** được bảo đảm ở tầng lưu trữ.
`ResetRepository.Create` (`internal/modules/auth/infrastructure/implement/postgres/reset.go`) chạy
`UPDATE password_reset_requests SET used_at = now() WHERE user_id = $1 AND used_at IS NULL` rồi
`INSERT` dưới READ COMMITTED: khi không có dòng mở nào thấy được, hai giao dịch đồng thời đều chèn
được một dòng `used_at IS NULL`, nên có thể cùng tồn tại **hai** reset token dùng được cho một tài
khoản. Thêm nữa `Consume` là `UPDATE ... WHERE id = $1` (thiếu `AND used_at IS NULL`) nên hai
`ResetPassword` đồng thời trên cùng một token đều trả thành công. Luồng reset cũng không có test
đồng thời nào (đăng ký đã có `TestConcurrentVerificationCreatesExactlyOneAccount`). Đây là gap
`partial`, đã báo orchestrator; quyết định D11 ("không cần khoá thêm") cần dev xác nhận.

- [X] T036 [US3] Bảo đảm bất biến "tối đa một reset token dùng được cho mỗi tài khoản" và "một token chỉ dùng được một lần" dưới truy cập đồng thời per FR-006 + FR-012 + edge case "Two resets race for the same account" + plan: research D11 (partial): làm `ResetRepository.Consume` trong `internal/modules/auth/infrastructure/implement/postgres/reset.go` có điều kiện (`WHERE id = $1 AND used_at IS NULL`, đọc số dòng bị ảnh hưởng và trả `domainerr.ErrResetInvalid` khi 0) để lần consume thứ hai bị từ chối; tuần tự hoá `Create` theo tài khoản trong cùng transaction (khoá dòng `users` bằng `SELECT ... FOR UPDATE` hoặc advisory lock trước `UPDATE`+`INSERT`) để hai `verify-otp` đồng thời không tạo được hai token mở; ưu tiên cơ chế **không cần migration** — nếu buộc phải thêm chỉ mục unique từng phần `(user_id) WHERE used_at IS NULL` thì ghi rõ sai lệch so với "no migration" của `plan.md` (Complexity Tracking/ADR). Thêm integration test đồng thời (tag `integration`) trong `internal/modules/auth/presentation/http/http_integration_test.go` chứng minh: hai `POST /password/forgot/verify-otp` đồng thời cùng một mã chỉ để lại một token dùng được, và hai `POST /password/reset` đồng thời trên cùng token chỉ một thành công (cái kia `400 AUTH_RESET_INVALID`). Cập nhật `docs/api-reference.md` và `specs/013-password-reset-otp/frontend-guide.md` nếu hành vi lỗi thay đổi (Constitution VIII).
