# Feature Specification: OTP-Gated Registration (No Account Before Verification)

**Feature Branch**: `012-fix-auth-registration`

**Created**: 2026-10-12

**Status**: Draft

**Input**: User description: "Chỉnh luồng auth lại theo `docs/modules/fix-auth.md` — đăng ký chỉ lưu tạm, chỉ tạo tài khoản chính thức sau khi xác thực OTP, xử lý thoát giữa chừng / OTP hết hạn / gửi lại / lỗi hệ thống."

## Clarifications

### Session 2026-10-12

- Q: Trường `username` trong luồng đăng ký mới? → A: Giữ `username` (tùy chọn) + `displayName`; `username` vẫn duy nhất; kiểm tra trùng ngay khi đăng ký để báo sớm và vẫn ràng buộc lại khi tạo tài khoản sau OTP.
- Q: Cách thay đổi endpoint đăng ký/xác thực? → A: Dùng route mới theo `registrationId` (`POST /auth/register`, `/auth/register/verify-otp`, `/auth/register/resend-otp`, `/auth/register/cancel`) và bỏ `/auth/verify-email`, `/auth/resend-verification` cũ (breaking change có chủ đích).
- Q: Mức độ chi tiết mã lỗi khi xác thực OTP thất bại? → A: Phân biệt rõ, giữ các mã riêng: OTP sai / hết hạn / quá số lần thử (khi đó mã bị chặn) / còn cooldown.
- Q: Phản hồi sau khi xác thực OTP thành công? → A: Chỉ trả về kết quả thành công; client gọi `/auth/login` để lấy phiên (không tự động đăng nhập).
- Q: Nhiều đăng ký tạm cho cùng một email? → A: Mỗi email chỉ có tối đa một đăng ký tạm; đăng ký mới thay thế (ghi đè) bản cũ và vô hiệu mã cũ.

## User Scenarios & Testing *(mandatory)*

<!--
  Source plan: docs/modules/fix-auth.md. That document was written for a different
  product and stack; this feature applies its state machine and guarantees to this
  service. The adaptation is recorded in the Assumptions section.
-->

### User Story 1 - Registration holds no permanent account until email is verified (Priority: P1)

A new visitor submits an email, password, and optional profile details. The service
validates the input, checks that the email is not already registered, stores the
registration **temporarily only**, sends a one-time code to the email, and returns a
registration reference the client uses for the next steps. No account that can sign
in — or that occupies the email — exists until the code is confirmed.

**Why this priority**: This is the core of the change. Today a half-finished
registration already creates a durable account; the product wants the opposite, so
that abandoned or failed sign-ups leave nothing durable behind.

**Independent Test**: Submit a registration and confirm that no permanent account is
created and the email remains available for a fresh registration until the code is
verified. Delivers the deferred-creation guarantee on its own.

**Acceptance Scenarios**:

1. **Given** a visitor with an unused email, **When** they register with valid details,
   **Then** the service returns a registration reference with the code's remaining
   lifetime and the earliest resend time, and no permanent account is created.
2. **Given** a visitor with an unused email, **When** registration succeeds, **Then** a
   one-time code is emailed to that address and the earlier details are still held for
   the remaining verification window.
3. **Given** an email already belonging to a permanent account, **When** a visitor
   registers with it, **Then** registration is rejected with a clear "already
   registered" result and no temporary registration is created for it.
4. **Given** a visitor who never receives or never enters the code, **When** the
   verification window elapses, **Then** the temporary registration disappears on its
   own and nothing durable was created.

---

### User Story 2 - Confirming the code creates the account exactly once (Priority: P1)

The visitor submits the registration reference and the code. If the code is correct,
unexpired, and within the attempt limit, the service creates the account, makes it
usable for sign-in, and only then discards the code and marks the temporary registration
completed. If the account was already created by an earlier attempt whose response was
lost, repeating the same request does not create a second account.

**Why this priority**: Turning a verified registration into exactly one usable
account is the payoff; getting this wrong either blocks legitimate users or forges
duplicates.

**Independent Test**: Register, submit the correct code, then confirm a usable account
exists and that resubmitting the same confirmation does not create a second account
and does not fail the user.

**Acceptance Scenarios**:

1. **Given** a live temporary registration, **When** the correct code is submitted,
   **Then** the account is created and usable for sign-in, the code is discarded, and the
   registration is marked completed (the reference can no longer create another account).
2. **Given** a live temporary registration, **When** an incorrect code is submitted,
   **Then** no account is created, the failure count increases, the temporary
   registration is preserved, and a code-error result is returned.
