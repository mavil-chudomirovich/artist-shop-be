# Feature Specification: User Authentication & Session Management

**Feature Branch**: `001-user-auth`

**Created**: 2026-09-18

**Status**: Draft

**Input**: User description: "Tách các feature trong doc thành các module, và thực hiện auth đầu tiên"

## Clarifications

### Session 2026-09-18

- Q: Chính sách mật khẩu? → A: Tối thiểu 8 ký tự, phải có chữ cáa, chữ số và ký tự đặc biệt.
- Q: Cách tạo tài khoản admin? → A: Lệnh CLI seed admin, credential lấy từ biến môi trường (không qua đăng ký công khai).
- Q: Thời hạn token/phiên? → A: JWT access token 15 phút; refresh token 45 ngày.
- Q: Chuẩn hóa email & duy nhất? → A: Trim + lowercase trước khi lưu; duy nhất không phân biệt hoa/thường.
- Q: Phát hiện tái sử dụng refresh token? → A: Từ chối đăng nhập bằng token cũ; không thu hồi toàn bộ họ session.
- Q: Đổi mật khẩu ảnh hưởng token thế nào? → A: Cấp refresh token mới giữ nguyên thời hạn của refresh token cũ; đưa access token cũ vào blacklist.
- Q: Đăng ký có cần xác nhận email? → A: Có; gửi OTP xác nhận tới email đã đăng ký.
- Q: Lưu OTP và access-token blacklist ở đâu? → A: Redis (OTP để đối chiếu; blacklist access token trong Redis).
- Q: Xử lý nhập sai OTP? → A: Sai 3 lần → blacklist OTP; sau 1 phút mới được gửi lại OTP.

### Session 2026-10-06

Hai câu hỏi dưới đây được **thay đổi** so với session 2026-09-18 để khớp với
`docs/api-reference.md` (mục 1.6 và 3.10) — tài liệu authoritative cho API:

- Q: Phát hiện tái sử dụng refresh token? → A: Từ chối yêu cầu **và thu hồi toàn bộ phiên
  của tài khoản**, ghi `audit_logs` (`AUTH_REFRESH_REUSED`). Lý do: refresh token bị
  dùng lại nghĩa là token đã lộ ra ngoài thiết bị chủ sở hữu.
- Q: Đổi mật khẩu ảnh hưởng token thế nào? → A: `POST /password/change` **bắt buộc** có
  `refreshToken` hiện tại (thiếu → `VALIDATION_ERROR` 400); sau đó thu hồi toàn bộ phiên
  của tài khoản, blacklist access token cũ và cấp cặp token mới.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Account registration and sign-in (Priority: P1)

A new visitor creates an account with an email address and password, then signs in
to gain access to customer capabilities (purchases, commissions, order history).
An existing customer signs in with the same credentials.

**Why this priority**: Every account-based capability depends on being able to
create and access an account; without this, no other customer feature works.

**Independent Test**: Create a new account, sign in with those credentials, and
reach an authenticated-only area. Delivers a working customer login on its own.

**Acceptance Scenarios**:

1. **Given** a visitor with an unused email, **When** they register with a valid
   email and a sufficiently strong password, **Then** an account is created in a
   pending state with the customer role, an OTP is emailed to them, and after
   verifying the OTP they can sign in.
2. **Given** an existing account, **When** the user signs in with the correct
   email and password, **Then** they receive an authenticated session.
3. **Given** an existing account, **When** the user signs in with a wrong
   password, **Then** sign-in fails with a generic credential error and the
   failure is recorded for abuse detection.
4. **Given** an email already registered, **When** a visitor tries to register
   with it, **Then** registration is rejected without revealing account details.

---

### User Story 2 - Role-based access control (Priority: P2)

The artist (admin) signs in with an admin account and reaches management
capabilities, while customers are confined to customer capabilities. Attempts by
a customer to reach admin-only capabilities are denied.

**Why this priority**: The platform has exactly one privileged operator; the
boundary between customer and admin must be enforced before any admin feature is
built.

**Independent Test**: Sign in as admin and reach an admin-only capability; sign
in as a customer and confirm the same capability is denied.

**Acceptance Scenarios**:

1. **Given** a valid admin session, **When** the admin requests an admin-only
   capability, **Then** access is granted.
2. **Given** a valid customer session, **When** the customer requests an
   admin-only capability, **Then** access is denied and the attempt is recorded.
3. **Given** a visitor without a session, **When** they request any protected
   capability, **Then** access is denied.

---

### User Story 3 - Persistent session and sign-out (Priority: P3)

