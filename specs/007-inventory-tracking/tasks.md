---

description: "Task list for feature 007-inventory-tracking"
---

# Tasks: Inventory Tracking

**Input**: Design documents from `specs/007-inventory-tracking/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md

**Tests**: Included. The constitution makes inventory calculation one of the test-first critical
areas, and this module's substance *is* inventory arithmetic and a state consequence — the
non-negative rule, availability as a derived fact, the zero-crossing transition, hold expiry, and
exactly-once application. Each test task is written first and must fail before its implementation
task runs, and the guarantees a fake cannot prove — no-negative under concurrency and
source-reference uniqueness — are proven against real PostgreSQL.

**Organization**: One phase per user story, in the priority order the spec gives. US1 and US2 are
both P1 and ship together; the holding capability (US3) is the clarified addition; US4 closes
feature 006's obligation D1; US5 delivers the exactly-once capability the order flow will call; US6
is the operator's history. The dependencies between them are stated rather than hidden, because
several stories cannot be exercised before another one's adapter exists.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (US1…US6)
- Include exact file paths in descriptions

## Path Conventions

Go web service inside a modular monolith, following `plan.md`:

- `internal/modules/inventory/` — the new module, four layers
- `internal/contracts/product.go` — the cross-module interface this feature introduces
- `internal/modules/product/` — touched: a system-facing use case and a contract adapter
- `migrations/` — the additive schema change
- `cmd/api/main.go` — composition, including the sweeper's start and stop
- `docs/` — documentation that must change with the code (Constitution VIII)

---

## Phase 1: Setup (Baseline)

**Purpose**: Establish a green baseline so a later failure is attributable to this feature. This
feature adds no project scaffolding — the module skeleton is Phase 2's work.

- [x] T001 Confirm the baseline is green and the migration number is free: run `make lint`, `make test` and `make test-integration` (all must exit 0, and the integration run must be proven to execute rather than skip), and confirm `migrations/` ends at `00006_product.sql` so the new migration is `00007`

**Checkpoint**: The tree is green and the next migration number is known.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The cross-module contract and its product-side adapter, the three tables, the entities
and their rules, the ports and the adapter — everything all six user stories stand on.

**⚠️ CRITICAL**: No user story work begins until this phase is complete. Every story needs the
adapter (T020); US1 and US2 both need the non-negative enforcement it carries; US4 needs the
contract (T002–T004); US3 needs the hold table (T005).

### The availability contract and the product side (research D4)

- [x] T002 [P] Declare **two** contracts in `internal/contracts/product.go`. `ProductAvailability`: `MarkOutOfStock(ctx, productID)` and `MarkOnSale(ctx, productID)`, documented as a **signal** the product module maps onto its own two edges, idempotent and tolerant (a no-op when the product is already in the target state or is announced/retired), running in the caller's transaction, and reporting not-found for an unknown product. `ProductLookup`: `ProductExists(ctx, productID) (bool, error)`, the question a read or a decrease must ask because the foreign key only fires on an insert (research D4, D12). The package MUST NOT import a module
- [x] T003 [P] Add the system-facing availability use case in `internal/modules/product/application/implement/availability.go` and its method on the product service port in `internal/modules/product/application/interface/ports.go`: load the product, move `ACTIVE → OUT_OF_STOCK` with `SellOut` and `OUT_OF_STOCK → ACTIVE` with `Restock`, leave announced and retired products untouched, never write a state directly, and audit `PRODUCT_STATE_CHANGED` with a **nil actor** (this path has no human actor). Unit tests in `internal/modules/product/application/implement/availability_test.go`: one positive and one negative per signal (the negative being a terminal or announced product the signal must not move)
- [x] T004 [P] Implement the product-side adapter in `internal/modules/product/infrastructure/implement/availability/availability.go`, satisfying **both** `contracts.ProductAvailability` (delegating to T003's use case) and `contracts.ProductLookup` (a single existence read over the product repository); it joins the transaction the inventory use case put in the context and never opens one. Add the compile-time assertions `var _ contracts.ProductAvailability = (*Adapter)(nil)` and `var _ contracts.ProductLookup = (*Adapter)(nil)` **in the adapter's own package**, not in `internal/contracts` (which stays dependency-free)

### Schema

- [x] T005 Write `migrations/00007_inventory.sql` following `docs/development/migration.md`: the three tables `data-model.md` specifies with every column and `COMMENT ON` where the purpose is not obvious — `stock_levels` (PK on `product_id`, `CHECK quantity >= 0`, cascading FK), `inventory_transactions` (kind check, `delta <> 0` check, `resulting_quantity >= 0` check, a nullable `note` column, the **partial unique index on `source_reference`**, cascading FK, the `(product_id, created_at, id)` history index), and `stock_holds` (quantity/status checks, the `(status = 'ACTIVE') = (resolved_at IS NULL)` check, the **partial unique index on `(order_id, product_id)` where active**, the active-product and sweep indexes) — plus both `-- +goose Up` and `-- +goose Down` sections
- [x] T006 Add `migrations/inventory_guard_test.go` asserting the migration text carries every constraint **and** drops it in the down section, matching `migrations/product_guard_test.go`, because the runner rejects a one-way migration. It must assert the non-negative check, the source-reference unique index and the active-hold unique index are present, since those are what US2 and US5 rest on

### Domain

- [x] T007 [P] Create the module skeleton under `internal/modules/inventory/` with the four layers from `plan.md` — `domain/{constant,error,model,repository}`, `application/{dto,interface,implement,mapper}`, `infrastructure/implement/{postgres,auditor}`, `presentation/{dto,http,worker}` — each package carrying a `// Package …` comment (revive enforces this)
- [x] T008 [P] Define the movement kinds in `internal/modules/inventory/domain/constant/movement.go` (`RESTOCK`, `DAMAGE`, `ADJUSTMENT`, `SALE`) with their signed meaning (research D9)
- [x] T009 [P] Define the hold statuses and the fixed hold window in `internal/modules/inventory/domain/constant/hold.go` (`ACTIVE`, `CONSUMED`, `RELEASED`, and `HoldTTL = 15 * time.Minute`), documenting that the window is a single business constant (spec Assumption)
- [x] T010 [P] Define the machine codes in `internal/modules/inventory/domain/constant/codes.go` (`INVENTORY_INSUFFICIENT_STOCK`) and the audit actions in `internal/modules/inventory/domain/constant/audit.go` (`INVENTORY_RESTOCKED`, `INVENTORY_DAMAGED`, `INVENTORY_ADJUSTED`, `INVENTORY_SALE_APPLIED`), each with a comment (FR-005, FR-006, FR-020)
- [x] T011 [P] Implement the stock rule in `internal/modules/inventory/domain/model/stock.go`: a physical quantity that is never negative, the availability rule `physical − active holds` with its own non-negative guarantee, and the refusal when an operation would take either below zero (FR-009, FR-012, FR-018, research D1)
- [x] T012 [P] Implement the movement entity and its kind rules in `internal/modules/inventory/domain/model/movement.go`: a signed delta that is never zero, a resulting quantity that is never negative, an optional free-text note, and the four kinds (FR-004, research D9)
- [x] T013 [P] Implement the hold entity and its expiry rule in `internal/modules/inventory/domain/model/hold.go`: an active hold is active only while its expiry is in the future; a hold ends by being consumed, released or expired, and a resolved one carries the moment it ended (clarification, research D6)
- [x] T014 [P] Define the module's sentinel errors in `internal/modules/inventory/domain/error/errors.go` — insufficient stock (the one refusal the module owns), a not-found carrier for an unknown product, and an invalid-value carrier that names the offending field (FR-005, FR-009, FR-012)
- [x] T015 Write the domain tests in `internal/modules/inventory/domain/model/{stock_test.go,movement_test.go,hold_test.go}` **before** T011–T013 are finished, and confirm they fail: a decrease that would go below zero is refused and one that reaches exactly zero is allowed; availability is physical minus active holds and never negative; a zero delta is refused; an expired hold is no longer active and a resolved one cannot be consumed twice
- [x] T016 [P] Declare the repository contract in `internal/modules/inventory/domain/repository/inventory.go` — the level read/upsert/conditional-decrease, the ledger insert and paged history read, and the hold insert/sum/lock/resolve/sweep operations — with the concurrency obligations stated as behaviour, and no transaction opened inside it (Constitution I)

