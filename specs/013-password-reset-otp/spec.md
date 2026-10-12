# Feature Specification: Password Reset via Emailed Code

**Feature Branch**: `013-password-reset-otp`

**Created**: 2026-10-12

**Status**: Draft

**Input**: User description: "Chỉnh luồng quên mật khẩu: người dùng nhập email → nhận mã OTP qua email → xác minh OTP → hệ thống cấp reset token riêng → người dùng nhập mật khẩu mới → xác nhận → quay lại trang đăng nhập. Hiện tại backend gửi thẳng reset token qua email và đặt lại xong tự đăng nhập, chưa có bước OTP."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Request a reset code (Priority: P1)

A user who has forgotten their password enters their email address and asks to reset it. The service responds with a generic acknowledgement and, if the address belongs to an account, emails a short one-time code to it. Nothing about whether the address is registered is revealed.

**Why this priority**: Every later step depends on the code being requested and delivered; without it the flow cannot start.

**Independent Test**: Submit an address, confirm a generic acknowledgement is returned and a code arrives by email; submit an unknown address and confirm the acknowledgement is identical and no code is sent.

**Acceptance Scenarios**:

1. **Given** a registered address, **When** the user requests a reset, **Then** a generic acknowledgement is returned and a one-time code is emailed to that address.
2. **Given** an address that is not registered, **When** the user requests a reset, **Then** the same generic acknowledgement is returned and no code is sent.
3. **Given** a request already made recently, **When** the user requests another before the waiting period elapses, **Then** the request is refused until the waiting period passes.

---

### User Story 2 - Verify the code and receive a reset token (Priority: P1)

The user submits the code. When it is correct, unexpired, and within the attempt limit, the service issues a separate, single-use token whose only purpose is to set a new password, and returns it to the client.

**Why this priority**: This is the step the current product lacks; it is what makes the flow code-verified rather than trust-by-email.

**Independent Test**: Request a code, submit the correct code, and confirm a single-use reset token is returned; submit a wrong code and confirm no token is issued.

**Acceptance Scenarios**:

1. **Given** a live reset request, **When** the correct code is submitted, **Then** a single-use reset token is returned together with its lifetime, and the code can no longer be used.
2. **Given** a live reset request, **When** an incorrect code is submitted, **Then** no token is returned, the failure count rises, and the request is preserved for another attempt.
3. **Given** the attempt limit for the current code is reached, **When** another code is submitted, **Then** the code is refused and a new code must be requested.
4. **Given** an expired or unknown code, **When** it is submitted, **Then** the request is refused and a new code must be requested.

---

### User Story 3 - Set the new password and return to sign-in (Priority: P2)

The user submits the reset token with a new password. The service replaces the password, invalidates the token, ends every existing session of the account, and reports success. The user then signs in with the new password. The service does not sign the user in automatically.

**Why this priority**: It completes the flow; without it the verified code has no purpose. The "return to sign-in" behaviour is a deliberate product choice.

**Independent Test**: Complete a reset with a valid token and confirm success is returned without a session, the old password no longer works, the new password signs in, and the token cannot be reused.

**Acceptance Scenarios**:

1. **Given** a valid, unused, unexpired reset token, **When** a new password meeting the policy is submitted, **Then** the password is changed, success is reported without issuing a session, and the token can no longer be used.
2. **Given** a successful reset, **When** any previously issued session is used, **Then** it is rejected.
3. **Given** a successful reset, **When** the user signs in with the new password, **Then** sign-in succeeds; signing in with the old password fails.
4. **Given** an invalid, used, or expired reset token, **When** a new password is submitted, **Then** the request is refused and the password is unchanged.

---

### User Story 4 - Abuse and interruption handling (Priority: P3)

Requests and code submissions are rate-limited per source; closing the page does not leave durable state; a delivery failure is reported without losing the ability to try again.

**Why this priority**: Protects the mailer and makes the flow safe under failure, but the core flow works without it.

**Independent Test**: Exceed the request limit and confirm throttling; force a delivery failure and confirm a clear, retryable result.

**Acceptance Scenarios**:

1. **Given** repeated requests from one source, **When** the limit is exceeded, **Then** further requests are refused until the window passes.
2. **Given** the code cannot be delivered, **When** the request is made, **Then** the failure is reported as a temporary, retryable condition and the waiting period is not consumed.

---

### Edge Cases

