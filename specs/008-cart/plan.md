# Implementation Plan: Shopping Cart

**Branch**: `008-cart` | **Date**: 2026-10-09 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/008-cart/spec.md`

## Summary

Module 06 Cart, the first module of V1.0: a signed-in customer gathers the products they intend to
buy — add, change and remove lines — and sees an accurate subtotal, while the cart refuses anything
that cannot be bought. It is the first module to **consume two delivered modules at once**: the
product module says whether a product is on sale, what it costs and what it is called, and the
inventory module says how many are available. Neither fact is the cart's to own, so this feature
introduces the two cross-module contracts that reach them: a new product-facts read and a new
**inventory availability read**. The reservation contract module 05's `deferred.md` D1 still owes to
the order flow is a different contract and stays open — this feature neither uses nor closes it.

Three things shape this module. First, the cart **checks availability but never holds it**: holding
is the inventory module's job and happens when a customer begins paying, so the checkout re-checks
before an order is created. Second, the **price is a snapshot** captured when a product is added and
shown to the customer, and the checkout re-checks it — the cart never silently charges a stale
price. Third, **viewing the cart re-checks every line** against the product's current state and
availability, and tells the customer what changed rather than letting them discover it at checkout.

## Technical Context

**Language/Version**: Go 1.26.0

**Primary Dependencies**: Standard library plus what the foundation already provides — the chi
router, pgx, the shared response/error envelope, the shared `UnitOfWork`, the shared generic
repository and `share/access`. **No new dependency**: `go.mod` and `go.sum` stay untouched.

**Storage**: PostgreSQL 16, one additive migration `migrations/00008_cart.sql` creating two tables
(`carts` and `cart_items`). Redis is not involved.

**Testing**: `testing` with `net/http/httptest` for the handler and router layers; in-memory fakes
for the use cases and for the two cross-module contracts; `testcontainers-go` behind the `integration`
build tag for the adapter — including what a fake cannot prove: the one-cart-per-account unique
index, the one-line-per-product unique index, a cart line surviving its product's removal, and two
concurrent adds to the same cart never leaving a line above what is available.

**Target Platform**: Linux server (Docker) in production, local development on Windows via Docker
Desktop.

**Project Type**: web-service

**Performance Goals**: A cart view answers inside the platform's existing response target. The view
makes a **fixed two cross-module calls** — one product-facts read and one availability read for the
whole cart — rather than one per line, and both adapters answer the whole set in **one indexed query**
(`= ANY(...)`), so neither the number of cross-module calls nor the number of database queries grows
with the number of lines.

**Constraints**: Money is **integer minor units plus an explicit currency**; floating point is never
used. The MVP shop is **single-currency**, and all products in one cart share a currency, so a cart
never mixes currencies. An empty cart has no money to express, so its `subtotal` is **absent (null)**
rather than a zero in an invented currency. The cart
**never reserves stock**. Every read and change requires a session, and the owner comes from the
session, never the request. There is no cart for a guest; no discount, upsell or saved-cart
features. No dedicated module rate limit.

**Scale/Scope**: Two new tables, one new module, **two new cross-module contracts**
(`ProductCatalog` in `internal/contracts/product.go`, `InventoryAvailability` in a new
`internal/contracts/inventory.go`) with one adapter each in modules 04 and 05, four customer
endpoints, one migration, and the documentation that must move with all of it.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-checked after Phase 1 design.*

- [x] **I. Modular Monolith & Clean Architecture**: A new module at `internal/modules/cart` with the
      four layers the project uses. `domain` imports only the standard library, `internal/share/access`
      and `github.com/google/uuid`; `application` depends on `domain` and its own ports;
      `infrastructure` implements those ports; `presentation` translates HTTP. The cart's two
      cross-module dependencies are contracts in `internal/contracts` — `ProductCatalog` (provided by
      module 04) and `InventoryAvailability` (provided by module 05) — each implemented by the
      **providing** module's adapter at the composition root, so the cart never imports another
      module's internals and the dependency runs one way (`cart → product`, `cart → inventory`).
- [x] **II. Transactional Integrity**: Cart writes run inside a single transaction (the `UnitOfWork`
      port), and money is integer minor units with an explicit currency (FR-009). No stock is changed
      and no payment stage is touched — holding stock is the inventory module's job and happens at
      payment — so the `inventory_transactions` rule does not apply here. The no-over-availability
      guarantee is made race-free by locking the cart's row for the whole add/update (research D9).
- [x] **III. State Machines & Invariants**: The cart has no lifecycle state machine (it is a
      collection, not a lifecycle). Its invariants — one cart per account, one line per product, a
      positive quantity, and a line never above what is available — are enforced where they can be
      bypassed: the first two as unique indexes at the storage layer, the last under the cart's row
      lock.
- [x] **IV. Test-First for Critical Logic**: The critical logic here is money — the line total and
      the subtotal — plus the quantity and availability rules and the "can it be bought" decision.
      Each is written as a failing test first. The guarantees a fake cannot prove — the two unique
      indexes and a concurrent add — are proven against real PostgreSQL.
- [x] **V. Security & Least Privilege**: Every route requires a session, and the cart's owner is
      taken from the session, never the request, so cross-account access is impossible by
      construction (FR-010). The cart is customer-only; there is no admin surface and no upload.
- [x] **VI. Observability**: Cart operations are a customer's own actions, not administrative
      mutations, so they write no `audit_logs` entry; the shared structured logging with the request
      correlation id covers them. No secrets, no PII beyond the account the session already carries.
- [x] **VII. Simplicity (YAGNI)**: No guest cart, no saved/later carts, no discounts, no upsell, no
      stock reservation, no pagination for a single bounded resource. The two contracts are the
      smallest shape each consumer needs (research D1, D2), and the price snapshot is the one the
      spec names rather than a second store of the product.
- [x] **VIII. API Documentation as a Contract**: The four endpoints, their requests and responses and
      the new error codes update `docs/api-reference.md`, this feature's OpenAPI contract and the
      module documents in the same change.
- [x] **Frontend Integration Guide**: This feature adds client-facing endpoints, so
      `specs/008-cart/frontend-guide.md` is a required deliverable, enumerating each endpoint as new
      with its shape and its refusals. It is re-read and corrected against the source in the final
      phase and after any convergence pass.
- [x] **Swagger annotations**: Every endpoint this feature adds carries handler annotations
      (`@Summary`, `@Tags`, `@Param`, `@Success`, `@Failure`, `@Router`) and `docs/swagger/` is
      regenerated with `make swagger` in the same change, so `make swagger-check` passes
      (Constitution VIII, Definition of Done).

**Post-design re-check**: all ten still hold. Nothing in Phase 0 or Phase 1 forced a violation.
Complexity Tracking records the three deliberate choices below.

## Project Structure

### Documentation (this feature)

```text
specs/008-cart/
├── plan.md              # This file
├── spec.md              # Feature specification
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/
│   ├── openapi.yaml     # The four customer endpoints
│   └── error-codes.md   # Every code this module can answer with
├── frontend-guide.md    # Required by the constitution: the client-facing hand-off
├── deferred.md          # What this feature does not carry
└── checklists/
    └── requirements.md  # Spec quality checklist