### Ports, DTOs and the adapter

- [x] T017 [P] Declare the application ports in `internal/modules/inventory/application/interface/ports.go`: the use-case surface, the `ProductAvailability` alias (from `internal/contracts`), the `UnitOfWork` port, the auditor port, and a one-method `Clock` port defaulting to `time.Now` (research D15). Add the actor port in `internal/modules/inventory/application/interface/actor.go`, taking the acting administrator from the context the session filled, exactly as modules 03 and 04 do
- [x] T018 [P] Define the application DTOs in `internal/modules/inventory/application/dto/dto.go` — the manual-operation inputs (each carrying an optional `note`), the stock view (physical, held, available), the movement (including its note), and the hold-operation inputs — separate from the HTTP shapes
- [x] T019 [P] Implement the single mapper in `internal/modules/inventory/application/mapper/mapper.go`; it stays the only place a model becomes a DTO
- [x] T020 Implement the PostgreSQL adapter in `internal/modules/inventory/infrastructure/implement/postgres/inventory.go`, embedding the shared `Base` with an **explicit `Columns` projection** where it is used, binding every value as a parameter, and: an increase as an upsert that materialises the level row, a decrease as a **conditional update** `... WHERE product_id = $1 AND quantity >= $2 RETURNING quantity` whose empty result is reported as the insufficient-stock sentinel, the ledger insert (carrying the optional note) in the same call chain, the active-hold sum and the sweep selection both taking the injected clock's instant **as the `$now` parameter** (never the database's `now()`, research D15), the `SELECT ... FOR UPDATE` level lock the hold paths need, the paged history read ordered by `created_at, id`, and the foreign-key violation classified by SQLSTATE into the not-found sentinel as the storage guard (research D2, D12, D14). The adapter does **not** decide existence on the read or decrease paths — the use case answers that through `ProductLookup` (T027). It MUST NOT open a transaction
- [x] T021 [P] Implement the audit adapter in `internal/modules/inventory/infrastructure/implement/auditor/auditor.go`, delegating to the foundation's shared audit writer rather than opening a second one (FR-006)
- [x] T022 Write `internal/modules/inventory/infrastructure/implement/postgres/inventory_integration_test.go` behind the `integration` tag, proving against real PostgreSQL what a fake cannot: a level row for a product that does not exist is refused by the foreign key; deleting a product removes its level, ledger and hold rows (FR-011, research D11); the `quantity >= 0` check rejects a direct negative write; the partial unique index refuses a second movement with the same source reference and a second active hold for the same order and product (FR-019, FR-022); a movement's `delta`, `resulting_quantity` and `kind` checks reject a value that bypassed the application; and the history read is stable across two reads sharing a timestamp (FR-008, research D14). Confirm each test fails if its constraint is removed from the migration