- The same address requests a code repeatedly: the most recent code is the only valid one.
- A code is submitted after several other requests issued a newer code: the newer code is required.
- A reset token is presented after a later reset for the same account: it is refused (earlier open requests are invalidated).
- Two resets race for the same account: at most one succeeds; the other is refused.
- The address is registered but the account is disabled: behaviour is treated the same as a valid request (no information leak) — see Assumptions.
- No code is ever requested, yet a token is submitted: refused.
- A password that fails the policy is submitted with a valid token: refused and the token stays usable.
- The reset is completed while signed in on another device: that session is ended.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: A user MUST be able to request a password reset by submitting an email address.
- **FR-002**: The service MUST answer every reset request with the same generic acknowledgement, whether or not the address is registered, and MUST NOT create a reset for an address that has no account.
- **FR-003**: For a registered address, the service MUST email a short one-time code to that address; the email MUST NOT contain the reset token.
- **FR-004**: The code MUST be stored only in a non-reversible form, MUST expire on its own, and MUST be single-active (requesting a new code replaces the previous one).
- **FR-005**: A user MUST be able to submit the email and the code to verify it.
- **FR-006**: On a correct, unexpired code within the attempt limit, the service MUST issue a separate single-use reset token whose only purpose is to set a new password, and return it with its lifetime.
- **FR-007**: An incorrect code MUST NOT issue a token, MUST increase the attempt count for the current code, and MUST preserve the request for another attempt until the limit is reached.
- **FR-008**: After the attempt limit for a code is reached, that code MUST be refused and a new code MUST be requested.
- **FR-009**: An expired, used, or unknown code MUST be refused, and the response MUST NOT reveal whether the address is registered.
- **FR-010**: The reset request (the code and its attempt state) MUST be held in ephemeral shared storage with its own lifetime, separate from any account record, so losing that storage at worst forces the user to request a new code.
- **FR-011**: A user MUST be able to set a new password by submitting a valid reset token and a password that meets the policy.
- **FR-012**: On a successful reset, the service MUST replace the password, mark the reset token used, and end every existing session of the account, and MUST reject previously issued access credentials.
- **FR-013**: A successful reset MUST report only success and MUST NOT issue a signed-in session; the user signs in afterwards through the existing sign-in flow.
- **FR-014**: An invalid, used, or expired reset token MUST be refused and MUST NOT change the password.
- **FR-015**: Requesting a code MUST be refused until a configurable waiting period has elapsed since the last send for that address.
- **FR-016**: Password-reset request and verification endpoints MUST be rate-limited per
  client source (the requesting IP, consistent with the service's existing rate limiting) to
  slow abuse and protect the mailer.
- **FR-017**: The flow MUST NOT store, log, or email plaintext passwords, the plaintext
  reset code, or the plaintext reset token; the code MUST be sent only to the registered
  address (and the reset token is never emailed, per FR-003).
- **FR-018**: The service MUST record security-relevant events for this flow (reset requested, code verified, code failed, reset completed, delivery failed) without recording secrets.
- **FR-019**: The flow MUST expose the request, verify and complete steps under the service's auth API, and the change MUST be reflected in the API documentation. The request step replaces the previous behaviour of emailing a reset token; the complete step no longer issues a session.
- **FR-020**: Existing sign-in, refresh, logout, password change, and registration behaviours MUST continue to work unchanged.

### Key Entities *(include if data involved)*

- **Account**: An existing user able to authenticate; the subject of a reset. Holds a normalized email unique case-insensitively and a non-reversible password representation.
- **Reset Code (OTP)**: A short one-time secret emailed for the reset. Held outside the account record, with a non-reversible representation, an expiry, an attempt count, a block state after too many failures, and a send-cooldown marker. Replaced when a new code is requested. Single-use.
- **Reset Token**: A separate, single-use, time-limited credential issued only after a code is verified. Its sole purpose is to authorize setting a new password; it is not a session and cannot authenticate a request. Invalidated on use, on expiry, and when a newer reset request supersedes it.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A user who can receive email can complete a request → verify → set-new-password flow and sign in with the new password in under 5 minutes without support intervention.
- **SC-002**: 100% of tested reset requests for unregistered addresses return the same acknowledgement as registered ones and send no code.
- **SC-003**: 100% of tested resets with a correct code yield exactly one single-use token; 0% of tested wrong, expired, or limit-exceeded codes yield a token.
- **SC-004**: 0% of tested resets with an invalid, used, or expired token change the password.
- **SC-005**: After a successful reset, 100% of tested previously issued sessions are rejected and a new session can only be obtained by signing in with the new password.
- **SC-006**: No plaintext password or reusable code/token value exists in the stored data or logs, as verified by reviewing persisted and logged data.
- **SC-007**: Sustained reset requests from one source are throttled within the configured limit, while legitimate users can still complete the flow.
- **SC-008**: The existing sign-in, refresh, logout, password-change and registration test suites continue to pass after the change.

## Assumptions

- **Chosen defaults (confirmed with the product owner)**:
  - A successful reset returns success only and does **not** sign the user in; the client returns to the sign-in screen (FR-013).
  - There is **no separate resend endpoint**; requesting again through the same step is the resend, gated by the waiting period (FR-015).
  - The new verification step lives alongside the existing request and complete steps of the auth password-reset API (FR-019).
- **Code policy reuses the registration policy**: code lifetime 5 minutes; a maximum of 5 wrong attempts per code; a 60-second block after too many attempts; a 60-second send waiting period. These are configurable starting values, not fixed rules.
- **Reset-token lifetime**: the reset token issued after verification follows the existing password-reset lifetime used by the service (a configurable window, default 30 minutes).
- **Storage**: codes and attempt state live in the service's ephemeral shared cache (consistent with the registration flow); the reset token follows the service's existing durable reset-request storage. Losing the cache at worst forces a new code and never creates a partial reset.
- **Disabled accounts**: a reset requested for a disabled account is answered the same as any request; whether a disabled account can actually sign in afterwards is outside this feature.
- **Confirm-password**: matching the new password twice is a client-side concern; the service receives one new password and enforces the password policy.
- **Out of scope**: third-party identity providers, two-factor authentication, and changing the registration or sign-in flows.