A signed-in user stays signed in across visits without re-entering credentials,
can use the account from more than one device, and can explicitly sign out so a
session can no longer be used.

**Why this priority**: Frequent re-login harms conversion on a shop; explicit
sign-out and revocation are required for customer trust and security.

**Independent Test**: Sign in, remain signed in after the access token expires,
then sign out and confirm the session can no longer access protected
capabilities.

**Acceptance Scenarios**:

1. **Given** a signed-in user whose access token has expired, **When**
   they continue using the app, **Then** their session is renewed without asking
   for credentials again.
2. **Given** a signed-in user on two devices, **When** they sign out on one
   device, **Then** only that device's session is invalidated and the other
   continues to work.
3. **Given** a signed-out session, **When** it is reused, **Then** access is
   denied and every session of that account is revoked.

---

### User Story 4 - Password reset (Priority: P4)

A user who forgot their password requests a reset, receives a time-limited reset
link by email, sets a new password, and signs in with it.

**Why this priority**: Important for retention but not required for the first
usable login experience; it can ship after core auth is stable.

**Independent Test**: Request a reset for an existing account, complete the reset
with the emailed link, and sign in with the new password.

**Acceptance Scenarios**:

1. **Given** a registered email, **When** the user requests a password reset,
   **Then** they receive a reset link by email and the response does not reveal
   whether the email exists.
2. **Given** a valid, unused, unexpired reset link, **When** the user submits a
   new password meeting policy, **Then** the password changes and the link can no
   longer be used.
3. **Given** an expired or already-used reset link, **When** the user submits a
   new password, **Then** the request is rejected.
4. **Given** a successful password reset, **Then** previously issued sessions are
   revoked and their access tokens are blacklisted and rejected; a fresh 45-day
   refresh token is issued. The same rule applies to a password change performed
   while signed in.

---

### Edge Cases

- Repeated failed sign-in attempts from the same source: further attempts are
  slowed or blocked for a period.
- Registration or reset requests made repeatedly for the same email: responses
  stay generic and requests are throttled.
- A session token is altered or forged: access is denied; no details leak.
- A password reset is requested while the user is signed in: previously issued
  sessions are invalidated after the reset completes.
- The same account is used concurrently on multiple devices: all remain valid
  until explicitly signed out or globally revoked (password change, password
  reset, or refresh-token replay detection).
- An admin account cannot be created through public registration.
- Email delivery fails during reset: the user can request again after the retry
  window; no account state changes until the new password is submitted.
- The email-confirmation OTP is wrong, expired, or replayed: confirmation fails
  and the account stays pending; after 3 wrong attempts the OTP is blacklisted and
  a fresh OTP can only be requested after a 1-minute cooldown.
- A pending (unconfirmed) account attempts to sign in: sign-in is denied.
- A rotated-out refresh token is reused: the request is denied and every session
  of that account is revoked, because reuse means the token leaked.
- A password change while signed in omits the current refresh token, or supplies a
  revoked token or a token belonging to another account: the request is rejected.
- Access tokens issued before a password change are presented afterwards: they
  are rejected (blacklisted); a new token pair is issued.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: Visitors MUST be able to register an account using an email address
  and a password.
- **FR-002**: The system MUST normalize email addresses (trim and lowercase) and
  enforce that each normalized email maps to exactly one account, case-insensitively.
- **FR-003**: The system MUST require passwords of at least 8 characters
  containing at least one letter, one digit, and one special character, and MUST
  reject weak passwords with actionable guidance.
- **FR-004**: The system MUST verify credentials against a stored, non-reversible
  password representation and MUST never store or log plaintext passwords.
- **FR-005**: On successful authentication, the system MUST issue a JWT access
  token valid for 15 minutes and a renewable refresh token valid for 45 days.
- **FR-006**: The system MUST reject expired, revoked, or blacklisted access
  credentials. Blacklisted access tokens MUST be recorded in the shared cache
  (Redis) and checked on every protected request.
- **FR-007**: The system MUST allow renewing a session using a valid refresh
  token without re-entering the password, and MUST rotate the refresh token on
  renewal. A rotated-out refresh token that is presented again MUST be rejected
  (sign-in denied), MUST revoke every session of that account, and MUST be
  recorded in `audit_logs`.
- **FR-008**: Users MUST be able to sign out, which immediately invalidates the
  refresh token used.
- **FR-009**: The system MUST support multiple concurrent sessions per account,
  and signing out of one MUST NOT affect others.
- **FR-010**: The system MUST provide a password reset flow that emails a
  single-use, time-limited reset link to a registered address.
- **FR-011**: The system MUST respond to registration and reset requests with
  generic messages that do not reveal whether an email is registered.