**Checkpoint**: The contract, the tables, the entities, the ports and the adapter all exist and the
storage guarantees are proven. US1 and US2 can proceed independently.

---

## Phase 3: User Story 1 - An operator manages the stock of a product (Priority: P1) 🎯 MVP

**Goal**: An administrator receives goods, records damage and corrects a count after a stocktake,
each operation changing one product's physical quantity and leaving a ledger record naming who did
it and why.

**Independent Test**: Restock a product, damage it, and correct it to a recounted value through the
administrator endpoints, confirming after each step the physical quantity, the ledger entry it
produced, and the audit entry. `quickstart.md` scenarios 1, 2, 3, 4, 10.

### Tests for User Story 1 ⚠️

> **NOTE: Write these tests FIRST, ensure they FAIL before implementing.**

- [x] T023 [P] [US1] Use-case tests in `internal/modules/inventory/application/implement/stock_test.go` over an in-memory repository and fake auditor, `ProductLookup` and clock: a restock raises the physical quantity by exactly the amount and writes one `RESTOCK` movement with the actor and its note (FR-001, FR-004); damage lowers it and writes one `DAMAGE` movement (FR-002); damage larger than the shelf is refused naming the field and writes nothing (FR-002, FR-009); an adjustment sets the counted value and records only the signed difference, and correcting to the stored value writes no movement (FR-003, research D9); a zero, negative or fractional quantity on a restock or a damage is refused naming `quantity`, while an adjustment accepts zero as a valid counted value (FR-012, research D9); a product the lookup says does not exist answers not-found while one that exists but has never been stocked answers zero (FR-007, research D12); each manual operation records its audit action (FR-006, SC-006)
- [x] T024 [P] [US1] HTTP tests in `internal/modules/inventory/presentation/http/stock_test.go`: no token is `401` and a customer token is `403` on every route (FR-005); `200` and the stock shape `physicalQuantity`/`heldQuantity`/`availableQuantity` on each write; an unknown identifier is `404 PRODUCT_NOT_FOUND`; a malformed identifier is `400 VALIDATION_ERROR` with the field named; a bad quantity is `400` naming `quantity`; a damage over the shelf is `409 INVENTORY_INSUFFICIENT_STOCK` and the stock is unchanged
- [x] T025 [P] [US1] Error-mapping tests in `internal/modules/inventory/presentation/http/errors_test.go` covering every code the module can answer with: the insufficient-stock sentinel to `409`, the field carrier to `400` with `details[].field`, and the shared codes passed through
- [x] T026 [P] [US1] Router test in `internal/modules/inventory/presentation/http/router_test.go` asserting the four paths US1 owns (`GET` the stock, and the `restock`/`damage`/`adjustment` writes) resolve and that the group carries the administrator role guard. The `/movements` path is asserted in US6 (T053), so this test passes before the history endpoint exists

### Implementation for User Story 1

