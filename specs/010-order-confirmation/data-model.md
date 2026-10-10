# Phase 1 Data Model: Order Confirmation & Editing

One migration changes `orders` (status set, renamed nullable deadline, version, confirmation time) and
adds `order_edit_history`. The decisions behind the shape are in `research.md`; this file records what
is stored, what is constrained, and what the read paths project.

## Entity: Order (changed)

A purchase a customer has committed to: who owns it, where it goes, what it costs, where it is in its
life, which version of its content was last accepted, and — once confirmed — when it must be paid.

### Stored columns

| Column | Type | Null | Change | Meaning |
|---|---|---|---|---|
| `id` | uuid | no | — | Primary key |
| `user_id` | uuid | no | — | Owner. Loose reference, no foreign key (research D12 of `009`); a transfer changes it |
| `status` | text | no | **check widened** | One of the six states (research D1) |
| `total_amount` | bigint | no | — | Committed total in the currency's minor unit |
| `currency` | char(3) | no | — | Three uppercase letters |
| `recipient_name` … `street_address` | text | no | — | Delivery address snapshot |
| `order_version` | bigint | no | **new** | Content version; starts at 1, bumps on edit and confirmation (research D10) |
| `confirmed_at` | timestamptz | yes | **new** | When the artist confirmed; `NULL` while awaiting confirmation (research D4) |
| `payment_expires_at` | timestamptz | yes | **renamed from `expires_at`, now nullable** | When an awaiting-payment order cancels itself; `NULL` while awaiting the artist (research D4) |
| `created_at` | timestamptz | no | — | Set once |
| `updated_at` | timestamptz | no | — | Touched on every write |

### Constraints, and why each is at the storage layer

| Constraint | Kind | Requirement | Why not only in the application |
|---|---|---|---|
| `orders_status_ck` | check: `status IN ('PENDING','PAYMENT_PENDING','PAID','SHIPPED','COMPLETED','CANCELLED')` | FR-001, FR-006, FR-018 | An unlisted state cannot be stored, so the domain's transition table cannot be bypassed |
| `orders_version_ck` | check: `order_version >= 1` | FR-017 | A version is a positive counter; a zero or negative version is refused where it is stored |
| `orders_total_ck` | check: `total_amount > 0` | FR-025 | An order always has at least one positive-price line (the empty edit is refused, research D6) |
| `orders_currency_ck` | check: `currency ~ '^[A-Z]{3}$'` | FR-025 | The currency is unambiguous |
| `orders_owner_idx` | btree on `(user_id, created_at, id)` | FR-018 (of `009`) | The customer's list is one owner's orders, newest first |
| `orders_admin_idx` | btree on `(created_at, id)` | FR-021 (of `009`) | The operator's list is every order |
| `orders_expiry_idx` | btree on `(status, payment_expires_at)` | FR-008 | The sweeper finds awaiting-payment orders past their deadline without scanning every order |

**On `payment_expires_at` being nullable**: an order awaiting the artist holds no goods, so it has no
deadline (FR-009). The deadline exists only between confirmation and payment (research D4).

**On existing `PENDING_PAYMENT` rows**: the migration renames the status value in place — `UPDATE orders
SET status = 'PAYMENT_PENDING' WHERE status = 'PENDING_PAYMENT'` — before it widens `orders_status_ck`, so
a database that already holds `009` orders migrates cleanly rather than failing the new check on the old
value (research D1, T007). This is the data-migration note the constitution requires for a schema change.

**On the missing foreign key to `users`**: unchanged from `009` — `user_id` is a loose reference so an
order outlives the account it names.

## Entity: Order line (unchanged shape, now mutable before payment)

One item of an order, holding a snapshot of the product as it was when the line was written. Before
payment, a customer edit **replaces** the line set (research D6); the snapshot rule (a removed product
leaves the line intact) is unchanged from `009`.

