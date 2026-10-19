# Phase 0 Research: Order Delivery & Completion

All technical unknowns resolved here. No decision depends on an unanswered question.

## D1: The order's new state machine

**Decision**: The order has **seven** states: `PENDING` (awaiting the artist), `PAYMENT_PENDING`
(confirmed, awaiting payment), `PAID`, `SHIPPING` (in transit, **renamed from `SHIPPED`**), `DELIVERED`
(received), `COMPLETED`, `CANCELLED`. The allowed edges become:

```
PENDING ──confirm──► PAYMENT_PENDING ──pay──► PAID ──ship──► SHIPPING ──deliver──► DELIVERED
   │  ▲                     │                                                          │
   │  └── edit ─────────────┘                       customer confirm / auto 5d ───────┤
   ├──reject──► CANCELLED                                                              ▼
   PAYMENT_PENDING ──cancel / expire──► CANCELLED                                  COMPLETED
```

`SHIPPED` is **renamed** `SHIPPING` (in transit), and a **new `DELIVERED`** state is added. The
`COMPLETED` edge is now driven by the **customer** (`ConfirmReceived`) or by the **automatic sweep**,
not by an operator.

**Rationale**: The feature needs "handed to the carrier" and "received by the customer" as distinct
moments; the old single `SHIPPED` cannot express both, and once two delivery states exist, naming one
`SHIPPED` while it means "in transit" is misleading. The user chose the rename.

**Ripple the rename forces**: the operator statistics/read-model queries in
`internal/modules/order/infrastructure/implement/postgres/order.go` gate revenue on a hard-coded
`status IN ('PAID','SHIPPED','COMPLETED')`. That set is the **earned states** (money already collected);
after the rename and the split it becomes `('PAID','SHIPPING','DELIVERED','COMPLETED')`, or revenue silently
drops every dispatched/delivered order. Every other `SHIPPED` literal — the admin dashboard's Swagger
description, and the category/product/admin integration fixtures — moves with the enum. The rename task
grep MUST match both the double-quoted Go literal and the single-quoted SQL literal.

**Alternatives considered**: Adding only `DELIVERED` and leaving `SHIPPED` = "in transit" — rejected by
the user's decision. Reusing `SHIPPED` = "delivered" and inserting a new `SHIPPING` — rejected: it keeps
a state whose name no longer matches any step and duplicates a concept. A `FAILED`/`RETURNED` state —
deferred (D14).

## D2: The rename is a versioned, reversible migration with a backfill

**Decision**: One migration `migrations/00018_order_delivery.sql` **drops** `orders_status_ck`,
**backfills** `UPDATE orders SET status = 'SHIPPING' WHERE status = 'SHIPPED'`, then **adds** the
seven-value check that includes `DELIVERED`. The `Down` section restores the six-value check after
backfilling `SHIPPED` back and deleting the new columns/tables.

**Rationale**: The precedent is feature 010's `00011_order_confirmation.sql`, which does exactly this
shape for `PENDING_PAYMENT` → `PAYMENT_PENDING`: a database that already holds orders migrates cleanly
instead of failing the new check on an old value. Dropping then re-adding the check is required because
the value set changes; the backfill runs while no check constrains the old value. This is the
data-migration note the constitution requires.

**Alternatives considered**: Adding `SHIPPING` to the check beside `SHIPPED` and migrating lazily —
rejected: it leaves two values meaning the same thing and a client cannot tell which it will read.
Renaming at the application layer only — rejected: the storage check would reject `SHIPPING` rows.

## D3: Dispatch and delivery photos are one table, keyed by stage

**Decision**: A new table `order_delivery_photos(id, order_id, stage, public_id, url, width, height,
position, created_at)` stores the evidence, with `stage IN ('SHIPPING','DELIVERED')` and a per-order
per-stage ceiling of **five** enforced by the use case (a count, which no unique index can express).
`order_id` is an FK with `ON DELETE CASCADE`; the read path orders by `(stage, position, id)`.

**Rationale**: Both steps attach the same kind of evidence; one table with a `stage` discriminator keeps
one shape, one mapper and one ceiling rule, and lets the order detail return both groups. The product
module's `product_images` is the precedent (provider reference + dimensions + position).

