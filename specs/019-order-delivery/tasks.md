---
description: "Task list for Order Delivery & Completion (module 07 extension)"
---

# Tasks: Order Delivery & Completion

**Input**: Design documents from `specs/019-order-delivery/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/{openapi.yaml, error-codes.md}, quickstart.md

**Tests**: Included. The constitution mandates test-first coverage for **state transitions (positive + negative)** and order totals, and the plan requires the guarantees a fake cannot prove (the status check, the rename backfill, the five-photo ceiling under concurrency, the completion-exactly-once race, the single-feedback index) to be proven against real PostgreSQL.

**Organization**: Tasks are grouped by user story so each story is independently implementable and testable.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (US1–US6)
- Exact file paths are given in each task

## Path Conventions

- Module root: `internal/modules/order/`
- Shared media port (application imports only the `Store` interface): `internal/share/media/`
- Migrations: `migrations/`
- Composition root: `cmd/api/main.go`

> This feature does **not** modify `internal/modules/product/` (the image rule stays per-module).

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Confirm the ground is ready and the next migration number is free.

- [X] T001 Confirm the baseline: run `make lint` and `make test` green; confirm `migrations/` ends at `00017_order_item_category_snapshot.sql` so this feature's migration is `00018_order_delivery.sql`; confirm the next ADR number is `023`; confirm the order worker lives at `internal/modules/order/presentation/worker/sweeper.go` and the order `Service` struct is declared in `internal/modules/order/application/implement/checkout.go`

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The seven-state domain, the rename, the schema, the repository, the shared image rule, the new ports/DTOs and the worker's second sweep. **Nothing in any user story can be built without these.**

**⚠️ CRITICAL**: No user story work can begin until this phase is complete

### Domain

- [X] T002 [P] In `internal/modules/order/domain/constant/codes.go`: rename `StatusShipped` → `StatusShipping` (value `"SHIPPING"`) and add `StatusDelivered` (value `"DELIVERED"`); update `IsValidStatus` to the seven states; add the error codes `CodeImageTypeUnsupported = "ORDER_IMAGE_TYPE_UNSUPPORTED"`, `CodeImageTooLarge = "ORDER_IMAGE_TOO_LARGE"`, `CodeImageLimitReached = "ORDER_IMAGE_LIMIT_REACHED"`, `CodeFeedbackNotAllowed = "ORDER_FEEDBACK_NOT_ALLOWED"`, `CodeFeedbackAlreadySent = "ORDER_FEEDBACK_ALREADY_SENT"`; add `AuditOrderDelivered = "ORDER_DELIVERED"` and **keep** `AuditOrderShipped`/`AuditOrderCompleted` unchanged (research D9, D10)
- [X] T003 [P] In `internal/modules/order/domain/error/errors.go`: add sentinels `ErrImageTypeUnsupported`, `ErrImageTooLarge`, `ErrImageLimitReached`, `ErrFeedbackNotAllowed`, `ErrFeedbackAlreadySent`, `ErrMediaUnavailable` (the not-allowed one carries the current state for the message), keeping every existing sentinel
- [X] T004 Create `internal/modules/order/domain/model/order_test.go` **first** (fails until T005): every edge of the seven-state machine one positive and one negative (`PENDING→PAYMENT_PENDING` confirm, `PENDING→CANCELLED` reject/cancel, `PAYMENT_PENDING→PAID`, `PAYMENT_PENDING→CANCELLED`, `PAID→SHIPPING`, `SHIPPING→DELIVERED`, `DELIVERED→COMPLETED`), that `Cancel` is allowed from `PENDING`/`PAYMENT_PENDING` but not `PAID`, that the single `Complete` method is allowed only from `DELIVERED` (it serves both the customer's confirmation and the sweep), that a refused move names the current state, and that `DeliveryCompletionWindow` is 120 hours (FR-002, FR-003, FR-011, FR-012)
- [X] T005 In `internal/modules/order/domain/model/order.go`: rename `Ship()` → `StartShipping()` (`PAID → SHIPPING`); add `MarkDelivered()` (`SHIPPING → DELIVERED`, stamping `DeliveredAt`); repurpose the **single** `Complete()` to `DELIVERED → COMPLETED` (both the customer's `ConfirmReceived` use case and the sweep call it — there is **no** separate domain `ConfirmReceived` method); add the fields `DeliveredAt`, `CompletedAt` and `CompletedBy` (`*uuid.UUID`), the constant `DeliveryCompletionWindow = 5 * 24 * time.Hour`, and `IsAwaitingCustomerConfirmation()`; keep `transition` the only writer of `Status` (FR-005, FR-007, FR-011, FR-012)
- [X] T006 [P] Create `internal/modules/order/domain/model/delivery_photo.go`: the `DeliveryPhoto` entity (`ID`, `OrderID`, `Stage`, `PublicID`, `URL`, `Width`, `Height`, `Position`, `CreatedAt`), a `Stage` value type with the constants `StageShipping`/`StageDelivered`, and `MaxDeliveryPhotos = 5` plus `EnsurePhotoCapacity(stage, existing int) error` (research D3)
- [X] T007 [P] Create `internal/modules/order/domain/model/feedback.go`: the `Feedback` entity (`ID`, `OrderID`, `AuthorID`, `Content`, `CreatedAt`) and a `ValidateFeedbackContent(content string) error` refusing empty/blank content **and content longer than 2000 characters** (TrimSpace then count) (research D8)

### Order's dependency-free image rule (mirrors avatar/picture)

- [X] T008 [P] Create `internal/modules/order/domain/model/photo_image.go`: `DetectDeliveryImage(content []byte) bool` (JPEG/PNG/WebP signature sniff, standard library only) and `ValidateDeliveryPhoto(content []byte, maxBytes int64) error` returning the order-domain sentinels `ErrImageTypeUnsupported`/`ErrImageTooLarge`, with a documented default ceiling — **stdlib-only, in the order's own `domain`; do NOT import `share/media` from `domain`** (research D4)
- [X] T009 [P] Create `internal/modules/order/domain/model/photo_image_test.go` (test-first, before T008's body is finalized): JPEG/PNG/WebP accepted, GIF/PDF/text refused as `ErrImageTypeUnsupported`, an over-ceiling payload refused as `ErrImageTooLarge`, and the type checked before the size (research D4)

### Repository, migration, ports, DTOs

- [X] T011 In `internal/modules/order/domain/repository/order.go`: add `ListDeliveryPhotos(ctx, orderID)`, `AddDeliveryPhotos(ctx, orderID, photos, now)`, `CountDeliveryPhotos(ctx, orderID, stage)`, `StartShipping(ctx, order, now)`, `SaveDelivered(ctx, order, now)`, `SaveCompleted(ctx, order, now)`, `InsertFeedback(ctx, feedback)`, `FindFeedback(ctx, orderID)` and `ListAwaitingCompletion(ctx, cutoff) ([]uuid.UUID, error)` (research D3, D6, D8)
- [X] T012 [P] In `internal/modules/order/application/interface/ports.go`: add `MediaStore = media.Store` (the shared port, re-exported as module 04 does) **only**. Do **not** widen `OrderService` here: each user story's implementation task adds its own method (T025 `StartShippingByAdmin`, T030 `DeliverByAdmin`, T035 `ConfirmReceived` and removal of `CompleteByAdmin`, T039 `AutoCompleteDelivered`, T044 `SubmitFeedback`), so the foundational phase always compiles and its gate is green (research D7)
- [X] T013 [P] In `internal/modules/order/application/dto/dto.go`: add `EvidenceInput` (`OrderID uuid.UUID`, `Photos []UploadedPhoto`), `UploadedPhoto` (`Content []byte`), `FeedbackInput` (`OrderID uuid.UUID`, `Content string`); extend the detail views with `DeliveredAt`, `CompletedAt`, `DeliveryPhotos []DeliveryPhotoView` and `Feedback *FeedbackView` (research D7, D11)
- [X] T014 In `internal/modules/order/application/mapper/mapper.go`: map the new order fields and the photo/feedback collections into the detail views (both customer and admin), keeping `Version` and `CompletedBy` internal (research D11)
- [X] T015 Create `migrations/00018_order_delivery.sql` per `data-model.md`: **drop** `orders_status_ck`, run `UPDATE orders SET status = 'SHIPPING' WHERE status = 'SHIPPED'`, **add** the seven-value check, add `delivered_at timestamptz`, `completed_at timestamptz`, `completed_by uuid`, add `orders_completion_idx (status, delivered_at)`, and create `order_delivery_photos` and `order_feedback` (with the unique `(order_id)` and the `content` length BETWEEN 1 AND 2000 check) — with a reversible `Down` that backfills `SHIPPING → SHIPPED` and restores the six-value check (research D1, D2)
- [X] T016 Extend `migrations/order_guard_test.go` to assert (mirroring `TestOrderConfirmationMigrationBackfillsAndReshapes`): the `SHIPPED → SHIPPING` **backfill precedes** the widened check, the seven-value `orders_status_ck` lists `SHIPPING` and `DELIVERED` and no longer `SHIPPED`, the `Down` restores the six-value check including `SHIPPED`, the `order_delivery_photos` FK/cascade and stage check, the `order_feedback` FK/cascade, unique `(order_id)` and the `content` length check, and that `delivered_at`/`completed_at`/`completed_by` are nullable (research D2)
- [X] T010 (runs **after** T015 creates `migrations/00018_order_delivery.sql`) Extend a real-database migration integration test (mirroring `migrations/admin_dashboard_migration_integration_test.go`'s `applyUpToAdmin` harness): seed an order with status `SHIPPED` at schema version 17, apply `00018`, then assert the row now reads `SHIPPING` with no data loss, and that inserting a status outside the seven values is refused by `orders_status_ck` (SC-007, FR-001, FR-002, research D2)
- [X] T017 Update `internal/modules/order/infrastructure/implement/postgres/order.go`: extend `orderColumns`/`scanOrder` for `delivered_at`/`completed_at`/`completed_by`; implement the new repository methods (photo rows, feedback row, the delivery/completion writes and `ListAwaitingCompletion` ordered by `delivered_at, id`) (research D3, D5, D6)
- [X] T018 Extend `internal/modules/order/infrastructure/implement/postgres/order_integration_test.go`: round-trip of the new columns, photo insert/read ordering by `(stage, position)`, the `order_feedback` unique index refusing a second row, and `ListAwaitingCompletion` selecting only `DELIVERED` past the cutoff
- [X] T064 [US1] (read-projection completion; found during US2) Extend `internal/modules/order/presentation/dto/dto.go` and the handlers `toOrderResponse`/`toAdminOrderResponse` in `presentation/http/handler.go` to expose the delivery facts the application DTO already carries — `deliveredAt`, `completedAt`, `deliveryPhotos[]`, `feedback` — on both `OrderResponse` and `AdminOrderResponse` (detail only, not the list), matching `contracts/openapi.yaml`'s `OrderDetail`; add HTTP tests asserting the detail response carries the photos and feedback. (FR-010, FR-017, research D11)
- [X] T019 In `migrations/00018_order_delivery.sql`: refresh `COMMENT ON COLUMN orders.status` to list the seven states in the Up section and restore the previous six-value comment in the Down section, so the stored documentation does not drift after the rename (L1, research D1)

> **Note (worker)**: the completion sweep (a second sweep on the existing ticker) is implemented in **US4 (T040)**, not here, once `AutoCompleteDelivered` exists — so this foundational phase keeps the worker expiry-only and compiles cleanly (research D6, plan).
- [X] T020 Rename the state across all **runtime** code. `grep -rn "StatusShipped" internal/` for the Go identifier and `grep -rn "'SHIPPED'\|\"SHIPPED\"" internal/` for **both** the single-quoted SQL literal and the double-quoted Go literal, then fix every hit: the order domain constant and its tests, the order HTTP handler status-enum Swagger text and its integration tests, the operator **statistics/read-model** SQL in `internal/modules/order/infrastructure/implement/postgres/order.go` (its earned-states set `('PAID','SHIPPED','COMPLETED')` becomes `('PAID','SHIPPING','DELIVERED','COMPLETED')`, and `internal/modules/order/infrastructure/implement/statistics/statistics_integration_test.go` follows), the admin dashboard Swagger description in `internal/modules/admin/presentation/http/handler.go` and its tests `internal/modules/admin/application/implement/overview_test.go` and `internal/modules/admin/presentation/http/overview_integration_test.go`, and the category/product hide-guard test fixtures. **Do NOT touch `migrations/`**: historical `00009`/`00011`, `order_guard_test.go` and the `Down` of `00018` legitimately keep `'SHIPPED'` (the Down restores the old check). No `StatusShipped` identifier and no `SHIPPED` value remain in `internal/` runtime code (research D1 ripple; H1)
- [X] T021 In `internal/modules/order/presentation/http/errors.go` and `errors_test.go`: map the new codes to their statuses (`ORDER_IMAGE_TYPE_UNSUPPORTED` 400, `ORDER_IMAGE_TOO_LARGE` 413, `ORDER_IMAGE_LIMIT_REACHED` 409, `ORDER_FEEDBACK_NOT_ALLOWED` 409, `ORDER_FEEDBACK_ALREADY_SENT` 409, `ORDER_MEDIA_UNAVAILABLE` 503 from `media.ErrUnavailable`) naming the offending member (contracts/error-codes.md)
- [X] T022 In `internal/modules/order/application/implement/checkout.go` (the `Service` struct): add the `Media` (`MediaStore`) and image-ceiling/config fields the delivery use cases need; in `cmd/api/main.go` pass the **shared** `media` store (already built for the user/product modules) into the order service. **Also**: raise `config.MaxBodyBytes`'s default ceiling and add a composition-root startup guard (mirroring `userhttp.RequireAvatarUploadCeiling`) so the global pipeline ceiling cannot sit below the delivery route's `5 × 2 MiB + envelope` limit — otherwise a five-photo dispatch is refused by the pipeline before the route; the per-photo ceiling is the domain constant `model.MaxDeliveryPhotoBytes` used everywhere (no separate config knob) (research D4, D5, M2)

**Checkpoint**: Foundation ready — seven states, the rename migration, the shared image rule, the repository, the ports/DTOs and the two-sweep worker exist.

---

## Phase 3: User Story 1 - The artist hands the order to the carrier, with dispatch photos (Priority: P1) 🎯 MVP

**Goal**: A paid order is marked in transit (`SHIPPING`) by the operator with up to five dispatch photos.

**Independent Test**: With a `PAID` order, dispatch with photos → `SHIPPING` and the photos read back; dispatch again → refused naming `SHIPPING`.

### Tests for User Story 1 ⚠️

- [X] T023 [P] [US1] Use-case tests `internal/modules/order/application/implement/delivery_test.go` (over a fake media store): `StartShippingByAdmin` drives `PAID → SHIPPING`, uploads 0–5 photos and persists their references; refuses a non-`PAID` order (`ORDER_STATE_TRANSITION_INVALID` naming the state) and an over-ceiling (6) or unsupported/oversized photo **before** the media store is called (FR-005, FR-006, FR-009)
- [X] T024 [P] [US1] HTTP tests `internal/modules/order/presentation/http/admin_test.go`: `POST /admin/orders/{id}/ship` accepts multipart with 0–5 `photos` and returns `200`; `403` for a customer, `401` with no session, `404` unknown, `409` on the wrong state, `409 ORDER_IMAGE_LIMIT_REACHED`, `400 ORDER_IMAGE_TYPE_UNSUPPORTED`, `413 ORDER_IMAGE_TOO_LARGE` (FR-005, FR-006, contracts/error-codes.md)

### Implementation for User Story 1

- [X] T025 [US1] In `internal/modules/order/application/implement/delivery.go`: add `StartShippingByAdmin(in EvidenceInput)` — validate each payload, first transaction locks the order and checks state + per-stage count, upload each photo through `MediaStore` **outside** the transaction, second transaction re-checks the count under the lock, drives `StartShipping`, inserts the photo rows, touches `updated_at` and audits `ORDER_SHIPPED`; release a just-stored asset if the insert fails (best-effort — an orphaned asset if the release itself fails is accepted). Add `StartShippingByAdmin` to the `OrderService` interface and **remove the superseded `ShipByAdmin`** (and the legacy lifecycle `Ship(ctx, orderID)`), updating every call site: `admin.go`, `presentation/http/{handler.go,router.go}` (the `/ship` route and its `Service` interface), the presentation fakes in `presentation/http/admin_test.go`, and the tests `internal/modules/order/application/implement/{admin_test.go,lifecycle_test.go,notify_test.go}`. If the `(*Service).transition` helper is left with no caller it becomes dead code and `make lint` (golangci `unused`, U1000) fails — either reuse it for the new moves or delete it (research D5, D10; H4, I1, I5, I6)
- [X] T026 [US1] In `internal/modules/order/presentation/dto/dto.go`, `presentation/http/{handler.go,router.go}`: make `POST /admin/orders/{id}/ship` a **multipart** route installing a route-specific body ceiling (5 × the image ceiling + envelope, allowing `multipart/form-data`) whose refusal maps to **`ORDER_IMAGE_TOO_LARGE` (413)**; decode the photos, and add the Swagger annotations (`@Summary`, `@Tags`, `@Param`, `@Success`, `@Failure`, `@Router`) (research D7; Constitution VIII; M2, M3)
- [X] T027 [US1] Extend `internal/modules/order/presentation/http/order_integration_test.go`: dispatch a `PAID` order with two photos on real PostgreSQL → `SHIPPING` with two `SHIPPING`-stage rows; a **six-photo** request is refused with `ORDER_IMAGE_LIMIT_REACHED` before the media store is called; and a second dispatch of the same order is refused with `ORDER_STATE_TRANSITION_INVALID` naming `SHIPPING` (the counter is not a concurrency guard — the edge fires once) (SC-001, SC-004)

**Checkpoint**: User Story 1 works on its own — a paid order stops at "in transit" with evidence.

---

## Phase 4: User Story 2 - The artist marks the order delivered, with proof photos (Priority: P1)

**Goal**: An in-transit order is marked delivered (`DELIVERED`) with up to five photos, and the delivery timestamp is recorded.

**Independent Test**: With a `SHIPPING` order, deliver with photos → `DELIVERED`, `deliveredAt` set, photos read back; deliver from a wrong state → refused.

### Tests for User Story 2 ⚠️

- [X] T028 [P] [US2] Use-case tests `internal/modules/order/application/implement/delivery_test.go`: `DeliverByAdmin` drives `SHIPPING → DELIVERED`, uploads 0–5 photos, stamps `DeliveredAt`; refuses a non-`SHIPPING` order (naming the state) and an over-ceiling/unsupported/oversized photo before the media store is called (FR-007, FR-008, FR-009)
- [X] T029 [P] [US2] HTTP tests `internal/modules/order/presentation/http/admin_test.go`: `POST /admin/orders/{id}/deliver` multipart returns `200`; the same guard/error matrix as T024 (FR-007, FR-008)

### Implementation for User Story 2

- [X] T030 [US2] In `internal/modules/order/application/implement/delivery.go`: add `DeliverByAdmin(in EvidenceInput)` — the same validate → lock/check → upload → re-check/insert shape as `StartShippingByAdmin`, driving `MarkDelivered`, stamping `DeliveredAt`, persisting the `DELIVERED`-stage photos and auditing `ORDER_DELIVERED`; add `DeliverByAdmin` to the `OrderService` interface (research D5, D10)
- [X] T031 [US2] In `internal/modules/order/presentation/dto/dto.go`, `presentation/http/{handler.go,router.go}`: add `POST /admin/orders/{id}/deliver` (multipart, same ceiling and `ORDER_IMAGE_TOO_LARGE` refusal) with Swagger annotations (research D7; Constitution VIII; M3)
- [X] T032 [US2] Extend `internal/modules/order/presentation/http/order_integration_test.go`: deliver → `DELIVERED` with `delivered_at` set and the `DELIVERED`-stage photos persisted; and assert the delivery wrote **no** `inventory_transactions` row and left `total_amount` unchanged (SC-001, FR-020)

**Checkpoint**: A dispatched order can be delivered with proof.

---

## Phase 5: User Story 3 - The customer confirms receipt and completes the order (Priority: P2)

**Goal**: The order's owner confirms receipt of a `DELIVERED` order, moving it to `COMPLETED` and recording who and when.

**Independent Test**: A `DELIVERED` order of the customer completes on confirmation; another customer's order answers not-found; a non-`DELIVERED` order is refused.

### Tests for User Story 3 ⚠️

- [X] T033 [P] [US3] Use-case tests `internal/modules/order/application/implement/completion_test.go`: `ConfirmReceived` drives `DELIVERED → COMPLETED`, stamps `CompletedAt` and `CompletedBy = customer`, audits `ORDER_COMPLETED`; refuses a non-`DELIVERED` order (naming the state) and another customer's order as not-found (FR-011, FR-014, FR-018)
- [X] T034 [P] [US3] HTTP tests `internal/modules/order/presentation/http/orders_test.go`: `POST /orders/{id}/complete` returns `200`; `404` for another customer's order; `409` when the order is not `DELIVERED`; `401` with no session (FR-011, FR-018)

### Implementation for User Story 3

- [X] T035 [US3] In `internal/modules/order/application/implement/completion.go`: add `ConfirmReceived(in OrderRefInput)` — take the owner from the session, lock the order (foreign ≡ not-found), drive the domain `Complete`, stamp `CompletedAt`/`CompletedBy`, persist and audit, all in one transaction. Add `ConfirmReceived` to the `OrderService` interface and **remove the superseded `CompleteByAdmin`** and the legacy lifecycle `Complete(ctx, orderID)` from the interface and `admin.go`/`lifecycle.go`, updating their call sites in `internal/modules/order/application/implement/{admin_test.go,notify_test.go,lifecycle_test.go}` (FR-011, FR-014, FR-018; H4, I2)
- [X] T036 [US3] In `internal/modules/order/presentation/dto/dto.go`, `presentation/http/{handler.go,router.go}`: add `POST /orders/{id}/complete` (empty body) with Swagger annotations, and **remove** `POST /admin/orders/{id}/complete` — removing its handler function, its route, and `CompleteByAdmin` from the handler's `Service` interface in `handler.go` (research D7; Constitution VIII; M3, I7)
- [X] T037 [US3] Extend `internal/modules/order/presentation/http/order_integration_test.go`: delivery → customer confirms → `COMPLETED` with `completed_at`/`completed_by` set; another customer is refused; a removed admin complete route answers not-found/method-not-allowed; and assert completion wrote **no** `inventory_transactions` row and left `total_amount` unchanged (SC-002, SC-006, FR-020)

**Checkpoint**: The customer completes a delivered order, and the old admin completion is gone.

---

## Phase 6: User Story 4 - A delivered order completes itself after 5 days (Priority: P2)

**Goal**: A `DELIVERED` order not confirmed within five days completes itself exactly once; an order not yet delivered never completes by time.

**Independent Test**: A `DELIVERED` order past its window becomes `COMPLETED` once (even across repeats); a `SHIPPING` order left long stays `SHIPPING`.

### Tests for User Story 4 ⚠️

- [X] T038 [P] [US4] Use-case and sweeper tests `internal/modules/order/application/implement/completion_test.go` and `internal/modules/order/presentation/worker/sweeper_test.go` (injected clock): a `DELIVERED` order past `delivered_at + 5 days` is completed with no actor, exactly once across repeats; an order not `DELIVERED` is never selected; a customer confirm racing the sweep yields `COMPLETED` once (FR-012, FR-013, FR-014)

### Implementation for User Story 4

- [X] T039 [US4] In `internal/modules/order/application/implement/completion.go`: add `AutoCompleteDelivered(ctx)` — select `DELIVERED` orders with `delivered_at <= now − DeliveryCompletionWindow` (oldest first) through `ListAwaitingCompletion`, and for each run the same domain `Complete` path with no actor, skipping an order already completed (idempotent); add `AutoCompleteDelivered` to the `OrderService` interface (research D6)
- [X] T040 [US4] Generalize `internal/modules/order/presentation/worker/sweeper.go` so one tick runs **two** idempotent sweeps — expiry and completion — via an application capability exposing both (`ExpireOrders`, `AutoCompleteDelivered`); update `sweeper_test.go` to prove both are invoked per tick and a failure of one does not stop the other; wire it in `cmd/api/main.go` (research D6)
- [X] T041 [US4] Extend `internal/modules/order/presentation/http/order_integration_test.go` (or `completion` integration test): a `DELIVERED` order past the window completes exactly once on real PostgreSQL; a customer confirmation and the sweep racing produce a single `COMPLETED` (SC-002)

**Checkpoint**: Delivered orders complete themselves after five days, never twice.

---

## Phase 7: User Story 5 - The customer sends feedback to the artist (Priority: P3)

**Goal**: The customer sends one order-level feedback to the artist once the order is delivered.

**Independent Test**: Feedback on a `DELIVERED` order is stored and readable by the artist; a second submission is refused; feedback before delivery is refused.

### Tests for User Story 5 ⚠️

- [X] T042 [P] [US5] Use-case tests `internal/modules/order/application/implement/feedback_test.go`: `SubmitFeedback` stores one row for the owner when the order is `DELIVERED`/`COMPLETED`; refuses a non-`DELIVERED`/`COMPLETED` order (`ORDER_FEEDBACK_NOT_ALLOWED`, naming the state), a second feedback (`ORDER_FEEDBACK_ALREADY_SENT`), empty/blank content and content over 2000 characters (validation) and another customer's order as not-found (FR-015, FR-016, FR-017, FR-018)
- [X] T043 [P] [US5] HTTP tests `internal/modules/order/presentation/http/orders_test.go`: `POST /orders/{id}/feedback` returns `200`; `400 VALIDATION_ERROR` on empty `content` and on `content` over 2000 characters; `409` for the two feedback refusals; `404` for another customer's order (FR-015, FR-016)

### Implementation for User Story 5

- [X] T044 [US5] Create `internal/modules/order/application/implement/feedback.go`: `SubmitFeedback(in FeedbackInput)` — owner from the session, lock the order, require `DELIVERED`/`COMPLETED`, refuse an existing feedback, validate content (1–2000 characters), insert the row, all in one transaction; add `SubmitFeedback` to the `OrderService` interface (FR-015, FR-016, FR-017, research D8)
- [X] T045 [US5] In `internal/modules/order/presentation/dto/dto.go` and `presentation/http/{handler.go,router.go}`: add `POST /orders/{id}/feedback` with body validation (`content` required, 1–2000 characters) and Swagger annotations (research D7; Constitution VIII; M3)
- [X] T046 [US5] Extend `internal/modules/order/presentation/http/order_integration_test.go`: feedback persists and reads back; the `order_feedback_order_key` unique index refuses a concurrent second insert on real PostgreSQL (SC-005)

**Checkpoint**: Feedback reaches the artist, at most once per order.

---

## Phase 8: User Story 6 - Both sides are notified as the order moves (Priority: P3)

**Goal**: The customer is emailed on `SHIPPING`/`DELIVERED`/`COMPLETED`; the artist is emailed on feedback; a send failure never fails the operation.

**Independent Test**: Dispatch/deliver/complete → the customer's email records each status change; feedback → the artist's email records it; a notifier error leaves the order/feedback correct.

### Tests for User Story 6 ⚠️

- [X] T047 [P] [US6] Use-case tests `internal/modules/order/application/implement/notify_test.go` (over a fake notifier): the customer is notified on `SHIPPING`, `DELIVERED` and `COMPLETED` (including the automatic completion); the artist is notified when feedback arrives; a notifier error does not fail the operation (FR-021, FR-022, FR-023)

### Implementation for User Story 6

- [X] T048 [US6] In `internal/modules/order/application/implement/{delivery.go,completion.go,feedback.go}`: compose and send the notifications after the transaction commits (best-effort, errors logged) — the customer on each new status, the artist on feedback — reusing the existing `Notifier` port and `notifyCustomerStatusChange` (research D12)
- [X] T049 [US6] Extend `internal/modules/order/presentation/http/order_integration_test.go` and `internal/modules/order/application/implement/notify_test.go` to assert the notifications fire for the new status changes and the feedback, and that a failing notifier leaves the order and feedback correct (SC-005, FR-023)

**Checkpoint**: Both sides are notified, and email never breaks an order operation.

---

## Phase 9: Polish & Cross-Cutting Concerns

**Purpose**: Documentation, the frontend guide, the ADR, Swagger, deferred debt, and the close-out gates.

- [ ] T050 [P] Update `docs/api-reference.md`: §1.4 add the five new error codes; §9 the changed/new/removed endpoints, the **seven-value** status table (`SHIPPED` → `SHIPPING`, `DELIVERED` added), and the completion-by-customer/120-hour note; **§11 the admin-dashboard section** (revenue now counts `PAID`/`SHIPPING`/`DELIVERED`/`COMPLETED`; `recentOrders[].status` lists seven values); §12 the summary tables; and §14 the **change log** entry for the breaking enum change (Constitution VIII; M1)
- [ ] T051 [P] Update `docs/modules/07-order.md`: the new state machine, the dispatch/delivery/confirm flows, the photos, the feedback, the 120-hour completion and the media dependency
- [ ] T052 [P] Update `docs/modules/09-shipping.md` (note that dispatch evidence photos are delivered by this feature, not the full module 09) and `docs/modules.md` (its status-flow line gains `SHIPPING`/`DELIVERED` and drops `SHIPPED`) (M1)
- [ ] T053 [P] Create `docs/decisions/023-order-delivery-and-completion.md`: supersede ADR 018's post-`PAID` edges (the `SHIPPED` → `SHIPPING` rename and the new `DELIVERED`, dispatch/delivery photos, completion by the customer or a 120-hour sweep, order-level feedback, the dependency-free per-module image rule, and the widened statistics earned-states set — noting it supersedes the same set named in ADR 022); ADR 018 is **not** edited
- [ ] T054 [P] Create `specs/019-order-delivery/deferred.md`: the failed-delivery/returns gap (no `FAILED`/`RETURNED`, no refund path), the transfer scope across `SHIPPING`/`DELIVERED`, the shipping-fee/carrier tracking (module 09), product reviews (module 13), in-app notifications (module 12); state what each is, why out of scope, and what unblocks it (Constitution §Governance)

### Frontend Integration Guide *(bắt buộc — hiến pháp §Governance)*

- [ ] T055 **Có đổi bề mặt client** → viết `specs/019-order-delivery/frontend-guide.md`: liệt kê từng endpoint **mới/đổi/bị xoá** (`POST /admin/orders/{id}/ship` **đổi sang multipart**, `POST /admin/orders/{id}/deliver` **mới**, `POST /orders/{id}/complete` **mới**, `POST /orders/{id}/feedback` **mới**, `POST /admin/orders/{id}/complete` **bị xoá**), enum `OrderStatus` **bảy giá trị** (đánh dấu `SHIPPED` **bị đổi tên** thành `SHIPPING`, `DELIVERED` **mới**), mọi trường DTO thêm vào detail (`deliveredAt`, `completedAt`, `deliveryPhotos`, `feedback`), và mọi mã lỗi mới; **đánh dấu `specs/010-order-confirmation/frontend-guide.md` đã bị thay thế**, và sửa/hiệu chỉnh `specs/017-admin-dashboard/frontend-guide.md` (enum `recentOrders[].status` nay bảy giá trị) (M1)
- [ ] T056 **Đồng bộ guide** ở phase cuối, SAU khi mã nguồn ổn định: đối chiếu `specs/019-order-delivery/frontend-guide.md` với mã nguồn — từng route trong `presentation/http/router.go`, từng trường DTO trong `presentation/dto/dto.go` và `application/dto/dto.go`, từng giá trị enum `OrderStatus` trong `domain/constant/codes.go`, từng mã HTTP trong `presentation/http/errors.go` — **có đếm**; sửa mọi chỗ lệch, ghi rõ phần chưa kiểm nếu có

### Swagger annotations *(bắt buộc — feature có endpoint)*

- [ ] T057 Regenerate `docs/swagger/` with `make swagger` after all handler changes (the per-endpoint Swagger annotations are added in T026/T031/T036/T045) so `make swagger-check` (in `make check`) passes (Constitution VIII; D1)

### Phát hiện ngoài phạm vi *(hiến pháp §Governance)*

- [ ] T058 Rà lại các phát hiện ngoài phạm vi và ghi hết vào `specs/019-order-delivery/deferred.md`, KHÔNG để nguyên `- [ ]` ở file này; nêu rõ **là gì**, **vì sao ngoài phạm vi**, **điều gì gỡ được** nó; nếu một phát hiện làm mất hiệu lực bằng chứng của một task đã tick `[X]` thì nêu rõ số task đó

### Close-out

- [ ] T059 Normalize the new files to UTF-8 (LF) and check for BOM across `specs/019-order-delivery/`, `migrations/00018_order_delivery.sql` and the touched `internal/modules/order/**` files
- [ ] T060 Run `make lint` and `make test`
- [ ] T061 Run `make test-integration` (needs Docker) and confirm it did not skip
- [ ] T062 Run `quickstart.md` validation
- [ ] T063 Run `make check` and confirm `swagger-check` passes

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies
- **Foundational (Phase 2)**: Depends on Setup; **blocks all user stories**
- **User Stories (Phase 3–8)**: All depend on Foundational
  - US1 (P1) and US2 (P1) are the MVP (dispatch + delivery with photos); US3–US6 follow
  - US2 reuses US1's `delivery.go` shape; US3 completes a delivered order; US4 reuses US3's completion path; US5 is independent of the delivery chain; US6 attaches to the transitions US1–US5 introduce
- **Polish (Phase 9)**: Depends on all desired stories being complete

### User Story Dependencies

- **US1 (P1)**: after Foundational; no dependency on other stories
- **US2 (P1)**: after US1 (shares `delivery.go`; a delivered order must first be in transit)
- **US3 (P2)**: after US2 (needs a `DELIVERED` order); independently testable with a row set to `DELIVERED`
- **US4 (P2)**: after US3 (reuses the same completion path)
- **US5 (P3)**: after Foundational; independently testable with a `DELIVERED`/`COMPLETED` order
- **US6 (P3)**: after the transitions it notifies (US1–US5)

### Within Each User Story

- Tests first (they must fail), then implementation; core before endpoints

### Parallel Opportunities

- All `[P]` tasks in Phase 2 are different files and run together (constants, errors, model entities, shared image rule, repository interface, ports, dto, mapper)
- After Foundational, the test files of a story run together; US5 can be worked in parallel with the US1–US4 chain
- The Phase 9 documents (T050–T054) are independent files

---

## Parallel Example: Foundational

```bash
# Different files, no dependencies:
Task: "constant codes — T002"
Task: "domain errors — T003"
Task: "delivery_photo.go — T006"
Task: "feedback.go — T007"
Task: "order-domain photo_image.go — T008"
Task: "order-domain photo_image_test.go — T009"
Task: "ports.go (MediaStore alias only) — T012"
Task: "dto.go — T013"
```

---

## Implementation Strategy

### MVP First (User Stories 1 and 2)

1. Phase 1: Setup
2. Phase 2: Foundational (CRITICAL — blocks all stories)
3. Phase 3: User Story 1 (dispatch with photos)
4. Phase 4: User Story 2 (delivery with photos)
5. **STOP and VALIDATE**: a paid order is dispatched with evidence, then delivered with evidence

### Incremental Delivery

1. Setup + Foundational → foundation ready
2. US1 + US2 → the dispatch/delivery journey with photos (MVP)
3. US3 → the customer completes a delivered order
4. US4 → the five-day automatic completion
5. US5 → order-level feedback
6. US6 → notifications
7. Polish → docs, ADR, frontend guide, Swagger, deferred, gates

### Notes

- The order holds **no** stock of its own; after `PAID` this feature touches neither stock nor money
- Money is integer minor units + currency; the total carries **no** shipping fee (ADR 015 §5)
- The paid transition is module 08's and has **no HTTP endpoint**; dispatch/delivery are the first post-paid operator actions to have one
- Migration is `00018`; the rename backfills `SHIPPED → SHIPPING` before the widened check
- The `SHIPPED` → `SHIPPING` rename is a **breaking client contract change** — the frontend guide is mandatory
- Commit after each phase once its gate is green; never push/tag without explicit authorization
