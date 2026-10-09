---

description: "Task list for feature 008-cart"
---

# Tasks: Shopping Cart

**Input**: Design documents from `specs/008-cart/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md

**Tests**: Included. The constitution makes pricing one of the test-first critical areas, and this
module's substance is money — the line total and the subtotal — plus the quantity and availability
rules and the "can it be bought" decision. Each test task is written first and must fail before its
implementation task runs, and the guarantees a fake cannot prove — the two unique indexes, a line
surviving its product's removal, and two concurrent adds — are proven against real PostgreSQL.

**Organization**: One phase per user story, in the priority order the spec gives. US1 and US2 are
both P1 and ship together; US3 is the ownership guarantee. The two cross-module contracts and the
module's spine are Phase 2, shared by every story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (US1…US3)
- Include exact file paths in descriptions

## Path Conventions

Go web service inside a modular monolith, following `plan.md`:

- `internal/modules/cart/` — the new module, four layers
- `internal/contracts/` — the two cross-module interfaces this feature introduces or extends
- `internal/modules/product/`, `internal/modules/inventory/` — touched: one adapter each
- `migrations/` — the additive schema change
- `cmd/api/main.go` — composition
- `docs/` — documentation that must change with the code (Constitution VIII), plus `make swagger`

---

## Phase 1: Setup (Baseline)

**Purpose**: Establish a green baseline so a later failure is attributable to this feature. This
feature adds no project scaffolding — the module skeleton is Phase 2's work.

- [x] T001 Confirm the baseline is green and the migration number is free: run `make lint`, `make test` and `make test-integration` (all must exit 0, and the integration run must be proven to execute rather than skip), and confirm `migrations/` ends at `00007_inventory.sql` so the new migration is `00008`

**Checkpoint**: The tree is green and the next migration number is known.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The two cross-module contracts and their adapters, the two tables, the entity and its
money rule, the ports and the adapter — everything all three user stories stand on.

**⚠️ CRITICAL**: No user story work begins until this phase is complete. Every story needs the
adapter (T017); US1 needs both contracts (T002–T005); US2 needs the availability contract.

### The cross-module contracts (research D1, D2)

- [x] T002 [P] Declare `ProductCatalog`, `ProductSummary` and `ProductPrice` in `internal/contracts/product.go`: a **bulk** `Products(ctx, productIDs) ([]ProductSummary, error)` returning the facts of every requested product that exists (an unknown identifier is absent from the result, not an error). `ProductSummary` carries `ID`, `Name`, `Slug`, `OnSale` (**a boolean, deliberately not module 04's four-state enum**) and `Price`. The package MUST NOT import a module
- [x] T003 [P] Create `internal/contracts/inventory.go` declaring `InventoryAvailability` and `Availability`: a **bulk** `AvailableQuantity(ctx, productIDs) ([]Availability, error)` returning one entry per requested identifier, in order, with zero for a product that has no stock row. Document that availability is physical stock minus active holds, computed by the adapter at the inventory module's clock (no instant parameter)
- [x] T004 [P] Implement the product-side adapter in `internal/modules/product/infrastructure/implement/catalog/catalog.go`, satisfying `contracts.ProductCatalog` over the product repository: load the requested products, map each to `ProductSummary` (`OnSale` is `sell_state == ACTIVE`; the price is the product's). Add the compile-time assertion `var _ contracts.ProductCatalog = (*Adapter)(nil)` **in the adapter's own package**. Unit test in `catalog_test.go`
- [x] T005 [P] Implement the inventory-side adapter in `internal/modules/inventory/infrastructure/implement/availability/availability.go`, satisfying `contracts.InventoryAvailability` over the inventory repository: for each identifier, `available = Level − ActiveHeld(clock.Now())`, with a missing level row answering zero. Add the compile-time assertion in the adapter's own package. Unit test over a fake repository, including a product with an active hold (available below physical)

### Schema

- [x] T006 Write `migrations/00008_cart.sql` following `docs/development/migration.md`: `carts` (`id` PK, `user_id` NOT NULL with a **unique index** and a cascading FK to `users`, timestamps) and `cart_items` (`id` PK, `cart_id` cascading FK, `product_id` NOT NULL **with no foreign key** (the loose reference of research D3), `quantity` with `CHECK (quantity >= 1)`, `unit_price_amount` with `CHECK (> 0)`, `currency` with a three-uppercase-letter check, timestamps, and a **unique index on `(cart_id, product_id)`**) — plus `COMMENT ON` where the purpose is not obvious, and both `-- +goose Up` and `-- +goose Down` sections
- [x] T007 Add `migrations/cart_guard_test.go` asserting the migration text carries every constraint and index **and** drops it in the down section, matching `migrations/product_guard_test.go`. It must assert the two unique indexes are present (US1/US3 rest on them) and that `cart_items.product_id` carries **no** foreign key (D3), the negative half of the loose reference

### Domain

- [x] T008 [P] Create the module skeleton under `internal/modules/cart/` with the four layers from `plan.md` — `domain/{constant,error,model,repository}`, `application/{dto,interface,implement,mapper}`, `infrastructure/implement/postgres`, `presentation/{dto,http}` — each package carrying a `// Package …` comment (revive enforces this)
- [x] T009 [P] Define the module's machine codes in `internal/modules/cart/domain/constant/codes.go` (`CART_PRODUCT_NOT_PURCHASABLE`, `CART_QUANTITY_EXCEEDS_AVAILABLE`), each with a comment (contracts/error-codes.md)
- [x] T010 [P] Define the module's sentinel errors in `internal/modules/cart/domain/error/errors.go` — a not-purchasable carrier, a quantity-exceeds-available carrier carrying the available quantity, and an invalid-value carrier that names the offending field (FR-006, FR-007, FR-005)
- [x] T011 [P] Implement the cart entity and its rules in `internal/modules/cart/domain/model/cart.go`: a `Cart` holding lines, a `CartLine` carrying product, quantity and captured price, the positive-whole-quantity rule, the line total and the subtotal as `int64` minor units (FR-009), and the `buyable` decision (on sale and availability at least the quantity) with the short-line case that reports the available quantity (research D6, D10)
- [x] T012 Write the domain tests in `internal/modules/cart/domain/model/cart_test.go` **before** T011 is finished, and confirm they fail: the subtotal equals the sum of quantity times captured price with no rounding; a non-positive or fractional quantity is refused naming the field; `buyable` is true only when on sale and available at least the quantity; a short line reports the available quantity; an off-sale or gone line reports no quantity