- [x] T027 [US1] Implement the manual stock use cases in `internal/modules/inventory/application/implement/stock.go` — restock, damage, adjustment and the single-product read — taking the actor from the session and never from the request, resolving product existence through the `ProductLookup` contract so an unknown product answers not-found while a never-stocked one answers zero (FR-007, research D12), carrying the optional note into the ledger, and running each write inside the `UnitOfWork` transaction and recording its audit action (FR-001 to FR-007, FR-011)
- [x] T028 [US1] Implement the administrator HTTP shapes in `internal/modules/inventory/presentation/dto/dto.go` and the handler, router and error mapping in `internal/modules/inventory/presentation/http/{handler.go,router.go,errors.go}`, with the administrator role guard (FR-001 to FR-007, FR-012)
- [x] T029 [US1] Mount the module's `/admin/inventory` route group in `cmd/api/main.go`, constructing the repository, the auditor and the `UnitOfWork`; confirm `go build ./...` succeeds
- [x] T030 [US1] Add the integration test in `internal/modules/inventory/presentation/http/stock_integration_test.go` behind the `integration` tag: against real PostgreSQL and a real product, restock to a value, damage part of it, correct the count, and confirm after each step the response and `quickstart.md` scenario 2c — `SUM(delta) over the product's movements equals stock_levels.quantity` (SC-001)

**Checkpoint**: US1 is fully functional and independently testable. The shop can count what it
holds, which is value on its own even before holds land.

---

## Phase 4: User Story 2 - The shop can never oversell (Priority: P1)

**Goal**: No operation, neither alone nor concurrent with another, can drive a product's physical
stock or available quantity below zero, and a refusal is a real refusal.

**Independent Test**: Attempt to remove more than the shelf holds and confirm it is refused with an
answer the operator can act on; drive two competing decreases at the same product simultaneously and
confirm exactly one succeeds; attempt the same against PostgreSQL directly and confirm the database
refuses. `quickstart.md` scenarios 3, 11a, 11e.

**Dependency**: US2 shares the adapter US1 built (T020) and proves the guarantee it carries; it is
P1 and ships with US1. Its enforcement lives in the storage layer, which is what these tasks prove.

### Tests for User Story 2 ⚠️

> **NOTE: Write these tests FIRST, ensure they FAIL before implementing.** T022 already proves the
> CHECK at the storage layer; these prove the behaviour an operator and a concurrent writer meet.

- [x] T031 [P] [US2] HTTP test in `internal/modules/inventory/presentation/http/stock_test.go`: a damage that would take the shelf below zero answers `409 INVENTORY_INSUFFICIENT_STOCK`, and a follow-up read shows the quantity unchanged and the history with no new entry (FR-009)
- [x] T032 [P] [US2] Integration test in `internal/modules/inventory/presentation/http/stock_integration_test.go` behind the `integration` tag: two concurrent damages that together exceed the stock leave exactly one applied and the stored quantity never below zero; and `INSERT INTO stock_levels (product_id, quantity) VALUES ($1, -1)` is refused by the check (FR-010, SC-002, `quickstart.md` 11a)
- [x] T033 [US2] Use-case test in `internal/modules/inventory/application/implement/stock_test.go` asserting the empty result of the conditional decrease is reported as the insufficient-stock sentinel rather than a generic storage error, so the handler can answer `409` and not `500` (FR-009, `quickstart.md` 3b)

### Implementation for User Story 2

- [x] T034 [US2] Confirm the conditional decrease in `internal/modules/inventory/infrastructure/implement/postgres/inventory.go` is the sole enforcement path **for the non-negative rule** — no read-then-write check for it exists in `application/implement/stock.go` — and classify the empty result into the sentinel in `internal/modules/inventory/domain/error/errors.go`'s vocabulary (FR-010, research D2). This is a deliberate verification task: the guarantee is the storage statement, and an application-level pre-check would reintroduce the race. (The below-held guard added later in T039 is a separate and deliberately narrow check, and is not what this task forbids.)

**Checkpoint**: US1 and US2 both work independently, and the non-negative guarantee is real under
concurrency and at the storage layer.

---

## Phase 5: User Story 3 - Stock is held while a customer pays (Priority: P2)

**Goal**: Beginning a payment sets goods aside for that order for fifteen minutes; the hold expires
by itself if unpaid, is consumed when paid, and is released when cancelled — and the shelf itself
never moves for a hold.

**Independent Test**: With a product holding several units, hold some for an order and confirm the
rest remains available to another; let the hold expire with an injected clock and confirm the
quantity returns; hold again, pay, and confirm physical stock falls once; hold, cancel, and confirm
the quantity returns. `quickstart.md` scenarios 5, 6c, 6d.

**Dependency**: US3 needs the adapter (T020) and the read endpoint (T028); it is P2 because the
manual ledger is P1 and works without holds.

### Tests for User Story 3 ⚠️

> **NOTE: Write these tests FIRST, ensure they FAIL before implementing.**