3. **Given** a correct code whose creation step failed after the response could not be
   delivered, **When** the same confirmation is retried, **Then** the service reports
   success without creating a second account.
4. **Given** the same registration reference, **When** two confirmation requests arrive
   at the same time, **Then** at most one account is created.
5. **Given** a registration already confirmed successfully, **When** the same confirmation
   is repeated, **Then** the service reports success without creating a second account, and
   the reference still cannot be used to create a new account.

---

### User Story 3 - Requesting a fresh code (Priority: P2)

A visitor whose code expired or never arrived requests a new one using the same
registration reference, subject to a short waiting period. The new code invalidates
the previous one and restarts the attempt budget.

**Why this priority**: Without a resend path, an expired code means starting over,
which loses a nearly-complete registration.

**Independent Test**: Register, request a resend before and after the waiting period,
and confirm the too-early request is refused while the eligible request issues a code
that replaces the old one.

**Acceptance Scenarios**:

1. **Given** a live temporary registration within the resend waiting period, **When** a
   resend is requested, **Then** it is refused and the response states the remaining
   wait.
2. **Given** a live temporary registration past the waiting period, **When** a resend is
   requested, **Then** a new code is emailed, the previous code no longer works, and
   the attempt budget is reset.
3. **Given** an unknown, expired, or already-completed registration reference, **When** a
   resend is requested, **Then** the response stays generic and reveals nothing about
   whether the reference ever existed.

---

### User Story 4 - Abandoning or cancelling a registration (Priority: P3)

A visitor who leaves the verification screen is not penalized: their temporary
registration survives until it expires on its own. A visitor who explicitly gives up
can cancel it immediately.

**Why this priority**: The product explicitly wants leaving the screen to be harmless;
explicit cancel is a convenience, not the core flow.

**Independent Test**: Register, leave without verifying, and confirm the temporary data
survives within its window; register again and cancel, confirming the reference is
immediately unusable.

**Acceptance Scenarios**:

1. **Given** a live temporary registration, **When** the visitor closes the page or
   stops interacting, **Then** the temporary registration is retained until it expires
   by its own lifetime.
2. **Given** a live temporary registration, **When** the visitor cancels it, **Then**
   the temporary data is removed and the reference and its code become unusable.
3. **Given** an expired or unknown reference, **When** a cancel is requested, **Then**
   the response is idempotent and reveals nothing about whether it ever existed.

---

### User Story 5 - The flow survives partial failures and abuse (Priority: P2)

When the durable account store is briefly unavailable at the moment a correct code is
confirmed, the registration is not thrown away: the visitor can retry successfully
once the store recovers, still ending with exactly one account. Gradual guessing of the
code is bounded.

**Why this priority**: A registration that is lost to a transient failure is a lost
customer, and an unbounded code endpoint is a brute-force target.

**Independent Test**: Simulate a failure while creating the account after a correct
code, then retry and confirm exactly one account results; submit wrong codes past the
limit and confirm the code is refused until a new one is requested.

**Acceptance Scenarios**:

1. **Given** a correct code and a temporarily failing account store, **When**
   confirmation is attempted, **Then** the temporary registration is preserved and the
   visitor can retry to a single successful account.
2. **Given** a live temporary registration, **When** wrong codes exceed the allowed
   number of attempts, **Then** the code is refused and a new code must be requested.
3. **Given** a successful registration, **When** the visitor signs in, **Then** sign-in
   works and all existing authentication behaviors are unchanged.

---

### Edge Cases

- A visitor submits the same registration twice for the same email in quick succession:
  the newest registration replaces the previous one, only the newest reference and code
  work, and the response does not leak prior state (FR-026).
- The temporary store is unavailable when a registration starts: the request fails
  cleanly and no partial account is created.
- The durable account store is unavailable when a registration starts (email-existence
  check): the request fails cleanly and no temporary registration is left half-written.
- A new code is issued, then the old code is presented: the old code is rejected.
- A code is submitted after its lifetime has elapsed: the code is rejected and a resend
  is required; no account is created.
- A registration reference is guessed or tampered with: it is rejected; references are
  unpredictable and not derived from the email.
- The same email is registered concurrently from two places before either verifies:
  only one account may ultimately be created; the second confirmation must not create a
  duplicate.
- A username becomes taken by another registration between start and confirmation: the
  confirmation returns a conflict, creates no account, and leaves the temporary
  registration until it is cancelled or expires.
- Sign-in, token refresh, logout, password reset, and password change are unaffected by
  the registration rework.