### Repository, ports and the adapter

- [x] T013 [P] Declare the repository contract in `internal/modules/cart/domain/repository/cart.go` — find a cart by owner, create one for an owner, lock it inside the caller's transaction, upsert a line (summing the quantity of a product already held), set a line's quantity, delete a line, and list a cart's lines in a stable order. It MUST NOT open a transaction (Constitution I)
- [x] T014 [P] Declare the application ports in `internal/modules/cart/application/interface/ports.go`: the `CartService` use-case surface, the `ProductCatalog` and `InventoryAvailability` aliases (from `internal/contracts`), the `UnitOfWork` port and a `Clock` port. Add the actor port in `actor.go`, taking the acting customer from the context the session filled, exactly as the other modules do
- [x] T015 [P] Define the application DTOs in `internal/modules/cart/application/dto/dto.go` — the add/change/remove inputs, the cart view (lines and subtotal), and each line's product facts, quantity, captured price, total, `buyable` and optional available quantity
- [x] T016 [P] Implement the single mapper in `internal/modules/cart/application/mapper/mapper.go`; it stays the only place a model becomes a DTO
- [x] T017 Implement the PostgreSQL adapter in `internal/modules/cart/infrastructure/implement/postgres/cart.go`, embedding the shared `Base` with an **explicit `Columns` projection**, binding every value as a parameter: create-on-first-add under the unique index, `SELECT … FOR UPDATE` to lock a cart for a write, an upsert on `(cart_id, product_id)` that sums the quantity, a set-quantity update, a delete, and a stable-ordered line read. It MUST NOT open a transaction
- [x] T018 Write `internal/modules/cart/infrastructure/implement/postgres/cart_integration_test.go` behind the `integration` tag, proving against real PostgreSQL what a fake cannot: `carts_user_key` refuses a second cart for one account; `cart_items_cart_product_key` refuses a second line for one product; `cart_items_quantity_ck`, `cart_items_unit_price_ck` and `cart_items_currency_ck` reject a value that bypassed the application; a cart line **survives** deleting its product (no foreign key); and two concurrent upserts to one cart leave one line whose quantity is the sum. Confirm each test fails if its constraint is removed from the migration

**Checkpoint**: The contracts, the tables, the entity, the ports and the adapter all exist and the
storage guarantees are proven. US1 and US2 can proceed.

---

## Phase 3: User Story 1 - A customer builds a cart (Priority: P1) 🎯 MVP