- [x] T035 [P] [US3] Hold use-case tests in `internal/modules/inventory/application/implement/holds_test.go`: reserving reduces availability while leaving physical unchanged (FR-014); reserving more than is available is refused (FR-018); a hold becomes inactive once the injected clock passes its expiry and its quantity returns to availability (FR-015); releasing a hold returns its quantity with no physical change (FR-017); a second reserve for the same order and product does not double-hold (FR-019); and a physical decrease that would leave less than is held is refused (FR-009)
- [x] T036 [P] [US3] Sweeper test in `internal/modules/inventory/presentation/worker/sweeper_test.go`: the sweeper calls the release-expired use case on each tick with an injected clock and a controllable interval, stops on context cancellation, and does not run two sweeps at once
- [x] T037 [P] [US3] HTTP test in `internal/modules/inventory/presentation/http/stock_test.go`: after a hold, the read answers `heldQuantity` and a lower `availableQuantity` while `physicalQuantity` is unchanged (`quickstart.md` 5a)

### Implementation for User Story 3

- [x] T038 [US3] Implement the hold use cases in `internal/modules/inventory/application/implement/holds.go` — reserve, release and expire — each inside the `UnitOfWork` transaction, reserving under the level row lock so two concurrent holds cannot set aside the same unit, returning the quantity to availability on release or expiry, and deciding "active" with the injected clock's instant passed as the `$now` parameter rather than the database clock (FR-014, FR-015, FR-017, FR-018, research D2, D15)
- [x] T039 [US3] Add the "never below held" guard to the physical decreases in `internal/modules/inventory/application/implement/stock.go`: compute the held quantity under the same lock and refuse a damage or adjustment that would leave the shelf below what is promised (FR-009)
- [x] T040 [US3] Implement the sweeper in `internal/modules/inventory/presentation/worker/sweeper.go` as a `time.Ticker` loop that calls the expire use case every interval (default 30 seconds) and stops on context cancellation, carrying no business rule of its own (research D6)
- [x] T041 [US3] Start and stop the sweeper in `cmd/api/main.go` alongside the server and the audit writer, so a shutdown cancels it with everything else
- [x] T042 [US3] Add the integration test in `internal/modules/inventory/infrastructure/implement/postgres/inventory_integration_test.go` behind the `integration` tag: reserve and confirm the stored level is unchanged while the active-hold sum rises; run the expire use case past the expiry and confirm the hold is resolved and no longer counted; and confirm an expired hold is not returned by the active sum even before it is swept (research D6)

**Checkpoint**: US3 is functional and availability is now a truthful derived fact.

---

## Phase 6: User Story 4 - Availability follows the stock by itself (Priority: P2)

**Goal**: When availability reaches zero the product stops being offered, and when it leaves zero it
is offered again — automatically, in the same operation, and never for an announced or retired
product. This closes feature 006's obligation D1.

**Independent Test**: Drive an on-sale product's availability to zero and confirm it becomes out of
stock, then make it available again and confirm it returns to sale; confirm an announced product and
a retired product are untouched. `quickstart.md` scenarios 6, 7.

**Dependency**: US4 needs US1's write path and US3's holds (a hold is one way availability reaches
zero), and it wires the contract T002–T004 provided.

### Tests for User Story 4 ⚠️

> **NOTE: Write these tests FIRST, ensure they FAIL before implementing.**

- [x] T043 [P] [US4] Reconcile tests in `internal/modules/inventory/application/implement/reconcile_test.go`: the four rows of `research.md` D10 — a non-crossing change signals nothing; availability to zero signals out-of-stock; zero to positive signals on-sale; zero to zero signals nothing — asserted by counting the fake availability port's calls, so a non-crossing change proves it did **not** call (FR-024 to FR-028, SC-005)
- [x] T044 [P] [US4] Product-side test in `internal/modules/product/application/implement/availability_test.go` asserting the signal is refused to move an announced product and a retired product, and that a signal for the state it is already in is a no-op rather than an error (FR-024, FR-025, FR-027, research D4)

### Implementation for User Story 4

- [x] T045 [US4] Implement the reconciliation in `internal/modules/inventory/application/implement/reconcile.go` — capture availability before and after a change and call `ProductAvailability` only on a zero crossing, inside the same transaction as the change — **and wire that call into the writing use cases it can reach** (`application/implement/stock.go` and `holds.go`), since FR-026 requires the signal to travel with the change itself (FR-024, FR-026, FR-027, FR-028, research D10). This task edits files created in US1 and US3, so it runs after them; the sale path in `events.go` does not exist until T050, and T050 wires the same call there
- [x] T046 [US4] Wire the availability contract into the inventory service in `cmd/api/main.go`, supplying module 04's adapter (T004) as the provider, and confirm `go build ./...` succeeds (Constitution I)
- [x] T047 [US4] Add the integration test in `internal/modules/inventory/presentation/http/stock_integration_test.go` behind the `integration` tag: with a real on-sale product, damaging the last unit leaves the product's stored `sell_state` at `OUT_OF_STOCK`, a restock returns it to `ACTIVE`, retiring the product then restocking leaves it `DISCONTINUED`, and holding the last available unit moves it out of stock while physical is unchanged (FR-024 to FR-028, SC-005, `quickstart.md` 6a–6d, 7b)

