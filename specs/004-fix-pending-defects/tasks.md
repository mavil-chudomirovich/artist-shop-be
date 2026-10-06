---

description: "Task list for feature 004-fix-pending-defects"
---

# Tasks: Fix Verification Defects

**Input**: Design documents from `specs/004-fix-pending-defects/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/contract-changes.md

**Tests**: Tests are included because the constitution mandates test-first coverage for state
transitions, and every defect in this feature is a behavioural contract. Each test task is
written first and must FAIL before its implementation task runs.

**Organization**: Tasks are grouped by user story. The three stories touch disjoint files and
can be implemented in any order — that independence is a property of this feature, not an
accident, and it is the reason the tasks are split this way.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (US1, US2, US3)
- Include exact file paths in descriptions

## Path Conventions

This is a Go web service inside a modular monolith. Paths follow `plan.md`:

- `internal/modules/user/` and `internal/modules/auth/` for the two modules this feature touches
- `internal/share/` for the foundation, including the middleware whose refusal reason changes
- `cmd/api/main.go` for composition
- `docs/` for the documentation that must change with the code (Constitution VIII)

---

## Phase 1: Setup (Baseline)

**Purpose**: Confirm the defects are real before changing anything. This feature adds no
directory and no dependency, so Setup is verification rather than construction.

- [x] T001 Reproduce all three defects against a running stack following `quickstart.md` scenarios 1a, 2a and 4a, and record the observed status and error code for each. A defect that does not reproduce is a different problem and must be reported before any code changes
- [x] T002 [P] Confirm the baseline is green: `make lint`, `make test` and `make test-integration` all pass, so a later failure is attributable to this feature
- [x] T003 [P] Verify no file in the feature's scope is left with CRLF endings or a BOM, per `.editorconfig` and `docs/development/code-hygiene.md`

**Checkpoint**: The three defects are reproduced and recorded; the tree is green.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The two pieces every story depends on — the shared body-limit middleware's ability
to report a caller-supplied reason, and the audit constant the new failure trace needs.

**⚠️ CRITICAL**: T004 and T005 must be complete before any user story task starts.

- [x] T004 Change `middleware.BodyLimit` (FR-001, FR-003) in `internal/share/middleware/bodylimit.go` so the caller can supply the error code its refusal reports, keeping the generic request-too-large code as the default so every existing caller is unchanged. Per `docs/development/code-hygiene.md` no behaviour change ships without a test
- [x] T005 [P] Add the failed-delivery audit constant (FR-011) next to `AuditRegister` in `internal/modules/auth/domain/constant/audit.go`, reusing the existing `OutcomeFailure` value rather than adding a new outcome

**Checkpoint**: The shared middleware can speak a caller's reason, and the new audit action is
declared. US1, US2 and US3 can now proceed independently.

---

## Phase 3: User Story 1 - A customer learns why their photo was refused (Priority: P1) 🎯 MVP

**Goal**: An oversized avatar upload answers the documented image-too-large reason whether or
not the client declared a request length, and the profile is untouched either way.

**Independent Test**: Upload an oversized image three ways — declaring its length, omitting it,
understating it — and confirm all three answer `413 USER_AVATAR_TOO_LARGE` and never
`PAYLOAD_TOO_LARGE`, while a JSON request over the shared ceiling still answers the generic
reason. `quickstart.md` scenarios 1–3.

### Tests for User Story 1 ⚠️

> **NOTE: Write these tests FIRST, ensure they FAIL before implementation.** A test that passes
> before the fix proves nothing — the defect already produces a `413`, so the test must assert
> the **code**, not the status.

- [x] T006 [P] [US1] Avatar refusal tests, covering FR-001, FR-003 and FR-005, in `internal/modules/user/presentation/http/avatar_test.go`: a declared length over the route ceiling, an undeclared length, an understated length, an image exactly at the ceiling accepted, and a non-avatar JSON request over the shared ceiling still answering the generic code
- [x] T007 [P] [US1] Router-level test in `internal/modules/user/presentation/http/router_test.go`, covering FR-005, asserting the avatar route's refusal code, so the shared middleware's new parameter is pinned at the route that uses it

### Implementation for User Story 1

- [x] T008 [US1] Apply the T004 parameter (FR-001, FR-003) to the avatar route in `internal/modules/user/presentation/http/router.go`, passing the existing avatar-too-large code so a declared-length refusal names the image, not the request
- [x] T009 [US1] Verify FR-002 and FR-004 in `internal/modules/user/presentation/http/handler.go` that the handler's read ceiling already reports the same code for the other two request shapes, and change nothing if it does — `readAvatarFile` already funnels every ceiling path through one helper, so this task is a confirmation, not an edit

**Checkpoint**: User Story 1 is fully functional and independently testable. This story alone
is a valid MVP: it removes the confusing answer without touching anything else.

---

## Phase 4: User Story 2 - A customer can finish registering even when mail is down (Priority: P1)

**Goal**: A failed verification message answers `503` with the customer's next step, leaves an
audit trace, and does not strand the customer behind a cooldown they never asked for.

**Independent Test**: Make delivery fail, register, then confirm the `503`, the audit row, the
classified log line, and that requesting a new code immediately is **not** refused by the
cooldown. `quickstart.md` scenarios 4–6.

### Tests for User Story 2 ⚠️

> **NOTE: Write these tests FIRST.** The cooldown assertion is the one that catches a
> half-finished fix: without it, every other test passes while the customer is still stranded.

- [x] T008a [P] [US2] Registration failure tests, covering FR-006, FR-007, FR-008 and FR-010, in `internal/modules/auth/presentation/http/http_test.go`: delivery refused answers `503` with a message naming the next step and no provider detail; the address is not disclosed; a successful delivery still answers the same generic accepted answer
- [x] T009a [P] [US2] Use-case tests, covering FR-015, FR-016 and FR-017, in `internal/modules/auth/application/implement/auth_test.go` for the retry rule: a configuration-classified failure is not retried, a transient one is retried within the budget, the budget fits inside the response target, and a successful retry leaves exactly one verification message
- [x] T010a [P] [US2] Audit and log tests, covering FR-009, FR-011, FR-012 and FR-022, in `internal/modules/auth/application/implement/auth_test.go` and `internal/modules/auth/presentation/http/http_test.go`: the failed registration writes an audit entry with a failure outcome identifying the account, and neither the audit row nor the log line contains the verification code, a credential or the provider's wording
- [x] T011a [P] [US2] Cooldown test, covering FR-018, FR-024 and FR-025, in `internal/modules/auth/infrastructure/implement/redis/otp_test.go`: a failed delivery leaves the resend marker disarmed while the code itself and its lifetime are untouched, and a code issued for the failed attempt is no longer accepted once a newer one replaces it

### Implementation for User Story 2

- [x] T012a [US2] Expose a cooldown disarm (FR-024) on the OTP port in `internal/modules/auth/application/interface/ports.go` and implement it in `internal/modules/auth/infrastructure/implement/redis/otp.go`, deleting only the resend marker and leaving the code, the attempt counter and the block marker alone
- [x] T013a [US2] Classify a delivery failure (FR-015, FR-017, FR-018) in `internal/modules/auth/application/implement/email_failure.go` (new file) — on the provider's status category, never its message text — and retry transient failures within a bounded budget, sizing the budget and its waits to fit the documented response target
- [x] T014a [US2] Disarm the cooldown and record (FR-011, FR-024) the T005 audit trace with the failure classification when delivery fails, in `internal/modules/auth/application/implement/register.go`, and stop recording the success outcome for a registration that did not deliver
- [x] T015a [US2] Map a delivery failure to `503` (FR-006) with the customer-facing next step in `internal/modules/auth/presentation/http/errors.go`, reusing the existing shared service-unavailable code and adding no new client-facing code

**Checkpoint**: User Stories 1 AND 2 work independently. Neither depends on the other.

---

## Phase 5: User Story 3 - An operator finds a misconfiguration before a customer does (Priority: P2)

**Goal**: A shared request ceiling below what avatars need stops the service at startup with a
message naming both values, while a deployment without media starts normally.

**Independent Test**: Start with media configured and a ceiling below the avatar route's; the
service refuses to start naming the setting and both values. Start with no media and any
ceiling; the service starts. `quickstart.md` scenario 7.

### Tests for User Story 3 ⚠️

> **NOTE: Write these tests FIRST.** The equal-boundary case matters as much as the failing
> one: a guard that refuses an equal ceiling would block a configuration that works.

- [x] T016 [P] [US3] Guard tests, covering FR-019, in `internal/modules/user/presentation/http/router_test.go` or a dedicated file beside it: strictly below the avatar ceiling is refused, exactly equal starts, above starts — pinning the boundary in both directions
- [x] T017 [P] [US3] Message test (FR-020) in `internal/modules/user/presentation/http/startup_guard_test.go`, beside the function T018 puts the guard in, asserting the refusal names the setting and both values so an operator can correct it without reading source, and contains no credential value. **Not** in `cmd/api`: `run()` opens the database and runs migrations before it reaches the guard, so a test there would need real infrastructure

### Implementation for User Story 3

- [x] T018 [US3] Export the single figure and the guard function (FR-019) an avatar route needs from `internal/modules/user/presentation/http/router.go`, and add a pure exported guard function beside it taking only the two ceiling values and media-configured state, so `cmd/api` can call it and a unit test can exercise every branch without a database. The route and the guard must read the same figure so they cannot drift
- [x] T019 [US3] Apply the guard (FR-019, FR-013) in `cmd/api/main.go`, refusing to start only when media upload is configured and the shared ceiling is strictly lower, and logging rather than refusing when media is absent

**Checkpoint**: All three user stories are independently functional.

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: Documentation, decisions and the close-out gates.

- [x] T020 [P] Write the ADR (FR-019) at `docs/decisions/010-*.md` for the startup guard and the refusal-reason correction, following `docs/decisions/template.md`, and add it to the `docs/decisions/README.md` index
- [x] T021 [P] Update `docs/api-reference.md` (FR-006, FR-001) — the registration endpoint gains the failure branch, and the avatar endpoint's `413` reason becomes unambiguous — per Constitution VIII in the same change as the code
- [x] T022 [P] Update `docs/configuration.md`, covering FR-014, to state the shared ceiling's coupling to the avatar ceiling and the startup refusal, and add the row recording the observed provider failure modes
- [x] T023 [P] Record the new registration failure branch in `specs/003-user-profile/contracts/error-codes.md` beside the existing "codes deliberately not added" table
- [x] T024 [P] Verify FR-021 in `internal/share/logging/redact_test.go` that neither the new diagnostic classification nor the startup message can leak a credential, exercising both failure paths first
- [x] T025 Check every new and changed file for UTF-8 without BOM and LF endings, per `.editorconfig`; PowerShell's `Out-File` and `WriteAllLines` write CRLF, so write files in a way that ends up correct
- [x] T026 Run `make lint` and `make test` and clear every finding in the feature's scope
- [x] T027 Run `quickstart.md` end to end — scenarios 1 through 8 — and confirm each passes. A container-backed check that silently skips is a **fail**, not a pass
- [x] T029 [P] Publish the operator verification procedure required by FR-023 in `docs/configuration.md`, naming each value an operator must obtain and exactly where to get it: the media cloud name from the provider console (explicitly **not** the upload folder), and the mail provider's authorised sending address. `quickstart.md` states it, but a feature folder is not where an operator looks first, so the procedure must also live in the doc the configuration table is read from
- [x] T028 Re-run the media outcome against the real provider: a genuine 760x760 PNG uploaded through `POST /api/v1/users/me/avatar` is accepted, the stored width is 512 (the provider applied the transform), all four avatar columns are populated together, `USER_AVATAR_SET` is audited, a valid image refused by the provider leaves the previous avatar untouched (FR-017), and removing it clears the columns and destroys the asset
- [ ] T028b Mail outcome, **still blocked by configuration**: the mail relay answers `525 5.7.1 Unauthorized IP address` after a successful TLS handshake, so no message reaches a real inbox. Re-tested this run by calling the relay directly. Unblocks when the sending address is authorised in the provider console

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies — start immediately
- **Foundational (Phase 2)**: Depends on Setup — BLOCKS all user stories
- **User Stories (Phases 3–5)**: All depend on Foundational completion. **They touch disjoint
  files and can proceed fully in parallel**: US1 in the user module's presentation layer, US2 in
  the auth module, US3 in the composition root
- **Polish (Phase 6)**: Depends on the stories being implemented

### User Story Dependencies

- **User Story 1 (P1)**: Needs only T004. No dependency on any other story
- **User Story 2 (P1)**: Needs only T005. No dependency on any other story
- **User Story 3 (P2)**: Needs only T004's sibling work from Phase 2. No dependency on any other story

### Within Each User Story

- Tests MUST be written and FAIL before implementation. For US1 the test asserts the error
  **code**, because the defect already produces the right status
- The `[P]` test tasks in a story may run together
- Implementation tasks within a story are sequential and must not run in parallel: they edit
  files that depend on each other

### Critical Path

T001 → T004 → T008 → T018 → T019 → T027

US2 is a longer chain (five tasks) than US1 (three) but shares no file with it, so the two
finish at roughly the same time.

---

## Parallel Opportunities

- T002, T003 and T005 all touch different files and can run together
- T004 is the only shared prerequisite blocking US1 and US3; do it first
- All three stories' test tasks can be written in parallel — they are the highest-value work
  here, because each one fails before its fix and proves the fix afterwards
- All Polish documentation tasks (T020–T024) are independent of each other

---

## Parallel Example: User Story 1

```bash
# Both test tasks together, before any implementation:
Task: "Avatar refusal tests in internal/modules/user/presentation/http/avatar_test.go"
Task: "Router-level test in internal/modules/user/presentation/http/router_test.go"