**Goal**: A signed-in customer adds products, changes a quantity, removes a line, and reads an
accurate cart with a correct subtotal; adding a product already held raises its line.

**Independent Test**: Add two products with quantities, change one quantity, remove one item, and read
the cart after each step, confirming the lines and the subtotal are what the actions imply.
`quickstart.md` scenarios 1, 2, 3, 4.

### Tests for User Story 1 ⚠️

> **NOTE: Write these tests FIRST, ensure they FAIL before implementing.**

- [x] T019 [P] [US1] Use-case tests in `internal/modules/cart/application/implement/cart_test.go` over an in-memory repository and fakes of the two contracts: adding a product creates one line carrying its name and its **captured price** (FR-002, FR-008); adding it again raises the line's quantity rather than adding a line (FR-002); changing a quantity updates the line (FR-003); removing a line drops it (FR-003); reading returns every line with product, quantity, captured price and line total, and the subtotal as the exact sum (FR-004, FR-009); a customer with no cart reads an empty cart whose subtotal is **null** (FR-004)
- [x] T020 [P] [US1] HTTP tests in `internal/modules/cart/presentation/http/cart_test.go`: `GET /cart` answers `200` with `lines: []` and `subtotal: null`; `POST /cart/items` answers the updated cart; `PATCH` and `DELETE` on a line behave as the contract declares; a malformed product identifier is `400 VALIDATION_ERROR` with the field named; a **fractional** quantity (`1.5`) is `400 VALIDATION_ERROR` naming `quantity` — the request DTO decodes `quantity` as a `json.Number` (as module 05 does) so a fractional value is a field error, not a decode failure (contracts/openapi.yaml)
- [x] T021 [P] [US1] Error-mapping tests in `internal/modules/cart/presentation/http/errors_test.go` covering every code the module can answer with, including that a field-level rejection is `400` with `details[].field` populated
- [x] T022 [P] [US1] Router test in `internal/modules/cart/presentation/http/router_test.go` asserting the four routes of research D7 resolve under `/cart` and that the group requires a session

### Implementation for User Story 1

- [x] T023 [US1] Implement the cart use cases in `internal/modules/cart/application/implement/cart.go` — read, add, change quantity and remove — taking the owner from the session and never from the request, capturing the product's price on add (via `ProductCatalog`), and running each write inside the `UnitOfWork` transaction that locks the cart (research D8, D9). The module MUST call **no** inventory write: it never reserves or holds stock, so no path of this use case reaches `Reserve` (FR-011)
- [x] T024 [US1] Implement the customer HTTP shapes in `internal/modules/cart/presentation/dto/dto.go` and the handler, router and error mapping in `internal/modules/cart/presentation/http/{handler.go,router.go,errors.go}` — the singleton `/cart` group with the session guard and no cart identifier on any route
- [x] T025 [US1] Mount the `/cart` group in `cmd/api/main.go`, constructing the repository and the `UnitOfWork` and wiring module 04's `ProductCatalog` and module 05's `InventoryAvailability` adapters; confirm `go build ./...` succeeds
- [x] T026 [US1] Add the integration test in `internal/modules/cart/presentation/http/cart_integration_test.go` behind the `integration` tag: against real PostgreSQL and a real on-sale product, add, raise, change, remove and read, confirming after each step the response and that the subtotal equals the exact sum of the lines (SC-001); confirm that adding to the cart **does not change the product's availability** (FR-011); and confirm that two adds to one cart sent **at once** that together exceed the available amount leave the line no higher than the available amount (SC-002)

**Checkpoint**: US1 is fully functional and independently testable — a customer can build a cart.

---

## Phase 4: User Story 2 - The cart refuses what cannot be bought (Priority: P1)

**Goal**: The cart refuses a product that is not on sale and a quantity beyond what is available, and
a cart view reports, per line, whether it can still be bought and — when short — the available
quantity.

**Independent Test**: Attempt to add a product that is not on sale and a quantity beyond availability;
confirm each is refused with the right code and nothing changed; then take a held product off sale or
run it short and confirm the view reports it. `quickstart.md` scenarios 5, 6, 7.

**Dependency**: US2 shares US1's `cart.go` and handler (T023, T024) and adds the refusal semantics and
the view's re-check. It is P1 and ships with US1.

### Tests for User Story 2 ⚠️

> **NOTE: Write these tests FIRST, ensure they FAIL before implementing.** T018 already proves the
> storage rules; these prove the refusals and the view a customer reads.