**Checkpoint**: The sell state changes by itself in both directions, and obligation D1 of feature 006
is closed.

---

## Phase 7: User Story 5 - A repeated event changes stock exactly once (Priority: P2)

**Goal**: Paying a held order turns its hold into a sale, and if the same payment event arrives twice
the stock changes once, not twice.

**Independent Test**: Apply a payment event's sale once, then apply the identical event again and
confirm the physical quantity changed exactly once and only one movement carries the reference;
repeat with the two applications arriving simultaneously. `quickstart.md` scenario 8.

**Dependency**: US5 needs US3's holds (a sale consumes a hold) and the ledger. It is P2 because the
manual ledger is P1 and no order exists yet to send the event.

### Tests for User Story 5 ⚠️

> **NOTE: Write these tests FIRST, ensure they FAIL before implementing.**

- [x] T048 [P] [US5] Outside-event use-case tests in `internal/modules/inventory/application/implement/events_test.go`: consuming a hold writes one `SALE` movement with the payment event as its source reference and lowers physical by the held quantity while leaving availability unchanged (FR-016, FR-020); a replay of the same reference changes nothing and is accepted as success rather than an error (FR-021); an event that would take the shelf below zero is refused and records no movement (FR-023); and a hold that is no longer active cannot be consumed
- [x] T049 [P] [US5] Integration test in `internal/modules/inventory/infrastructure/implement/postgres/inventory_integration_test.go` behind the `integration` tag: two concurrent applications of the same source reference leave exactly one movement and one decrease, refused by the partial unique index and not by an application pre-check (FR-022, SC-003, `quickstart.md` 8b, 8c)

### Implementation for User Story 5

- [x] T050 [US5] Implement the outside-event applications in `internal/modules/inventory/application/implement/events.go` — the sale that consumes a hold and the idempotent application keyed by source reference — inside the `UnitOfWork` transaction, recognising a unique-violation on the reference as "already applied" and answering success, and writing an audit entry that names the reference with no human actor, and calling **no** reconciliation — a sale lowers physical stock and closes the hold by the same amount, so availability is unchanged and nothing crosses zero (FR-014 to FR-017, FR-020 to FR-023, research D5)
- [x] T051 [US5] Add the test in `internal/modules/inventory/application/implement/events_test.go` asserting the sale writes an audit entry naming the source reference with no human actor (FR-020, Constitution VI), that a manual movement carries no source reference while a sale carries one, so the two paths stay distinguishable, and that the audit entry carries no customer's personal data (FR-006, research D13)

**Checkpoint**: The exactly-once capability is delivered and tested directly, ready for module 07 to
call when it exists.

---

## Phase 8: User Story 6 - The operator can account for the stock (Priority: P3)

**Goal**: An administrator reads a product's movement history and sees every change in order, with
what happened, how much, when and by whom.

**Independent Test**: Make several changes to one product, read its history, and confirm every change
appears once, in order, with its kind, amount, resulting quantity and actor; confirm a product with
no changes answers an empty history. `quickstart.md` scenario 9.

**Dependency**: US6 needs US1's movements to exist. It is P3 because the shop runs without a history
view.

### Tests for User Story 6 ⚠️

> **NOTE: Write these tests FIRST, ensure they FAIL before implementing.**

- [x] T052 [P] [US6] History use-case tests in `internal/modules/inventory/application/implement/history_test.go`: the history is ordered oldest first and stable across two reads sharing a timestamp (FR-008, research D14); a product with no movements answers an empty page rather than an error; and the page metadata is the project's usual one
- [x] T053 [P] [US6] HTTP test in `internal/modules/inventory/presentation/http/stock_test.go`: `GET .../movements` is `200` with the movement shape including `resultingQuantity`, `sourceReference` and `actorId`, is `404` for an unknown product, `400` for a page outside its range, and `403` for a customer token (FR-008, `quickstart.md` 9)

### Implementation for User Story 6

- [x] T054 [US6] Implement the history read use case in `internal/modules/inventory/application/implement/history.go` over the adapter's paged read (FR-008)
- [x] T055 [US6] Add the `/admin/inventory/{productId}/movements` route and its response shape in `internal/modules/inventory/presentation/http/router.go` and `internal/modules/inventory/presentation/dto/dto.go` (FR-008)

**Checkpoint**: All six stories are independently functional.

---

## Phase 9: Polish & Cross-Cutting Concerns

**Purpose**: Documentation, the frontend hand-off, the decision record, the deferral record, and the
close-out gates.