# Then the implementation, sequentially — same file dependency chain:
Task: "Apply the parameter to the avatar route in router.go"
Task: "Verify the handler's read ceiling reports the same code"
```

---

## Implementation Strategy

### MVP First (User Story 1 only)

1. Phase 1: reproduce and confirm the baseline
2. Phase 2: the shared middleware parameter and the audit constant
3. Phase 3: User Story 1
4. **STOP and VALIDATE**: the three request shapes answer the documented reason, and the JSON
   route still answers the generic one
5. Deploy or demo if ready

This is a genuinely shippable increment: it removes the confusing answer on its own.

### Incremental Delivery

1. Phase 1 + 2 → foundation ready
2. US1 → test independently → deploy. MVP: the wrong reason is gone
3. US2 → test independently → deploy. Customers are no longer stranded by a `503`
4. US3 → test independently → deploy. Misconfiguration is caught at startup
5. Each story adds value without breaking the previous ones

### Parallel Team Strategy

With two developers:

1. Both complete Phase 1 and 2 together — T004 is the only contested file
2. Then split: **Developer A takes US1 + US3** (both in the user module and the composition,
   the same mental model), **Developer B takes US2** (the auth module, the largest chain)
3. Merge on disjoint files; only `router_test.go` is touched by two stories, so US1's T007 and
   US3's T016 must land in that file in sequence rather than in parallel

---

## Notes

- [P] tasks = different files, no dependencies
- [Story] labels map each task to a user story for traceability
- Each user story is independently completable and testable — this feature was split that way
  deliberately, and merging the stories back would create cross-story coupling for no gain
- Verify tests fail before implementing; a test that passes first proves nothing
- Commit after each task or logical group
- Stop at any checkpoint to validate a story independently
- Avoid: vague tasks, same-file conflicts, and cross-story dependencies. The one same-file
  overlap is `internal/modules/user/presentation/http/router_test.go`, shared by T007 (US1) and
  T016 (US3) — sequence those two
- T008a–T015a and T016–T019 carry a letter suffix because they were inserted after the IDs were
  already reported to the developer. The IDs remain unique and the order is unambiguous
