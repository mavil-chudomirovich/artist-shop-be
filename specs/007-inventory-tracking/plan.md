# Implementation Plan: Inventory Tracking

**Branch**: `007-inventory-tracking` | **Date**: 2026-10-08 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/007-inventory-tracking/spec.md`

## Summary

Module 05 Inventory: the shop can finally count what it holds. An administrator restocks, records
damage and corrects a count; every physical change leaves an append-only ledger entry; stock can
never go negative, even under concurrent operations; and a product's availability changes by itself
as stock crosses zero. A customer entering payment holds the goods for fifteen minutes, and the
hold is released, consumed or expired exactly once. This is the module feature 006 left the
automatic half of the sell state to, so it **closes obligation D1** and makes the product document's
third flow true.

Three things make this more than a copy of an earlier module. First, it is the **first module to
drive a change in a module it also depends on**: inventory depends on product, and it must ask
product to move between on-sale and out-of-stock, so two cross-module contracts appear and a
product adapter is written on the product side. Second, availability is a derived fact — physical
stock minus what active holds have set aside — so the module owns **holds**, not just a count. Third,
a hold must end by itself after fifteen minutes, which the delivered system has no mechanism for, so
this feature introduces the project's **first background sweeper**.

## Technical Context

**Language/Version**: Go 1.26.0

**Primary Dependencies**: Standard library plus what the foundation already provides — the chi
router, pgx, the shared response/error envelope, the shared `UnitOfWork` over `database.DB`, the
shared audit writer, the shared generic repository and `share/access`. **No new dependency**:
`go.mod` and `go.sum` stay untouched. Expiry is driven by `time.Ticker` in the composition root;
no scheduler library is added.

**Storage**: PostgreSQL 16, one additive migration `migrations/00007_inventory.sql` creating three
tables (`stock_levels` for the live physical count and the concurrency gate, `inventory_transactions`
for the append-only ledger, `stock_holds` for what is set aside). Redis is not involved: nothing here
is cached, rate-limited per module, or short-lived beyond the fifteen-minute hold, which is stored
in PostgreSQL because it is a business fact that must survive a restart.

**Testing**: `testing` with `net/http/httptest` for the handler and router layers; in-memory fakes
for the use cases; `testcontainers-go` behind the `integration` build tag for the adapter — including
the four guarantees a fake cannot prove: physical stock never goes below zero under two concurrent
writers, a source reference cannot be recorded twice, a product cannot hold the same order twice,
and a removed product takes its stock rows with it. The expiry sweep is tested with an injected
clock and an interval the test controls, never by sleeping for fifteen minutes.

**Target Platform**: Linux server (Docker) in production, local development on Windows via Docker
Desktop.

**Project Type**: web-service

**Performance Goals**: Administrator operations answer inside the platform's existing response
target. The stock reads and writes carry no new goal of their own: the shop holds a few rows per
product, and the availability sum is over one product's active holds, bounded by the orders in
flight. The platform's standing target applies rather than a new number invented here.

**Constraints**: A quantity is a **whole number** that is never negative; money is not involved
anywhere in this module. Availability = physical − active holds. A hold lives **fifteen minutes**.
Every physical change writes an `inventory_transactions` row in the same transaction as the level
update. Writes are administrator-only; there is **no customer-facing stock endpoint** — a customer
learns availability through the product's sell state, which this module drives through a contract.
The order/payment triggers that create and resolve a hold belong to modules that do not exist, so
this feature delivers the holding capability and its sweeper and tests them directly. No dedicated
module rate limit.

**Scale/Scope**: Three new tables, one new module, two cross-module contracts
(`internal/contracts/product.go`) with one product-side adapter, one system-facing product use case,
one background sweeper, five administrator endpoints, one migration, and the documentation that must
move with all of it.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-checked after Phase 1 design.*

- [x] **I. Modular Monolith & Clean Architecture**: A new module at `internal/modules/inventory`
      with the four layers the project uses. `domain` imports only the standard library,
      `internal/share/access` and `github.com/google/uuid`; `application` depends on `domain` and its
      own ports; `infrastructure` implements those ports; `presentation` translates HTTP and hosts
      the sweeper. The cross-module dependency is two contracts in `internal/contracts`:
      `ProductAvailability` (the signal) and `ProductLookup` (the existence question), and the
      **product module supplies the adapter at the composition root**, so inventory never imports
      product's internals and product never imports inventory's (research D4). Because the contracts
      are provided by product and consumed by inventory, the dependency still runs one way —
      `inventory → product` — as the roadmap states.
- [x] **II. Transactional Integrity**: Every stock change runs inside a single transaction that
      writes the level, the ledger row and any availability consequence together (FR-011), and the
      no-negative rule is enforced on the storage row rather than in application code (FR-010,
      research D2). Every physical change writes an `inventory_transactions` row (FR-004,
      Constitution II). Outside events are idempotent: the source reference is unique at the storage
      layer and a replay is a no-op (FR-020 to FR-022, research D5). Quantities are integers; money
      is not part of this module. **One reading of Constitution II is narrowed by the spec's
      clarification and is recorded rather than hidden**: the constitution's "a cancelled order
      restores it" describes an unpaid order, whose hold is released without a physical change; the
      system has no cancellation of a *paid* order (a paid order is transferred instead), so no
      physical stock is restored for one. See Complexity Tracking.
- [x] **III. State Machines & Invariants**: The sell state is product's explicit machine; this
      module **drives two of its edges** (`ACTIVE ↔ OUT_OF_STOCK`) through the contract rather than
      writing a state, and it refuses to touch announced or retired products (FR-024 to FR-028). The
      transition is idempotent and tolerant, so an already-target state or a terminal state is a
      no-op rather than a failure (research D4). Both directions are covered by positive and
      negative tests.
- [x] **IV. Test-First for Critical Logic**: Inventory calculation — physical, held, available, the
      no-negative refusal, the zero-crossing transition, hold expiry, and exactly-once application —
      is constitutional critical logic, so every piece is written as a failing test first. The two
      guarantees a fake cannot prove — no-negative under concurrency and source-reference uniqueness
      — are proven against real PostgreSQL.
- [x] **V. Security & Least Privilege**: Every administrator operation is refused unless the caller
      holds the administrator role, and the actor comes from the session, never from the request
      (FR-005). There is no customer-facing surface, so nothing is exposed to a customer beyond the
      product sell state the product module already serves. No new credential, no new secret, no
      upload.
- [x] **VI. Observability**: Every manual operation writes an audit entry naming the product, the
      operator, the kind and the amount (FR-006), reusing the shared audit writer. A stock change
      applied from an outside event is audited too, naming the event's source reference with no human
      actor (FR-020), so a payment event's inventory effect is traceable and Constitution VI's
      "payment events are recorded" is satisfied even though module 08 does not exist yet. A
      system-driven availability change is recorded where it is caused — the inventory ledger and a
      `PRODUCT_STATE_CHANGED` audit on the product side with no human actor (research D4). Expiring a
      hold is an operational act and is recorded on the hold row, not in the audit trail.
- [x] **VII. Simplicity (YAGNI)**: No multiple locations, no low-stock threshold, no forecasting, no
      customer-facing stock endpoint, and no published contract for the order/payment triggers,
      because the consumers do not exist — the holding use cases are delivered and tested directly
      and the contract is added when Order is specified (research D7, `deferred.md`). The one
      mechanism that is genuinely new — a periodic sweeper — is the smallest way to make an expiring
      hold release itself, and its alternative is recorded in research D6.
- [x] **VIII. API Documentation as a Contract**: The five administrator endpoints, their request and
      response shapes and the new `INVENTORY_INSUFFICIENT_STOCK` code update `docs/api-reference.md`,
      this feature's OpenAPI contract and the module documents in the same change as the code.
- [x] **Frontend Integration Guide**: This feature adds client-facing administrator endpoints, so
      `specs/007-inventory-tracking/frontend-guide.md` is a required deliverable, enumerating each
      endpoint as new with its shape and its refusals. It is re-read and corrected against the
      source in the final phase and after any convergence pass.

**Post-design re-check**: all nine still hold. Nothing in Phase 0 or Phase 1 forced a violation.
Complexity Tracking records the four deliberate choices below; each is a choice a later reader
would otherwise question, and none breaks a principle.

## Project Structure

### Documentation (this feature)

```text
specs/007-inventory-tracking/
├── plan.md              # This file
├── spec.md              # Feature specification
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/
│   ├── openapi.yaml     # The five administrator endpoints
│   └── error-codes.md   # Every code this module can answer with
├── frontend-guide.md    # Required by the constitution: the client-facing hand-off
├── deferred.md          # What this feature deliberately does not carry
└── checklists/
    └── requirements.md  # Spec quality checklist
