---
description: "Task list for Online Order Payment (module 08)"
---

# Tasks: Online Order Payment

**Input**: Design documents from `specs/011-order-payment/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/{openapi.yaml, error-codes.md}, quickstart.md

**Tests**: Included. The constitution mandates test-first coverage for **money amounts, order totals, state transitions (positive + negative) and payment application idempotency**, and the plan requires the guarantees a fake cannot prove to be proven against real PostgreSQL and against the PayOS documentation's signature vectors.

**Organization**: Tasks are grouped by user story so each story is independently implementable and testable.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (US1–US5)
- Exact file paths are given in each task

## Path Conventions

- Module root: `internal/modules/payment/`
- Cross-module contracts: `internal/contracts/`
- Providing module for `OrderPayment`: `internal/modules/order/`
- Config: `internal/share/config/`
- Migrations: `migrations/`
- Composition root: `cmd/api/main.go`

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Confirm the ground is ready and the next migration number is free.

- [X] T001 Confirm the baseline: run `make lint` and `make test` green, confirm `migrations/` ends at `00011_order_confirmation.sql` so this feature's migration is `00012_payment.sql`, confirm neither `internal/contracts/order.go` nor `internal/contracts/payment.go` exists yet, and that `internal/share/config/config.go` has no `PAYOS_*` settings yet

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The two cross-module contracts with their adapters, the payment module skeleton, its domain and storage, the PayOS client over the standard library, and the configuration.

**⚠️ CRITICAL**: No user story work can begin until this phase is complete

### The two cross-module contracts

- [X] T002 [P] Create `internal/contracts/order.go`: the `OrderPayment` interface (`PaymentTarget(ctx, orderID) (OrderPaymentTarget, error)` and `MarkPaid(ctx, orderID, sourceReference string, expectedVersion int64) error`), the **contract-local** `OrderPaymentTarget` struct (`UserID uuid.UUID`, `Amount int64`, `Currency string`, `Version int64`, `PaymentExpiresAt time.Time`, `AwaitingPayment bool` — a flag, not module 07's status type, so the package imports no module), and an `ErrOrderNotFound` sentinel, per `research.md` D5 and `docs/system-design/contract-purity.md`
- [X] T003 [P] Create `internal/contracts/payment.go`: the `PaymentCancellation` interface (`CancelForOrder(ctx, orderID uuid.UUID)`), provided by module 08 and consumed by module 07, with a comment that it is best-effort and a nil adapter is a no-op (research D9)

### Module 07's side of both contracts

- [X] T004 Extend `internal/modules/order/application/implement/lifecycle.go` and `internal/modules/order/application/interface/ports.go`: make `MarkPaid` **version-aware** — it keeps module 07's delivered no-op success for an order already `PAID`/`SHIPPED`/`COMPLETED` (so a retried callback cannot double-apply), refuses an order awaiting the artist, cancelled or expired, and refuses when the order's `Version` differs from `expectedVersion` naming the version. Update the `OrderService` interface signature **and every call site and test** in module 07 (`lifecycle_test.go`, `lifecycle_integration_test.go`, `notify_test.go`, `presentation/http/order_integration_test.go`) so the module still builds and its tests pass (research D5)
- [X] T005 [P] Create `internal/modules/order/infrastructure/implement/payment/payment.go`: module 07's adapter implementing `contracts.OrderPayment` over its own use cases and repository, with a compile-time assertion and a unit test `.../payment/payment_test.go` (research D5)
- [X] T006 In `internal/modules/order/application/interface/ports.go`, `application/implement/lifecycle.go` and `cmd/api/main.go`: give `OrderService` a nil-tolerant `Cancellations` port (`appinterface.PaymentCancellation`) and call `CancelForOrder(ctx, orderID)` from `Cancel` **after the transaction commits** — `Cancel` is the one use case both `CancelMine` and the expiry sweep drive, so one call site covers both; a nil port is a no-op (research D9)

### Module skeleton and domain

- [X] T007 [P] Create the module skeleton `internal/modules/payment/` with the four layers (`domain/{constant,error,model,repository}`, `application/{dto,interface,implement,mapper}`, `infrastructure/implement/{postgres,payos,auditor}`, `presentation/{dto,http}`) and a `doc.go` for each package
- [X] T008 [P] Create `internal/modules/payment/domain/constant/codes.go`: the **four** payment statuses (`PENDING`, `PAID`, `CANCELLED`, `UNMATCHED`) with `IsValidStatus`, the error codes from `contracts/error-codes.md`, and the audit actions `PAYMENT_CREATED`, `PAYMENT_APPLIED`, `PAYMENT_CANCELLED`, `PAYMENT_UNMATCHED`
- [X] T009 [P] Create `internal/modules/payment/domain/error/errors.go`: the sentinels (`ErrOrderNotFound`, `ErrOrderNotPayable`, `ErrAlreadyLive`, `ErrProviderFailure`, `ErrStaleConfirmation`, `ErrNotFound`, …) each carrying the message pieces the presentation layer needs
- [X] T010 Create `internal/modules/payment/domain/model/payment_test.go` **first** (fails until T011): every edge of the four-state machine one positive and one negative, that a repeat on a `PAID` payment is a no-op success, that `UNMATCHED` requires a reason and no other state may carry one, that the order code is a random positive 53-bit integer (generated many times, in range and distinct), and that the amount is the order total with no shipping fee
- [X] T011 Create `internal/modules/payment/domain/model/payment.go`: the `Payment` entity, the four states and the allowed edges (`PENDING→PAID`, `PENDING→CANCELLED`, any→`UNMATCHED`), the transitions, the order-code generator, and `IsTerminal`/`IsLive`
- [X] T012 [P] Create `internal/modules/payment/domain/repository/payment.go`: the persistence port (`Create`, `FindByID`, `FindByOrder`, `FindLiveByOrder`, `FindByOrderCode`, `ListAll` with a status filter, lock by id, and the writes for status/provider reference/unmatched reason)

### Ports, DTOs, mapper

- [X] T013 [P] Create `internal/modules/payment/application/interface/ports.go`: the `PaymentService` interface, the `UnitOfWork`, `Clock` and `Auditor` ports, the **provider port** (`Provider` with `CreateLink` and `CancelLink`), and the `OrderPayment` contract alias — a nil auditor is tolerated
- [X] T014 [P] Create `internal/modules/payment/application/dto/dto.go`: the start/read/list inputs (the owner always from the session), the payment views (`PaymentView` with `CheckoutURL`/`QRCode`/`UnmatchedReason`), and the webhook input carrying the provider's verified fields
- [X] T015 [P] Create `internal/modules/payment/application/mapper/mapper.go`: model → view, money as minor units + currency

### Schema and configuration

- [X] T016 Create `migrations/00012_payment.sql`: the `payments` and `payment_events` tables with every column, check, index and foreign key in `data-model.md` (the four-value status check, currency check, amount check, version check, unique `order_code`, partial unique `provider_reference`, the `UNMATCHED` ⇔ reason check, and the cascading events foreign key)
- [X] T017 Create `migrations/payment_guard_test.go` asserting the storage constraints directly: an out-of-set `status` and `currency`, a non-positive `amount`, a non-positive `order_version`, the unique `order_code`, the partial unique `provider_reference`, the `UNMATCHED` ⇔ reason check, and the `payment_events` cascade
- [X] T018 [P] In `internal/share/config/config.go`: add `PAYOS_CLIENT_ID`, `PAYOS_API_KEY`, `PAYOS_CHECKSUM_KEY`, `PAYOS_BASE_URL` (default `https://api-merchant.payos.vn`), `PAYMENT_RETURN_URL` and `PAYMENT_CANCEL_URL`, and add empty placeholders with English comments to `.env.example` (**never** the real values)

