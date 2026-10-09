# Implementation Plan: Order Checkout

**Branch**: `009-order` | **Date**: 2026-10-09 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/009-order/spec.md`

## Summary

Module 07 Order, the fourth module of V1.0: a signed-in customer's cart becomes an order. Checkout
records what the customer is buying as a **snapshot** — the product's name, price and currency, and
the delivery address, as they were at that moment — **holds** the goods for the customer while they
pay, and empties the cart, all in one operation. The order then moves through a small, explicit set
of states, expires itself if it is left unpaid, and — because the system has no "cancel a paid
order" — can be handed to another account by transfer.

This is the module three earlier features leaned on, and it **pays three debts**: it consumes the
**reservation** contract module 05 withheld until the order flow existed (D1), it **snapshots** its
line's product so it survives the product's removal (D3 of 006/007), and it turns "cancel a paid
order" into a transfer that touches no stock (D2). It is also the first module to reach **five**
other modules at once — cart (to read and clear), product (the line's current facts), inventory (to
hold, consume and release), user (the address) and auth (the transfer target's account).

## Technical Context

**Language/Version**: Go 1.26.0

**Primary Dependencies**: Standard library plus the foundation — the chi router, pgx, the shared
response/error envelope, the shared `UnitOfWork`, the shared generic repository and `share/access`.
**No new dependency.**

**Storage**: PostgreSQL 16, one additive migration `migrations/00009_order.sql` creating `orders`
and `order_items`. Redis is not involved.

**Testing**: `testing` with `net/http/httptest`; in-memory fakes for the use cases and every
cross-module contract; `testcontainers-go` behind the `integration` tag for the adapter and the
checkout path — including what a fake cannot prove: the order state machine's storage check, the two
concurrency cases (two checkouts of one cart, and a hold that frees once), and that a removed product
leaves an order untouched. The expiry sweep is tested with an injected clock.

**Target Platform**: Linux server (Docker) in production, local development on Windows via Docker
Desktop.

**Project Type**: web-service

**Performance Goals**: The customer's and the operator's lists are paginated under the project's
existing convention. Checkout makes a **fixed** set of cross-module calls (one cart read, one bulk
product read, one bulk availability read, and one hold per line), not one read per line beyond the
holds, which are inherently per line.

**Constraints**: Money is **integer minor units plus an explicit currency**; the order total is the
sum of its line snapshots and carries **no shipping fee** (ADR 015 §5, the customer pays shipping on
delivery). The order does not hold stock itself: holding, consuming and releasing are module 05's,
reached through a contract. Every order action requires a session; the owner comes from the session.
The paid transition is module 08's to drive; this feature defines and tests it directly.

**Scale/Scope**: Two new tables, one new module, **three** new cross-module contracts
(`CartCheckout`, `InventoryReservation`, `AccountLookup`) with one adapter each in modules 06, 05 and
01, **three** contracts reused (`ProductCatalog` and `InventoryAvailability` of module 04/05, and
`CustomerLookupService` of module 02), a background expiry sweeper, nine endpoints, one migration,
and the documentation that moves with all of it.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-checked after Phase 1 design.*

- [x] **I. Modular Monolith & Clean Architecture**: A new module at `internal/modules/order` with the
      four layers. `domain` imports only the standard library, `internal/share/access` and
      `github.com/google/uuid`. The order reaches other modules **only** through `internal/contracts`:
      `CartCheckout` (06), `ProductCatalog` (04), `InventoryAvailability` and `InventoryReservation`
      (05), `CustomerLookupService` (02) and `AccountLookup` (01) — each implemented by the providing
      module's adapter at the composition root.
- [x] **II. Transactional Integrity**: Checkout runs in a single transaction: the cart is read, the
      order and its snapshot lines are written, the goods are held, and the cart is cleared, or none
      of it happens. Money is integer minor units with an explicit currency. Holding, consuming and
      releasing stock are module 05's use cases, and every hold change writes an
      `inventory_transactions` row there; the consume is idempotent by a per-line source reference
      (FR-014). The order itself changes no stock, so it writes no stock transaction of its own.
- [x] **III. State Machines & Invariants**: The order's five states and their allowed edges are
      explicit in the domain (FR-009), the state is changed only through the transition functions, and
      every edge is tested both ways. The state is a storage-constrained column, so an unlisted value
      cannot be written.
- [x] **IV. Test-First for Critical Logic**: Order totals, the state transitions and the stock hold
      are critical logic and are written test-first. The guarantees a fake cannot prove — the state
      check, two checkouts of one cart, and a hold freeing exactly once — are proven against real
      PostgreSQL.
- [x] **V. Security & Least Privilege**: The customer's order is the session's, never the request's
      (FR-020); the operator's endpoints require the administrator role (FR-023); a transfer changes
      ownership only through an administrator and only to an account that exists (FR-024). No upload,
      no new credential.
- [x] **VI. Observability**: Every administrator action — ship, complete, transfer — writes an audit
      entry naming the order and the administrator (FR-023). A customer's own acts (checkout, cancel)
      and the system's acts (paid, expiry) are recorded on the order's state and timestamps, not in
      the audit trail, which records administrative mutations.
- [x] **VII. Simplicity (YAGNI)**: No shipping fee, no refunds, no split orders, no discount codes,
      no separate payment-status axis (clarification). The expiry sweeper is the smallest way an
      unpaid order frees itself, mirroring module 05's.
- [x] **VIII. API Documentation as a Contract**: The nine endpoints, their shapes and the new codes
      update `docs/api-reference.md`, this feature's OpenAPI contract and the module documents in the
      same change.
- [x] **Frontend Integration Guide**: This feature adds client-facing endpoints, so
      `specs/009-order/frontend-guide.md` is a required deliverable, re-read and corrected in the
      final phase.
- [x] **Swagger annotations**: Every endpoint carries handler annotations and `docs/swagger/` is
      regenerated with `make swagger` in the same change, so `make swagger-check` passes.

**Post-design re-check**: all ten still hold. Nothing in Phase 0 or Phase 1 forced a violation.
Complexity Tracking records the deliberate choices below.

## Project Structure

### Documentation (this feature)

```text
specs/009-order/
├── plan.md  ├── spec.md  ├── research.md  ├── data-model.md  ├── quickstart.md
├── contracts/{openapi.yaml, error-codes.md}
├── frontend-guide.md  ├── deferred.md
└── checklists/requirements.md

