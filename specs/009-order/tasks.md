---
description: "Task list for Order Checkout (module 07)"
---

# Tasks: Order Checkout

**Input**: Design documents from `specs/009-order/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/{openapi.yaml, error-codes.md}, quickstart.md

**Tests**: Included. The constitution mandates test-first coverage for **order totals, state transitions (positive + negative) and the stock hold**, and the plan requires the guarantees a fake cannot prove to be proven against real PostgreSQL.

**Organization**: Tasks are grouped by user story so each story is independently implementable and testable.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (US1–US5)
- Exact file paths are given in each task

## Path Conventions

- Module root: `internal/modules/order/`
- Cross-module contracts: `internal/contracts/`
- Providing modules' adapters: `internal/modules/{cart,inventory,auth}/infrastructure/implement/`
- Migrations: `migrations/`
- Composition root: `cmd/api/main.go`

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Confirm the ground is ready and the next migration number is free.

- [X] T001 Confirm the working tree baseline: run `make lint` and `make test` green, and confirm `migrations/` ends at `00008_cart.sql` so the new migration is `00009_order.sql`

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The module skeleton, the three new cross-module contracts with their adapters, the schema, the domain (with the state machine) and the storage adapter. **Nothing in any user story can be built without these.**

**⚠️ CRITICAL**: No user story work can begin until this phase is complete

### Cross-module contracts and their adapters (the widest reach in the project)

- [X] T002 [P] Create `internal/contracts/cart.go`: `CartCheckout` interface (`CartLines(ctx, userID) ([]CartLine, error)`, `ClearCart(ctx, userID) error`) and the `CartLine{ProductID, Quantity, UnitPriceAmount, Currency}` struct, with a package comment naming module 06 as the provider (research D1)
- [X] T003 [P] Extend `internal/contracts/inventory.go`: add the `InventoryReservation` interface (`Reserve`, `Release`, `ApplySale`, `HoldWindow() time.Duration`) with its input types and a comment naming module 05 as the provider and module 07 as the consumer that closes feature 007's `D1` (research D4, D5, D6)
- [X] T004 [P] Create `internal/contracts/account.go`: `AccountLookup` interface (`UserIDByEmail(ctx, email) (uuid.UUID, error)`) and a sentinel `ErrAccountNotFound`, naming module 01 as the provider (research D8)
- [X] T005 [P] In module 06: add `ClearLines(ctx, cartID)` to `internal/modules/cart/domain/repository/cart.go` and its postgres adapter; create `internal/modules/cart/infrastructure/implement/checkout/checkout.go` implementing `CartCheckout` over the cart repository; add a compile-time assertion and a unit test `internal/modules/cart/infrastructure/implement/checkout/checkout_test.go`
- [X] T006 [P] In module 05: create `internal/modules/inventory/infrastructure/implement/reservation/reservation.go` implementing `InventoryReservation` over the inventory service (`Reserve`/`Release`/`ApplySale`, per-line source reference); add a compile-time assertion and a unit test `.../reservation/reservation_test.go`
- [X] T007 [P] In module 01: create `internal/modules/auth/infrastructure/implement/account/account.go` implementing `AccountLookup` over the auth repository; add a compile-time assertion and a unit test `.../account/account_test.go`

### Schema

- [X] T008 Create `migrations/00009_order.sql`: the `orders` and `order_items` tables with every column, check, index and foreign key in `data-model.md` (status check, currency checks, positive-total/quantity/price checks, owner/admin/expiry/ordering indexes, cascading order-items FK, unique `(order_id, product_id)`, no FK on `orders.user_id` or `order_items.product_id`)
- [X] T009 Create `migrations/order_guard_test.go` asserting the storage constraints directly: an out-of-set `status` and `currency` are refused, a non-positive `total_amount`, `unit_price_amount` and `quantity` are refused, the unique `(order_id, product_id)` is enforced, a deleted order takes its lines, and a deleted product leaves its lines

### Module skeleton and domain

- [X] T010 [P] Create the module skeleton `internal/modules/order/` with the four layers (`domain/{constant,error,model,repository}`, `application/{dto,interface,implement,mapper}`, `infrastructure/implement/{postgres,auditor}`, `presentation/{dto,http,worker}`) and `doc.go` for each package
- [X] T011 [P] Create `internal/modules/order/domain/constant/codes.go`: the five status constants, the error codes from `contracts/error-codes.md`, and the audit actions `ORDER_SHIPPED`, `ORDER_COMPLETED`, `ORDER_TRANSFERRED`
- [X] T012 [P] Create `internal/modules/order/domain/error/errors.go`: the sentinel errors (`ErrNotFound`, `ErrCartEmpty`, `ErrItemNotPurchasable`, `ErrItemPriceChanged`, `ErrQuantityExceedsAvailable`, `ErrNoAddress`, `ErrStateTransitionInvalid`, `ErrNotTransferable`, `ErrTransferTargetNotFound`) each carrying the message pieces the presentation layer needs
- [X] T013 Create `internal/modules/order/domain/model/order_test.go` **first** (fails until T014): the state machine's every edge, one positive and one negative per move, a refused move names the current state and leaves the order unchanged, and the total equals the sum of the line snapshots with **no** shipping fee and no rounding
- [X] T014 Create `internal/modules/order/domain/model/order.go`: the `Order` entity and `OrderLine`, the five states and the allowed-edge table, the transition functions (`MarkPaid`, `Cancel`, `Ship`, `Complete`) that refuse a move the current state does not allow, and the total computed from the lines (FR-008, FR-009, FR-010)

### Repository, ports, DTO, mapper

- [X] T015 [P] Create `internal/modules/order/domain/repository/order.go`: `Create`, `FindByID`, `FindByOwner`, `ListOwner`, `ListAll`, `LockByID` (for a transition), `UpdateStatus`, `UpdateOwner` (research D10, D14)
- [X] T016 [P] Create `internal/modules/order/application/interface/ports.go`: the `OrderService` interface, the `UnitOfWork` and `Clock` ports, and the imported contract aliases (`CartCheckout`, `ProductCatalog`, `InventoryAvailability`, `InventoryReservation`, `CustomerLookupService`, `AccountLookup`, auditor)
- [X] T017 [P] Create `internal/modules/order/application/dto/dto.go`: the checkout input, the list/pagination input, the transfer input, and the read outputs the use cases return
- [X] T018 [P] Create `internal/modules/order/application/mapper/mapper.go`: order/line model → read DTO, money as minor units + currency
- [X] T019 Create `internal/modules/order/infrastructure/implement/postgres/order.go`: the postgres order repository over the shared generic repository, including the ordered line load and the owner/admin lists (newest first)
- [X] T020 Create `internal/modules/order/infrastructure/implement/auditor/auditor.go`: the audit writer for `ORDER_SHIPPED`, `ORDER_COMPLETED`, `ORDER_TRANSFERRED`
- [X] T021 Create `internal/modules/order/infrastructure/implement/postgres/order_integration_test.go`: the adapter's storage guarantees against real PostgreSQL — create/read round trip, ordered lines, owner list newest-first, admin list newest-first, and that a removed product leaves the line

**Checkpoint**: Foundation ready — the contracts, schema, domain and storage adapter exist; user stories can now be built.

---

## Phase 3: User Story 1 - A customer checks out (Priority: P1) 🎯 MVP

**Goal**: A signed-in customer turns their cart into an order — snapshot of each line and the address, goods held, cart emptied — in one operation, refusing an empty or unbuyable cart.

**Independent Test**: With a cart holding two buyable products, check out and confirm an order was created carrying a snapshot of both lines and the delivery address, that the goods are held (available fell, physical did not move) and the cart is now empty.

### Tests for User Story 1 ⚠️

- [X] T022 [P] [US1] Use-case tests `internal/modules/order/application/implement/checkout_test.go`: a successful checkout snapshots each line and the address, holds each line, and clears the cart in one transaction; and the refusals — empty cart, an off-sale/removed product, a price that changed after adding, a quantity above what is available (naming the available amount), a customer with no address, and a line whose hold cannot be taken because another customer holds the last unit (FR-017, `Reserve` returning an error) — each leaving nothing created
- [X] T023 [P] [US1] HTTP tests `internal/modules/order/presentation/http/checkout_test.go`: `POST /orders` returns `201` with the created order, `401` without a session, and each `409`/`400` code from `contracts/error-codes.md`
- [X] T024 [P] [US1] Error-mapping test `internal/modules/order/presentation/http/errors_test.go`: each domain sentinel maps to its documented status and code, and an unknown failure maps to `INTERNAL_ERROR`

### Implementation for User Story 1

- [X] T025 [US1] Create `internal/modules/order/application/implement/checkout.go`: the checkout use case — read the cart through `CartCheckout`, re-read each line's current facts through `ProductCatalog` and `InventoryAvailability`, refuse on any unbuyable/changed/over-available line or missing address, snapshot the lines and address (via `CustomerLookupService`), create the order with `expires_at = now + InventoryReservation.HoldWindow()`, hold every line through `InventoryReservation`, and clear the cart — all inside one `UnitOfWork` (FR-001–FR-007, FR-013, FR-016, FR-017)
- [X] T026 [US1] Create `internal/modules/order/presentation/dto/dto.go`: the request/response shapes for `POST /orders`
- [X] T027 [US1] Create `internal/modules/order/presentation/http/{handler.go,router.go,errors.go}`: the checkout handler and its route, the error mapping, and the actor-from-session extraction (FR-020)
- [X] T028 [US1] In `cmd/api/main.go`: mount the order module behind `/api/v1`, wire the `CartCheckout`, `ProductCatalog`, `InventoryAvailability`, `InventoryReservation`, `CustomerLookupService` and `AccountLookup` adapters into the order service, and depend on nothing else
- [X] T029 [US1] Create `internal/modules/order/presentation/http/order_integration_test.go`: checkout end-to-end against real PostgreSQL — the order is created with its snapshots and address, the goods are held (available fell, physical unchanged), the cart is empty, and **two concurrent checkouts of one cart produce exactly one order** (SC-001, SC-002)

**Checkpoint**: User Story 1 is fully functional and testable on its own — a cart becomes an order.

---

## Phase 4: User Story 2 - The order's selling life can be trusted (Priority: P1)

**Goal**: The order moves through its five states by the allowed edges only; a refused move names the current state; confirming payment turns the hold into a sale exactly once; an unpaid order expires and returns its goods exactly once.

**Independent Test**: Drive one order through every allowed move to completed, attempt a forbidden move and confirm it is refused with the current state named and the order untouched; let an unpaid order pass its window and confirm it cancels itself and its goods are available again.

### Tests for User Story 2 ⚠️

- [X] T030 [P] [US2] Use-case tests `internal/modules/order/application/implement/lifecycle_test.go`: `MarkPaid` turns the hold into a sale through `InventoryReservation.ApplySale` with the per-line source reference, exactly once even when replayed; `Cancel` returns the hold through `Release`, exactly once; `Ship`/`Complete` call the transitions and refuse an illegal move naming the state
- [X] T031 [P] [US2] Sweeper tests `internal/modules/order/presentation/worker/sweeper_test.go` with an injected clock: an unpaid order past its window becomes `CANCELLED` and its goods are released, once; a paid/other order is untouched

### Implementation for User Story 2

- [X] T032 [US2] Create `internal/modules/order/application/implement/lifecycle.go`: `MarkPaid`, `Cancel`, `Ship`, `Complete` — each in a `UnitOfWork`, locking the order (`LockByID`), driving the domain transition, and calling module 05 (`ApplySale` on pay, `Release` on cancel) in the same transaction (FR-009–FR-012, FR-014, FR-015)
- [X] T033 [US2] Create `internal/modules/order/presentation/worker/sweeper.go`: the expiry sweep — find unpaid orders past `expires_at` (`orders_expiry_idx`) and cancel then release each, idempotent against module 05's own sweep (FR-012, research D6)
- [X] T034 [US2] In `cmd/api/main.go`: start and stop the expiry sweeper with the application lifecycle, mirroring module 05's
- [X] T035 [US2] Create `internal/modules/order/application/implement/lifecycle_integration_test.go`: against real PostgreSQL — the status check rejects an unlisted state, confirming payment twice sells the held quantity **once**, and an expired order frees its goods **exactly once** across both the order and inventory sweeps (SC-003, SC-004)

**Checkpoint**: User Story 1 and 2 both work; the states and the hold are trustworthy.

---

## Phase 5: User Story 3 - A customer sees and manages only their own orders (Priority: P2)

**Goal**: A customer lists and reads the orders they placed, and cancels one that is still awaiting payment; no customer can see or change another's.

**Independent Test**: With two customers, place an order for one and confirm the other's list does not contain it and that neither can read or change the other's order by any request.

### Tests for User Story 3 ⚠️

- [X] T036 [P] [US3] Use-case tests `internal/modules/order/application/implement/orders_test.go`: the owner list is the caller's only, newest first, paginated; reading another owner's order answers not-found; cancelling an unpaid order returns its goods; cancelling a paid order is refused
- [X] T037 [P] [US3] HTTP tests `internal/modules/order/presentation/http/orders_test.go`: `401` without a session; the list and detail return only the caller's; another customer's identifier answers `404 ORDER_NOT_FOUND`; cancel returns `200` then `409 ORDER_STATE_TRANSITION_INVALID` on the second attempt

### Implementation for User Story 3

- [X] T038 [US3] Create `internal/modules/order/application/implement/orders.go`: `ListMine`, `GetMine`, `CancelMine` — the owner taken from the session, the cross-account read answering not-found, cancellation using the US2 `Cancel` transition (FR-018–FR-020)
- [X] T039 [US3] Add to `internal/modules/order/presentation/http/{handler.go,router.go}`: `GET /orders`, `GET /orders/{id}`, `POST /orders/{id}/cancel`, session-guarded
- [X] T040 [US3] Extend `internal/modules/order/presentation/http/order_integration_test.go`: one owner's orders are not another's, reading another's answers `404`, and cancelling an unpaid order makes its goods available again (SC-005)

**Checkpoint**: A customer's own orders and cancellation work independently.

---

## Phase 6: User Story 4 - The operator runs the order desk (Priority: P2)

**Goal**: An administrator lists every order, reads one in full, and advances fulfilment (ship, complete), each act audited; a customer's session is refused.

**Independent Test**: As an administrator, list orders, open one, mark it shipped and then completed, confirm the state changes and each act left an audit entry; confirm a customer's session is refused.

### Tests for User Story 4 ⚠️

- [X] T041 [P] [US4] Use-case and HTTP tests `internal/modules/order/application/implement/admin_test.go` and `internal/modules/order/presentation/http/admin_test.go`: the admin list is every order, newest first, with owner, state and total; ship then complete succeed and each writes an audit entry; a customer's session is refused `403 FORBIDDEN`; an illegal move is refused naming the state

### Implementation for User Story 4

- [X] T042 [US4] Create `internal/modules/order/application/implement/admin.go`: `ListAll`, `GetByIDAdmin`, `ShipByAdmin`, `CompleteByAdmin` — listing every order, and ship/complete driving the US2 transitions and writing the audit entry (via `auditor`) in the same transaction (FR-021–FR-023)
- [X] T043 [US4] Add to `internal/modules/order/presentation/http/{handler.go,router.go}`: `GET /admin/orders`, `GET /admin/orders/{id}`, `POST /admin/orders/{id}/ship`, `POST /admin/orders/{id}/complete`, all behind the administrator-role guard
- [X] T044 [US4] Extend `internal/modules/order/presentation/http/order_integration_test.go`: the admin list/read, ship then complete, the resulting audit rows naming the order and the administrator, and a customer session refused (SC-005)

**Checkpoint**: The operator's desk works; the shop can be run.

---

## Phase 7: User Story 5 - A paid order can be handed to another account (Priority: P3)

**Goal**: An administrator transfers a **paid** order to another existing account, named by email, changing only the owner.

**Independent Test**: Transfer a paid order to a second account and confirm the order now belongs to the second account, its lines and state are unchanged, and no stock moved.

### Tests for User Story 5 ⚠️

- [X] T045 [P] [US5] Use-case and HTTP tests `internal/modules/order/application/implement/transfer_test.go` and `internal/modules/order/presentation/http/transfer_test.go`: a paid order's owner changes with lines/state/total unchanged and no stock movement; an email no account carries answers `404 ORDER_TRANSFER_TARGET_NOT_FOUND`; an unpaid order answers `409 ORDER_NOT_TRANSFERABLE`; a customer's session is refused `403`

### Implementation for User Story 5

- [X] T046 [US5] Create `internal/modules/order/application/implement/transfer.go`: `Transfer` — resolve the recipient through `AccountLookup` (`UserIDByEmail`), refuse a not-paid order, change only the owner, and write the `ORDER_TRANSFERRED` audit entry, all in one `UnitOfWork` (FR-024)
- [X] T047 [US5] Add to `internal/modules/order/presentation/http/{handler.go,router.go}`: `POST /admin/orders/{id}/transfer`, administrator-only
- [X] T048 [US5] Extend `internal/modules/order/presentation/http/order_integration_test.go`: after a transfer the order belongs to the recipient, its lines/state/total are unchanged and no product's physical or available stock moved (SC-006)

**Checkpoint**: All user stories are independently functional.

---

## Phase 8: Polish & Cross-Cutting Concerns

**Purpose**: Documentation, the frontend guide, Swagger, and the close-out gates.

- [X] T049 [P] Update `docs/api-reference.md`: the nine endpoints, their shapes and the new error codes
- [X] T050 [P] Update/rewrite `docs/modules/07-order.md` (the stub exists): the module's endpoints, states, the hold, the expiry and the transfer, marking that it closes feature 007's `D1`/`D2` and 006/007's `D3`, and adding module 01 (`auth`) to the module's dependency list (the transfer target lookup)
- [X] T051 [P] Update `docs/modules.md`: mark module 07 delivered in the V1.0 roadmap and add `auth` to module 07's dependency column (the transfer target lookup)
- [X] T052 [P] Create `docs/decisions/017-order-checkout.md`: the choices a reader would otherwise misread — the snapshot line and its informational `product_id`, the order-owned expiry beside module 05's hold, transfer as an owner change rather than a state, and the paid transition delivered but left for module 08 to drive
- [X] T053 [P] Create `specs/009-order/deferred.md`: the payment confirmation and PayOS provider (module 08), shipping fee and tracking (module 09), refunds/split orders/discount codes — each with what will close it; and note that this feature closes feature 007's `D1`/`D2` and 006/007's `D3`

### Frontend Integration Guide *(bắt buộc — hiến pháp §Governance)*

- [X] T054 **Có đổi bề mặt client** → viết `specs/009-order/frontend-guide.md`: liệt kê từng endpoint mới (`POST /orders`, `GET /orders`, `GET /orders/{id}`, `POST /orders/{id}/cancel`, `GET /admin/orders`, `GET /admin/orders/{id}`, `POST /admin/orders/{id}/ship`, `POST /admin/orders/{id}/complete`, `POST /admin/orders/{id}/transfer`) kèm method, path, **mới/đổi/bị xoá**, mọi trường JSON của DTO, mọi giá trị enum `OrderStatus`, và mọi mã lỗi mới
- [X] T055 **Đồng bộ guide** ở phase cuối, SAU khi mã nguồn ổn định: đối chiếu từng route, từng trường DTO, từng giá trị enum, từng mã HTTP với mã nguồn, có đếm số; sửa mọi chỗ lệch, ghi rõ phần chưa kiểm nếu có

### Swagger annotations *(bắt buộc — feature có endpoint)*

- [X] T056 Thêm annotation Swagger cho **mọi** handler (`@Summary`, `@Tags`, `@Param`, `@Success`, `@Failure`, `@Router`) rồi chạy `make swagger`; `make swagger-check` (trong `make check`) phải xanh (Constitution VIII)

### Phát hiện ngoài phạm vi *(hiến pháp §Governance)*

- [X] T057 Rà lại các phát hiện ngoài phạm vi và ghi hết vào `specs/009-order/deferred.md`, KHÔNG để nguyên `- [ ]` ở file này; nêu rõ **là gì**, **vì sao ngoài phạm vi**, **điều gì gỡ được** nó; nếu một phát hiện làm mất hiệu lực bằng chứng của một task đã tick `[X]` thì nêu rõ số task đó

### Close-out

- [X] T058 Normalize the new files to UTF-8 (LF); check for BOM
- [X] T059 Run `make lint` and `make test`
- [ ] T060 Run `make test-integration` (needs Docker) and confirm it did not skip
- [ ] T061 Run `quickstart.md` validation
- [ ] T062 Run `make check` and confirm `swagger-check` passes

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies
- **Foundational (Phase 2)**: Depends on Setup; **blocks all user stories**
- **User Stories (Phase 3–7)**: All depend on Foundational
  - US1 (P1) and US2 (P1) are the MVP and come first; US3–US5 follow
  - US3's cancel and US4's ship/complete reuse US2's transitions; US5 reuses the US2 state rules
- **Polish (Phase 8)**: Depends on all desired stories being complete

### User Story Dependencies

- **US1 (P1)**: after Foundational; no dependency on other stories
- **US2 (P1)**: after Foundational; defines the transitions and hold effects US3/US4/US5 use
- **US3 (P2)**: after Foundational; its cancel uses US2's `Cancel`; independently testable
- **US4 (P2)**: after Foundational; its ship/complete use US2's transitions; independently testable
- **US5 (P3)**: after Foundational; needs a paid order (US2's `MarkPaid` reachable in tests); independently testable

### Within Each User Story

- Tests first (they must fail), then implementation; core before endpoints

### Parallel Opportunities

- All `[P]` tasks in Phase 2 are different files and run together (the three contracts, the three adapters, and the domain/ports/dto/mapper files)
- After Foundational, the three test files of a story run together; US3, US4 and US5 can be worked in parallel by different people once US1/US2's transitions exist
- The Phase 8 documents (T049–T053) are independent files

---

## Parallel Example: Foundational contracts

```bash
# The three cross-module contracts and their adapters touch different files:
Task: "Create internal/contracts/cart.go (CartCheckout) — T002"
Task: "Extend internal/contracts/inventory.go (InventoryReservation) — T003"
Task: "Create internal/contracts/account.go (AccountLookup) — T004"
Task: "Cart adapter + ClearLines — T005"
Task: "Inventory reservation adapter — T006"
Task: "Auth account adapter — T007"
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Phase 1: Setup
2. Phase 2: Foundational (CRITICAL — blocks all stories)
3. Phase 3: User Story 1 (checkout)
4. **STOP and VALIDATE**: checkout works on its own — order created, goods held, cart empty
5. Demo if ready