### Infrastructure adapters

- [X] T019 Create `internal/modules/payment/infrastructure/implement/payos/payos_test.go` **first** (fails until T020): the **create-link** signature over `amount=$amount&cancelUrl=$cancelUrl&description=$description&orderCode=$orderCode&returnUrl=$returnUrl`, and the **webhook** signature over the `data` object's fields sorted alphabetically with nulls as empty strings, both against the **PayOS documentation's documented sample vectors** (research D2, D3)
- [X] T020 Create `internal/modules/payment/infrastructure/implement/payos/payos.go`: the stdlib PayOS client (`net/http` + `crypto/hmac` + `crypto/sha256`, **no new dependency**) implementing the provider port — `CreateLink` signs and calls `POST /v2/payment-requests`, `CancelLink` calls `POST /v2/payment-requests/{id}/cancel`, and a webhook **verifier**; plus a `LogProvider` fallback used when the settings are empty, and a compile-time assertion
- [X] T021 [P] Create `internal/modules/payment/infrastructure/implement/auditor/auditor.go`: the audit writer for the four payment actions
- [X] T022 Create `internal/modules/payment/infrastructure/implement/postgres/payment.go`: the postgres payment repository over the shared generic repository, including the live-by-order lookup, the status-filtered list, the lock, and the writes for status/provider reference/unmatched reason
- [X] T023 Create `internal/modules/payment/infrastructure/implement/postgres/payment_integration_test.go`: the adapter's storage guarantees against real PostgreSQL — create/read round trip, the live-by-order lookup, the unique `order_code` collision, the status-filtered list, and the events cascade