Columns and constraints are exactly `009`'s: `order_items(id, order_id, product_id, name, slug,
unit_price_amount, currency, quantity, position, created_at)`, FK `order_id → orders(id) ON DELETE
CASCADE`, unique `(order_id, product_id)`, `quantity >= 1`, `unit_price_amount > 0`, currency check,
index `(order_id, position, id)`. An edit deletes the order's lines and inserts the new set in the same
transaction, so the unique pair and the checks still hold.

## Entity: Order edit history (new)

One row per accepted edit, so the shop can see what changed, by whom, and when (FR-017, research D14).

### Stored columns

| Column | Type | Null | Meaning |
|---|---|---|---|
| `id` | uuid | no | Primary key |
| `order_id` | uuid | no | The order edited. FK → `orders(id)` **ON DELETE CASCADE** |
| `version` | bigint | no | The `order_version` the edit produced |
| `actor_id` | uuid | no | The account that edited. A loose reference, no foreign key |
| `before` | jsonb | no | The lines and address before the edit |
| `after` | jsonb | no | The lines and address after the edit |
| `created_at` | timestamptz | no | Set once |

### Constraints

| Constraint | Kind | Requirement | Why |
|---|---|---|---|
| `order_edit_history_pkey` | primary key on `id` | FR-017 | A record is addressable |
| `order_edit_history_order_fk` | FK → `orders(id)` ON DELETE CASCADE | FR-017 | A removed order takes its history with it |
| `order_edit_history_version_ck` | check: `version >= 1` | FR-017 | The version is the order's positive counter |
| `order_edit_history_order_idx` | btree on `(order_id, version)` | FR-017 | The history is read in version order |

**On the JSONB snapshots**: `before`/`after` are read whole, never queried by field, so a normalized
schema would add tables and joins for no behavioural gain (research D14, Complexity Tracking).

## Transitions

```
PENDING ──confirm──► PAYMENT_PENDING ──pay──► PAID ──ship──► SHIPPED ──complete──► COMPLETED
   │  ▲                     │
   │  └── edit ─────────────┘
   ├──reject──► CANCELLED
   ├──cancel──► CANCELLED
   PAYMENT_PENDING ──cancel / expire──► CANCELLED
```

| Transition | From | To | Refuses when | Trigger | Stock effect | Audit |
|---|---|---|---|---|---|---|
| confirm | `PENDING` | `PAYMENT_PENDING` | the order is not awaiting confirmation, or a line cannot be held | administrator | **Reserve every line** (all-or-nothing) | `ORDER_CONFIRMED` |
| reject | `PENDING` | `CANCELLED` | the order is not awaiting confirmation | administrator | none (nothing held) | `ORDER_REJECTED` |
| edit | `PENDING` | `PENDING` | the order is not editable, a line is invalid, or the result is empty | the customer | none (nothing held) | — (customer act; edit history instead) |
| edit | `PAYMENT_PENDING` | `PENDING` | the order is not editable, a line is invalid, or the result is empty | the customer | **Release every line**, exactly once | — (customer act; edit history instead) |
| pay | `PAYMENT_PENDING` | `PAID` | the order is not awaiting payment | module 08, through `MarkPaid` | Reserve → **Sale** per line | — |
| cancel | `PENDING` | `CANCELLED` | the order is not awaiting confirmation or payment | the customer | none | — |
| cancel | `PAYMENT_PENDING` | `CANCELLED` | the order is not awaiting confirmation or payment | the customer, or the expiry sweep | **Release** every line | — |
| ship | `PAID` | `SHIPPED` | the order is not paid | administrator | none | `ORDER_SHIPPED` |
| complete | `SHIPPED` | `COMPLETED` | the order is not shipped | administrator | none | `ORDER_COMPLETED` |
| transfer | *(unchanged)* | *(unchanged)* | the order is not paid | administrator | none | `ORDER_TRANSFERRED` |

**A refused transition names the current state** and leaves the order exactly as it was.
**Edit is not a state transition**: it changes the lines/address (and may move `PAYMENT_PENDING` back to
`PENDING`), bumps the version, and writes an edit-history row.

**Every move runs inside a `UnitOfWork` transaction** that locks the order (`LockByID`) and calls module
05 in the same transaction, so the order's state/version and the stock effect commit together or not at
all (Constitution II). Confirmation's all-or-nothing hold is the single transaction's rollback on any
line's failure (research D3).

## What each read path projects

| Path | Reader | Columns | Requirement |
|---|---|---|---|
| customer list | signed-in customer | `id`, `status`, `total_amount`, `currency`, `created_at`, line count | FR-018 (`009`) |
| customer detail | signed-in customer | every order column (the response exposes a subset; `order_version` stays internal, research D10), every line | FR-018 |
| administrator list | administrator | the customer list plus the owner; optional `status` filter and `sort` | FR-021 (`009`), FR-026 (`010`), research D11 |
| administrator detail | administrator | every order column (response subset; `order_version` internal), every line | FR-021 |

Both lists are paginated under the project's convention; the administrator list gains an optional
`status` filter and a `sort` (newest default, `oldest` for the FIFO confirmation queue).

## What this feature changes outside its own tables

| Change | Where | Why |
|---|---|---|
| Hold window raised to 60 minutes | `internal/modules/inventory/domain/constant/hold.go` | The payment window is 60 minutes; the hold must not lapse first (research D13) |
| A new application port `Notifier` + adapter | `internal/modules/order/application/interface` and `.../infrastructure/implement/notifier` | Email the artist (confirmation needed) and the customer (status change) (research D9) |
| A new shared mailer | `internal/share/mailer` | Send email without a module dependency or touching module 01 (research D9) |
| A command-line wiring point | `cmd/api/main.go` | Wire the notifier and the artist email, mount the new routes |

No column, index or constraint of `order_items` is touched; the only data migration is the
`PENDING_PAYMENT` → `PAYMENT_PENDING` status backfill on `orders` (research D1, T007).