docs/decisions/017-order-checkout.md   # the ADR this feature records
```

### Source Code (repository root)

```text
internal/modules/order/
├── domain/{constant,error,model,repository}
├── application/{dto,interface,implement,mapper}
├── infrastructure/implement/{postgres,auditor}
└── presentation/{dto,http,worker}   # worker: the unpaid-order expiry sweeper

internal/contracts/cart.go         # NEW: CartCheckout (provided by module 06)
internal/contracts/inventory.go    # + InventoryReservation (provided by module 05)
internal/contracts/account.go      # NEW: AccountLookup (provided by module 01)
internal/modules/cart/infrastructure/implement/checkout/checkout.go        # cart's adapter
internal/modules/inventory/infrastructure/implement/reservation/reservation.go  # inventory's adapter
internal/modules/auth/infrastructure/implement/account/account.go          # auth's adapter

migrations/00009_order.sql         # orders, order_items
cmd/api/main.go                    # mounts the module, wires the contracts, starts the sweeper
docs/api-reference.md              # the nine endpoints and the new codes
docs/modules/07-order.md  docs/modules.md
```

**Structure Decision**: A new module mirroring modules 03–06. Four things are specific to this
feature and recorded rather than assumed:

1. **Checkout reaches four modules at once**, each through `internal/contracts` — the cart (to read
   and clear), the product and inventory (to re-check), the user (the address) and auth (the transfer
   target). This is the widest cross-module reach in the project so far, and it is why the spec's
   three debts come due here.
2. **The order owns its expiry**, not module 05. Module 05 already frees an expired hold; the order
   must also leave "awaiting payment", so it stores `expires_at` and a sweeper cancels an unpaid
   order past it and asks inventory to release (idempotent, so the two sweeps cannot double-free).
3. **A line stores a snapshot, not a reference.** `order_items` carries the product's name, slug,
   price and currency as text rows and only an **informational** `product_id` with no foreign key, so
   the order never depends on a product that may be removed.
4. **Transfer is not a state change.** It changes the order's owner only, through an administrator
   and to an account that exists, and touches no stock (feature 007's `deferred.md` D2).

## Complexity Tracking

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| `AccountLookup` is a new cross-module contract against module 01 | A transfer is named by the recipient's email (clarification), and email is the auth module's fact; the order may not read auth's table, so it asks through a contract, whose adapter auth supplies at the composition root | Reading the account out of module 02's `CustomerLookupService` — rejected: that service is keyed by identifier, not email, and the email it returns is owned by module 01. Looking the account up by identifier and dropping email — rejected by the clarification |
| `order_items` keeps an informational `product_id` with no foreign key | The module document lists a product field on a line, and the plan records which product was bought, without the line depending on the product still existing | A foreign key with `ON DELETE CASCADE` would delete order lines when a product is removed, contradicting the snapshot; `RESTRICT` would block module 04's removal, which it deliberately allows |
| An order stores `expires_at` and sweeps its own expiry, mirroring module 05 | FR-012 requires an unpaid order to cancel itself, not merely for its hold to be freed; only the order can move its own state, so it needs its own deadline and sweep | Relying on module 05's hold expiry alone would free the goods but leave the order payable, so a later payment would find no hold — the inconsistent state FR-012 exists to prevent |
| `domain` imports `github.com/google/uuid` | `Order` and `OrderLine` express their primary keys as `uuid.UUID`, and the repository and contract inputs use the same type; Constitution I names this library the one allowed value-type import and requires the owning feature to record it here | Inventing an order-local identifier type — rejected: it duplicates the UUID concept for no behavioural gain, exactly as the constitution's amendment notes |
