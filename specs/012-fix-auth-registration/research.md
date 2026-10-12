# Phase 0 Research: OTP-Gated Registration

The feature spec left no `NEEDS CLARIFICATION` markers; the decisions below resolve the
open design questions the spec's Assumptions deferred, grounded in the current auth
module (`internal/modules/auth`) and the platform (`internal/share`).

## D1 — Where the temporary registration is stored

**Decision**: Store the temporary registration in Redis, keyed by an opaque
`registrationId`, alongside the OTP challenge state.

**Rationale**: A registration pending email confirmation is short-lived, is only read by
its own flow, and is never referenced by other modules; it belongs with the OTP state
already held in `internal/share/cache`. Keeping it out of PostgreSQL makes "no durable
account before verification" structurally true — abandoning the flow leaves nothing in
the account store. Redis is already a platform dependency with a `Cache` wrapper and a
`Client()` escape hatch for hash/Lua operations.

**Alternatives considered**:
- *Staging table in PostgreSQL* — would create durable rows for every aborted sign-up,
  need its own cleanup job, and blur the "no account until verified" guarantee. Rejected.
- *Signed stateless token carrying the profile* — would put the password hash in the
  client's hands and make code invalidation/replacement hard. Rejected.

## D2 — The registration reference

**Decision**: Generate `registrationId` as 32 bytes from `crypto/rand`, encoded
base64url (unpadded). It is independent of the email and stored as the lookup key.

**Rationale**: The client must prove continuity without supplying the email (which it
could forge). A high-entropy random reference is unpredictable and cannot be derived
from the address (FR-005). `crypto/rand` is stdlib, so the generator can live in
`domain/model` (constitution I allows stdlib in `domain`).

**Alternatives considered**:
- *A UUIDv4* — also fine, but a raw 32-byte reference has more entropy per character and
  avoids implying the reference is a database key. Both acceptable; chosen for
  simplicity of "opaque, non-guessable".
- *Email + OTP as the key (current design)* — rejected: it lets anyone drive another
  address's flow and forces the client to resend the email on every call.

## D3 — One live registration per email (FR-026)

**Decision**: Keep a pointer key `auth:reg:email:{email} → registrationId`. On
`Save`, run a small Lua script that reads the pointer, deletes the previous
registration's keys if present, writes the new record and re-points the address key —
all atomically.

**Rationale**: The spec chose "newest replaces previous" so that a refreshed sign-up
invalidates the old code and the email has exactly one live challenge. The
pointer + atomic replace makes this correct under a concurrent double-submit without a
distributed lock.

**Alternatives considered**:
- *Read-then-delete-then-write from the app* — racy; two concurrent registrations can
  both survive or leave orphaned keys. Rejected.
- *Allow many registrations per email and rely on the DB unique constraint* — rejected by
  the spec's clarification (multiple live codes for one address confuse users and widen
  the guessing surface).

## D4 — Verifying by reference, reusing the OTP machinery

**Decision**: Re-key the existing OTP mechanics (hash, attempt counter, block marker,
resend cooldown) from `{email}` to `{registrationId}` and expose them through a single
`RegistrationStore` port alongside the registration record's save/load/delete. The
use case still generates the six-digit code (existing `generateOTP`) and only the store
persists its Argon2id hash.

**Rationale**: The current `OTPStore` already implements exactly the hashed-code,
attempt-limit, block and cooldown semantics the spec reuses (FR-006, FR-008, FR-012,
FR-013, FR-014, FR-015). Re-keying it is less code and less risk than a second
implementation, and keeps `application` free of Redis.

**Alternatives considered**:
- *Store the OTP inside the registration hash with an expiry field* — would require the
  store to compare timestamps and lose the natural per-key TTL. Rejected; separate keys
  with independent TTLs match the spec's "code and registration expire independently"
  (FR-007) more directly.

## D5 — Idempotency and concurrency at account creation (FR-010, FR-011)

**Decision**: On a correct code, `INSERT` the account (`users` has `UNIQUE(email)` and a
unique lower-case `username` index). On a unique violation from the insert, look the
account up by the registration's email: **if it exists**, this is a retry of an
already-successful confirmation — delete the temporary registration and report success.
**If it does not exist**, the violation is the username (a different registration holds
that handle) — report a conflict (`ErrUsernameTaken`) and leave the temporary
registration in place. Any non-unique database error is treated as retryable: leave the
temporary registration (and its code) and return the mapped error.