```

### Source Code (repository root)

```text
internal/modules/inventory/
├── domain/
│   ├── constant/
│   │   ├── audit.go          # the audit actions this module records
│   │   ├── codes.go          # the INVENTORY_* machine codes
│   │   ├── hold.go           # hold status names and the fifteen-minute hold window
│   │   └── movement.go       # the four movement kinds
│   ├── error/errors.go       # the module's business sentinel errors
│   ├── model/
│   │   ├── stock.go          # physical count and the availability rule
│   │   ├── movement.go       # an append-only ledger entry and its kind rules
│   │   └── hold.go           # a hold, its status and its expiry rule
│   └── repository/inventory.go # the persistence contract, no transaction inside it
├── application/
│   ├── dto/dto.go            # use-case input and output types
│   ├── interface/
│   │   ├── actor.go          # the acting administrator, from the session
│   │   └── ports.go          # the use-case surface, the availability contract, UnitOfWork, Clock
│   ├── implement/
│   │   ├── stock.go          # restock, damage, adjustment and the stock read
│   │   ├── holds.go          # reserve, release and expire
│   │   ├── events.go         # the sale that consumes a hold, idempotent by source reference
│   │   ├── history.go        # the paged movement history
│   │   └── reconcile.go      # availability → the product's sell state, in the same transaction
│   └── mapper/mapper.go      # the single place model becomes dto
├── infrastructure/implement/
│   ├── postgres/inventory.go # the adapter: level, ledger and holds, with explicit projections
│   └── auditor/auditor.go    # the audit adapter over the shared writer
└── presentation/
    ├── dto/dto.go            # the administrator HTTP shapes: stock, movement, request bodies
    ├── worker/sweeper.go     # the ticker that releases expired holds
    └── http/
        ├── handler.go        # request to use case, use case to response
        ├── router.go         # the /admin/inventory group
        └── errors.go         # sentinel to status, code and field detail