- [x] T027 [P] [US2] Use-case tests in `internal/modules/cart/application/implement/cart_test.go`: adding a product that is not on sale is refused with `CART_PRODUCT_NOT_PURCHASABLE` (FR-005); adding a quantity above what is available is refused with `CART_QUANTITY_EXCEEDS_AVAILABLE` (FR-007); a product that does not exist is not found (FR-005); **changing a line's quantity is refused the same way** — for a product now off sale (`CART_PRODUCT_NOT_PURCHASABLE`) and above the new availability (`CART_QUANTITY_EXCEEDS_AVAILABLE`) (FR-005, FR-007); reading re-checks each line and reports `buyable=false`; a line short of what is available reports the available quantity; a line off sale reports no quantity; a line whose product is gone reports no name and no quantity (FR-012)
- [x] T028 [P] [US2] HTTP tests in `internal/modules/cart/presentation/http/cart_test.go`: a not-on-sale add is `409 CART_PRODUCT_NOT_PURCHASABLE`; an over-available add is `409 CART_QUANTITY_EXCEEDS_AVAILABLE`; the same two `409` codes answer a `PATCH` onto an off-sale or over-available line; an unknown product is `404 PRODUCT_NOT_FOUND`; a fractional quantity on `POST`/`PATCH` is `400 VALIDATION_ERROR` naming `quantity`; the view shape carries `buyable` always and `availableQuantity` only for a short line; the two `409` codes carry `details[].field` (`productId` / `quantity`), per contracts/error-codes.md

### Implementation for User Story 2

- [x] T029 [US2] Add the refusal and the view re-check to `internal/modules/cart/application/implement/cart.go` and `presentation/dto/dto.go`: the on-sale check via `ProductCatalog` and the availability check via `InventoryAvailability` on add and change (naming the field and the available quantity), and the per-line `buyable`/`availableQuantity` projection on read (FR-005, FR-007, FR-012, research D10)
- [x] T030 [US2] Add the integration test in `internal/modules/cart/presentation/http/cart_integration_test.go` behind the `integration` tag: a product removed after being added leaves its line in the cart, which reads as `buyable: false` with no name and no available quantity, and the customer can remove it (FR-012, `quickstart.md` 7)

**Checkpoint**: US1 and US2 both work independently and a cart can never be checked out with a line
that cannot be bought.

---

## Phase 5: User Story 3 - The cart is the customer's own, and singular (Priority: P2)

**Goal**: Each signed-in customer has exactly one cart, no customer can read or change another's, and
a request without a session is refused.

**Independent Test**: With two customers, fill one cart and confirm the other's is untouched and that
neither can reach the other's by any request. `quickstart.md` scenario 8.

### Tests for User Story 3 ⚠️

> **NOTE: Write these tests FIRST, ensure they FAIL before implementing.** T018 already proves the
> one-cart-per-account index; these prove the route never confirms another cart and no route names a cart.

- [x] T031 [P] [US3] HTTP tests in `internal/modules/cart/presentation/http/cart_test.go`: `GET`/`POST`/`PATCH`/`DELETE` with no token answer `401 UNAUTHENTICATED`; two token holders each get their own cart and one never sees the other's lines (FR-001, FR-010); changing or removing a product only another customer holds answers `404 PRODUCT_NOT_FOUND`; a body that names an owner is `400 MALFORMED_REQUEST` (the decoder refuses an unknown field, FR-010)

### Implementation for User Story 3

- [x] T032 [US3] Confirm and, where needed, harden that the router and handler take the owner from the session and expose **no** cart or owner identifier on any route, and that the request decoder refuses an owner member (FR-010, research D13). This is a deliberate verification task: the guarantee is the absence of a route that names a cart, and a second path that reads an owner would defeat it

**Checkpoint**: All three stories are independently functional and the cart is unshareable.

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: Documentation, the frontend hand-off, the decision record, the deferral record, and the
close-out gates.