- No password or code value is written to logs or to the temporary store in readable
  form.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: Starting a registration MUST validate the submitted details (email format,
  password policy, optional profile fields) before any temporary or durable state is
  written.
- **FR-002**: The service MUST normalize the email (trim, lowercase) before checking for
  duplicates and MUST treat email uniqueness as case-insensitive.
- **FR-003**: Starting a registration MUST reject an email that already belongs to a
  permanent account, using a result that does not reveal unrelated account details.
- **FR-004**: Starting a registration MUST NOT create a permanent account; it MUST store
  the registration (normalized email, password representation, and the profile fields
  needed to finish) in a temporary store only.
- **FR-005**: The temporary registration MUST carry a reference that is unpredictable
  and independent of the email; the client uses this reference — not a client-supplied
  email — to verify, resend, and cancel.
- **FR-006**: The service MUST generate a one-time code, store only a non-reversible
  representation of it, and email the code to the registered address.
- **FR-007**: The temporary registration and the code MUST each expire on their own
  independent, configurable lifetime; an expired registration or code MUST NOT be
  usable and MUST be discarded without creating an account.
- **FR-008**: Confirming the code MUST require a valid registration reference and a
  matching, unexpired code, and MUST reject the submission when the attempt limit for
  the current code has been exceeded.
- **FR-009**: On a correct, unexpired code within the attempt limit, the service MUST
  create exactly one permanent account with the customer role and a state usable for
  sign-in.
- **FR-010**: The service MUST discard the temporary registration only after the account
  has been durably created; if creation fails, the temporary registration MUST be
  preserved so the confirmation can be retried. Discarding on success means removing the
  code (and the email pointer) and **marking the record completed**; the completed record
  is retained until its own expiry so a repeated confirmation can report success without
  creating a second account (FR-011).
- **FR-011**: Confirming the same registration reference more than once, including
  concurrent requests and retries after a lost response, MUST NOT create a second
  account and MUST report a successful outcome to the visitor. A unique-email violation
  on this insert means the account was already created by this registration, so the
  service MUST look the account up by the registration's email: if it exists, the
  repeated confirmation is a success; if it does not, the violation is a username
  conflict raised by a **different** registration and MUST be reported as a conflict
  (FR-023), never as success.
- **FR-012**: An incorrect code MUST NOT create an account, MUST increase the attempt
  counter for the current code, and MUST preserve the temporary registration until
  either the code or the registration expires or the attempt limit is reached.
- **FR-013**: After the attempt limit is reached for a code, that code MUST be refused
  and a new code MUST be required before further confirmation attempts.
- **FR-014**: The service MUST offer a resend operation keyed by the registration
  reference, refused until a configurable waiting period has elapsed since the last
  send.
- **FR-015**: A resend MUST issue a new code, invalidate the previous code, and reset
  the attempt budget for that registration.
- **FR-016**: Resend and cancel responses MUST NOT reveal whether a given registration
  reference exists.
- **FR-017**: The service MUST offer a cancel operation that removes the temporary
  registration and invalidates its reference and code.
- **FR-018**: Recording, resending, and cancelling MUST be rate-limited per source so
  the code cannot be guessed or the mailer abused.
- **FR-019**: The registration flow MUST NOT store, log, or return plaintext passwords
  or plaintext code values; only non-reversible representations may be persisted.
- **FR-020**: The service MUST record security-relevant registration events (registration
  started, code verified, code failed, code resent, registration cancelled, delivery
  failed) for auditing, without recording secrets.
- **FR-021**: After a successful registration, sign-in and every existing authentication
  behavior (refresh, logout, password reset, password change, role enforcement) MUST
  continue to work unchanged.
- **FR-022**: The flow MUST be exposed as `POST /auth/register`,
  `POST /auth/register/verify-otp`, `POST /auth/register/resend-otp`, and
  `POST /auth/register/cancel`; the former email-keyed `POST /auth/verify-email` and
  `POST /auth/resend-verification` MUST be removed. Every add/change/remove MUST be
  reflected in the service's API contract documentation.
- **FR-023**: The service MUST keep accepting an optional `username` and an optional
  display name; `username` MUST remain unique, checked at registration for early feedback
  and enforced again when the account is created, so a `username` taken between the check
  and creation is rejected as a conflict rather than producing a duplicate. When the
  confirmation detects this conflict it MUST create no account and MUST leave the
  temporary registration in place (it is only removed after a successful creation, FR-010).