**Alternatives considered**: One table per step — rejected: it duplicates the schema and the mapper for
no gain. Putting photos on the `orders` row as a JSON column — rejected: the schema, not JSON, is where
the count and FK belong, and it would break the media-reference pattern.

## D4: The image rule is a stdlib-only function in the order's own `domain`

**Decision**: The order module validates a photo with its **own** dependency-free function
(`internal/modules/order/domain/model/photo_image.go`: `DetectDeliveryImage` / `ValidateDeliveryPhoto`
returning the order-domain sentinels `ErrImageTypeUnsupported` / `ErrImageTooLarge`), exactly the way the
user module's `domain/model/avatar.go` (`ValidateAvatarUpload`) and the product module's
`domain/model/picture.go` (`ValidatePictureUpload`) already do. The order's **application** imports
`internal/share/media` **only** for the `Store` interface (aliased `MediaStore`); the `domain` imports no
media package at all.

**Rationale**: Constitution I forbids `domain` importing anything but the standard library, dependency-free
shared value packages and `uuid`. `internal/share/media` is **not** dependency-free — it imports
`share/config`, `share/logging`, `net/http`, `mime/multipart`, `encoding/json`, `crypto/sha1` — so having
the product/order `domain` call a helper there would violate the MUST principle. The project's established
pattern is a per-module, stdlib-only sniff (avatar and picture each carry their own); the order follows it.
The rule is 15 lines and stable, so duplication is cheaper than a new package and keeps every `domain`
dependency-free.

**Alternatives considered**: Extracting to a new `internal/share/media/image.go` — **rejected: violates
Constitution I**, because `share/media` is not dependency-free and `domain` cannot import it. A new
stdlib-only shared package (e.g. `internal/share/imaging`) — rejected as unnecessary ceremony: it would be
a third home for a rule that avatar and picture already keep locally, and the pattern is stable. Importing
module 04's rule — rejected: a module may not import another module's internals (Constitution I).

## D5: Photos are uploaded outside the transaction, rows written inside

**Decision**: `StartShippingByAdmin` and `DeliverByAdmin` follow the product module's order of
operations: (1) validate every uploaded payload on its own bytes; (2) a first transaction locks the
order, checks state and the per-stage count, and refuses an over-ceiling or wrong-state request before
any byte is uploaded; (3) upload each photo through `MediaStore` **outside** any transaction; (4) a
second transaction re-checks the count under the lock, applies the domain transition, inserts the photo
rows, touches `updated_at`, and audits. A failure in step (4) releases the just-stored assets; the worst
case of a failed release is an orphaned asset, never an order that references a missing one.