- **FR-012**: The system MUST assign every new account the customer role by
  default; an admin account MUST NOT be creatable through public registration.
  The single admin account MUST be provisioned by an administrative seed command
  that reads its credentials from the environment.
- **FR-013**: The system MUST protect every non-public capability, granting access
  only to authenticated sessions.
- **FR-014**: The system MUST restrict admin-only capabilities to accounts holding
  the admin role and deny (and record) attempts by non-admin sessions.
- **FR-015**: The system MUST rate-limit authentication, registration, and reset
  requests to slow brute-force and abuse (sign-in 10 requests/minute per source;
  registration, reset, and OTP resend 5 requests/minute per source). Additionally,
  failed sign-in attempts from one source MUST be locked out after 10 failures
  within 15 minutes, the lockout MUST expire automatically, and a successful
  sign-in MUST clear the counter.
- **FR-016**: The system MUST record security-relevant events (sign-in success and
  failure, sign-out, password change, privilege denial) with actor, action, and
  time for auditing.
- **FR-017**: A password change performed while signed in MUST require the current
  refresh token in the request body, MUST revoke every session of the account,
  MUST blacklist previously issued access tokens, and MUST issue a new token pair.
- **FR-018**: Registration MUST NOT grant sign-in capability until the registered
  email is confirmed with a one-time passcode (OTP) sent to that address.
- **FR-019**: The email-confirmation OTP MUST be stored in the shared cache
  (Redis), single-use and time-limited. After 3 failed verification attempts the
  OTP MUST be blacklisted, and a new OTP MUST NOT be resendable until a 1-minute
  cooldown has elapsed.

### Key Entities *(include if data involved)*

- **Account**: A person who can authenticate. Holds a normalized (trimmed,
  lowercased) email unique case-insensitively, a non-reversible password
  representation, a role (customer or admin), a status (pending email
  confirmation, active, or disabled), and creation/update times.
- **Session**: A renewable login session belonging to an account. Holds a
  renewable credential reference, device/context hints, issued/expiry times, a
  revoked flag, and parent/child linkage used for rotation.
- **Password Reset Request**: A pending request to reset an account's password.
  Holds the account reference, a single-use token, expiry, and used/consumed
  state.
- **Email Verification (OTP)**: A pending email-confirmation challenge, stored in
  Redis with a time-to-live. Holds the account reference, a non-reversible OTP
  representation, attempt count, and blacklist state.
- **Revoked Access Token**: An access token invalidated before its natural
  expiry (e.g., after a password change), recorded in Redis until the token's
  original expiry.
- **Role**: The access level of an account: customer (default) or admin
  (privileged, provisioned internally).

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A new visitor can register and reach an authenticated area in under
  2 minutes without assistance.
- **SC-002**: At least 95% of sign-in attempts with correct credentials succeed on
  the first try.
- **SC-003**: A user can complete a password reset and sign in again in under 5
  minutes from the request.
- **SC-004**: No plaintext or reversibly stored password exists in the system, as
  verified by reviewing stored credential data.
- **SC-005**: After sign-out, a reused session can no longer access protected
  capabilities within 5 seconds.
- **SC-006**: Sustained failed sign-in attempts from one source are blocked within
  10 attempts, while legitimate users can still sign in.
- **SC-007**: A customer session never reaches an admin-only capability (100% of
  tested attempts denied and recorded).
- **SC-008**: 100% of tested sign-in attempts for an unconfirmed account are
  denied until the emailed OTP is verified.
- **SC-009**: After a password change, 100% of tested pre-change access tokens
  are rejected and the replacement refresh token retains the original expiry;
  reuse of a rotated-out refresh token is denied without affecting other
  sessions.

## Assumptions

- The platform serves a single artist; exactly one admin account exists and is
  provisioned internally via a CLI seed command using environment-provided
  credentials (not via public registration).
- Email confirmation via OTP is REQUIRED before an account can sign in;
  registration creates a pending account. Email is used for OTP confirmation and
  password reset.
- Authenticating with third-party identity providers (Google, Facebook, etc.) and
  two-factor authentication are out of scope for this feature.
- A transactional email capability is available to deliver reset links (delivery
  mechanism is an implementation concern for planning).
- Sessions are multi-device by default; the access token expires after 15
  minutes and the refresh token after 45 days, after which re-authentication is
  required.
- Standard web security expectations apply (transport encryption, secure
  credential storage, throttling) unless superseded by the constitution.
- A Redis instance is available to the service for OTP storage and the access-token
  blacklist; Redis is treated as ephemeral, so losing it at worst forces OTP
  re-issuance and re-validates at most short-lived access tokens.
