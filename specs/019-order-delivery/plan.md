# Implementation Plan: Order Delivery & Completion

**Branch**: `019-order-delivery` | **Date**: 2026-10-18 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/019-order-delivery/spec.md`

## Summary

Module 07 Order, extension of feature `010-order-confirmation`: the order's life **after payment** becomes
a real delivery journey. Today a paid order skips straight from `PAID` to `SHIPPED` to `COMPLETED` by two
administrator clicks. This feature inserts a **dispatch** step and moves the **completion** decision to
the customer: the operator marks the order **`SHIPPING`** (in transit) when it is handed to the carrier,
attaching **up to five photos** as dispatch evidence; the operator marks it **`DELIVERED`** when the
customer receives it, attaching **up to five more photos**; the **customer confirms receipt** to move it
to `COMPLETED`, or an **automatic sweep completes it after five days** from delivery. The customer may
also leave **order-level feedback** to the artist once the order is delivered.

This **renames** the existing `SHIPPED` state to **`SHIPPING`** (in transit) and **adds** a new
**`DELIVERED`** state, so the lifecycle holds **seven** states: `PENDING`, `PAYMENT_PENDING`, `PAID`,
`SHIPPING`, `DELIVERED`, `COMPLETED`, `CANCELLED`. The rename is a deliberate **breaking change to the
client-facing status enum**, superseding the state machine of `010-order-confirmation` (ADR 023 records
it; ADR 018 is not edited). The feature is also the order module's first use of the **shared media
store** to persist photo bytes. The image format/size rule stays a **dependency-free function inside the
order's own `domain`** (standard library only), exactly as the user module's avatar rule and the product
module's picture rule already do — no module imports another module's validation, and `domain` gains no
non-stdlib dependency.

## Technical Context

**Language/Version**: Go 1.26.0

**Primary Dependencies**: Standard library plus the foundation — chi, pgx, the shared
response/error envelope, the shared transaction-aware `UnitOfWork`, the shared generic repository,
`share/access`, the shared **`share/media`** store (Cloudinary, already used by product/avatar), and the
shared **`share/mailer`**-backed `Notifier`. **No new dependency.**

**Storage**: PostgreSQL 16, one migration `migrations/00018_order_delivery.sql` (next free number; `00017` is the catalog-reorder snapshot migration). It
(a) rewrites `orders_status_ck` — drop, backfill `UPDATE orders SET status = 'SHIPPING' WHERE status =
'SHIPPED'`, then add the seven-value check that includes `DELIVERED`; (b) adds `delivered_at`,
`completed_at` and `completed_by` to `orders`; (c) adds `orders_completion_idx (status, delivered_at)`
for the completion sweep; (d) creates `order_delivery_photos` and `order_feedback`. Redis is not involved.

**Testing**: `testing` with `net/http/httptest`; in-memory fakes for the use cases, the media store and
the notifier; `testcontainers-go` behind the `integration` tag for the storage and the delivery/complete
paths — including what a fake cannot prove: the seven-value status check on real rows, the `SHIPPED →
SHIPPING` backfill on a real database, an over-five-photo request refused before storage, the
completion-exactly-once race between the customer and the sweep, and the single-feedback unique index.
Deadlines are tested with an injected clock.

**Target Platform**: Linux server (Docker) in production, local development on Windows via Docker
Desktop.

**Project Type**: web-service

**Performance Goals**: Both order lists stay paginated under the project's convention. Each sweep is
index-bounded (`orders_completion_idx`); a tick does two short sweeps (expiry, completion).

**Constraints**: Money stays **integer minor units plus an explicit currency**, and the total is
unchanged by this feature (still **no shipping fee**, ADR 015 §5). Photo bytes live **outside** the
database (Constitution, Media); only the reference is stored, with the same **2 MB / JPEG-PNG-WebP**
ceiling as avatars and product pictures. The per-step photo ceiling is **five**; because five 2 MB photos
exceed the default 4 MiB pipeline ceiling, the composition root **raises `MAX_BODY_BYTES`** and guards it at
startup (mirroring `RequireAvatarUploadCeiling`) so the pipeline never cuts a legitimately-sized dispatch. The auto-completion
window is **five days** from `delivered_at`, owned by the order module as a constant. Every customer
route takes the owner from the session; the operator routes require `ADMIN`. The media store must not be
called while a database lock is held.

**Scale/Scope**: One migration (1 status-check rewrite + backfill, 3 columns on `orders`, 2 new tables, 1
index); **three** new endpoints (`POST /admin/orders/{id}/deliver`, `POST /orders/{id}/complete`, `POST
/orders/{id}/feedback`), **one changed** (`POST /admin/orders/{id}/ship` becomes multipart) and **one removed**
(`POST /admin/orders/{id}/complete`);
**one renamed** and **one new** state; one new sweeper responsibility on the existing order worker; the operator
statistics/read-model earned-states set widened to include the two new states; a dependency-free image rule added
to the order `domain` (mirroring avatar/picture); new error codes; and the documentation, ADR and frontend guide
that move with all of it.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-checked after Phase 1 design.*

- [x] **I. Modular Monolith & Clean Architecture**: The work stays inside `internal/modules/order` with
      its four layers. The order's image format/size rule is a **standard-library-only function in the
      order's own `domain`**, exactly as the user module's `ValidateAvatarUpload` and the product module's
      `ValidatePictureUpload` already are; no module imports another module's validation, and `domain`
      gains no non-stdlib dependency. The order's application imports `internal/share/media` **only for the
      `Store` interface** (`MediaStore`), which module 04 also does; the media adapter stays in
      `internal/share/media`. `domain` still imports only the standard library, `share/access` and
      `github.com/google/uuid`.
- [x] **II. Transactional Integrity**: Each delivery transition, the completion, the feedback and the
      auto-completion sweep run in one `UnitOfWork` transaction under the order's row lock. A photo is
      uploaded **outside** the transaction and its row inserted inside, so no database lock is held
      across a provider call (the product module's pattern). After `PAID` no stock or money changes, so
      the delivery steps touch neither `inventory_transactions` nor totals.
- [x] **III. State Machines & Invariants**: The seven states and their allowed edges are explicit in the
      domain, changed only through the transition methods, and every edge is tested both ways. The state
      is a storage-constrained column (rewritten to seven values), so an unlisted value cannot be
      written; the `SHIPPED` value is backfilled in the same migration.
- [x] **IV. Test-First for Critical Logic**: The state transitions and the completion (customer vs
      automatic, exactly once) are critical logic and are written test-first. What a fake cannot prove —
      the status check, the rename backfill, the five-photo ceiling under concurrency, the
      completion-exactly-once race, and the single-feedback index — is proven against real PostgreSQL.
- [x] **V. Security & Least Privilege**: The owner comes from the session, never the request; only the
      order's owner confirms receipt or leaves feedback; only the operator (ADMIN) marks dispatch and
      delivery. Uploads are validated (format sniffed from bytes, size and count capped) **before**
      storage. No new credential; the media store reads the existing `MEDIA_*` settings.
- [x] **VI. Observability**: The operator moves — start shipping, delivery — write an audit entry naming
      the order and the operator; completion writes an audit entry with the acting customer, or none for
      the automatic sweep. Logs stay structured with a correlation ID and no secrets/PII.
- [x] **VII. Simplicity (YAGNI)**: No new module, no shipping-fee engine, no carrier-integration, no
      product-review model — only what the feature needs: two states, two small tables, a standard-library
      image rule in the order's own domain, and one extra sweep. Failed delivery/returns are explicitly
      deferred.
- [x] **VIII. API Documentation as a Contract**: The new/changed/removed endpoints and the new codes
      update `docs/api-reference.md`, this feature's OpenAPI contract and the module documents in the
      same change.
- [x] **Frontend Integration Guide**: This feature **renames a client-visible enum value** and changes
      endpoints, so `specs/019-order-delivery/frontend-guide.md` is a required deliverable and the guide
      of `010-order-confirmation` is marked superseded.
- [x] **Swagger annotations**: Every endpoint this feature adds or changes carries handler annotations
      (`@Summary`, `@Tags`, `@Param`, `@Success`, `@Failure`, `@Router`) and `docs/swagger/` is
      regenerated with `make swagger` in the same change, so `make swagger-check` passes.

**Post-design re-check**: all ten still hold. Nothing in Phase 0 or Phase 1 forced a violation.
Complexity Tracking records the deliberate choices below.

## Project Structure

### Documentation (this feature)

```text
specs/019-order-delivery/
├── plan.md  ├── spec.md  ├── research.md  ├── data-model.md  ├── quickstart.md
├── contracts/{openapi.yaml, error-codes.md}
├── frontend-guide.md  ├── deferred.md
└── checklists/requirements.md