**Rationale**: Holding a database lock across a third-party call is how a slow provider becomes a
database problem (the product module's research D6). The ceiling is a count, so it must be re-checked
under the lock to admit at most five under two concurrent uploads.

**Alternatives considered**: Uploading inside the transaction — rejected: it holds the order's row lock
across the network. Uploading after the transition commits — rejected: a committed transition would then
depend on a provider call that can fail, leaving the order with no evidence and no error.

## D6: Completion is the customer's act or a five-day sweep

**Decision**: `ConfirmReceived` (customer, `DELIVERED → COMPLETED`) locks the order, drives the domain
`Complete`, stamps `completed_at = now` and `completed_by = <customer>`, audits, and notifies.
`AutoCompleteDelivered` is the sweep use case: it asks the repository for `DELIVERED` orders whose
`delivered_at <= now − DeliveryCompletionWindow` (oldest first), and for each calls the same completion
path with no actor, skipping an order already completed (idempotent). The window is a domain constant
of **five days, defined as `5 × 24h = 120h`** (an elapsed duration, not calendar-day boundaries) from the
`delivered_at` instant; the cutoff instant is computed with the injected clock and passed to the query, exactly
as the expiry sweep passes `now`.

**Rationale**: FR-011 to FR-014. Both routes into `COMPLETED` go through one domain transition, so
"exactly once" is the row lock plus the state check: whichever of the customer or the sweep commits
second finds the order no longer `DELIVERED` and is refused. Passing the cutoff (not a SQL interval)
keeps the window owned by the domain and the sweep reproducible in tests.

**Alternatives considered**: A separate `AUTO_COMPLETED` state — rejected: how an order completed is an
event, not a state (research D1's reasoning). Hardcoding the interval in SQL — rejected: the window
would live in two places and tests could not move it with the clock.

## D7: Endpoints and permissions

**Decision**:

| Method & path | Who | Body | Effect |
|---|---|---|---|
| `POST /api/v1/admin/orders/{orderId}/ship` (**changed**: multipart) | ADMIN | 0–5 `photos` parts | `PAID → SHIPPING` |
| `POST /api/v1/admin/orders/{orderId}/deliver` (**new**) | ADMIN | 0–5 `photos` parts | `SHIPPING → DELIVERED` |
| `POST /api/v1/orders/{orderId}/complete` (**new**) | owner | empty | `DELIVERED → COMPLETED` |
| `POST /api/v1/orders/{orderId}/feedback` (**new**) | owner | `{ "content": string }` | records feedback |
| `POST /api/v1/admin/orders/{orderId}/complete` (**removed**) | — | — | replaced by the customer route and the sweep |

The two operator routes install a **route-specific body ceiling** (five photos × the 2 MB image ceiling
plus the multipart envelope) and allow `multipart/form-data` on that route, exactly as the product
image routes do; the handler caps each file again while it reads. Ownership for the customer routes
comes from the session; another customer's order answers the same `ORDER_NOT_FOUND`.

**Rationale**: A transition is its own endpoint (the project's existing rule), so dispatch, delivery,
confirmation and feedback are separate routes. The customer, not the operator, owns completion (the
user's flow), so the admin complete route is removed. Keeping the dispatch route at `/ship` avoids
churning the path while its payload becomes multipart.

**Alternatives considered**: A single `/delivery` route with a `stage` field — rejected: a stage is a
transition and a transition can be refused, which separate endpoints express honestly. A JSON body with
base64 photos — rejected: it inflates the payload ~33% and departs from the avatar/product multipart
pattern.

## D8: Feedback is order-level, once, and only after delivery

**Decision**: `POST /orders/{orderId}/feedback` stores one `order_feedback` row (`content` only, no
rating, no moderation) for the order's owner when the order is `DELIVERED` or `COMPLETED`. `content` MUST
be **1 to 2000 characters** (trimmed) — the ceiling is enforced by `ValidateFeedbackContent` and by a
storage check. A unique index on `order_id` enforces "one per order" at the storage layer; a second
submission is refused. The artist is notified through the existing `Notifier` port.

**Rationale**: FR-015 to FR-017, and the user's choice of order-level feedback to the artist (not a
product review). The unique index is the storage-layer guarantee the constitutions asks for on a
one-per-order rule. Text-only keeps it minimal (YAGNI); a rating/moderation model is module 13's.

**Alternatives considered**: Product-level reviews now — rejected: module 13 (V2.0) owns reviews with
ratings and moderation. Allowing feedback before delivery — rejected by the user (needs receipt).

## D9: Error codes

**Decision**: New codes — `ORDER_IMAGE_TYPE_UNSUPPORTED` (400), `ORDER_IMAGE_TOO_LARGE` (413),
`ORDER_IMAGE_LIMIT_REACHED` (409, more than five at a stage), `ORDER_FEEDBACK_NOT_ALLOWED` (409, the
order is not delivered/completed), `ORDER_FEEDBACK_ALREADY_SENT` (409, a second feedback),
`ORDER_MEDIA_UNAVAILABLE` (503, the provider rejected an upload or is unreachable — mirroring module 04's
`PRODUCT_MEDIA_UNAVAILABLE`). Reused:
`ORDER_STATE_TRANSITION_INVALID` (dispatch/delivery/confirm on the wrong state, naming the current
state), `ORDER_NOT_FOUND`, `VALIDATION_ERROR` (missing/oversized `content`, wrong part), plus the shared
`MALFORMED_REQUEST`, `UNAUTHENTICATED`, `FORBIDDEN`, `RATE_LIMITED`, `INTERNAL_ERROR`.

**Rationale**: The three photo codes mirror module 04's `PRODUCT_IMAGE_*` codes and statuses so a client
handles uploads the same way everywhere. Feedback refusals lead to different next steps (wait until
delivered; you already sent one), so they earn their own codes; a wrong-state transition keeps the code
the project already uses.

**Alternatives considered**: Reusing `PRODUCT_IMAGE_*` for order photos — rejected: a client branching on
`PRODUCT_` for an order upload reads a vocabulary that does not describe the resource. Folding feedback
refusals into `ORDER_STATE_TRANSITION_INVALID` — rejected: feedback is not a state transition.

## D10: Completion and delivery are recorded on the order and audited

**Decision**: `orders` gains `delivered_at` (set by delivery), `completed_at` (set by whichever completes)
and `completed_by` (the acting customer, `NULL` for the sweep). The operator moves audit
`ORDER_SHIPPED` (kept, now emitted by dispatch) and the new `ORDER_DELIVERED`; completion audits
`ORDER_COMPLETED` (kept) with the acting customer, or no actor for the sweep. Existing audit constants
are **not renamed** — the values are part of the historical audit contract.

**Rationale**: FR-014 asks to record time and who; columns on the order are the honest place, and the
audit log already records operator mutations (Constitution VI). Not renaming `ORDER_SHIPPED`/
`ORDER_COMPLETED` keeps historical rows decodable, which the constants' own comments require.

**Alternatives considered**: A separate completion-history table — rejected: three columns answer the
question; an edit-history-style table would be material no read path needs. Renaming `ORDER_SHIPPED` to
`ORDER_SHIPPING_STARTED` — rejected: it would make historical rows undecodable.

## D11: The read projection gains delivery facts

**Decision**: The order **detail** response (customer and admin) gains `deliveredAt`, `completedAt`,
`deliveryPhotos` (an array of `{ id, stage, url, width, height, position }`) and `feedback`
(`{ content, createdAt }` or `null`). The order **summary** (list) is unchanged. `completed_by` and
`order_version` stay internal.

**Rationale**: A tracked order's evidence belongs in its full read; a list does not need five photos per
row. This mirrors how lines are read on the detail only.

**Alternatives considered**: Separate photo/feedback sub-resources (`GET …/photos`) — rejected: they are
always read with the order and never paginated, so a sub-resource would add round-trips for no gain.

## D12: Notifications reuse the existing Notifier port

**Decision**: No new port. The customer is emailed on `SHIPPING`, `DELIVERED` and `COMPLETED` via the
existing `notifyCustomerStatusChange`; the artist is emailed when feedback is submitted. Sending stays
best-effort, after commit, never failing the operation.

**Rationale**: FR-021 to FR-023 and the existing port already carry subject/body; the new states are new
messages, not a new transport.

**Alternatives considered**: A new notifier method per event — rejected: `Send(to, subject, body)` already
expresses every message; more methods add nothing.

## D13: Docs, ADR, frontend guide and deferred

**Decision**: Record **ADR 023-order-delivery-and-completion** (supersedes the state machine of ADR 018
for the post-`PAID` edges; ADR 018 is not edited). Update `docs/api-reference.md` §9 (endpoint list,
seven-value status table, new codes), `docs/modules/07-order.md` (state machine + flows),
`docs/modules/09-shipping.md` (note that dispatch photos are this feature, not the full module 09), and
`docs/modules.md` if module scope wording moves. Ship `specs/019-order-delivery/frontend-guide.md`
(the enum rename is a **breaking change** with before → after) and `specs/019-order-delivery/deferred.md`
(failed delivery/returns; transfer scope across the new states). Regenerate `docs/swagger/`.

**Rationale**: Constitution VIII and the governance rules on the frontend guide and ADRs.

**Alternatives considered**: Folding the ADR into `010`'s — rejected: decisions are superseded, not edited.

## D14: Explicitly deferred

**Decision**: (1) **Failed delivery / customer refuses** — no `FAILED`/`RETURNED` state, no refund path;
an order can rest at `SHIPPING`/`DELIVERED`. (2) **Transfer scope** — this feature does not change the
existing transfer mechanism beyond the renamed enum; restricting or extending it for `SHIPPING`/
`DELIVERED` is deferred. (3) **Shipping fee / carrier tracking** — module 09 proper. (4) **Product
reviews** — module 13. (5) **In-app notifications** — module 12.

**Rationale**: Constitution VII and the user's decisions; each goes to `deferred.md` with what would
unblock it.

**Alternatives considered**: Modelling failure/returns now — rejected: it needs a refund/return domain
the project does not have.

## D15: What is tested, and how

**Decision**: Unit tests cover the state machine (one positive and one negative per edge, seven states),
the photo-ceiling and format/size refusals, the completion window and the completion-exactly-once
reasoning, and the feedback one-per-order rule — over fakes for the repository, media and notifier. HTTP
tests cover session/role guards, ownership, the multipart shape, the new codes and the read projection.
Integration tests prove against real PostgreSQL what a fake cannot: the seven-value status check, the
`SHIPPED → SHIPPING` backfill (using the existing `applyUpTo` migration harness), an over-five-photo
request refused before storage, the completion race between the customer and the sweep (exactly one), and
the single-feedback unique index. The photo **ceiling** is a per-request count check, not a concurrency
race: the `PAID → SHIPPING` edge can fire only once, so a second dispatch is refused by the **state**, not
by the counter — the tests assert that, rather than a concurrency guarantee the edge cannot produce.

**Rationale**: The transitions and completion are Constitution-IV critical logic and are written
test-first; the storage and concurrency rules can only be proven against the database.

**Alternatives considered**: Fakes for the storage guarantees — rejected: they prove the fake, not the
schema.

## D16: Feedback photos reuse the delivery-evidence rule and shape

**Decision**: A customer's order-level feedback may carry up to **five** optional photos, stored exactly
like delivery evidence: a new table `order_feedback_photos(id, feedback_id, public_id, url, width,
height, position, created_at)` with `feedback_id → order_feedback(id) ON DELETE CASCADE` and a read
ordered by `(position, id)`; the count is enforced by the use case (a count, which no unique index
expresses). The order's own stdlib-only rule is reused: `ValidateFeedbackPhoto` delegates to
`ValidateDeliveryPhoto` + `MaxDeliveryPhotoBytes`, so the format sniff and the 2 MB ceiling cannot
diverge, and `EnsureFeedbackPhotoCapacity` returns the same `ORDER_IMAGE_LIMIT_REACHED` sentinel the
delivery steps use. `POST /orders/{orderId}/feedback` becomes **multipart** (a `content` field plus 0–5
`photos` parts) and installs the same `DeliveryEvidenceUploadCeiling()` body limit with the
`ORDER_IMAGE_TOO_LARGE` (413) refusal. The submission takes the delivery use-case shape: validate every
payload before anything is stored; lock and check (owner, state, no existing feedback) in a first
transaction; upload through `MediaStore` **outside** the transaction; then re-check and insert the
feedback row and its photo rows in a second transaction, releasing just-stored assets on failure; notify
the artist after commit.

**Rationale**: A feedback image is the same "customer-supplied media" risk as delivery evidence, so it
must obey the same type/size/count rules and the same upload-outside-the-transaction discipline (FR-025,
Constitution V, research D4, D5). One rule and one shape mean a client handles a feedback image exactly
as it handles a dispatch image, and the `domain` stays stdlib-only (Constitution I).

**Alternatives considered**: Base64 photos in a JSON body — rejected: it inflates the payload ~33% and
departs from the shared multipart pattern the evidence routes use. Reusing `order_delivery_photos` with a
nullable stage — rejected: the FK to `order_feedback`, the distinct read projection and the different
ceiling are what the table needs; a delivery row is not a feedback row. A per-feedback unique index —
rejected: a photo **count** is not expressible as a unique index, so a use-case check under the row lock
is the rule (research D5).

## Open questions carried into implementation

None. Every decision above is settled and each is testable.