**Checkpoint**: Foundation ready — the two contracts with their adapters, the domain and schema, the PayOS client with proven signatures, and the configuration exist.

---

## Phase 3: User Story 1 - A customer pays an awaiting-payment order online (Priority: P1) 🎯 MVP

**Goal**: A customer starts a payment for their awaiting-payment order and receives a PayOS link/QR; a repeat start reuses the live one.

**Independent Test**: For an order in `PAYMENT_PENDING`, start a payment → receive a link for the exact order total; start it again → the same link; another customer's order → not found; an order not awaiting payment → refused.

### Tests for User Story 1 ⚠️

- [ ] T024 [P] [US1] Use-case tests `internal/modules/payment/application/implement/payment_test.go` (over a fake provider): a start creates one payment for the order's exact total and returns the link; a second start **reuses** the live payment and returns the same link; an order not awaiting payment is refused; an order belonging to someone else answers not-found; a live payment whose link can no longer be returned answers `ErrAlreadyLive`; a provider failure refuses the start and leaves the order untouched
- [ ] T025 [P] [US1] HTTP tests `internal/modules/payment/presentation/http/payment_test.go`: `POST /orders/{id}/payment` returns `200` with the link; `401` without a session; `404 PAYMENT_ORDER_NOT_FOUND` for another customer's order; `409 PAYMENT_ORDER_NOT_PAYABLE`; `422 PAYMENT_PROVIDER_ERROR` when the fake provider fails

### Implementation for User Story 1

- [ ] T026 [US1] Create `internal/modules/payment/application/implement/payment.go`: `Start` (read the order through `OrderPayment.PaymentTarget`, refuse unless it is awaiting payment and it is the caller's, reuse a live payment otherwise, generate the order code, call the provider, write the row in `PENDING` and the `PAYMENT_CREATED` event, all in one transaction) and `Read` (the caller's live payment for their order, `PAYMENT_NOT_FOUND` when none)
- [ ] T027 [US1] In `internal/modules/payment/presentation/dto/dto.go`, `handler.go`, `router.go`: add the customer payment routes (`POST /` and `GET /` **relative to the payment sub-router**) with Swagger annotations and the error mapping; the router is mounted at `/orders/{orderId}/payment` (the handler reads `orderId` with `chi.URLParam`) — do **not** mount anything at `/orders` (chi panics on a duplicate Mount)
- [ ] T028 [US1] In `cmd/api/main.go`: mount the payment module at `/orders/{orderId}/payment` (customer), wire the `OrderPayment` adapter, the `PaymentCancellation` adapter, the PayOS client (or the log provider when unset), the return/cancel URLs, and the `Cancellations` port on the order service
- [ ] T029 [US1] Create `internal/modules/payment/presentation/http/payment_integration_test.go`: starting a payment for a `PAYMENT_PENDING` order on real PostgreSQL — one payment row, the amount equals the order total, a second start adds no row (SC-001)

**Checkpoint**: User Story 1 works on its own — a customer can ask to pay.

---

## Phase 4: User Story 2 - The shop trusts the gateway's confirmation exactly once (Priority: P1)

**Goal**: A verified webhook is matched to the order and amount and applied exactly once; a replay applies nothing.

**Independent Test**: Deliver the documented valid webhook → the payment is `PAID`, the order is `PAID`, the goods are sold once; deliver it again → nothing applies; tamper the signature → refused and the order untouched.

### Tests for User Story 2 ⚠️