docs/decisions/023-order-delivery-and-completion.md   # the ADR this feature records
```

### Source Code (repository root)

```text
internal/modules/order/
├── domain/{constant,error,model,repository}
│     constant: +StatusShipping, +StatusDelivered, -StatusShipped; +ORDER_IMAGE_* / ORDER_FEEDBACK_* codes;
│               +AuditOrderDelivered (keep AuditOrderShipped/AuditOrderCompleted)
│     model:    Ship->StartShipping (PAID→SHIPPING), +MarkDelivered (SHIPPING→DELIVERED),
│               Complete (DELIVERED→COMPLETED) driven by customer/auto; +DeliveredAt/CompletedAt;
│               +DeliveryPhoto entity; +Feedback entity; +DeliveryCompletionWindow (120h = 5 days);
│               +ValidateDeliveryPhoto (stdlib-only format/size sniff, in the module's own domain)
│     repository: +delivery-photo reads/writes, +feedback read/write, +ListAwaitingCompletion,
│               +UpdateDelivery/UpdateCompletion
├── application/{dto,interface,implement,mapper}
│     interface: +MediaStore alias (= share/media.Store) only; each story adds its own method to OrderService
│               (StartShippingByAdmin, DeliverByAdmin, ConfirmReceived, SubmitFeedback, AutoCompleteDelivered)
│               and removes the superseded ones (legacy Ship, ShipByAdmin, legacy Complete, CompleteByAdmin),
│               so a phase only widens/narrows the interface when it implements it
│     implement: +delivery.go (shipping/delivery transitions with photos), +completion.go (confirm + sweep),
│               +feedback.go; order reads gain photos/feedback
├── infrastructure/implement/{postgres,auditor,notifier}
│     postgres: new queries + row mappers for photos/feedback, delivered_at/completed_at,
│               ListAwaitingCompletion(status, delivered_at); earned-states set widened (T020)
└── presentation/{dto,http,worker}
      http: /admin/orders/{id}/ship now multipart; +/admin/orders/{id}/deliver;
            +/orders/{id}/complete, +/orders/{id}/feedback; -/admin/orders/{id}/complete
      worker: the sweeper runs expiry AND completion each tick (added when US4 lands)

migrations/00018_order_delivery.sql   # status check rewrite+backfill, delivered_at/completed_at/completed_by,
                                      # order_delivery_photos, order_feedback, orders_completion_idx
cmd/api/main.go                       # wires the order MediaStore (shared Cloudinary) + mounts new routes
docs/api-reference.md  docs/modules/07-order.md  docs/modules/09-shipping.md  docs/modules.md
```

**Structure Decision**: The work extends the existing module 07 rather than adding a module. Four things
are specific to this feature and recorded rather than assumed:

1. **The completion decision moves from the operator to the customer** (or a five-day sweep). The old
   admin `complete` route is removed; a customer `complete` route and a completion sweep take its place.
2. **The order module gains a media dependency.** Photos are dispatch/delivery evidence, not product
   content, so they are stored by the order module through the **shared** `share/media.Store`, exactly as
   module 04 stores product pictures. The image-format/size rule is **not** extracted: it stays a
   **standard-library-only function in the order's own `domain`**, mirroring the user module's avatar rule
   and the product module's picture rule. Extracting it into `share/media` would make the product/order
   `domain` import a package that carries the Cloudinary adapter, `net/http`, `config` and `logging`,
   violating Constitution I's domain import allowlist; duplicating a 15-line pure sniff across module
   domains is the established, compliant pattern (avatar and picture already do it).
3. **The rename is a breaking contract change.** `SHIPPED` → `SHIPPING` plus a new `DELIVERED`; the
   storage value is rewritten in place, and the frontend guide records before → after.
4. **The existing expiry sweeper grows a second sweep.** One worker, one tick, two idempotent sweeps
   (cancel expired payment-pending orders; complete delivered orders past their window).

## Complexity Tracking

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| The client-facing status enum is **renamed** (`SHIPPED` → `SHIPPING`) and a state is added (`DELIVERED`) | The user's flow needs "in transit" (dispatch) and "delivered" as distinct, and the old `SHIPPED` name is misleading once an explicit delivery step exists | Keeping `SHIPPED` and adding only `DELIVERED` — rejected: the user chose the rename; a state named "shipped" that means "in transit" while a separate "delivered" exists is the misleading vocabulary the rename removes. Nothing is deployed, so the break is contained by the frontend guide and a versioned migration |
| The order module now depends on a **media store**, and the image format/size rule is **duplicated** (a stdlib-only function in the order's own `domain`, as avatar and picture already do) instead of extracted to `share/` | FR-006/FR-008/FR-009 require dispatch and delivery photos. Extracting the rule to `internal/share/media` would put the Cloudinary adapter's dependencies (`net/http`, `share/config`, `share/logging`) behind the product/order `domain`, violating Constitution I's domain import allowlist; a dependency-free shared package would still be a new package for a 15-line pure function | Extracting to `internal/share/media` — rejected: it is not dependency-free, so the `domain` import violates §I (the analyze gate flagged this). A new stdlib-only shared package — rejected as unnecessary ceremony: avatar and picture already duplicate the same rule per module and the pattern is stable. Skipping photos — rejected: they are the feature's stated purpose |
| The completion **sweep** is added to the existing order worker | FR-012 needs a delivered order to complete itself after five days; the project already runs one order sweeper (expiry), so a second capability on the same ticker is the smallest shape | A third background worker — rejected by YAGNI: one ticker, two idempotent sweeps is enough. A database scheduled job — rejected: it adds infrastructure the project does not use |
| `orders.delivered_at` is stored and the window is derived in the use case (not a stored `auto_complete_at`) | The spec fixes a single five-day window; the existing expiry pattern computes the cutoff from an injected clock and passes it to the query, so an unhardcoded window keeps the deadline in one place | Storing an explicit `auto_complete_at` — rejected: a second timestamp to keep in step with `delivered_at` for no behavioural gain |
