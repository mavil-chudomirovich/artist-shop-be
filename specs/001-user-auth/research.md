# Phase 0 Research: User Authentication & Session Management

All technical unknowns from the plan's Technical Context are resolved here. Each
decision records the choice, rationale, and alternatives considered.

## 1. Password hashing

- **Decision**: Argon2id via `golang.org/x/crypto/argon2` with per-password random
  salt; parameters `memory = 64 MiB`, `iterations = 1`, `parallelism = 4`,
  `keyLen = 32`; store a self-describing encoded string (`$argon2id$v=19$m=...,t=...,p=...$salt$hash`).
- **Rationale**: Argon2id is required by the constitution; the chosen cost keeps a
  single hash well under ~250 ms on target hardware while remaining memory-hard.
  The encoded string carries its parameters so they can be tuned later without
  breaking existing hashes.
- **Alternatives considered**: bcrypt (allowed fallback, weaker against GPU/ASIC);
  scrypt (fine but Argon2id is preferred); PBKDF2 (not memory-hard).

## 2. Access token format

- **Decision**: JWT signed with HS256; claims `sub` (account id), `role`, `jti`,
  `iat`, `exp` (15 minutes), `iss`. Secret from `JWT_SECRET` (≥ 32 bytes,
  validated at startup).
- **Rationale**: Stateless verification keeps protected endpoints fast and simple;
  HS256 with a strong shared secret is sufficient for a single-service monolith and
  avoids key-distribution complexity. `jti` enables targeted revocation.
- **Alternatives considered**: RS256 (public-key verification, unnecessary without
  multiple services); opaque access tokens (every request hits the DB).

## 3. Refresh token model

- **Decision**: Opaque random token (32 bytes, base64url), stored only as a
  SHA-256 hash in `sessions`; 45-day expiry; rotated on every refresh (new row,
  old row marked revoked and linked via `rotated_from`). Presenting a rotated-out
  token is rejected and does **not** revoke the family (per clarification).
- **Rationale**: Opaque tokens cannot be forged or introspected; hashing at rest
  limits damage from a database leak; rotation with reuse rejection is the standard
  defense. Storing the hash (not the token) is mandatory since a DB leak must not
  grant sessions.
- **Alternatives considered**: JWT refresh tokens (hard to revoke server-side);
  non-rotating refresh tokens (weaker); token families with full revocation on
  reuse (rejected by the clarification).

## 4. Access-token blacklist (Redis)

- **Decision**: Access-token revocation is kept in Redis: `auth:blacklist:jti:{jti}`
  (TTL = remaining token life) for targeted revocation, and
  `auth:blacklist:user:{userID}` = "minimum valid `iat`" (TTL = access-token TTL)
  for bulk revocation on password change/reset. Access-token validation rejects a
  token whose `jti` is blacklisted or whose `iat` precedes the user's minimum.
- **Rationale**: The blacklist is inherently ephemeral (entries only matter until
  the token would expire anyway), which fits Redis TTLs perfectly and keeps
  protected requests fast. The user explicitly required Redis for the blacklist.
- **Alternatives considered**: a PostgreSQL `revoked_access_tokens` table
  (durable but grows with every revocation and adds a DB read per request);
  DB `credentials_valid_after` cutoff (viable, but the user asked for Redis);
  no blacklist (fails FR-006/FR-017).
- **Accepted trade-off**: Redis is treated as ephemeral; if it is flushed, at most
  short-lived (≤15 min) access tokens could become valid again. Durable
  session/refresh state remains in PostgreSQL.

## 5. Email confirmation OTP (Redis)

- **Decision**: 6-digit numeric OTP stored in Redis with keys
  `auth:otp:{email}` (value: Argon2id hash of the OTP, TTL = `OTP_TTL`),
  `auth:otp:attempts:{email}` (counter), `auth:otp:blocked:{email}` (set when
  attempts ≥ 3, TTL = 1 minute), and `auth:otp:sent:{email}` (resend cooldown,
  TTL = 1 minute). After 3 failed attempts the OTP is deleted and blacklisted, and
  a new OTP cannot be sent until the 1-minute cooldown elapses. Registration
  creates the account as `pending`; a correct OTP deletes the key and activates it.
- **Rationale**: The user required OTP storage/verification in Redis. Per-key TTLs
  make expiry and cooldown trivial and avoid DB rows for short-lived challenges.
  Hashing the stored OTP protects it if Redis is exposed.
- **Alternatives considered**: a PostgreSQL `email_verifications` table (durable
  but unnecessary for short-lived OTPs); storing the OTP in plaintext (rejected);
  magic links (user chose OTP).

## 6. Password reset flow

