---
description: "Task list for Order Confirmation & Editing (module 07 extension)"
---

# Tasks: Order Confirmation & Editing

**Input**: Design documents from `specs/010-order-confirmation/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/{openapi.yaml, error-codes.md}, quickstart.md

**Tests**: Included. The constitution mandates test-first coverage for **order totals, state transitions (positive + negative) and the stock hold**, and the plan requires the guarantees a fake cannot prove to be proven against real PostgreSQL.

**Organization**: Tasks are grouped by user story so each story is independently implementable and testable.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (US1–US6)
- Exact file paths are given in each task

## Path Conventions

- Module root: `internal/modules/order/`
- Shared code: `internal/share/`
- Providing module: `internal/modules/inventory/`
- Migrations: `migrations/`
- Composition root: `cmd/api/main.go`

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Confirm the ground is ready and the next migration number is free.

- [X] T001 Confirm the baseline: run `make lint` and `make test` green, confirm `migrations/` ends at `00010_add_username_to_users.sql` (another session's) so this feature's migration is `00011_order_confirmation.sql`, and confirm `HoldTTL` lives in `internal/modules/inventory/domain/constant/hold.go`

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The domain states and transitions, the schema, the repository, the new ports, the mailer and the notifier. **Nothing in any user story can be built without these.**

**⚠️ CRITICAL**: No user story work can begin until this phase is complete

### Domain

- [X] T002 [P] In `internal/modules/order/domain/constant/codes.go`: rename `StatusPendingPayment` → `StatusPaymentPending` (value `PAYMENT_PENDING`), add `StatusPending` (`PENDING`), update `IsValidStatus` to the six states, add the error codes `CodeNotEditable = "ORDER_NOT_EDITABLE"` and `CodeEmptyOrder = "ORDER_EMPTY"`, and add the audit actions `AuditOrderConfirmed = "ORDER_CONFIRMED"` and `AuditOrderRejected = "ORDER_REJECTED"`
- [X] T003 [P] In `internal/modules/order/domain/error/errors.go`: add sentinels `ErrNotEditable` and `ErrEmptyOrder` (with the message pieces the presentation layer needs), keeping the existing sentinels
- [X] T004 Create `internal/modules/order/domain/model/order_test.go` **first** (fails until T005): every edge of the six-state machine one positive and one negative (`PENDING→PAYMENT_PENDING` confirm, `PENDING→CANCELLED` reject/cancel, `PAYMENT_PENDING→PAID`, `PAYMENT_PENDING→CANCELLED`, `PAID→SHIPPED`, `SHIPPED→COMPLETED`), that `Cancel` is allowed from `PENDING` and `PAYMENT_PENDING` but not `PAID`, that `MarkPaid` is allowed only from `PAYMENT_PENDING`, that `Edit` is allowed only while `PENDING`/`PAYMENT_PENDING` and otherwise `ErrNotEditable`, that a refused move names the current state, and that the total equals the sum of the line snapshots
- [X] T005 In `internal/modules/order/domain/model/order.go`: add `Version int64`, `ConfirmedAt *time.Time`, `PaymentExpiresAt *time.Time` and remove `ExpiresAt`; make `NewOrder` start in `PENDING` with `Version: 1` and no deadline; add `Confirm()`, `Reject()`, `Edit()` (guarded by an `IsEditable()` check) and allow `Cancel()` from either pre-paid state; change `MarkPaid()` to `PAYMENT_PENDING → PAID`; keep `transition` the only writer of `Status` (FR-001, FR-004, FR-010, FR-012, FR-018)

### Repository, migration, ports

- [X] T006 [P] In `internal/modules/order/domain/repository/order.go`: update `ListExpiredPending` (now `PAYMENT_PENDING` past `payment_expires_at`), extend `ListAll` with a `status` filter and an ascending/descending sort, and add `SaveConfirm(ctx, order, now)`, `ReplaceLines(ctx, order, now)` and `InsertEditHistory(ctx, record)` (research D6, D10, D11)
- [X] T007 Create `migrations/00011_order_confirmation.sql`: **backfill** `UPDATE orders SET status = 'PAYMENT_PENDING' WHERE status = 'PENDING_PAYMENT'`, then widen `orders_status_ck` to the six states, rename `expires_at` → `payment_expires_at` (drop `NOT NULL`), add `order_version bigint NOT NULL DEFAULT 1` with `orders_version_ck (>= 1)`, add `confirmed_at timestamptz`, recreate `orders_expiry_idx` on `(status, payment_expires_at)`, and create `order_edit_history` per `data-model.md`
- [X] T008 Extend `migrations/order_guard_test.go` to assert the new `orders_status_ck` set, `orders_version_ck`, the nullable `payment_expires_at`, and the `order_edit_history` foreign key, cascade and version check
- [X] T009 [P] In `internal/modules/order/application/interface/ports.go`: add the `Notifier` port (`Send(ctx, to, subject, body string) error`) with a comment that sending is best-effort and never fails a business operation (FR-021, research D9)
- [X] T010 [P] In `internal/modules/order/application/dto/dto.go`: add `EditInput` (`OrderID`, optional `AddressID *uuid.UUID`, `Lines []EditLineInput`) and `EditLineInput` (`ProductID`, `Quantity`); add `Status *constant.Status` and `Sort` to `ListInput`; keep the existing inputs
- [X] T011 [P] In `internal/modules/order/application/mapper/mapper.go`: keep mapping order/line models to views; do **not** expose `Version` (it is an internal seam, research D10)

### Infrastructure (mailer, notifier, postgres, module 05)

- [X] T012 [P] Create `internal/share/mailer/mailer.go`: a `Sender` interface (`Send(ctx, to, subject, body string) error`) with a `LogSender` (logs recipient + subject, never the body) and an `SMTPSender` (`net/smtp`), plus `NewSender(host string, port int, username, password, from string, logger *slog.Logger)` (a config shape local to the package, **not** `config.AuthConfig`) choosing SMTP when the host is set — mirroring module 01's design without touching module 01 (research D9)
- [X] T013 [P] Create `internal/modules/order/infrastructure/implement/notifier/notifier.go`: implement the `Notifier` port over `internal/share/mailer`, sending best-effort (a failure is logged, never returned), with a compile-time assertion and a unit test `.../notifier/notifier_test.go` (research D9)
- [X] T014 Update `internal/modules/order/infrastructure/implement/postgres/order.go`: adjust `orderColumns`/`scanOrder`/`Create` for `payment_expires_at`, `order_version`, `confirmed_at`; implement `SaveConfirm`, `ReplaceLines` (delete the order's lines, insert the new set, update total/address/version/status/clearing the deadline), `InsertEditHistory`, the updated `ListExpiredPending`, and the `status`/`sort` list variants
- [X] T015 Extend `internal/modules/order/infrastructure/implement/postgres/order_integration_test.go`: round-trip of the new columns, `ReplaceLines` ordering and unique `(order_id, product_id)`, an edit-history row, `ListExpiredPending` selecting only `PAYMENT_PENDING` past `payment_expires_at`, and the `status`/`sort` list behaviour
- [X] T016 In `internal/modules/inventory/domain/constant/hold.go` change `HoldTTL` to `60 * time.Minute`, update the module 05 tests that assert the old window, and fix the now-stale "fifteen minutes" comments in `internal/contracts/inventory.go` and `internal/modules/order/presentation/worker/sweeper.go` (research D13)

**Checkpoint**: Foundation ready — the domain, schema, repository, mailer, notifier and the 60-minute hold window exist.

---

## Phase 3: User Story 1 - A placed order waits for the artist, then becomes payable (Priority: P1) 🎯 MVP

**Goal**: Checkout creates an order awaiting the artist and holding nothing; the artist confirms, which holds the whole order and opens the payment window; the order then continues to paid/shipped/completed.

**Independent Test**: Place an order → it is `PENDING` and available quantity is unchanged; confirm as the artist → `PAYMENT_PENDING` and available quantity fell (physical unchanged); confirm payment → `PAID`.

### Tests for User Story 1 ⚠️

- [X] T017 [P] [US1] Use-case tests `internal/modules/order/application/implement/checkout_test.go` and `lifecycle_test.go`: checkout creates `PENDING` and holds nothing; `Confirm` holds every line, sets `confirmed_at`/`payment_expires_at`, bumps the version and reaches `PAYMENT_PENDING`; `MarkPaid` from `PAYMENT_PENDING` sells the hold once; `Cancel` works from `PENDING` and `PAYMENT_PENDING` (releasing the hold exactly once from `PAYMENT_PENDING`) but is refused from `PAID` (FR-011)
- [X] T018 [P] [US1] HTTP tests `internal/modules/order/presentation/http/checkout_test.go` and `admin_test.go`: checkout returns `201` with `status: "PENDING"`; `POST /admin/orders/{id}/confirm` returns `200`; `POST /orders/{id}/cancel` returns `200` from `PENDING`/`PAYMENT_PENDING` and `409` from `PAID`; `401` without a session, `403` for a customer, `404` unknown, `409` on a wrong state
- [X] T019 [P] [US1] Error-mapping test `internal/modules/order/presentation/http/errors_test.go`: the new codes (`ORDER_NOT_EDITABLE`, `ORDER_EMPTY`) map to their statuses and the reused codes still map correctly

### Implementation for User Story 1

- [X] T020 [US1] In `internal/modules/order/application/implement/checkout.go`: create the order in `PENDING`, **remove** the `Reserve` loop and the deadline (the goods are not held at checkout); keep the cart read, the re-check, the snapshot and the clear (FR-001, FR-002, FR-003)
- [X] T021 [US1] In `internal/modules/order/application/implement/lifecycle.go`: add `ConfirmByAdmin` (lock, drive `Confirm`, `Reserve` every line in the same transaction so any shortage rolls the whole hold back, set `confirmed_at`/`payment_expires_at = now + HoldWindow()`, bump the version, audit `ORDER_CONFIRMED`) and change `MarkPaid` to the `PAYMENT_PENDING` edge (FR-004, FR-005, FR-006, FR-010, FR-024, research D3)
- [X] T022 [US1] In `internal/modules/order/presentation/dto/dto.go`, `handler.go`, `router.go`: add `POST /admin/orders/{id}/confirm` with Swagger annotations, and update the checkout handler's Swagger annotation so it says the order awaits the artist's confirmation (not "awaiting payment")
- [X] T023 [US1] In `cmd/api/main.go`: wire the `Notifier` adapter and the artist email (from config) into the order service, and mount the confirm route
- [X] T024 [US1] Extend `internal/modules/order/presentation/http/order_integration_test.go`: checkout → `PENDING` with available quantity unchanged → confirm → `PAYMENT_PENDING` with the goods held (available fell, physical unchanged) → the paid transition sells once (SC-001)

**Checkpoint**: User Story 1 works on its own — the confirmation lifecycle is real.

---

## Phase 4: User Story 2 - Goods are held only when the artist accepts, all or nothing (Priority: P1)

**Goal**: Confirmation holds the whole order or nothing; two orders competing for the last unit never oversell.

**Independent Test**: An order with a short line is refused at confirmation, stays `PENDING`, and holds no line; two confirmations of the last unit yield exactly one `PAYMENT_PENDING`.

### Tests for User Story 2 ⚠️

- [X] T025 [P] [US2] Use-case test in `internal/modules/order/application/implement/lifecycle_test.go`: `ConfirmByAdmin` on a multi-line order whose one line is short is refused (`ORDER_QUANTITY_EXCEEDS_AVAILABLE`, the item named), the order stays `PENDING`, and **no** line is held
- [X] T026 [P] [US2] Integration test in `internal/modules/order/application/implement/lifecycle_integration_test.go`: two `PENDING` orders competing for the last unit — exactly one confirmation commits and the other is refused; the available quantity never goes negative (FR-024, SC-002, SC-005)
- [X] T027 [US2] Ensure the confirm shortage maps to `ORDER_QUANTITY_EXCEEDS_AVAILABLE` in `internal/modules/order/presentation/http/errors.go` and cover it in `errors_test.go` (reuse the `009` code, research D12)

**Checkpoint**: The hold is provably all-or-nothing and never oversells.

---

## Phase 5: User Story 3 - A customer changes an order before paying (Priority: P2)

**Goal**: A customer edits an order while it awaits the artist or payment; editing an awaiting-payment order releases its goods and returns it to awaiting confirmation.

**Independent Test**: Edit an awaiting-payment order → it returns to `PENDING`, the goods are released, and the artist must re-confirm; edit an awaiting-confirmation order → it stays `PENDING`.

### Tests for User Story 3 ⚠️

- [ ] T028 [P] [US3] Use-case tests `internal/modules/order/application/implement/edit_test.go`: a `PENDING` edit replaces lines/address, recomputes the total and bumps the version, staying `PENDING`; a `PAYMENT_PENDING` edit releases the old hold (once), clears the deadline, bumps the version and returns to `PENDING`; refusals — not editable (`ORDER_NOT_EDITABLE`), empty (`ORDER_EMPTY`), an invalid line (`ORDER_ITEM_NOT_PURCHASABLE`/`ORDER_ITEM_PRICE_CHANGED`/`ORDER_QUANTITY_EXCEEDS_AVAILABLE`), a foreign `addressId` (`VALIDATION_ERROR`) — each leaving the order unchanged; an edit writes an edit-history row
- [ ] T029 [P] [US3] HTTP tests `internal/modules/order/presentation/http/edit_test.go`: `PUT /orders/{id}` returns `200`; `400` for a bad body/`addressId`, `404` for another customer's order, `409` for `ORDER_NOT_EDITABLE`/`ORDER_EMPTY`/line refusals

### Implementation for User Story 3

- [ ] T030 [US3] Create `internal/modules/order/application/implement/edit.go`: `EditMine` — lock the order (owner from the session, foreign ≡ not-found), check `IsEditable`, release the current hold line by line when it was `PAYMENT_PENDING`, re-check each new line through `ProductCatalog`/`InventoryAvailability`, resolve the address through `CustomerLookupService` (omitted keeps the current), refuse an empty set, replace the lines and total, bump the version, move `PAYMENT_PENDING → PENDING`, write the edit history and notify the artist — all in one transaction (FR-012–FR-017, research D6, D7)
- [ ] T031 [US3] In `internal/modules/order/presentation/dto/dto.go`, `handler.go`, `router.go`: add `PUT /orders/{id}` with its body, error mapping and Swagger annotations
- [ ] T032 [US3] Extend `internal/modules/order/presentation/http/order_integration_test.go`: edit a `PENDING` order; edit a `PAYMENT_PENDING` order and prove the hold is released exactly once and the order is `PENDING` again with an edit-history row (SC-003)

**Checkpoint**: A customer can change an order before paying, and an accepted order is re-confirmed.

---

## Phase 6: User Story 4 - The artist declines an order (Priority: P2)

**Goal**: The artist declines an order awaiting confirmation; it becomes cancelled and no goods were held.

**Independent Test**: Reject a `PENDING` order → `CANCELLED`, stock unchanged.

### Tests for User Story 4 ⚠️

- [ ] T033 [P] [US4] Use-case and HTTP tests `internal/modules/order/application/implement/admin_test.go` and `presentation/http/admin_test.go`: reject a `PENDING` order → `CANCELLED`; reject a non-`PENDING` order → `409 ORDER_STATE_TRANSITION_INVALID` naming the state; a customer's session → `403`; the act is audited (`ORDER_REJECTED`); and the admin list honours `status` and `sort` (`status=PENDING&sort=oldest` is the FIFO confirmation queue, invalid values → `400 VALIDATION_ERROR`)

### Implementation for User Story 4

- [ ] T034 [US4] Add `RejectByAdmin` to `internal/modules/order/application/implement/admin.go` (lock, drive `Reject`, audit `ORDER_REJECTED`, notify the customer); extend `ListAll` in the same file and the `GET /admin/orders` handler in `presentation/http/{handler.go,router.go}` to accept and validate `status` and `sort` (default newest), passing them through `dto.ListInput`, with Swagger `@Param`s; and add `POST /admin/orders/{id}/reject` with Swagger annotations (FR-018, FR-026, research D11)
- [ ] T035 [US4] Extend `internal/modules/order/presentation/http/order_integration_test.go`: reject → `CANCELLED`, no stock movement, an audit row naming the order and the administrator

**Checkpoint**: The artist can decline an order.

---

## Phase 7: User Story 5 - Both sides are notified (Priority: P2)

**Goal**: The artist is emailed when an order needs confirmation; the customer is emailed on every status change; a send failure never fails the order.

**Independent Test**: Place an order → the artist's email (fake notifier) records a confirmation-needed message; confirm → the customer's email records a status change; a notifier error leaves the order's status change successful.

### Tests for User Story 5 ⚠️

- [ ] T036 [P] [US5] Use-case tests `internal/modules/order/application/implement/notify_test.go` (over a fake notifier): the artist is notified when an order enters `PENDING` (checkout and edit); the customer is notified on every status change (confirm, reject, paid, ship, complete, cancel, expire); a notifier error does not fail the operation (FR-019, FR-020, FR-021)

### Implementation for User Story 5

- [ ] T037 [US5] In `internal/modules/order/application/implement/{checkout.go,lifecycle.go,admin.go,edit.go}`: compose and send the notifications after the transaction commits (best-effort, errors logged) — the artist on entering `PENDING`, the customer on each status change — reading the customer email through `CustomerLookupService` and the artist email from the service's configured value (FR-019, FR-020, FR-021, research D9)
- [ ] T038 [US5] Extend the HTTP/integration tests to assert the notifications fire for the customer's status changes and that a failing notifier leaves the order correct

**Checkpoint**: Both sides are notified, and email never breaks an order operation.

---

## Phase 8: User Story 6 - An accepted-but-unpaid order expires; a waiting order does not (Priority: P3)

**Goal**: An awaiting-payment order past its 60-minute window cancels itself and returns its goods exactly once; an order awaiting the artist never expires by time.

**Independent Test**: An awaiting-payment order past its window becomes `CANCELLED` with its goods returned once; an awaiting-confirmation order left long stays `PENDING`.

### Tests for User Story 6 ⚠️

- [ ] T039 [P] [US6] Use-case and sweeper tests `internal/modules/order/application/implement/lifecycle_test.go` and `presentation/worker/sweeper_test.go` (injected clock): an awaiting-payment order past `payment_expires_at` is cancelled and its goods released exactly once; an order awaiting the artist is never selected (FR-008, FR-009)

### Implementation for User Story 6

- [ ] T040 [US6] Update `ExpireOrders` in `internal/modules/order/application/implement/lifecycle.go` to select `PAYMENT_PENDING` orders past `payment_expires_at` (the sweeper is otherwise unchanged) (FR-008, FR-009, research D5)
- [ ] T041 [US6] Extend `internal/modules/order/application/implement/lifecycle_integration_test.go`: expiry frees the goods exactly once across both the order and inventory sweeps, and a `PENDING` order is never expired (SC-004)

**Checkpoint**: Expiry is correct and bounded to the payment window.

---

## Phase 9: Polish & Cross-Cutting Concerns

**Purpose**: Documentation, the frontend guide, Swagger, and the close-out gates.

- [ ] T042 [P] Update `docs/api-reference.md`: the three new endpoints and the changed ones, the six-value status enum, the two new error codes, and the hold-window text (order and inventory) to the 60-minute window
- [ ] T043 [P] Update `docs/modules/07-order.md`: the new state machine, the hold-at-confirmation, the editing flow, the notifications and the version
- [ ] T044 [P] Update `docs/modules.md` (note the order confirmation flow in the V1.0 roadmap) and `docs/modules/05-inventory.md` (the hold window is now 60 minutes)
- [ ] T045 [P] Create `docs/decisions/018-order-confirmation-and-editing.md`: supersede `009`'s state machine (the rename and the new states), hold-only-on-confirmation, the 60-minute window and the `HoldTTL` change, whole-order editing with the return-to-PENDING rule, the `order_version` seam for module 08, and the minimal shared mailer
- [ ] T046 [P] Create `specs/010-order-confirmation/deferred.md`: the payment attempt/session and IPN/late-callback handling (module 08), the provider payment-session cancellation on edit (module 08), in-app notifications and the remaining emails (module 12); state what each is, why out of scope, and what unblocks it

### Frontend Integration Guide *(bắt buộc — hiến pháp §Governance)*

- [ ] T047 **Có đổi bề mặt client** → viết `specs/010-order-confirmation/frontend-guide.md`: liệt kê từng endpoint thêm/đổi (`PUT /orders/{id}`, `POST /admin/orders/{id}/confirm`, `POST /admin/orders/{id}/reject`, `GET /admin/orders` thêm `status`/`sort`; `POST /orders` và `POST /orders/{id}/cancel` đổi hành vi), enum `OrderStatus` **6 giá trị** (đánh dấu `PENDING_PAYMENT` **bị đổi tên** thành `PAYMENT_PENDING`), mọi trường DTO, và mọi mã lỗi mới; **đánh dấu `specs/009-order/frontend-guide.md` đã bị thay thế**
- [ ] T048 **Đồng bộ guide** ở phase cuối, SAU khi mã nguồn ổn định: đối chiếu từng route, từng trường DTO, từng giá trị enum, từng mã HTTP với mã nguồn, **có đếm**; sửa mọi chỗ lệch, ghi rõ phần chưa kiểm nếu có

### Swagger annotations *(bắt buộc — feature có endpoint)*

- [ ] T049 Thêm annotation Swagger cho **mọi** handler mới/đổi (`@Summary`, `@Tags`, `@Param`, `@Success`, `@Failure`, `@Router`) rồi chạy `make swagger`; `make swagger-check` (trong `make check`) phải xanh (Constitution VIII)

### Phát hiện ngoài phạm vi *(hiến pháp §Governance)*

- [ ] T050 Rà lại các phát hiện ngoài phạm vi và ghi hết vào `specs/010-order-confirmation/deferred.md`, KHÔNG để nguyên `- [ ]` ở file này; nêu rõ **là gì**, **vì sao ngoài phạm vi**, **điều gì gỡ được** nó; nếu một phát hiện làm mất hiệu lực bằng chứng của một task đã tick `[X]` thì nêu rõ số task đó

### Close-out

- [ ] T051 Normalize the new files to UTF-8 (LF); check for BOM
- [ ] T052 Run `make lint` and `make test`
- [ ] T053 Run `make test-integration` (needs Docker) and confirm it did not skip
- [ ] T054 Run `quickstart.md` validation
- [ ] T055 Run `make check` and confirm `swagger-check` passes

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies
- **Foundational (Phase 2)**: Depends on Setup; **blocks all user stories**
- **User Stories (Phase 3–8)**: All depend on Foundational
  - US1 (P1) and US2 (P1) are the MVP; US3–US6 follow
  - US2 proves the guarantee US1's `Confirm` provides; US3's edit reuses US1's confirm; US4 reuses the state machine; US5 notifies all transitions; US6 reuses the cancel/release path
- **Polish (Phase 9)**: Depends on all desired stories being complete

### User Story Dependencies

- **US1 (P1)**: after Foundational; no dependency on other stories
- **US2 (P1)**: after US1 (its `Confirm` is what it proves)
- **US3 (P2)**: after Foundational; uses the state machine and the release path; independently testable
- **US4 (P2)**: after Foundational; independently testable
- **US5 (P2)**: after Foundational; its notifications attach to the transitions US1/US3/US4 introduce
- **US6 (P3)**: after Foundational; uses `Cancel`/`Release`; independently testable

### Within Each User Story

- Tests first (they must fail), then implementation; core before endpoints

### Parallel Opportunities

- All `[P]` tasks in Phase 2 are different files and run together (domain codes/errors, ports, dto, mapper, mailer, notifier)
- After Foundational, the test files of a story run together; US3, US4 and US5 can be worked in parallel by different people
- The Phase 9 documents (T042–T046) are independent files

---

## Parallel Example: Foundational

```bash
# The domain, ports and infrastructure touch different files:
Task: "constant codes — T002"
Task: "domain errors — T003"
Task: "repository interface — T006"
Task: "Notifier port — T009"
Task: "dto — T010"
Task: "mapper — T011"
Task: "share/mailer — T012"
Task: "notifier adapter — T013"
```

---

## Implementation Strategy

### MVP First (User Stories 1 and 2)

1. Phase 1: Setup
2. Phase 2: Foundational (CRITICAL — blocks all stories)
3. Phase 3: User Story 1 (the confirmation lifecycle)
4. Phase 4: User Story 2 (the hold guarantee)
5. **STOP and VALIDATE**: an order waits for the artist, is confirmed into a held awaiting-payment order, and never oversells

### Incremental Delivery

1. Setup + Foundational → foundation ready
2. US1 → the confirmation lifecycle (MVP)
3. US2 → the all-or-nothing guarantee
4. US3 → editing before payment
5. US4 → declining
6. US5 → notifications
7. US6 → expiry
8. Polish → docs, frontend guide, Swagger, gates

### Notes

- The order holds **no** stock of its own: every hold/sale/release goes through `InventoryReservation` (module 05). Checkout holds nothing; confirmation holds everything or nothing
- Money is integer minor units + currency; the total carries **no** shipping fee (ADR 015 §5)
- The paid transition is delivered and tested but has **no HTTP endpoint** (module 08 drives it)
- Migration is `00011` (`00010` belongs to another session); do not touch `internal/modules/auth/**`
- Commit after each phase once its gate is green; never push/tag without explicit authorization