- [x] T033 [P] Update `docs/api-reference.md` with the four cart endpoints in the existing format (info table including its rate limit, request, response, errors, notes), the two new codes in the error section, the summary table and a change-log row. Required by Constitution VIII in the same change as the code
- [x] T034 [P] Write `specs/008-cart/frontend-guide.md`: every endpoint this feature adds, marked new, with its method, path, request and response shape, and its refusals. Required by the constitution for any feature that changes a client-facing contract
- [x] T035 [P] Add Swagger annotations (`@Summary`, `@Tags`, `@Param`, `@Success`, `@Failure`, `@Router`) to every cart handler and run **`make swagger`** so `docs/swagger/` is regenerated in the same change; confirm `make swagger-check` passes. Required by Constitution VIII and the Definition of Done; an endpoint without annotations is incomplete
- [x] T036 [P] Update `docs/modules/06-cart.md` (status, spec pointer, the completion criteria this feature meets) and `docs/modules.md` (module 06 status), and add a note to `specs/007-inventory-tracking/deferred.md` recording that module 05 now also publishes an **availability-read** contract for the cart, while **D1 — the reservation contract — stays open** for module 07. Do NOT mark D1 closed
- [x] T037 [P] Write the ADR at `docs/decisions/016-*.md` for the decisions a later reader will question: the singleton cart addressed without an identifier; the loose product reference on a cart line; the two bulk read contracts against modules 04 and 05; and the price snapshot. Follow `docs/decisions/template.md`, write it in Vietnamese, add it to `docs/decisions/README.md`, and state the accepted costs. **Check the number against `docs/decisions/` at the time of writing** — 015 is taken
- [x] T038 Write `specs/008-cart/deferred.md` recording what this feature deliberately does not carry: the checkout that turns a cart into an order (module 07); holding stock while a customer pays (module 05's capability, invoked at payment); and the module document's own deferrals — a guest cart, a saved/later cart, discount codes and upsell. Each entry states what it is, why it is out of scope, and what would unblock it
- [x] T039 Check every new and changed file for UTF-8 without BOM and LF endings, per `.editorconfig`; PowerShell's `Out-File` and `WriteAllLines` write CRLF, so write files in a way that ends up correct. Report the method and the count
- [x] T040 Run `make lint` and `make test` and clear every finding in the feature's scope
- [x] T041 Run `make test-integration` and confirm `quickstart.md` scenario 9 passes — the two unique indexes, the loose-reference survival and the concurrent-add case. A container-backed check that silently skips is a **fail**, not a pass
- [x] T042 Run `quickstart.md` end to end — scenarios 1 through 9 — and confirm each passes
- [x] T043 Run `make check` — the close-out gate — once, and confirm `git status` shows only intended files before anything is staged
- [x] T044 Re-read `specs/008-cart/frontend-guide.md` against the implementation with counts — JSON fields, enum values, routes, HTTP statuses — and correct it. The constitution requires this re-read in the feature's final phase, and a comparison that produces no counts did not happen

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies
- **Foundational (Phase 2)**: Depends on Setup — **BLOCKS all three user stories**
- **US1 (Phase 3)**: Depends on Phase 2 only
- **US2 (Phase 4)**: Depends on US1 — it adds refusals to US1's use case and handler and the re-check to the view
- **US3 (Phase 5)**: Depends on US1 — it proves the ownership of US1's routes
- **Polish (Phase 6)**: Depends on all three stories

### Within Each Phase

- Tests are written and must FAIL before the implementation they cover
- The entity and its rules precede the adapter; the adapter precedes the use cases; the use cases precede the handlers
- T002 and T003 (the contracts) must precede T004 and T005 (their adapters); T006 must precede T018; T012 is deliberately written before T011 lands, because a money rule test that passes before the rule exists is testing nothing
- T029 extends `cart.go` and the DTOs created by T023/T024, so it runs after US1

### Critical Path

T001 → T002 → T004 → T006 → T011 → T017 → T023 → T024 → T025 → T026 → T029 → T035 → T043

The contracts (T002–T005) and the adapter (T017) sit on the path because every story depends on them.
The swagger regeneration (T035) sits at the end because it is the Definition of Done for the endpoints.

### Parallel Opportunities

- T002 and T003 are independent files; T004 depends on T002, T005 on T003
- T008–T016 are largely independent files once the skeleton exists
- Within US1, T019, T020, T021 and T022 are four different test files
- Within US2, T027 and T028 are two different test files
- All of Phase 6's documentation tasks (T033–T038) are independent of each other

---

## Parallel Example: User Story 1

```bash
# All four test files, before any implementation:
Task: "Use-case tests in application/implement/cart_test.go"
Task: "HTTP tests in presentation/http/cart_test.go"
Task: "Error-mapping tests in presentation/http/errors_test.go"
Task: "Router test in presentation/http/router_test.go"

# Then the implementation, in order — each depends on the one before:
Task: "Implement the cart use cases in application/implement/cart.go"
Task: "Implement the customer handler, router and error mapping"
Task: "Mount the /cart group in cmd/api/main.go"
Task: "Add the HTTP integration test"
```

---

## Implementation Strategy

### MVP First (User Story 1 + User Story 2)

Both P1 stories are the MVP: the customer can build a cart and cannot fill it with anything that
cannot be bought. US3 is the ownership guarantee that a multi-customer shop cannot be correct without.

1. Phase 1: confirm the baseline
2. Phase 2: the two contracts and their adapters, the migration, the entity, the adapter
3. Phase 3 + 4: US1 and US2
4. **STOP and VALIDATE**: add products, raise a quantity, take one off sale and confirm the refusal,
   run one short and confirm the view reports it
5. Deploy or demo if ready

### Incremental Delivery

1. Phase 1 + 2 → the tables, the contracts and the storage guarantees exist and are proven
2. US1 → a cart can be built and read → validate independently
3. US2 → a cart refuses the unbuyable and reports it on read → validate independently → the feature is usable
4. US3 → the cart is unshareable → validate independently
5. Phase 6 → documentation, the frontend hand-off, the swagger spec and the decision record, then the close-out gate

### Parallel Team Strategy

With two developers:

1. Both complete Phase 1 and 2 together — T004 and T005 (the two adapters, one in each of modules 04
   and 05) are the pieces that touch another module and should not be split; T017 is the other
   contested file
2. Then split: **Developer A takes US1 + US2** (the use case, the view and the HTTP surface),
   **Developer B takes US3** (the ownership tests and the router/decoder hardening). They touch
   different files: US1 owns `application/implement/cart.go`; US3 owns the router/decoder verification
3. Sequence `application/implement/cart_test.go` (US1 and US2 both write it), `presentation/http/cart_test.go`
   (US1, US2 and US3 all write it) and `cmd/api/main.go`

---

## Notes

- [P] tasks = different files, no dependencies
- [Story] labels map each task to a user story
- US1 and US2 ship together and share `cart.go`; US3 proves US1's ownership. That is stated rather than hidden
- Verify tests fail before implementing. A test that passes before its implementation exists is
  testing nothing, and T012 and T018 are the ones most likely to be written that way by accident
- Commit after each phase, not after each task
- The same-file overlaps are `application/implement/cart.go` (US1 and US2), `application/implement/cart_test.go`
  (US1 and US2), `presentation/http/cart_test.go` (US1, US2 and US3) and `cmd/api/main.go` — sequence them
- This feature must **not** create a cart row on a read and must **not** hold stock: a GET answers an
  empty cart without a write, and a cart line is not a reservation (research D8, D12)

---

## Phase 7: Convergence

- [x] T045 Thêm ca integration (tag `integration`) chứng minh **trường hợp đồng thời** của SC-002: hai thao tác ghi vào **cùng một giỏ** gửi **cùng lúc** mà tổng vượt tồn khả dụng (ví dụ tồn khả dụng = 1, mỗi thao tác thêm 1) phải để dòng **không cao hơn** tồn khả dụng — một thao tác thành công, thao tác kia bị `409 CART_QUANTITY_EXCEEDS_AVAILABLE` và **không đổi gì**; thêm vào `internal/modules/cart/presentation/http/cart_integration_test.go` per SC-002 / T026 (partial). Hiện file này ghi rõ ca đó *cố ý không* được khẳng định (`TestConcurrentAddsConvergeToOneLineAgainstPostgres` chỉ dùng tồn = 10 cho hai add 1+1, không tiệm cận tồn; ca concurrent ở `infrastructure/implement/postgres/cart_integration_test.go` gọi thẳng `UpsertLine`, không qua kiểm tra khả dụng), nên điều kiện `quickstart.md` 9d và edge case "hai cập nhật cùng lúc không được để dòng vượt tồn khả dụng" chưa có test tự động
- [x] T046 Sửa ví dụ JSON của `CartLineResponse` trong `specs/008-cart/frontend-guide.md` (mục "Kiểu dữ liệu"): ví dụ hiện đặt `"buyable": true` cùng `"availableQuantity": 1`, hai giá trị **không thể cùng xuất hiện** (`availableQuantity` chỉ có khi dòng còn bán nhưng thiếu, tức `buyable: false`), trái với chính đoạn văn mô tả ngay dưới và với `docs/api-reference.md` §8.1; tách thành hai ví dụ hoặc bỏ `availableQuantity` khỏi ví dụ `buyable: true` per T044 (contradicts)
