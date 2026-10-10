# Implementation Plan: Order Confirmation & Editing

**Branch**: `010-order-confirmation` | **Date**: 2026-10-10 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/010-order-confirmation/spec.md`

## Summary

Module 07 Order, extension of feature `009-order`: an order no longer jumps straight to "awaiting
payment". Checkout now creates an order **awaiting the artist's confirmation** and **holds no goods**;
the artist (the shop operator) **confirms** it, which **holds the whole order at once** and opens a
**60-minute payment window**; only then does the customer pay, and the order continues through
`PAID → SHIPPED → COMPLETED` as before. The artist can **decline** an order. Before paying, the
customer may **change** an order — changing an order that is already awaiting payment **returns it to
awaiting confirmation** and releases its goods. The artist is **notified** whenever an order needs
confirmation and the customer is **notified** on every status change. Every edit is **recorded** and
bumps the order's **version**, which module 08 will use to reject a payment that belongs to an old
version of the order.

This supersedes the state machine of `009-order` (ADR 017): `PENDING_PAYMENT` is renamed
`PAYMENT_PENDING` and a new `PENDING` state is added; `CONFIRMED` is folded into `PAYMENT_PENDING`,
and `REJECTED`/`EXPIRED` reuse `CANCELLED`. It is also the first feature to **send email**, so a
minimal shared mailer is introduced without touching module 01's own sender.

## Technical Context

**Language/Version**: Go 1.26.0

**Primary Dependencies**: Standard library plus the foundation — the chi router, pgx, the shared
response/error envelope, the shared `UnitOfWork` (already transaction-aware since `009`), the shared
generic repository and `share/access`. Email uses `net/smtp`, exactly as module 01 already does.
**No new dependency.**

**Storage**: PostgreSQL 16, one schema-changing migration `migrations/00011_order_confirmation.sql`
(it renames a column, drops `NOT NULL`, widens the status check and backfills the old `PENDING_PAYMENT`
value). It changes the `orders_status_ck` check to the new six-value set, renames `expires_at` →
`payment_expires_at` (now nullable), and adds `order_version`, `confirmed_at`; it also creates
`order_edit_history`. Redis is not involved.

**Testing**: `testing` with `net/http/httptest`; in-memory fakes for the use cases and every
cross-module contract (including the new mailer); `testcontainers-go` behind the `integration` tag for
the storage and the checkout/confirm/edit paths — including what a fake cannot prove: the state
check, the all-or-nothing reservation under two competing confirmations, a hold released exactly once
on edit/expiry, and the edit history. Deadlines are tested with an injected clock.

**Target Platform**: Linux server (Docker) in production, local development on Windows via Docker
Desktop.

**Project Type**: web-service

**Performance Goals**: Both lists remain paginated under the project's convention. Confirmation makes
one hold call per line (inherently per line); checkout no longer holds at all.

**Constraints**: Money is **integer minor units plus an explicit currency**; the order total is the
sum of its line snapshots with **no shipping fee** (ADR 015 §5). Holding, releasing and selling stock
are module 05's, reached through the existing `InventoryReservation` contract. The payment window is
**60 minutes**, which requires aligning module 05's hold window (`HoldTTL`) to 60 minutes so a hold
does not lapse before the customer pays. Every order action requires a session; the owner comes from
the session. The paid transition stays module 08's to drive.

**Scale/Scope**: One migration (3 column changes on `orders`, 1 new table), **three** new endpoints
(`PUT /orders/{id}`, `POST /admin/orders/{id}/confirm`, `POST /admin/orders/{id}/reject`) plus
`status`/`sort` query parameters on the admin list, **two** new states, one new application port
(`Notifier`) with an adapter, one new shared package (`internal/share/mailer`), a change to module 05's
hold window, and the documentation that moves with all of it.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-checked after Phase 1 design.*

- [x] **I. Modular Monolith & Clean Architecture**: The work stays inside `internal/modules/order`
      with its four layers; the new `Notifier` port lives in `application/interface`, its adapter in
      `infrastructure/implement/notifier`, and the mailer it uses lives in `internal/share/mailer`.
      `domain` imports only the standard library, `internal/share/access` and `github.com/google/uuid`.
      Cross-module calls stay in `internal/contracts` (`InventoryReservation`, `ProductCatalog`,
      `InventoryAvailability`, `CustomerLookupService`).
- [x] **II. Transactional Integrity**: Confirm, edit, cancel and paid each run in one transaction;
      confirming holds every line in that one transaction, so a failure on any line rolls back the
      whole hold (all-or-nothing). Stock changes are module 05's use cases and write
      `inventory_transactions` there; a release is idempotent. Money is integer minor units + currency.
      Payment-callback idempotency stays module 08's.
- [x] **III. State Machines & Invariants**: The six states and their allowed edges are explicit in the
      domain, changed only through the transition methods, and every edge is tested both ways. The
      state is a storage-constrained column, so an unlisted value cannot be written.
- [x] **IV. Test-First for Critical Logic**: Order totals, the state transitions and the stock hold are
      critical logic and are written test-first. The guarantees a fake cannot prove — the state check,
      two confirmations competing for the last unit, and a hold freed exactly once — are proven against
      real PostgreSQL.
- [x] **V. Security & Least Privilege**: The owner comes from the session, never the request; only the
      order's owner edits/cancels; only the operator confirms/rejects/advances. No upload, no new
      credential; the mailer reads SMTP settings from the environment like module 01.
- [x] **VI. Observability**: Every administrator move — confirm, reject, ship, complete, transfer —
      writes an audit entry naming the order and the administrator. A customer's own acts (checkout,
      edit, cancel) are recorded on the order's version, timestamps and edit history.
- [x] **VII. Simplicity (YAGNI)**: No notification module, no in-app notifications, no payment
      provider — only the minimum the feature needs: a port, a mailer and an edit-history table. The
      mailer is one small package, not a new module.
- [x] **VIII. API Documentation as a Contract**: The new and changed endpoints and the new codes update
      `docs/api-reference.md`, this feature's OpenAPI contract and the module documents in the same
      change.
- [x] **Frontend Integration Guide**: This feature changes the client-facing status enum and adds
      endpoints, so `specs/010-order-confirmation/frontend-guide.md` is a required deliverable and
      `specs/009-order/frontend-guide.md` is marked superseded.
- [x] **Swagger annotations**: Every endpoint this feature adds or changes carries handler annotations
      and `docs/swagger/` is regenerated with `make swagger` in the same change, so `make swagger-check`
      passes.

**Post-design re-check**: all ten still hold. Nothing in Phase 0 or Phase 1 forced a violation.
Complexity Tracking records the deliberate choices below.

## Project Structure

### Documentation (this feature)

```text
specs/010-order-confirmation/
├── plan.md  ├── spec.md  ├── research.md  ├── data-model.md  ├── quickstart.md
├── contracts/{openapi.yaml, error-codes.md}
├── frontend-guide.md  ├── deferred.md
└── checklists/requirements.md