- [x] T056 [P] Update `docs/api-reference.md` with the five administrator endpoints in the existing format (info table including its rate limit, request, response, errors, notes), the new `INVENTORY_INSUFFICIENT_STOCK` code in the error section, the summary table, and a change-log row. Required by Constitution VIII in the same change as the code
- [x] T057 [P] Write `specs/007-inventory-tracking/frontend-guide.md`: every endpoint this feature adds, marked new, with its method, path, request and response shape, and its refusals. Required by the constitution for any feature that changes a client-facing contract
- [x] T058 [P] Update `docs/modules/05-inventory.md` (status, spec pointer, the completion criteria this feature meets and the automatic order-driven half it does not), `docs/modules.md` (module 05 status), and `docs/modules/04-product.md` (mark the D1 obligation closed and point at the implementation). Marking a module complete without saying what was carried forward is how an unfinished rule becomes invisible
- [x] T059 [P] Write the ADR at `docs/decisions/014-*.md` for the decisions a later reader will question: the cross-module `ProductAvailability` signal and why the product adapter was written on the product side; the three-table model with a live level as the concurrency gate; and the background sweeper as the mechanism that releases an expiring hold. Follow `docs/decisions/template.md`, write it in Vietnamese like the existing ADRs, add it to `docs/decisions/README.md`, and state the accepted costs. **Check the number against `docs/decisions/` at the time of writing** — 013 is taken
- [x] T060 Write `specs/007-inventory-tracking/deferred.md` recording what this feature deliberately does not carry: no cross-module reservation contract until Order is specified (why: the consumer does not exist, Constitution VII; unblocks when module 07 is specified); no paid-order cancellation, and a transferred paid order does not change stock (why: the clarification settled it; belongs to the order flow); inventory history does not outlive a removed product (why: feature 006 named module 07 as the owner of the product-removal decision); and the deferred low-stock threshold and multi-location model. Each entry states what it is, why it is out of scope, and what would unblock it
- [x] T061 [P] Verify in `internal/modules/inventory/presentation/http/leak_test.go` that a stock response and an audit entry carry nothing they should not — in particular that the stock response carries only the three quantity fields and the product identifier and no internal hold or ledger detail, and that no audit entry carries a customer's personal data (FR-006, Constitution V, VI)
- [x] T062 Check every new and changed file for UTF-8 without BOM and LF endings, per `.editorconfig`; PowerShell's `Out-File` and `WriteAllLines` write CRLF, so write files in a way that ends up correct. Report the method and the count
- [x] T063 Run `make lint` and `make test` and clear every finding in the feature's scope
- [x] T064 Run `quickstart.md` end to end — scenarios 1 through 11 — and confirm each passes. A container-backed check that silently skips is a **fail**, not a pass. Scenarios 5, 6c, 6d, 8 and 11 are driven by the automated tests because holding stock has no HTTP surface; report them honestly rather than passing them vacuously
- [x] T065 Run `make check` — the close-out gate — once, and confirm `git status` shows only intended files before anything is staged
- [x] T066 Re-read `specs/007-inventory-tracking/frontend-guide.md` against the implementation with counts — JSON fields, movement kinds, routes, HTTP statuses — and correct it. The constitution requires this re-read in the feature's final phase, and a comparison that produces no counts did not happen

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies
- **Foundational (Phase 2)**: Depends on Setup — **BLOCKS all six user stories**
- **US1 (Phase 3)**: Depends on Phase 2 only
- **US2 (Phase 4)**: Depends on Phase 2 and shares US1's adapter; ships with US1
- **US3 (Phase 5)**: Depends on US1 — its independent test drives the read endpoint and the adapter
- **US4 (Phase 6)**: Depends on US1 and US3 — a hold is a way availability reaches zero — and wires the contract from T002–T004
- **US5 (Phase 7)**: Depends on US3 — a sale consumes a hold
- **US6 (Phase 8)**: Depends on US1 — its independent test needs movements to exist
- **Polish (Phase 9)**: Depends on all six stories

### Within Each Phase

- Tests are written and must FAIL before the implementation they cover
- The entity and its rules precede the adapter; the adapter precedes the use cases; the use cases precede the handlers
- T002 must precede T004 (the contract before its adapter); T005 must precede T022; T015 is deliberately written before T011–T013 land, because a rule test that passes before the rule exists is testing nothing
- T035–T037 are tests over the hold capability; T038–T041 are its implementation; T039 (the below-held guard) must follow T038's reserve so there is a hold to guard against
- T048 must follow T038 — a sale consumes a hold — but the idempotency mechanism (T050) is the same code that T048 tests

### Critical Path

T001 → T002 → T003 → T004 → T005 → T011 → T020 → T027 → T028 → T029 → T038 → T045 → T046 → T050 → T064 → T065

The contract (T002–T004) and the adapter (T020) sit on the path because every story depends on them.
US4's reconciliation (T045) and US5's exactly-once (T050) sit at the end: the first closes feature
006's debt, the second is the capability module 07 will call.

