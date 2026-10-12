# Phase 0 Research: Password Reset via Emailed Code

The spec left no `NEEDS CLARIFICATION` markers; the decisions below resolve the design
questions the spec's Assumptions deferred, grounded in the current auth module
(`internal/modules/auth`).

## D1 — Where the reset code is stored

**Decision**: A new, focused Redis store keyed by normalized email:
`auth:reset:otp:{email}` (Argon2id hash of the 6-digit code), `auth:reset:otp:attempts:{email}`,
`auth:reset:otp:blocked:{email}`, `auth:reset:otp:sent:{email}` (cooldown), each with its
own TTL. Exposed as the `PasswordResetOTPStore` port; implemented in
`infrastructure/implement/redis/reset_otp.go`.

**Rationale**: The code challenge is short-lived, is only read by this flow, and belongs
with the registration flow's code state in `internal/share/cache`. Keying by email (not a
registration reference) matches the input the user supplies. Keeping it out of PostgreSQL
means losing Redis at worst forces a new code and never creates a partial reset (FR-010).

**Alternatives considered**:
- *Add code columns to `password_reset_requests`* — would put ephemeral brute-force state in
  the durable table, need a migration and extra writes, and mix two lifetimes in one row.
  Rejected.
- *Reuse `RegistrationStore`* — it is keyed by an opaque registration reference and holds a
  pending account record that has no meaning here. Rejected (the product owner chose a
  separate store to avoid disturbing the just-shipped registration flow).

## D2 — The reset token keeps its existing mechanism

**Decision**: The post-verification "reset token" is the existing opaque,
single-use `password_reset_requests` token: 32 random bytes, stored only as a SHA-256 hash,
with `PASSWORD_RESET_TTL` lifetime, invalidated on use and when a newer request supersedes
it. Only its **issuance point** moves: from `forgot` (which emailed it) to
`verify-otp` (which returns it).

**Rationale**: The token already satisfies "a separate credential, single-use, only for
resetting a password". Reusing the repository (`ResetRepository`) and table means no
migration and no new lifecycle. `Create` already invalidates open requests for the account,
which gives the "newer request supersedes older token" behaviour for free (edge case).

**Alternatives considered**:
- *A second token type in Redis* — would duplicate durability and invalidation rules for a
  security-critical credential. Rejected.

## D3 — Verification exchanges the code for the token

**Decision**: `VerifyResetOTP(ctx, {Email, OTP}) (ResetTokenOutput, error)` normalizes the
email, verifies the code through the store (attempt/block/expiry rules), looks up the
account, and in one transaction creates a `password_reset_requests` row for that account;
then invalidates the code and returns `{ResetToken, ExpiresIn}`. The token is never emailed.

**Rationale**: This is the step the product lacks. Ordering create-token-then-invalidate-code
means a transient failure after token creation can be retried with the same code, mirroring
the registration flow's "consume only after the durable write" rule.

**Alternatives considered**:
- *Invalidate the code first, then create the token* — a failure between them would strand
  the user with no code and no token. Rejected.
- *Return no token and let `reset` re-verify the code* — contradicts the product owner's
  explicit "issue a separate reset token" step, and the user's email would have to carry the
  code to the final screen. Rejected.

## D4 — Reset reports success without a session

**Decision**: `ResetPassword(ctx, {Token, NewPassword})` keeps validating the token, changing
the password, consuming the token, revoking every session and blacklisting prior access
tokens — but returns `error` (success/failure) instead of a `SessionOutput`. The signature
changes from `(dto.SessionOutput, error)` to `error`.

**Rationale**: The product owner chose "return to sign-in" (spec Assumptions). Ending every
session remains a security requirement (FR-012); the user simply signs in again with the new
password. Not issuing a session also avoids the complexity of deciding what to do with a
session created inside the reset transaction.

**Alternatives considered**:
- *Keep auto-login* — contradicts the requested flow. Rejected.
- *Stop revoking sessions* — a password reset must evict an attacker holding a live session.
  Rejected.

## D5 — No separate resend endpoint

**Decision**: Repeated requests go through `POST /password/forgot`, which is refused until
`OTP_RESEND_COOLDOWN` has elapsed since the last send for that address (the same store
cooldown the registration flow uses). No `/password/forgot/resend-otp` endpoint is added.

**Rationale**: The product owner chose the minimal surface. The cooldown bounds abuse, and
FR-015 requires it. Note: `AUTH_RESEND_COOLDOWN` was removed from `/password/forgot` in a
2026-10-06 change (when the endpoint stopped issuing an OTP); this feature deliberately
**re-introduces** it now that the endpoint sends a code again, and `docs/api-reference.md` is
updated to reflect the new behaviour (D12/T028).

**Alternatives considered**: a dedicated resend endpoint — rejected as unnecessary surface
(spec Assumptions).

## D6 — Reuse the registration code policy and the reset lifetime

**Decision**: Code lifetime `OTP_TTL`, max attempts `OTP_MAX_ATTEMPTS`, block
`OTP_BLOCK_TTL`, resend cooldown `OTP_RESEND_COOLDOWN`; reset-token lifetime
`PASSWORD_RESET_TTL`. All already exist in `AuthConfig`; no new config keys.

**Rationale**: One code policy for the whole service is simpler to reason about and already
tuned (5 min / 5 attempts / 60 s / 60 s).

