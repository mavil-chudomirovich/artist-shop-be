# Research — Hide Guard with Pending Orders (016)

Phase 0 decisions. Each records what was chosen, why, and the alternatives rejected. The spec's
`Clarifications` (pending = PENDING + PAYMENT_PENDING; explicit product flag; every move away from
on-sale is checked; hard delete out of scope) are inputs here, not open questions.

## D1 — Pending-order lookup lives in the order module behind a new contract

**Decision**: add `PendingOrdersForVariants(variantIDs) → order IDs` to `internal/contracts/order.go`.
The order module supplies the adapter at the composition root; it answers from a single bulk query
over `order_items` joined to `orders`, filtered to `status IN ('PENDING','PAYMENT_PENDING')`.

**Rationale**: Constitution I forbids category/product/cart from reading order tables; a contract is the
prescribed arrangement. One bounded read satisfies spec SC-004 for a hundreds-of-variants category.

**Alternatives rejected**: product/category reading `order_items` directly (Constitution I violation);
one availability-style call per variant (unbounded round trips, breaks SC-004); reusing the payment
contract (wrong purpose and wrong consumer).

## D2 — Category-to-variant resolution lives in the product module behind a contract

**Decision**: add `VariantIDsByCategories(categoryIDs) → variant IDs` to `internal/contracts/product.go`,
implemented by the product module over its own tables in one bulk read. Category passes category IDs
and receives a flat variant-ID set; it never learns product internals.

**Rationale**: the category module cannot resolve "my variants" without reading product tables
(Constitution I). A flat ID set is all the check and the cleanup need.

**Alternatives rejected**: storing a variant list on the category (duplicated source of truth);
per-product fan-out calls from category (unbounded).

## D3 — Guard placement and atomicity

**Decision**: the check runs inside each hiding use case — category `UpdateCategory`, product
`ChangeVariantState` / product flag edit — within that use case's UnitOfWork transaction, before any
write. Refusal leaves everything untouched (spec SC-001). Category's Service gains the `Tx`
field its use case needs; product and cart already carry one.

**Rationale**: check-then-hide must be atomic or a racing checkout could slip between them; the
repository layer must not own the boundary (Constitution I), so the use case owns it as elsewhere.

**Alternatives rejected**: checking in the presentation layer (business rule in HTTP, forbidden);
checking outside a transaction (TOCTOU window the spec's edge case explicitly closes).

## D4 — Cart cleanup is a cart-module contract executed in the same transaction

**Decision**: add `RemoveVariantsFromAllCarts(variantIDs) → removed line count` to
`internal/contracts/cart.go`; the cart module implements it as one `DELETE FROM cart_items WHERE
variant_id = ANY($1)` in its repository, called by the hiding use case right after the hide succeeds,
inside the same transaction.

**Rationale**: only the cart module may write its tables; a single statement keeps cleanup bounded and
atomic with the hide, so a successful hide can never leave stale lines (spec SC-002) and a refused
hide never touches carts.

**Alternatives rejected**: per-cart iteration in application code (unbounded; breaks atomicity);
cleanup in a follow-up job (eventual consistency the spec forbids — SC-002 demands zero lines after a
successful hide, observably).

## D5 — System-initiated stock-out transitions go through the same guard

**Decision**: the inventory reconcile path (`reconcile.go` → product's `MarkVariantOutOfStock`
adapter) funnels through the same guarded application transition as `ChangeVariantState`.
`MarkVariantOnSale` (a move back to on-sale) never passes through the guard. A blocked system signal
is a no-op on state: the variant keeps its on-sale state and the blocked event is recorded;
inventory's stock operation never fails because of it.

**Rationale**: the clarify decision covers every move away from on-sale regardless of initiator.
Failing the stock write would corrupt inventory truth to serve a display rule; the existing
tolerant-signal design (no-op + record) already handles exactly this shape.

**Alternatives rejected**: exempting the system path (contradicts the recorded clarification);
failing the reconcile on block (turns a display guard into an inventory failure).

## D6 — Product flag column and the catalogue predicate

**Decision**: migration `00016_product_visibility_flag.sql` adds `products.is_visible boolean NOT NULL
DEFAULT true`; Down drops it. The public catalogue predicate gains one term (`p.is_visible`) alongside
the existing category and on-sale-variant terms; no new index (small table, existing predicates
dominate; YAGNI, Constitution VII).

**Rationale**: an explicit flag matches the category's `is_visible` precedent the clarification chose
over cascade; the predicate stays in SQL with the rest of the visibility rule.

**Alternatives rejected**: cascade-hiding variants (rejected in clarification — un-hide needs prior
states); a partial index on the flag (unmeasured need).

## D7 — Refusal shape: 409 with blocking order IDs

**Decision**: three new codes — `CATEGORY_HAS_PENDING_ORDERS`, `PRODUCT_HAS_PENDING_ORDERS`,
`VARIANT_HAS_PENDING_ORDERS` — each a 409 whose error detail carries the blocking order IDs. They are
admin-surface only (all three hide actions sit behind the ADMIN guard), so no customer information
leak. Documented in `docs/api-reference.md` and the feature's `contracts/error-codes.md`.

**Rationale**: the spec requires the refusal to name the orders so an administrator can act; the
project's coded-error + details shape already carries exactly this.

**Alternatives rejected**: a bare 409 with no detail (operator cannot act); 400 (the request is
well-formed — the state forbids it); 422 (no such convention in the error catalogue).

## D8 — Audit reuse, no new action vocabulary

**Decision**: hide mutations keep their existing audit actions (`AuditCategoryHidden`,
product hide/state-change actions); the blocked system signal is recorded through the same audit path
with a system actor; the cart-cleanup count travels as metadata on the hide event rather than as a new
action type.

**Rationale**: Constitution VI requires the mutation and actor recorded, not a new action per
sub-effect; reusing actions keeps the audit vocabulary stable.

**Alternatives rejected**: new audit actions per guard outcome (vocabulary churn for information the
metadata already carries).