- [ ] T030 [P] [US2] Use-case tests `internal/modules/payment/application/implement/webhook_test.go` (over a fake provider and a fake order contract): a verified matching webhook applies once (the order's version-aware `MarkPaid` is called with the stored version) and records the `PAYMENT_APPLIED` event; a replay is a no-op success; an unverified body is refused; a wrong amount records `UNMATCHED` and does not apply; an unknown order code is refused and **nothing is recorded** (only a warning)
- [ ] T031 [P] [US2] HTTP tests `internal/modules/payment/presentation/http/webhook_test.go`: a valid signature returns `200`; a tampered signature returns `401 PAYMENT_WEBHOOK_INVALID_SIGNATURE` and applies nothing; an amount mismatch still returns `200` but leaves the payment `UNMATCHED`; an unknown order code returns `404 PAYMENT_NOT_FOUND`; an unparseable body returns `400 MALFORMED_REQUEST`

### Implementation for User Story 2

- [ ] T032 [US2] Create `internal/modules/payment/application/implement/webhook.go`: the webhook use case — verify the signature first, find the payment by order code (refuse, logging a warning, when none), short-circuit when it is already `PAID`, compare the amount, call `OrderPayment.MarkPaid` with the stored version, then mark the payment `PAID` with the provider reference and record `PAYMENT_APPLIED` — **one transaction** with the order's paid transition (research D7)
- [ ] T033 [US2] In `internal/modules/payment/presentation/http/webhook.go` and `router.go`: add the **public** (session-free) `POST /payments/payos/webhook` route behind the trust-proxy/rate-limit middleware, with Swagger annotations, answering `2xx` quickly
- [ ] T034 [US2] Extend `internal/modules/payment/presentation/http/payment_integration_test.go`: delivering the webhook on real PostgreSQL applies once (the order becomes `PAID` and the goods are sold), a replay changes nothing, and two concurrent confirmations apply the sale once (SC-002)

**Checkpoint**: The shop can trust a confirmation exactly once.

---

## Phase 5: User Story 3 - A stale confirmation is never applied to the wrong content (Priority: P2)

**Goal**: A confirmation for an order whose content changed (edited then re-confirmed), or that is no longer awaiting payment, is never applied.

**Independent Test**: Create a payment, edit the order and have the artist re-confirm it (a new version), then deliver the **old** payment's confirmation → the order is not `PAID` and the payment is `UNMATCHED`.

### Tests for User Story 3 ⚠️

- [ ] T035 [P] [US3] Use-case tests in `internal/modules/payment/application/implement/webhook_test.go`: a confirmation whose stored `order_version` differs from the order's current version records `UNMATCHED` and does not apply; a confirmation for an order no longer awaiting payment (cancelled or expired) records `UNMATCHED`; the order's state is untouched in both cases; and a **re-delivered** `UNMATCHED` confirmation records no duplicate (research D8)

### Implementation for User Story 3

- [ ] T036 [US3] In `internal/modules/payment/application/implement/webhook.go`: map the order contract's version/state refusal to a `RecordUnmatched` with a reason naming the mismatch (never applying, never refunding), recording the `PAYMENT_UNMATCHED` event **once** (a re-delivery on an already-`UNMATCHED` payment is a no-op)
- [ ] T037 [US3] Extend `internal/modules/payment/presentation/http/payment_integration_test.go`: a stale confirmation after an edit and re-confirmation leaves the order not `PAID`, the payment `UNMATCHED` with a reason, and the goods unsold (SC-003)

**Checkpoint**: A superseded or late confirmation can never corrupt an order.

---

## Phase 6: User Story 4 - Cancelling an unpaid order cancels its payment request (Priority: P2)

**Goal**: When an awaiting-payment order is cancelled, its live PayOS link is cancelled best-effort and the goods are released.

**Independent Test**: Start a payment, cancel the order → the payment is `CANCELLED`, the provider's cancel was called, and the goods return to availability; a provider failure still cancels the order and logs.

### Tests for User Story 4 ⚠️

- [ ] T038 [P] [US4] Use-case tests `internal/modules/payment/application/implement/cancel_test.go` (over a fake provider): `CancelForOrder` cancels a live payment (calls the provider's cancel, marks `CANCELLED`, records the event); a provider failure is logged and leaves the payment for reconciliation without failing; an order with no live payment is a no-op; and module 07's `Cancel` invokes the port after commit (a fake port asserts the call)

### Implementation for User Story 4

- [ ] T039 [US4] Create `internal/modules/payment/application/implement/cancel.go` (`CancelForOrder`) and `internal/modules/payment/infrastructure/implement/payment/cancellation.go` — module 08's adapter implementing `contracts.PaymentCancellation` over the use case, with a compile-time assertion (the port on module 07's `OrderService` is wired in T028)
- [ ] T040 [US4] Extend the integration tests: cancelling an order cancels its payment, records the event, and releases the goods; a provider cancel failure still cancels the order and releases the goods (SC-004)

**Checkpoint**: A cancelled order cannot still be paid.

---

## Phase 7: User Story 5 - The payment state is visible and anomalies are reviewable (Priority: P2)

**Goal**: The customer sees their order's payment state and nobody else's; the operator sees every payment and the reconciliation queue.

**Independent Test**: The customer reads their payment; another customer cannot; the operator's list filters by status (`UNMATCHED` is the queue); a customer's session is refused on the operator list.

### Tests for User Story 5 ⚠️

- [ ] T041 [P] [US5] Use-case and HTTP tests `internal/modules/payment/application/implement/list_test.go` and `presentation/http/admin_test.go`: the operator list is paginated, newest first, and filters by status; an invalid `status` answers `400 VALIDATION_ERROR`; a customer's session answers `403 FORBIDDEN`; the customer's `GET /orders/{id}/payment` returns only their own order's payment, and `404 PAYMENT_NOT_FOUND` when none exists

### Implementation for User Story 5

- [ ] T042 [US5] In `internal/modules/payment/application/implement/list.go` and `presentation/http/{handler.go,router.go}`: add `ListAll` (paginated, `status` filter) and mount `GET /admin/payments` behind the ADMIN guard, with Swagger annotations
- [ ] T043 [US5] Extend the integration tests: the operator's list returns every payment and `status=UNMATCHED` returns only the reconciliation queue (SC-005)

**Checkpoint**: Both the customer and the operator can see the payment state, and anomalies are reviewable.

---

## Phase 8: Polish & Cross-Cutting Concerns

**Purpose**: Documentation, the frontend guide, Swagger, and the close-out gates.

- [ ] T044 [P] Update `docs/api-reference.md`: the four endpoints, the four-value payment status enum, and the six new error codes
- [ ] T045 [P] Rewrite `docs/modules/08-payment.md` to describe the PayOS flow actually built (the MoMo/VietQR text is superseded): the payments/payment_events tables, the four-state payment machine, the webhook, the `orderCode`, the cancellation and the reconciliation queue
- [ ] T046 [P] Update `docs/modules.md`: mark module 08 delivered and the V1.0 roadmap
- [ ] T047 [P] Create `docs/decisions/019-payos-payment.md`: the PayOS integration over the standard library (no dependency), the two contracts (`OrderPayment` with its version-aware paid transition and `PaymentCancellation`), the `orderCode` mapping, the two-layer apply-once, and "anomalies recorded, never refunded"
- [ ] T048 [P] Create `specs/011-order-payment/deferred.md`: refunds, multi-currency, recurring payments, the live-provider end-to-end payment, the one-time webhook URL registration (`POST /confirm-webhook`), the automatic recovery of an orphan link, and the operator's out-of-band resolution of `UNMATCHED` payments — each with what unblocks it

### Frontend Integration Guide *(bắt buộc — hiến pháp §Governance)*

- [ ] T049 **Có đổi bề mặt client** → viết `specs/011-order-payment/frontend-guide.md`: liệt kê từng endpoint (`POST`/`GET /orders/{orderId}/payment`, `GET /admin/payments`), đánh dấu **mới**, mọi trường JSON của mọi DTO (gồm `checkoutUrl`, `qrCode`, `unmatchedReason`), mọi giá trị enum `PaymentStatus` (**4** giá trị), mọi mã lỗi mới, và hành vi **trước → sau** so với `009`/`010` (lần đầu tiên khách có thể trả tiền)
- [ ] T050 **Đồng bộ guide** ở phase cuối, SAU khi mã nguồn ổn định: đối chiếu từng route, từng trường DTO, từng giá trị enum, từng mã HTTP với mã nguồn, **có đếm**; sửa mọi chỗ lệch, ghi rõ phần chưa kiểm nếu có

### Swagger annotations *(bắt buộc — feature có endpoint)*

- [ ] T051 Thêm annotation Swagger cho **mọi** handler (`@Summary`, `@Tags`, `@Param`, `@Success`, `@Failure`, `@Router`, kể cả webhook) rồi chạy `make swagger`; `make swagger-check` (trong `make check`) phải xanh (Constitution VIII)

### Phát hiện ngoài phạm vi *(hiến pháp §Governance)*

- [ ] T052 Rà lại các phát hiện ngoài phạm vi và ghi hết vào `specs/011-order-payment/deferred.md`, KHÔNG để nguyên `- [ ]` ở file này; nêu rõ **là gì**, **vì sao ngoài phạm vi**, **điều gì gỡ được** nó; nếu một phát hiện làm mất hiệu lực bằng chứng của một task đã tick `[X]` thì nêu rõ số task đó

### Close-out

- [ ] T053 Normalize the new files to UTF-8 (LF); check for BOM
- [ ] T054 Run `make lint` and `make test`
- [ ] T055 Run `make test-integration` (needs Docker) and confirm it did not skip
- [ ] T056 Run `quickstart.md` validation
- [ ] T057 Run `make check` and confirm `swagger-check` passes

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies
- **Foundational (Phase 2)**: Depends on Setup; **blocks all user stories**
- **User Stories (Phase 3–7)**: All depend on Foundational
  - US1 (P1) and US2 (P1) are the MVP; US3–US5 follow
  - US2 reuses US1's payment rows; US3 reuses US2's webhook path and module 07's version-aware transition; US4 reuses the provider client and the `PaymentCancellation` contract; US5 reuses the repository's list
- **Polish (Phase 8)**: Depends on all desired stories being complete

### User Story Dependencies

- **US1 (P1)**: after Foundational; no dependency on other stories
- **US2 (P1)**: after US1 (a payment must exist to confirm) and after Foundational (the provider client and the order adapter)
- **US3 (P2)**: after US2; independently testable
- **US4 (P2)**: after US1 (a live payment exists) and Foundational (the `PaymentCancellation` contract); independently testable
- **US5 (P2)**: after Foundational; independently testable

### Within Each User Story

- Tests first (they must fail), then implementation; core before endpoints

### Parallel Opportunities

- Phase 2 `[P]` tasks are different files: the two contracts (T002/T003), the order adapter test (T005), the skeleton (T007), constants/errors (T008/T009), the repository port (T012), ports/dto/mapper (T013–T015), config (T018), auditor (T021)
- The domain test (T010) precedes the model (T011); the signature test (T019) precedes the PayOS client (T020); T004 (version-aware `MarkPaid` + its call sites) precedes T005 and every payment use case
- After Foundational, the test files of a story run together; US3, US4 and US5 can be worked in parallel by different people
- The Phase 8 documents (T044–T048) are independent files

---

## Parallel Example: Foundational

```bash
# Independent files that can be built together:
Task: "contracts/order.go + contracts/payment.go — T002, T003"
Task: "module skeleton + doc.go — T007"
Task: "domain constants / errors — T008, T009"
Task: "repository port — T012"
Task: "application ports / dto / mapper — T013, T014, T015"
Task: "config PAYOS_* + .env.example — T018"
Task: "auditor adapter — T021"
```

---

## Implementation Strategy

### MVP First (User Story 1 and 2)

1. Phase 1: Setup
2. Phase 2: Foundational (CRITICAL — blocks all stories)
3. Phase 3: User Story 1 (start a payment)
4. Phase 4: User Story 2 (the webhook applies exactly once)
5. **STOP and VALIDATE**: a customer can ask to pay and a confirmation is trusted once

### Incremental Delivery

1. Setup + Foundational → foundation ready
2. US1 → starting a payment (MVP)
3. US2 → the webhook applied once
4. US3 → a stale confirmation is never applied
5. US4 → cancelling cancels the payment request
6. US5 → visibility and the reconciliation queue
7. Polish → docs, frontend guide, Swagger, gates

### Notes

- **No new dependency**: the PayOS client uses `net/http`, `crypto/hmac`, `crypto/sha256`, `encoding/json` (ADR 015 §5 D4)
- Money is integer minor units + currency; the amount equals the order total with **no** shipping fee (ADR 015 §5)
- The signatures are proven against the **PayOS documentation's documented vectors**, never against a live call
- Applying a payment, the order's paid transition and the stock sale commit in **one transaction**; cancelling a provider link deliberately does not
- `PAYOS_*` empty ⇒ the log-only provider, so every non-provider path still runs without credentials
- Migration is `00012_payment.sql` (`00011` belongs to feature 010); do not touch `internal/modules/auth/**`
- **FR-021 (no refunds; a paid order is not cancelled)** is satisfied by construction: this feature builds no refund path, and module 07 already refuses to cancel a paid order (it is transferred instead). No task implements it because there is nothing to build; the `UNMATCHED` reconciliation queue is where a stranded payment surfaces.
- Commit after each phase once its gate is green; never push/tag without explicit authorization