**Alternatives considered**: separate reset-specific TTLs — rejected (YAGNI; the values are
configurable if a need appears).

## D7 — Anti-enumeration posture

**Decision**: The request step always returns the same generic `202` for registered and
unregistered addresses and only sends a code when an account exists. Verify failures return
code errors (`AUTH_OTP_INVALID` / `AUTH_OTP_EXPIRED` / `AUTH_OTP_TOO_MANY_ATTEMPTS`) that do
not name the address; a request within the cooldown returns `AUTH_RESEND_COOLDOWN` only when
that address actually made a recent request (a first probe of an arbitrary address is
indistinguishable from a registered one).

**Rationale**: Matches the spec's FR-002/FR-009 and the existing auth module's generic
responses. Keeping `AUTH_RESEND_COOLDOWN` explicit preserves the documented `/password/forgot`
contract.

**Alternatives considered**: swallowing the cooldown to return `202` uniformly — rejected as
it makes the cooldown invisible to legitimate clients with no meaningful security gain (a
first probe already reveals nothing).

## D8 — Delivery failure handling

**Decision**: Reuse `sendWithRetry` and `classifyDeliveryFailure`. On a failure that cannot
be retried away, `forgot` disarms the send cooldown for the address (so the user is not
stranded), records `AuditPasswordResetDeliveryFailed`, and returns a temporary `503`
(`ErrVerificationDeliveryFailed` reused).

**Rationale**: The registration flow already implements the retry budget, classification and
"a send that did not happen must not consume the cooldown" rule; reusing it keeps one delivery
policy. The account-less nature of `forgot` means the failure path is even simpler than the
registration one (there is no durable write to reconcile).

**Alternatives considered**: a reset-specific delivery error type — only worth it if the
client must distinguish it; the reused `503` already tells the client "nothing was sent,
try again". Revisit if a distinct code is wanted.

## D9 — Interface signature change

**Decision**: `AuthService.ResetPassword` returns `error`; `AuthService.VerifyResetOTP` is
added. `ForgotPassword` keeps its `error` signature.

**Rationale**: Required by D4/D3. The change is contained in the auth module (interface,
use case, handler, tests).

**Alternatives considered**: keeping the old signature and adding a new method — would leave
a dead session-returning path. Rejected.

## D10 — Disabled accounts

**Decision**: A reset request for a disabled account is answered exactly like any other and
may issue a code; whether a disabled account can sign in after a reset remains governed by
the existing sign-in rule. This feature does not change account-status logic.

**Rationale**: Keeps the request non-enumerating and avoids scope creep into account status.

**Alternatives considered**: treating disabled accounts as unknown — would need status
inspection at request time with no clear product requirement. Rejected.

## D11 — Races and superseding

**Decision**: Make both invariants explicit at the storage layer, without a migration:
- **At most one open token per account.** `ResetRepository.Create` first locks the account
  row (`SELECT id FROM users WHERE id = $1 FOR UPDATE`) inside the caller's transaction, then
  runs its invalidate-open + insert. The lock serializes concurrent `verify-otp` calls for the
  same account, so the second sees the first's committed token, invalidates it and inserts its
  own — "newer supersedes older". (Without the lock, under READ COMMITTED two inserts can both
  land as open.)
- **A token is consumed once.** `ResetRepository.Consume` becomes conditional
  (`UPDATE ... SET used_at = now() WHERE id = $1 AND used_at IS NULL`) and reports
  `domainerr.ErrResetInvalid` when no row is updated, so two concurrent resets on the same
  token cannot both succeed; the loser's whole transaction rolls back.

**Rationale**: The durable table does **not** serialize these on its own (the earlier claim was
wrong); concurrent `verify-otp`/`reset` calls can otherwise produce two open tokens or a
double consume. A per-account row lock plus a conditional consume enforce the spec's edge case
("two resets race → at most one succeeds") using the existing table, so no schema change is
needed.

**Alternatives considered**:
- *A partial unique index `(user_id) WHERE used_at IS NULL`* — the strongest storage-level
  guarantee, but it needs a migration and would abort the second concurrent insert with a
  unique violation instead of superseding it. Kept as a future hardening if a migration is
  ever in scope.
- *A Redis lock* — the account row lock already exists and is transactional; a Redis lock would
  add a second failure domain. Rejected.

## D12 — Endpoints and contract artifacts

**Decision**: `POST /api/v1/auth/password/forgot` (changed: sends a code),
`POST /api/v1/auth/password/forgot/verify-otp` (new), `POST /api/v1/auth/password/reset`
(changed: no session). Update handler Swagger annotations, regenerate `docs/swagger/`,
update `docs/api-reference.md` §3.8/§3.9 + summary + changelog, and ship `frontend-guide.md`.

**Rationale**: Required by the spec's FR-019 and Constitution VIII + the frontend-guide rule.

**Alternatives considered**: `/password/verify-otp` or `/password/reset/verify-otp` — the
product owner chose the `/password/forgot/verify-otp` grouping for consistency with
`/register/verify-otp`.

## Resolved unknowns

No `NEEDS CLARIFICATION` items remain in `plan.md`; the Technical Context, Constitution Check
and the decisions above are sufficient to proceed to Phase 1 design.