- **FR-024**: Confirmation and resend failures MUST be distinguishable to the client so it
  can react correctly: incorrect code, expired code, attempt-limit reached (the code is
  then blocked and a resend is required — one condition, reported with the same code), and
  resend cooldown active MUST each be reported distinctly. However, unknown, expired, or
  cancelled registration references MUST stay generic (FR-016) and MUST NOT reveal whether
  a reference existed.
- **FR-025**: A successful confirmation MUST return only a success result and MUST NOT
  issue a session; the visitor signs in afterwards through the existing sign-in endpoint
  (preserving current behavior and avoiding duplicate-session issues on retry).
- **FR-026**: There MUST be at most one live temporary registration per email; a new
  registration for an email that already has a live temporary registration MUST replace
  it, invalidating the previous reference and code so only the newest can be verified.

### Key Entities *(include if data involved)*

- **Temporary Registration**: A not-yet-verified sign-up held outside the durable
  account store. Holds a registration reference, the normalized email, the password
  representation, the profile fields needed to finish, the state, the code's
  non-reversible representation, the attempt count, and the expiry times for both the
  registration and the code. Disappears on its own at expiry, on successful
  verification, or on explicit cancel.
- **Account**: A permanent, sign-in-capable account. Holds a normalized email unique
  case-insensitively, a password representation, a role (customer or admin), a status,
  and timestamps. Created only when a temporary registration's code is confirmed.
- **Registration Code (OTP)**: A short one-time secret sent to the registered email.
  Stored only as a non-reversible representation with its own lifetime; replaced when
  resent; bounded by an attempt limit.
- **Registration Reference**: An unpredictable identifier that binds the client's
  follow-up requests (verify, resend, cancel) to one temporary registration without
  trusting a client-supplied email.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 100% of tested registrations that are never verified leave no sign-in-capable
  account and free the email for a later registration.
- **SC-002**: 100% of tested confirmations with a correct code produce exactly one usable
  account, including when the same confirmation is replayed.
- **SC-003**: 0% of tested incorrect, expired, or reused codes create an account.
- **SC-004**: At least 95% of visitors who register and then enter a valid, unexpired code
  do so successfully on the first attempt without support intervention.
- **SC-005**: A visitor can request a new code after the waiting period and be blocked
  before it in 100% of tested cases.
- **SC-006**: No plaintext password or code value exists in logs or in the temporary
  store, as verified by reviewing persisted and logged data.
- **SC-007**: Concurrent confirmation of the same registration creates at most one account
  in 100% of tested runs.
- **SC-008**: A temporary registration is retained when the visitor abandons the screen
  and removed only by expiry, successful verification, or explicit cancel, in 100% of
  tested cases.
- **SC-009**: After the change, the existing sign-in, refresh, logout, password reset and
  password change flows continue to pass their current tests.

## Assumptions

- **Source-plan adaptation**: `docs/modules/fix-auth.md` was authored for another product
  (referred to there as "GreenWheel") and a different technology stack. This feature
  applies its state machine and guarantees to **this** service. Where the source names a
  different durable account store, the durable store is this service's primary database;
  where it names a specific validation library, the existing validation mechanism is used.
  These are the only intended deviations.
- **Temporary store**: The temporary registration and code representation live in the
  service's existing shared cache, consistent with how the OTP challenge is held today; the
  cache is treated as ephemeral, so losing it at worst forces the visitor to start
  registration over and never creates a partial account.
- **Endpoint design**: The flow is exposed under the existing auth base (`/api/v1/auth`)
  as `POST /auth/register`, `POST /auth/register/verify-otp`,
  `POST /auth/register/resend-otp`, and `POST /auth/register/cancel`. The current
  email-keyed `POST /auth/verify-email` and `POST /auth/resend-verification` are removed
  (a deliberate breaking change). Because this changes the client-facing contract, a
  per-feature frontend guide is required (Constitution).
- **Starting values** (configurable, not fixed rules): code lifetime 5 minutes;
  temporary-registration lifetime 15–30 minutes; resend waiting period 60 seconds;
  maximum wrong attempts 5 per code. These supersede the previously used 3-attempt
  limit if they differ, and may be tuned later without changing the flow.
- **Delivery**: A transactional email capability exists (already used by the current
  registration flow); sending is best-effort with a defined failure path.
- **Scope**: This feature changes the registration/verification path only. Password reset,
  sign-in, token lifecycle, roles, and account provisioning are out of scope except that
  they MUST keep working (FR-021).
- **Profile fields**: The optional username and display name collected today remain part
  of the temporary registration and are persisted when the account is created; username
  uniqueness is still enforced (see FR-023).
- **Customers vs. admins**: Only customer accounts are created through this flow; the admin
  account continues to be provisioned administratively.
