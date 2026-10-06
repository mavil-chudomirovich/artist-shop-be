# Implementation Plan: Fix Verification Defects

**Branch**: `004-fix-pending-defects` | **Date**: 2026-10-06 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/004-fix-pending-defects/spec.md`

## Summary

Three defects found while verifying module 02 User against real providers. An oversized
avatar upload is refused with the generic request-too-large reason instead of the
documented image-too-large one, and which of the two the customer gets depends on whether
their client happened to declare a request length. A registration whose verification email
cannot be delivered answers `503`, but the account it created is kept and the customer is
told to request a new code — a promise the resend cooldown currently makes false for sixty
seconds. And an operator who lowers the shared request-size ceiling below what avatars need
gets no warning at all until a customer hits it.

The technical approach is deliberately narrow: three changes in existing files, no new
dependency, no schema change, no new endpoint. Two change what a refusal says, one changes
what a failure leaves behind.

## Technical Context

**Language/Version**: Go 1.26.0

**Primary Dependencies**: Standard library only — `net/http`, `net/smtp`, `log/slog`, `crypto/sha1`.
No new dependency is introduced; `go.mod` and `go.sum` stay untouched.

**Storage**: PostgreSQL 16 (audit trail), Redis 7 (verification codes and their cooldown marker).
No schema change and no migration: this feature writes rows to tables that already exist.

**Testing**: `testing` with `net/http/httptest`; `testcontainers-go` for anything needing
real PostgreSQL or Redis, behind the `integration` build tag.

**Target Platform**: Linux server (Docker), plus local development on Windows via WSL/Docker
Desktop. Production is a single VPS.

**Project Type**: web-service

**Performance Goals**: An avatar upload at or above the ceiling, and a refused one, are both
answered within two seconds of the request reaching the service.

**Constraints**: The avatar ceiling stays at 2 MB and the stored maximum width stays at
512 px — this feature corrects the reason reported, never the limit enforced. The retry
budget for a transient mail failure must fit inside the two-second target rather than being
chosen independently of it.

**Scale/Scope**: Three defects, three changes, all inside code that already exists. Estimated
surface is under ten files; no new module, no new package.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-checked after Phase 1 design.*

- [x] **I. Modular Monolith & Clean Architecture**: No new layer is introduced. The refusal
      reason changes in `presentation`; the retry and the cooldown disarm change in
      `application`; the startup guard lives in the composition root because it compares a
      shared setting against a module-owned ceiling, and no module type may reach
      `share/config` (see Complexity Tracking). `domain` is untouched.
- [x] **II. Transactional Integrity**: Not applicable — no money, inventory or payment stage
      is involved. The account row was already committed before this feature runs; nothing
      here changes that ordering.
- [x] **III. State Machines & Invariants**: The only state transition touched is the
      verification-code marker, and it is *disarmed* on a failed delivery. That is a deliberate
      relaxation of an existing invariant, so it gets both a positive test (a retry after a
      failure succeeds) and a negative test (the flow-level rate limit still refuses a flood).
- [x] **IV. Test-First for Critical Logic**: Both user stories are behavioural contracts and
      both are written as failing tests first — the refusal reason across three request
      shapes, and the registration outcome when delivery fails. Each test must fail for a
      named missing behaviour, not for a compile error.
- [x] **V. Security & Least Privilege**: This feature is security-relevant in the positive
      direction. It stops a wrong reason from being reported to a client, stops a customer
      being stranded by a `503`, and requires that no credential, verification code or
      provider response body reaches a client or a log. Disarming the cooldown opens a
      sending path, so the flow-level rate limit is explicitly retained (FR-025).
- [x] **VI. Observability**: FR-011 adds the audit trace this path was missing — today a
      failed registration creates an account and leaves no audit row at all, which
      Constitution VI does not permit. A classified log line accompanies it so an operator
      can diagnose without querying stored data.
- [x] **VII. Simplicity (YAGNI)**: No queue, no background worker, no retry framework. A
      bounded in-request retry is enough for the transient failures that actually occur; a
      durable sending queue would be new infrastructure for a failure mode the mail provider
      does not exhibit. Recorded as a rejected alternative in research.md.
- [x] **VIII. API Documentation as a Contract**: The documentation and the OpenAPI already
      promise the image-too-large reason, so this feature changes the code to match the
      contract rather than weakening the contract. `docs/api-reference.md` and the module
      contract must be updated in the same change for the new `503` on registration and for
      the startup refusal, since both alter what an operator and a client observe.

**Post-design re-check**: all eight still hold. The startup guard's location was re-examined
after Phase 1 and remains justified — see Complexity Tracking.

## Project Structure

### Documentation (this feature)

```text
specs/004-fix-pending-defects/
├── plan.md              # This file
├── spec.md              # Feature specification
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/           # Phase 1 output
├── deferred.md          # Deliberate omissions carried in from feature 003
└── checklists/
    └── requirements.md  # Spec quality checklist (16/16)
```

### Source Code (repository root)

```text
internal/
├── share/
│   ├── httpserver/routes.go          # unchanged: keeps the coarse shared ceiling only
│   ├── middleware/bodylimit.go       # refusal reason for the avatar route
│   └── config/config.go              # unchanged; MAX_BODY_BYTES already exists
└── modules/
    ├── auth/
    │   ├── application/implement/    # retry, cooldown disarm, 503 on delivery failure
    │   ├── infrastructure/implement/redis/otp.go  # disarm the cooldown marker
    │   └── presentation/http/        # map the delivery failure to 503
    └── user/
        ├── presentation/http/handler.go  # avatar refusal reason
        └── presentation/http/router.go   # avatar route body limit
cmd/api/main.go                       # composition: startup guard
docs/
├── api-reference.md                  # 503 on registration, avatar 413 reason
├── configuration.md                  # the ceiling coupling
└── decisions/010-*.md                # startup guard and refusal-reason decisions
```

**Structure Decision**: This feature adds no directory. Every change lands in a file that
already exists, because all three defects are in behaviour that was delivered and is wrong.
The only new file is an ADR, which the constitution requires for a decision of this kind.
The startup guard belongs in `cmd/api/main.go` rather than in `share/config` because it
compares a shared setting against a module-owned constant, and the dependency may not point
that way.

## Complexity Tracking

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| The startup guard lives in the composition root (`cmd/api/main.go`) rather than in `share/config` where every other setting is validated | The rule compares a shared setting against a constant owned by module User. Constitution I forbids `share/config` from importing a module's types, so a cross-check inside `share/config` would either violate the layering rule or copy the `2 MB` figure into the shared layer and create a second place to keep in sync | Validating inside `share/config` against a duplicated literal is cheaper to write and is the trap this feature exists to close: two copies of `2 MB` drift, and the guard then silently protects the wrong number. Putting the check in the composition keeps one owner of the number. The cost is real and accepted: one-off commands (`migrate`, `seed`) that never build the API composition are not covered by the guard — which is correct, because they do not serve avatar uploads |
| The avatar route carries its own body-limit ceiling that duplicates part of the handler's ceiling arithmetic | The shared middleware refuses on a *declared* length before the handler reads anything, so without a route-level ceiling the customer gets the generic reason. Keeping the ceiling on the route and only fixing the *reason* it reports is the smallest change that satisfies FR-001 | Removing the route-level ceiling and letting the handler's read ceiling do all the work would refuse late, after buffering part of the upload, and would leave the declared-length case unbounded until the read completes. The duplication is confined to one route and is asserted by a test so the two cannot drift |