### Incremental Delivery

1. Setup + Foundational → foundation ready
2. US1 → checkout (MVP)
3. US2 → the selling life and expiry
4. US3 → a customer's own orders
5. US4 → the operator's desk
6. US5 → transfer
7. Polish → docs, frontend guide, Swagger, gates

### Notes

- `[P]` = different files, no dependency
- The order holds **no** stock of its own: every hold/sale/release goes through `InventoryReservation` (module 05) — do not add a quantity column to `orders`
- Money is integer minor units + currency; the total is the sum of the line snapshots and carries **no** shipping fee (ADR 015 §5)
- The paid transition is delivered and tested but has **no HTTP endpoint** (module 08 drives it) — do not add one
- Commit after each phase once its gate is green; never push/tag without explicit authorization

---

## Phase 9: Convergence

**Purpose**: Đóng hai lỗ hổng **bằng chứng** mà một lượt converge phát hiện giữa các success
criterion / yêu cầu chức năng và các test đang có. Mã nguồn đã đáp ứng mọi FR-001..FR-024,
SC-001..SC-006, quyết định trong `plan.md`/`research.md`/`data-model.md` và các nguyên tắc hiến
pháp; hai task dưới chỉ thêm khẳng định còn thiếu, **không** sửa hành vi.

- [X] T063 [US1] Thêm khẳng định **`slug`** (link segment) của dòng đơn round-trip đúng trong snapshot — đặt `Slug` **khác** `Name` ở fixture rồi khẳng định `lines[].slug` bằng giá trị đã chụp — vào `internal/modules/order/application/implement/checkout_test.go` và `internal/modules/order/presentation/http/order_integration_test.go` per FR-002, SC-001 (`partial`). Adapter postgres (`scanLine`/`lineQuery`) và mapper (`Mapper.Line`) đã lưu và ánh xạ `slug`, nhưng **không** test nào khẳng định trường này (mọi fixture đặt `Slug == Name`, và các khẳng định chỉ kiểm `name`/`unitPrice`/`quantity`), nên một hồi quy làm rơi `slug` trên đường lưu/đọc/ánh xạ sẽ không bị bắt.
- [X] T064 [US1] Thêm khẳng định mỗi lần checkout bị từ chối — off-sale, đã xoá, giá đổi, vượt tồn, không có địa chỉ, và không giữ được hàng — **để giỏ nguyên vẹn** (`f.cart.cleared == false`, và danh sách dòng không đổi) vào `internal/modules/order/application/implement/checkout_test.go` per SC-002 (`partial`). Hiện chỉ ca giỏ rỗng khẳng định `!f.cart.cleared`; các ca từ chối còn lại chỉ khẳng định "không tạo gì", nên vế *"leaves the cart unchanged"* của SC-002 chưa được chứng minh cho chúng.
