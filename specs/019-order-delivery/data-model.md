# Phase 1 Data Model: Order Delivery & Completion

One migration changes `orders` (status set, delivery/completion timestamps) and adds `order_delivery_photos`
and `order_feedback`, plus the completion index. The decisions behind the shape are in `research.md`; this
file records what is stored, what is constrained, and what the read paths project.

## Entity: Order (changed)

A purchase a customer committed to: who owns it, where it goes, what it costs, where it is in its life,
and — once dispatched and delivered — when each of those happened and whether the customer has been
satisfied.

### Stored columns

| Column | Type | Null | Change | Meaning |
|---|---|---|---|---|
| `id` | uuid | no | — | Primary key |
| `user_id` | uuid | no | — | Owner. Loose reference, no foreign key; a transfer changes it |
| `status` | text | no | **check rewritten** | One of the seven states (research D1) |
| `total_amount` | bigint | no | — | Committed total in the currency's minor unit (unchanged by this feature) |
| `currency` | char(3) | no | — | Three uppercase letters |
| `recipient_name` … `street_address` | text | no | — | Delivery address snapshot |
| `order_version` | bigint | no | — | Content version (unchanged; internal seam for module 08) |
| `confirmed_at` | timestamptz | yes | — | When the artist confirmed |
| `payment_expires_at` | timestamptz | yes | — | When an awaiting-payment order cancels itself |
| `delivered_at` | timestamptz | yes | **new** | When the operator marked the order delivered; anchors the five-day window; `NULL` before delivery (research D6) |
| `completed_at` | timestamptz | yes | **new** | When the order reached `COMPLETED` (by customer or the sweep); `NULL` before then (research D10) |
| `completed_by` | uuid | yes | **new** | The customer who confirmed receipt; `NULL` when the sweep completed it. Loose reference, no foreign key |
| `created_at` / `updated_at` | timestamptz | no | — | Unchanged |

### Constraints, and why each is at the storage layer

| Constraint | Kind | Requirement | Why not only in the application |
|---|---|---|---|
| `orders_status_ck` | check: `status IN ('PENDING','PAYMENT_PENDING','PAID','SHIPPING','DELIVERED','COMPLETED','CANCELLED')` | FR-001, FR-002 | An unlisted state cannot be stored, so the domain's transition table cannot be bypassed |
| `orders_completion_idx` | btree on `(status, delivered_at)` | FR-012 | The completion sweep finds delivered orders past their window without scanning every order |
| `orders_expiry_idx` | btree on `(status, payment_expires_at)` | FR-008 (of `010`) | Unchanged; the expiry sweep still finds awaiting-payment orders |
| `orders_total_ck` / `orders_currency_ck` / `orders_version_ck` | unchanged | unchanged | Unchanged by this feature |
| `orders_owner_idx` / `orders_admin_idx` | unchanged | unchanged | Unchanged |

**On the status rename**: the migration **drops** `orders_status_ck`, runs `UPDATE orders SET status =
'SHIPPING' WHERE status = 'SHIPPED'`, then **adds** the seven-value check. The backfill runs before the new
check exists, so an existing database migrates cleanly rather than failing the check on the old value
(research D2). This is the data-migration note the constitution requires for a schema change.

**On `delivered_at` vs a stored `auto_complete_at`**: the window is a single five-day constant owned by
the domain; the sweep computes `delivered_at <= now − window` from the injected clock, exactly as the
expiry sweep passes `now`. One timestamp, one owner (research D6).

## Entity: Order delivery photo (new)

One piece of evidence attached to an order at a delivery step (FR-006, FR-008, FR-009, research D3).

### Stored columns

| Column | Type | Null | Meaning |
|---|---|---|---|
| `id` | uuid | no | Primary key; the detail response addresses it by this |
| `order_id` | uuid | no | The order it belongs to. FK → `orders(id)` **ON DELETE CASCADE** |
| `stage` | text | no | `SHIPPING` (dispatch evidence) or `DELIVERED` (delivery evidence) |
| `public_id` | text | no | The provider's opaque identifier, used to release the asset |
| `url` | text | no | The displayable link returned to clients (HTTPS) |
| `width` / `height` | integer | no | Stored pixel dimensions the provider reported |
| `position` | integer | no | Order within its stage; insertion order |
| `created_at` | timestamptz | no | Set once |

### Constraints

| Constraint | Kind | Requirement | Why |
|---|---|---|---|
| `order_delivery_photos_pkey` | primary key on `id` | FR-010 | A photo is addressable |
| `order_delivery_photos_order_fk` | FK → `orders(id)` ON DELETE CASCADE | FR-010 | A removed order takes its evidence with it |
| `order_delivery_photos_stage_ck` | check: `stage IN ('SHIPPING','DELIVERED')` | FR-006, FR-008 | An unknown stage cannot be stored |
| `order_delivery_photos_ordering_idx` | btree on `(order_id, stage, position, id)` | FR-010 | Evidence is read in stage then position order |

**On the five-photo ceiling**: it is a **count per order per stage**, which no unique index expresses, so
it is enforced in the use case under the order's row lock and re-checked before the insert (research D5).
An eleventh-style over-ceiling upload is refused **before** the provider is called, so no orphan is left.

## Entity: Order feedback (new)

The customer's one message to the artist about a delivered order (FR-015 to FR-017, research D8).