```

### Source Code (repository root)

```text
internal/modules/cart/
├── domain/
│   ├── constant/codes.go     # the CART_* machine codes
│   ├── error/errors.go       # the module's business sentinel errors
│   ├── model/cart.go         # the cart, its line and the subtotal rule
│   └── repository/cart.go    # the persistence contract, no transaction inside it
├── application/
│   ├── dto/dto.go            # use-case input and output types
│   ├── interface/
│   │   ├── actor.go          # the acting customer, from the session
│   │   └── ports.go          # the use-case surface, the two contracts, UnitOfWork, Clock
│   ├── implement/cart.go     # get, add, change quantity, remove
│   └── mapper/mapper.go      # the single place model becomes dto
├── infrastructure/implement/postgres/cart.go  # the adapter, explicit Columns projection
└── presentation/
    ├── dto/dto.go            # the customer HTTP shapes
    └── http/
        ├── handler.go        # request to use case, use case to response
        ├── router.go         # the /cart group
        └── errors.go         # sentinel to status, code and field detail

internal/contracts/product.go        # + ProductCatalog (provided by module 04 for the cart)
internal/contracts/inventory.go      # NEW: InventoryAvailability (provided by module 05 for the cart)
internal/modules/product/infrastructure/implement/catalog/catalog.go
                                     # NEW: product's ProductCatalog adapter over its own repository