### Parallel Opportunities

- T002 is independent of T005 and T007–T019; T003 and T004 depend on T002
- T007–T013 and T016–T019 are largely independent files once the skeleton exists
- Within US1, T023, T024, T025 and T026 are four different test files
- Within US3, T035, T036 and T037 are three different test files
- Within US4, T043 and T044 are two different files, one in each module
- Within US5, T048 and T049 are two different test files
- All of Phase 9's documentation tasks (T056–T061) are independent of each other

---

## Parallel Example: User Story 1

```bash
# All four test files, before any implementation:
Task: "Use-case tests in application/implement/stock_test.go"
Task: "HTTP tests in presentation/http/stock_test.go"
Task: "Error-mapping tests in presentation/http/errors_test.go"
Task: "Router test in presentation/http/router_test.go"

# Then the implementation, in order — each depends on the one before:
Task: "Implement the manual stock use cases in application/implement/stock.go"
Task: "Implement the administrator handler, router and error mapping"
Task: "Mount the /admin/inventory route group in cmd/api/main.go"
Task: "Add the HTTP integration test"
```

---

## Implementation Strategy

### MVP First (User Story 1 + User Story 2)

Both P1 stories are the MVP. The shop can count what it holds and cannot oversell — the two
guarantees that make the module worth having. Holds, the auto sell-state and the history add on top.

1. Phase 1: confirm the baseline
2. Phase 2: the contract and its product adapter, the migration, the entity, its rules, the adapter
3. Phase 3 + 4: US1 and US2
4. **STOP and VALIDATE**: create a product, restock it, damage it past its stock and confirm the
   refusal, correct the count, and confirm the ledger reconciles
5. Deploy or demo if ready

### Incremental Delivery

1. Phase 1 + 2 → the tables, the entities and the storage guarantees exist and are proven
2. US1 → stock can be managed and read → validate independently
3. US2 → the shelf can never go negative → validate independently → the feature is usable
4. US3 → checkout can hold stock for fifteen minutes → validate independently
5. US4 → availability drives the sell state → validate independently → feature 006's debt is closed
6. US5 → a paid event changes stock exactly once → validate independently
7. US6 → the operator can account for the stock → validate independently
8. Phase 9 → documentation, the frontend hand-off, the ADR and the deferral record, then the close-out gate

### Parallel Team Strategy

With two developers:

1. Both complete Phase 1 and 2 together — T002–T004 (the contract and its product adapter) touch
   module 04 and should not be split; T020's adapter is the other contested file
2. Then split: **Developer A takes US1 + US2** (the manual write and read path), **Developer B takes
   US3** (the hold lifecycle and the sweeper). They touch different files: US1 owns
   `application/implement/stock.go` and the HTTP surface; US3 owns `application/implement/holds.go`
   and `presentation/worker/`
3. `presentation/http/router.go`, `presentation/dto/dto.go` and `application/implement/stock.go` are
   touched by more than one story — sequence those files rather than editing them in parallel
4. US4 wires the contract both completed; US5 follows US3; US6 follows US1

---

## Notes

- [P] tasks = different files, no dependencies
- [Story] labels map each task to a user story
- US1 and US2 ship together and share the adapter; US3, US4, US5 and US6 build on them, and that is
  stated rather than hidden
- Verify tests fail before implementing. A test that passes before its implementation exists is
  testing nothing, and T015, T022, T043 and T049 are the ones most likely to be written that way by
  accident
- Commit after each phase, not after each task
- The same-file overlaps are `presentation/http/stock_test.go` (US1, US2, US3, US6),
  `application/implement/stock.go` (US1's implementation and US3's below-held guard),
  `presentation/http/router.go`, `presentation/dto/dto.go` and `cmd/api/main.go` — sequence them
- This feature must **not** expose a customer-facing stock endpoint and must **not** add an
  application-level pre-check for the non-negative rule: an oversell is refused by the storage
  statement, or US2 has not been delivered

---

## Phase 10: Convergence

**Purpose**: Close the one coverage gap a convergence pass found between the spec's success
criteria and the tests that exist. Every other requirement, acceptance scenario, plan decision and
constitution principle is already satisfied by the delivered code; see the convergence report.

- [x] T067 [US3] Add a use-case test for the two customers contesting the last available unit that SC-004 names — a first order reserves the last unit so availability reaches zero while the physical shelf is unchanged, then a second order's reserve of one unit is refused with `INSUFFICIENT_STOCK` and holds nothing — in `internal/modules/inventory/application/implement/holds_test.go` per SC-004, US3/AC5 (`partial`). The delivered tests prove "a hold lowers availability" and "a hold larger than availability is refused" as separate facts, but none drives a second order against a unit already held, which is the scenario the success criterion calls out