### Stored columns

| Column | Type | Null | Meaning |
|---|---|---|---|
| `id` | uuid | no | Primary key |
| `order_id` | uuid | no | The order the feedback is about. FK → `orders(id)` **ON DELETE CASCADE** |
| `author_id` | uuid | no | The customer who wrote it. Loose reference, no foreign key |
| `content` | text | no | The feedback text |
| `created_at` | timestamptz | no | Set once |

### Constraints

| Constraint | Kind | Requirement | Why |
|---|---|---|---|
| `order_feedback_pkey` | primary key on `id` | FR-015 | A record is addressable |
| `order_feedback_order_fk` | FK → `orders(id)` ON DELETE CASCADE | FR-015 | A removed order takes its feedback with it |
| `order_feedback_order_key` | **unique** on `(order_id)` | FR-016 | One feedback per order is a rule no code path can bypass |
| `order_feedback_content_ck` | check: `length(btrim(content)) BETWEEN 1 AND 2000` | FR-015 | Empty feedback is not feedback, and the 2000-character ceiling keeps a client's message bounded (research D8) |

## Transitions

```
PENDING ──confirm──► PAYMENT_PENDING ──pay──► PAID ──ship──► SHIPPING ──deliver──► DELIVERED
   │  ▲                     │                                                          │
   │  └── edit ─────────────┘                       customer confirm / auto 5d ───────┤
   ├──reject──► CANCELLED                                                              ▼
   PAYMENT_PENDING ──cancel / expire──► CANCELLED                                  COMPLETED
```

| Transition | From | To | Refuses when | Trigger | Stock effect | Audit |
|---|---|---|---|---|---|---|
| ship (dispatch) | `PAID` | `SHIPPING` | the order is not `PAID`; or more than five `photos`, or a photo is unsupported/oversized | administrator (ADMIN) | none | `ORDER_SHIPPED` (kept) |
| deliver | `SHIPPING` | `DELIVERED` | the order is not `SHIPPING`; or an over-ceiling/unsupported/oversized photo | administrator (ADMIN) | none | `ORDER_DELIVERED` (new) |
| confirm received | `DELIVERED` | `COMPLETED` | the order is not `DELIVERED`; or the order is another customer's | the customer (owner) | none | `ORDER_COMPLETED` (kept) with the customer as actor |
| auto-complete | `DELIVERED` | `COMPLETED` | the order is not `DELIVERED` (already completed ⇒ skipped) | the completion sweep | none | `ORDER_COMPLETED` (kept), no actor |
| feedback | *(no state change)* | — | the order is not `DELIVERED`/`COMPLETED`; or feedback already exists | the customer (owner) | none | — (recorded in `order_feedback`, artist notified) |
| complete (admin) | — | — | **removed** | — | — | — |

**Refused transitions**: dispatch/delivery/confirm on a wrong state are refused with
`ORDER_STATE_TRANSITION_INVALID` naming the current state, and the order is left exactly as it was.

**Every move runs inside a `UnitOfWork` transaction** that locks the order (`LockByID`). Photos are
uploaded **outside** the transaction and their rows inserted inside; the count is re-checked under the
lock (research D5). After `PAID` nothing touches stock or money.

**Exactly once**: both routes to `COMPLETED` (customer and sweep) run the same `DELIVERED → COMPLETED`
domain transition under the row lock, so whichever commits second finds the order no longer `DELIVERED`
and is refused. The sweep skips an already-completed order rather than failing (FR-012).

## What each read path projects

| Path | Reader | Adds over `010` |
|---|---|---|
| customer list / admin list | session owner / ADMIN | unchanged (no photos/feedback on a summary) |
| customer detail | session owner | `deliveredAt`, `completedAt`, `deliveryPhotos[]`, `feedback` |
| admin detail | ADMIN | the same additions |

`completed_by` and `order_version` stay **internal** (never in a client response). Both lists stay
paginated under the project's convention.

## What this feature changes outside its own tables

| Change | Where | Why |
|---|---|---|
| Dependency-free image rule added to the order's own `domain` | `internal/modules/order/domain/model/photo_image.go` (new) | The format/size sniff mirrors the user module's avatar and the product module's picture rules; `domain` imports no media package, so Constitution I's allowlist holds (research D4) |
| The order's media dependency wired | `internal/modules/order/application/interface` (`MediaStore` alias) + `cmd/api/main.go` | The order stores dispatch/delivery photos through the shared store (research D4, D5) |
| The completion sweep added to the order worker | `internal/modules/order/presentation/worker` | FR-012 needs an automatic completion; the worker already ticks for expiry (plan, Complexity Tracking) |
| The operator statistics/read-model **earned-states** set | `internal/modules/order/infrastructure/implement/postgres/order.go` (revenue KPIs, daily trend, category breakdown) | Revenue counts post-payment orders; the set `('PAID','SHIPPED','COMPLETED')` becomes `('PAID','SHIPPING','DELIVERED','COMPLETED')` so a dispatched/delivered order is still counted (research D1 ripple) |
| A new audit action | `internal/modules/order/domain/constant` | `AuditOrderDelivered` (new); `ORDER_SHIPPED`/`ORDER_COMPLETED` kept (research D10) |

No column, index or constraint of `order_items` is touched; the only data migration is the
`SHIPPED` → `SHIPPING` status backfill on `orders` (research D2).