internal/modules/inventory/infrastructure/implement/availability/availability.go
                                     # NEW: inventory's InventoryAvailability adapter over its repo

migrations/00008_cart.sql            # carts, cart_items
cmd/api/main.go                      # mounts the module and wires both contracts
docs/api-reference.md                # the four endpoints and the new codes
docs/modules/06-cart.md              # status, spec pointer, completion criteria
docs/modules.md                      # module 06 status
```

**Structure Decision**: A new module directory mirroring modules 03–05, because they are the most
recently completed modules and the cheapest pattern to follow. Three things are specific to this
feature and recorded rather than assumed:

1. **The cart is the first module to depend on two modules at once**, and both are reached through
   `internal/contracts` rather than by reading a table (Constitution I). `ProductCatalog` is a new
   interface beside module 04's existing `ProductAvailability`/`ProductLookup`; `InventoryAvailability`
   is the first entry in a new `internal/contracts/inventory.go` — an **availability read** published
   now because a consumer exists. It is distinct from the **reservation** contract module 05's
   `deferred.md` D1 owes to the order flow, which stays open (research D2).
2. **A cart line references its product loosely**, with no foreign key, so that removing a product
   does not delete the customer's line and the line can be reported as no longer available rather
   than silently vanishing (FR-012). This is a deliberate asymmetry with an order line, which will
   snapshot its product in module 07.
3. **The cart read re-checks each line** against the two contracts (the Q1 clarification), which is
   why the read is not a plain table scan but a scan plus a fixed two cross-module lookups.

## Complexity Tracking

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| Module 05 gains an availability-read contract it had not published | The cart must show what is *available* (physical minus active holds), a fact only inventory can answer, and the cart may not read inventory's tables (Constitution I). This is a new **read**, separate from the **reservation** contract module 05's `deferred.md` D1 owes to the order flow — that one stays open | Reading `stock_levels`/`stock_holds` in the cart's query would put module 05's schema inside module 04's consumer and break the ownership rule. Per-product calls into the module would need a use case where a read answers a number |
| `domain` imports `github.com/google/uuid` | Constitution I allows exactly this one value-type library for UUID primary keys, and records the allowance in the owning feature's plan as the rule requires | Inventing a per-module identifier type would duplicate the concept without any behavioural gain — the same reasoning recorded when the allowance was added |
| Module 04 gains a second read contract (`ProductCatalog`) | The cart needs a product's name, its on-sale state and its price; `ProductLookup` answers only "does it exist", and `ProductAvailability` is a write signal. Adding the method to `ProductLookup` would force module 05's existence fake to grow a method it never uses | Reusing `ProductLookup` for the facts mixes a one-bit question with a richer read and widens a dependency module 05 already holds. Exposing the four-state sell enum instead of a boolean would leak module 04's state machine into module 06 |
| A cart line carries no foreign key to its product | Removing a product must leave the customer's line in place so the cart can tell them it is no longer available (FR-012), which a cascading key would silently delete | `ON DELETE CASCADE` would drop the line and silently change the cart, contradicting FR-012. `ON DELETE RESTRICT` would block removing a product, which module 04 chose to allow. A snapshot of the product mirrors an order line's future shape and is more than a cart needs |