- **Decision**: Authenticated-less flow: request by email → single-use token
  (32 random bytes, SHA-256 at rest, 30-minute TTL) emailed as a link → submit new
  password → revoke all sessions, set `credentials_valid_after`, and issue a
  replacement refresh/access token pair. Responses are generic regardless of
  whether the email exists.
- **Rationale**: Matches the spec (single-use, time-limited link; existing sessions
  invalidated). Auto-issuing tokens avoids an extra sign-in step after a successful
  reset.
- **Alternatives considered**: OTP for reset as well (kept OTP for registration
  only, per the user's wording); no auto-issue (requires immediate re-login).

## 7. Email delivery abstraction

- **Decision**: An `EmailSender` interface with two implementations: an SMTP sender
  configured by `SMTP_HOST/PORT/USERNAME/PASSWORD/FROM` and a log-only sender used
  when SMTP is not configured (local/dev). Tests use an in-memory fake.
- **Rationale**: Keeps a real vendor out of the MVP while making OTP/reset testable
  and deployable. The provider can be swapped behind the interface later.
- **Alternatives considered**: hard-coupling to a provider SDK (vendor lock-in);
  transactional outbox (overkill for a single low-volume service).

## 8. Data access: hand-written SQL over pgx

- **Decision**: Repository methods with explicit SQL over the platform
  `database.Querier`, no code generation for this module.
- **Rationale**: The auth query set is small and stable; avoiding a codegen
  toolchain keeps the build simple (YAGNI). The foundation already provides pooled
  connections and transactions.
- **Alternatives considered**: `sqlc` (planned for later, heavier query volume);
  `GORM` (implicit behavior, harder to reason about transactions).

## 9. Role model and admin provisioning

- **Decision**: `role` is one of `CUSTOMER` (default) or `ADMIN`. A new
  `cmd/seed` command reads `ADMIN_EMAIL`/`ADMIN_PASSWORD`, validates the password
  policy, and upserts a single `ADMIN` account with status `active`. Public
  registration can never set the role.
- **Rationale**: Matches the clarification and keeps the privileged account out of
  any public path.
- **Alternatives considered**: seeding via migration (couples credentials to
  schema history); first-user-becomes-admin (unsafe).

## 10. Rate limiting and lockout for authentication

- **Decision**: Reuse the foundation's in-process limiter with auth-specific
  thresholds (sign-in 10/minute, registration/reset/OTP-resend 5/minute per
  source). Additionally implement a failed-attempt lockout in Redis:
  `auth:login:failures:{source}` (counter, TTL 15 minutes) and
  `auth:login:blocked:{source}` (set at 10 failures, TTL 15 minutes). Sign-in
  returns `AUTH_LOGIN_LOCKED` while blocked; a successful sign-in deletes both
  keys.
- **Rationale**: Rate limiting smooths traffic; a failure-counter lockout is what
  SC-006 actually requires ("blocked within 10 attempts"). Redis TTLs make the
  counter and lockout expire automatically, and the user already required Redis.
- **Alternatives considered**: rate limiting alone (does not satisfy SC-006);
  per-account-only lockout (allows one source to lock another user's account).

## 11. Authorization integration

- **Decision**: The module implements `middleware.AuthHooks` (verify access token →
  `Identity{Subject, Role}`) and mounts `middleware.RequireAuthentication` /
  `RequireRole` as needed. Privilege denials are emitted as audit events.
- **Rationale**: Uses the foundation's pipeline and satisfies FR-013/FR-014.
- **Alternatives considered**: a separate auth middleware (duplicates the
  foundation).

## 12. Redis integration

- **Decision**: Add a platform package `internal/share/cache` that wraps
  `github.com/redis/go-redis/v9` (connection from `REDIS_ADDR`/`REDIS_PASSWORD`/
  `REDIS_DB`, pinged at startup). The auth module uses it through small typed
  stores (OTP store, blacklist store) with key prefixes under `auth:`.
- **Rationale**: Redis is cross-cutting infrastructure, so it belongs in the
  platform alongside the database. Keeping key construction in typed stores avoids
  stringly-typed key sprawl and makes TTLs explicit.
- **Alternatives considered**: putting the client inside the auth module (would
  duplicate when more modules need it); a generic cache abstraction with multiple
  backends (YAGNI — only Redis is needed).

## 13. Audit events

- **Decision**: Emit `audit.Event`s via the foundation `audit.Emitter` for sign-in
  success/failure, sign-out, email verification, password change/reset, and
  privilege denial, with actor, action, outcome, target, and correlation ID.
- **Rationale**: FR-016 and the constitution's observability principle; reuses the
  append-only async writer.
- **Alternatives considered**: logging only (not durable/queryable as required).