internal/contracts/product.go        # NEW: ProductAvailability and ProductLookup, provided by module 04 for module 05
internal/modules/product/infrastructure/implement/availability/availability.go
                                     # NEW: product's adapter over both contracts, over its own repo
internal/modules/product/application/implement/availability.go
                                     # NEW: the system-facing transition use case the adapter calls

migrations/00007_inventory.sql       # stock_levels, inventory_transactions, stock_holds
cmd/api/main.go                      # mounts the module, wires the contract, starts the sweeper
docs/api-reference.md                # the five endpoints and the new code
docs/modules/05-inventory.md         # status, spec pointer, completion criteria
docs/modules/04-product.md           # the D1 obligation that is now closed
docs/modules.md                      # module 05 status
```

**Structure Decision**: A new module directory mirroring module 04, because module 04 is the most
recently completed module and therefore the cheapest pattern to follow and the easiest to review
against. Three things break the mirror deliberately:

1. **Two cross-module contracts appear, in the other direction from module 04's.** Module 04's
   contract (`CategoryQuery`) is asked of module 03 and answered over module 03's repository. This
   feature's `ProductAvailability` is not a question but an instruction, and it makes the *consumed*
   module change its own data; `ProductLookup` is a question — whether a product exists — that a
   read or a decrease must answer and the foreign key cannot (research D4, D12). Both are declared in
   `internal/contracts` and both are implemented by the providing module at the composition root,
   which is exactly what Constitution I prescribes; product supplies the adapter because product owns
   the sell state and the product rows.
2. **A system-facing use case is added to module 04**, deliberately separate from
   `ChangeSellState`: the administrator endpoint requires an admin actor and audits one, while the
   availability signal has no human actor. Folding the two together would force a fake actor into the
   administrator path or an administrator requirement onto a system path.
3. **A background sweeper is introduced in `presentation/worker`**, the folder the architecture
   already reserves for one and which no earlier module has used. It is a thin loop calling an
   ordinary application use case, started and stopped by the composition root, so the business rule
   it enforces stays in the domain and the sweeper itself carries none.

## Complexity Tracking

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| The holding capability is built before its consumer exists | The clarified spec requires a fifteen-minute hold, and availability is a lie without it: nothing else can stop the last unit being sold twice between checkout and payment. The guarantee has to live here, because availability is inventory's to own | Deferring holds to module 07 Order was rejected: availability would then be computed without them, so the module would answer "in stock" for goods already promised elsewhere — the exact oversell US2 forbids. The use cases are delivered and tested directly; the cross-module contract that will carry them is added when Order is specified (research D7) |
| An inventory transaction can include a write to the product module (`ProductAvailability`) | FR-026 requires the product to stop being buyable in the *same* transaction the stock falls in, so the availability change must join the caller's transaction rather than run after it | Doing it outside the transaction was rejected: a crash between the stock write and the product write would leave a product buyable with nothing in stock, which is what FR-026 exists to prevent. Warning the product asynchronously has the same window and adds an event system the project does not have |
| The constitution's "a cancelled order restores stock" is read as an unpaid order's hold release, not a physical restore | The spec's clarification settles that paid orders are never cancelled — they are transferred — so no physical stock is ever restored for one; the only cancellation, of a still-held order, returns availability without touching the shelf | Restoring physical stock on a paid-order cancellation was rejected because no such flow exists (the clarification), and inventing one would be scope the operator did not ask for |
| Three tables where the module document names one (`inventory_transactions`) | The live physical count needs its own row to be the concurrency gate the no-negative rule is enforced on — `UPDATE ... WHERE quantity >= $n` is the only race-free way to refuse an oversell — and a hold is not a movement, so it cannot live in the ledger | Deriving physical stock by summing the ledger was rejected: with no row to lock, two concurrent decreases both read the same sum and both insert, producing a negative stock. Recording holds as ledger entries was rejected: FR-004 says a hold is not a physical change, and mixing them would corrupt the reconciliation SC-001 asserts |