docs/decisions/018-order-confirmation-and-editing.md   # the ADR this feature records
```

### Source Code (repository root)

```text
internal/modules/order/
├── domain/{constant,error,model,repository}      # +2 states, transitions Confirm/Reject/Edit, version
├── application/{dto,interface,implement,mapper}  # + Notifier port; Confirm/Reject/Edit use cases
├── infrastructure/implement/{postgres,auditor,notifier}   # + notifier adapter; edit-history writes
└── presentation/{dto,http,worker}                # +3 endpoints, FIFO list params; sweeper -> PAYMENT_PENDING

internal/share/mailer/            # NEW: LogSender + SMTPSender (net/smtp), used by the order notifier
internal/modules/inventory/domain/constant/hold.go   # HoldTTL 15m -> 60m (payment window)
internal/contracts/*              # unchanged (InventoryReservation, ProductCatalog, ... reused as-is)

migrations/00011_order_confirmation.sql   # orders status check, payment_expires_at, order_version, confirmed_at, order_edit_history
cmd/api/main.go                   # wires the notifier + artist email, mounts the new routes
docs/api-reference.md  docs/modules/07-order.md  docs/modules.md
```

**Structure Decision**: The work extends the existing module 07 rather than adding a module. Four
things are specific to this feature and recorded rather than assumed:

1. **Checkout stops holding; confirmation starts holding.** The order reaches module 05 only through
   the existing `InventoryReservation` contract, so moving the hold from checkout to confirmation is a
   use-case change, not a new contract.
2. **A minimal shared mailer.** Module 12 (Notification) is V1.2 and unbuilt, and module 01 owns its
   own sender (currently under parallel edit), so a small `internal/share/mailer` is introduced for the
   order's notifications and module 01 is left untouched; adopting the shared mailer there is deferred.
3. **Editing is a whole-order replacement under the row lock**, releasing the old hold exactly once and
   bumping the version, with the previous and new content written to `order_edit_history`.
4. **The payment window is 60 minutes**, so module 05's `HoldTTL` is raised to 60 minutes; the order
   still derives its own deadline from `InventoryReservation.HoldWindow()`, so the value has one owner.

## Complexity Tracking

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| A new `internal/share/mailer` used (for now) only by the order module | The feature must send email, module 12 is unbuilt, and module 01's own sender is under a parallel edit; a small shared mailer keeps the order decoupled and touches no other module | Reusing module 01's `EmailSender` — rejected: it is module-internal and importing it would break the module boundary; moving module 01 onto the shared mailer now — rejected: it conflicts with the parallel session's uncommitted work |
| `order_edit_history` stores before/after content as JSONB snapshots | FR-017 requires recording what changed; the history is an audit aid, not a value the shop transacts on, so a schema-per-field model would add tables and joins for no behavioural gain | A normalized line-history table — rejected: it duplicates `order_items` for a record that is only ever read, not queried by field |
| Module 05's `HoldTTL` is raised from 15 to 60 minutes | FR-008 gives the customer a 60-minute payment window from confirmation; module 05's hold must not lapse before the customer pays | Keeping 15 minutes — rejected: the goods would be released before the window closed, contradicting FR-008; making the window configurable per order — rejected by YAGNI, one shop-wide window suffices |
| The status enum is renamed (`PENDING_PAYMENT` → `PAYMENT_PENDING`, plus a new `PENDING`) | The new flow needs two distinct pre-payment states; the old single state cannot express "awaiting the artist" versus "awaiting payment" | Keeping one state — rejected: it cannot represent the confirmation step the feature exists to add; nothing is deployed, so the breaking rename is free |
| `domain` imports `github.com/google/uuid` | `Order` and `OrderLine` express their primary keys as `uuid.UUID`, as in `009`; Constitution I names this the one allowed value-type import and requires the owning feature to record it | An order-local identifier type — rejected: it duplicates the UUID concept for no behavioural gain |