**Rationale**: Redis and PostgreSQL cannot share a transaction, so the temporary record
must survive until the durable write commits. The unique constraints are the single
serialization point: two concurrent verifications both pass the code check, one insert
wins, the other observes the existing account (email present) and converges to the same
result. Distinguishing an email- from a username-violation by the email lookup keeps the
idempotent replay (FR-011) correct even when the repository reports the username index
first, and keeps a genuine username clash (FR-023) from being misread as success. This is
the approach the source plan calls out ("unique constraint … + idempotency/kiểm tra tài
khoản đã được tạo khi retry").

**Alternatives considered**:
- *Treat every unique violation as idempotent success* — would silently accept a
  username clash as a created account. Rejected.
- *Consume the code atomically before the insert* — loses the code when the insert then
  fails, breaking the required retry. Rejected.
- *A distributed lock around verification* — heavier and still not durable across a
  process restart; the unique constraints are simpler and authoritative.

## D6 — Account status on creation

**Decision**: Create the account with `status = 'active'` and role `CUSTOMER`; do not
route through the `pending` status.

**Rationale**: The account only exists after the email is confirmed, so it is
immediately sign-in-capable (FR-009). Keeping the `pending` state for self-registration
would contradict the new guarantee. The column/constraint and `Account.Activate`
remain for administrative/other use, but registration no longer creates pending rows.
No migration needed.

**Alternatives considered**:
- *Create `pending` then activate in the same request* — a pointless two-step that
  reintroduces a window with a durable non-usable row. Rejected.

## D7 — Anti-enumeration posture on `register`

**Decision**: Starting a registration returns `409 AUTH_EMAIL_TAKEN` (and
`409 AUTH_USERNAME_TAKEN`) when the address or handle is already registered. Follow-up
calls (verify/resend/cancel) stay generic for unknown/expired/cancelled references.

**Rationale**: This matches the current `Register` implementation (it already surfaces
`ErrEmailTaken` via `Users.Create`) and the spec's scenario 3 ("rejected with a clear
'already registered' result"). The existing `docs/api-reference.md` and
`specs/001-user-auth/contracts/README.md` still claim `register` is always a generic
`202`; that claim is stale relative to the code and is superseded here, so
`docs/api-reference.md` is updated in this change and the drift is called out in the
frontend guide.

**Alternatives considered**:
- *Keep a generic `202` for a taken email (pure anti-enumeration)* — would contradict the
  spec's scenario 3 and make the early duplicate check invisible. Rejected for this
  feature; noted as the trade-off.

## D8 — Tunable defaults

**Decision**: Add `PENDING_REGISTRATION_TTL` (default `30m`); change `OTP_TTL` default
to `5m` and `OTP_MAX_ATTEMPTS` default to `5`; keep `OTP_RESEND_COOLDOWN=60s` and
`OTP_BLOCK_TTL=60s`.

**Rationale**: The spec's starting values are 5-minute code, 15–30-minute registration,
60-second resend, 5 attempts, per code. They are configuration, not rules, so they stay
env-driven. Changing the two defaults aligns the default deployment with the spec; both
remain overridable.

**Alternatives considered**:
- *Keep existing defaults (15 m / 3 attempts)* — contradicts the spec's stated starting
  values. Rejected.

## D9 — Endpoints and contract artifacts

**Decision**: Expose `POST /api/v1/auth/register`,
`POST /api/v1/auth/register/verify-otp`, `POST /api/v1/auth/register/resend-otp`,
`POST /api/v1/auth/register/cancel`; remove `/auth/verify-email` and
`/auth/resend-verification`. Update handler Swagger annotations, regenerate
`docs/swagger/`, update `docs/api-reference.md`, and ship `frontend-guide.md`.

**Rationale**: Required by the spec clarification and by Constitution VIII + the
frontend-guide governance rule. The register response changes from a generic message to
`{registrationId, status, otpExpiresIn, resendAvailableIn}`.

**Alternatives considered**:
- *Keep the old paths with a changed payload* — rejected by the spec clarification, which
  chose a clean replacement over two parallel auth flows.

## D10 — Email delivery failure (FR-020 and the 503 path)

**Decision**: Reuse the existing `sendWithRetry`, `classifyDeliveryFailure` and
cooldown-disarm logic. On failure, keep the temporary registration and its code, disarm
the resend cooldown, record `AUTH_REGISTER_DELIVERY_FAILED`, and answer `503`. Parameterise
the disarm/log helpers by `registrationId` instead of the account id.

**Rationale**: The current machinery already implements the retry budget, classification
and "a send that did not happen must not consume the cooldown" rule. Only the identity
used in the diagnostic changes (an account id no longer exists at this point).

**Alternatives considered**: none material — reuse is strictly simpler.

## Resolved unknowns

No `NEEDS CLARIFICATION` items remain in `plan.md`. The Technical Context, Constitution
Check and the decisions above are sufficient to proceed to Phase 1 design.
