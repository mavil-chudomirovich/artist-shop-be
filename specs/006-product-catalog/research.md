# Phase 0 Research: Product Catalogue

All technical unknowns resolved here. No decision depends on an unanswered question.

## D1: How product visibility reaches the category's display state

**Decision**: A new interface in `internal/contracts`, provided by module 03 and consumed by
module 04 through the composition root:

```go
// CategoryQuery answers the two facts about categories that module 04 needs and cannot
// read itself: which categories a customer is allowed to see, and which category a
// customer-facing slug names.
type CategoryQuery interface {
    VisibleCategoryIDs(ctx context.Context) ([]uuid.UUID, error)
    CategoryIDBySlug(ctx context.Context, slug string) (uuid.UUID, bool, error)
}
```

Module 04's public use cases call it and pass the answers to the repository, which filters
`category_id = ANY($n)` **in SQL** — on the list and on the single-product read alike.

**Why two questions and not one**: the visibility predicate needs the *set* of visible categories;
the public list's optional `category` filter needs the identifier the *slug* names. Both are facts
about categories that module 04 cannot read, and both must reach the SQL for the same reason, so
they belong to one dependency rather than two — one interface, one adapter, one fake. They are not
the same question, which is why the interface is named for the asking rather than for visibility
alone: an earlier draft exposed only `VisibleCategoryIDs`, and the filter could not be built from
it.

**On `CategoryIDBySlug` returning a found-flag rather than a not-found error**: the filter must
answer an **empty list** for a hidden slug, an unknown slug and a slug naming a category with
nothing visible in it — all three indistinguishable (FR-006). A found-flag lets the use case answer
that empty list directly; an error would make the caller translate a not-found into a success,
which is the kind of inversion a later reader gets wrong. A *hidden* slug resolves to its
identifier and is then excluded by the visibility predicate anyway, so the two paths agree without
a special case.

**Rationale**: FR-002 makes a product visible only when its category is, and FR-006 requires the
same rule inside a paginated filter. That has three consequences that decide the shape:

- **The filter must be in the query, not in Go.** Fetching a page of products and then dropping
  the ones in hidden categories returns a short page and a `total` that counts rows the caller
  never received. Both the visible set and the filtered identifier therefore have to reach the SQL.
- **Module 04 may not read `categories.is_visible` or `categories.slug`.** Constitution I forbids
  one module reading another's tables, and a join would put module 03's schema inside module 04's
  query. A contract is the arrangement the constitution prescribes for exactly this.
- **The set is small by nature.** A shop's category catalogue is tens of rows, not thousands, so
  answering with the whole set costs one indexed scan and removes an N+1 that a per-product check
  would create.

The single-product read uses the same set, so no third method is needed and no membership check
happens in Go: the repository's detail query carries `category_id = ANY($n)` too, and a product in
a hidden category simply answers not-found.

**Why a set rather than `IsVisible(id)`**: an `IsVisible` method would be the natural shape for the
single read and the wrong one for the list, which needs the whole set to filter in SQL. One method
that serves both is one thing to implement, one thing to fake and one thing to test; the detail
path pays for a set it does not strictly need, and the spec's scale assumption is what makes that
acceptable rather than sloppy.

**Alternatives considered**: A join onto `categories` — rejected: the module-ownership violation
above. Filtering in Go after the page — rejected: breaks pagination and `total`. Denormalising the
category's visibility or slug onto the product — rejected: hiding a category, or renaming one,
would not propagate, so the copy would be wrong exactly when it mattered. Changing the public
filter from `category=<slug>` to `categoryId=<uuid>` — rejected: a customer-facing link is built
from the slug (research D11), so a UUID filter would make the slug decorative and contradict
`contracts/openapi.yaml` and `quickstart.md`. A per-product `IsVisible` call — rejected: it answers
the list's question N times. A second, separate interface for the slug lookup — rejected: two
dependencies and two fakes for two halves of the same question.

## D2: The image capability moves to `internal/share/media`

**Decision**: `MediaStore`/`MediaReference` and the Cloudinary adapter move from
`internal/modules/user/...` to `internal/share/media`, and both module 02 and module 04 depend on
the shared package. Behaviour does not change: same signature, same validation split, same
sanitised failure reporting.

**Rationale**: Constitution I requires code shared by more than one module to live in
`internal/share/`. Module 04 needs image storage and module 02 owns it, so the choice is between a
shared home and a second copy. A second copy is worse than it looks: the Cloudinary signature
scheme, the multipart upload shape and the sanitised failure classification are exactly the parts
that must not drift, and they are also the parts a second author would get subtly wrong.

The alternative of leaving the adapter where it is and importing it from module 04 is not
available: that would make two business modules depend on each other directly, which is the
coupling the module boundaries exist to prevent.

**What moves and what does not**: the port and the adapter move; module 02's *use case* does not.
`SetAvatar` keeps calling the same three methods through the shared port, and module 02's tests
keep passing unchanged — that is the evidence the move is a move and not a rewrite. The
`config.MediaConfig` the adapter reads already lives in `internal/share/config` and does not move.

**Alternatives considered**: An identical port declared in each module — rejected in the plan's
Complexity Tracking. A new, product-specific media package — rejected: two adapters against one
provider is two places for a signature bug to live.

## D3: How a price is stored

**Decision**: Two columns — `price_amount bigint` in the currency's **minor unit**, and
`currency char(3)` — with a storage check that the amount is positive. Money is never a float and
never a decimal string in this service.

**Rationale**: Constitution II requires integer minor units with an explicit currency, and the
spec's FR-030 requires the stored amount to equal the amount supplied exactly. A `bigint` of minor
units satisfies both with no rounding step anywhere in the path: the JSON number is parsed as an
integer, stored as an integer and returned as an integer.

**On the currency**: the spec lists it among the fields the administrator supplies (FR-010), so it
is a request field rather than a service constant, even though the shop trades in one currency
today. It is validated as exactly three uppercase ASCII letters. The service does **not** restrict
it to a supported list: the spec asks for an explicit currency, not for a currency policy, and a
list would be a business rule nothing in the spec authorises.

**Alternatives considered**: A `numeric` column — rejected: it invites fractional amounts for a
currency that has none, and it makes "the stored amount equals the supplied amount" a comparison
between two types rather than an identity. Storing the currency once per deployment — rejected:
the constitution says money carries an explicit currency, and a service-wide constant is not
carried by the value.

## D4: The slug rule is module 03's rule, reused verbatim

**Decision**: A product's link segment follows **exactly** the rule a category's does: lowercase
unaccented letters, digits and single hyphens, no leading or trailing hyphen, at most 140
characters counted in characters, unique across the catalogue ignoring letter case and surrounding
whitespace, written by the operator and editable afterwards.

**Rationale**: FR-027 to FR-029 say so, and the value of the rule is that it is one rule. An
operator who has learned what a valid category link looks like should not have to learn a second
shape for a product link. The implementation is the same pure function over the same Unicode
folding, so the two modules cannot disagree about what "the same slug" means.

**Alternatives considered**: Deriving the slug from the name — rejected for products for the same
reason module 03 rejected it: a derived value either freezes a typo or breaks saved links.
A different, product-specific shape — rejected: two rules to keep in step for no benefit.

## D5: The name is not unique

**Decision**: A product's name carries no uniqueness constraint. Only the slug does.

**Rationale**: `docs/modules/04-product.md` requires a unique slug and says nothing about the
name, and product names legitimately repeat — two prints of the same title, a set and its
headline item, the same design at two sizes. A unique-name rule would refuse a real catalogue.
The spec records this as an assumption and an edge case.

**Alternatives considered**: Unique like a category's — rejected: a category is a vocabulary the
shop controls; a product name is a title, and titles repeat. Making it unique would push the
operator into inventing artificial names.

## D6: The picture ceiling, and the transaction that makes it race-free

**Decision**: At most **10** pictures per product (FR-020). The count is checked inside a
`UnitOfWork` transaction that first locks the product's row, so two concurrent uploads cannot both
observe nine and both insert.

**Rationale**: The ceiling is an invariant of the product, and an application-level count with no
locking has exactly the race feature 003 was bitten by: two writers read the same count and both
proceed. Unlike a slug, a count cannot be expressed as a unique index, so the transaction is what
provides the guarantee — which is also why this is the module's one genuine use of the `UnitOfWork`
port.

The check happens **before** the provider is called, so a refused eleventh picture does not leave
an orphaned asset behind (FR-020). The provider call itself is deliberately outside the
transaction: holding a database lock across a network call to a third party is how a slow provider
becomes a database problem.

**Alternatives considered**: A `CHECK` on a stored count column — rejected: it would need the count
maintained on every picture write, i.e. a second source of truth for something the pictures
themselves already say. A trigger — rejected: business logic in the schema, invisible to the
domain and to tests. Counting in the application with no lock — rejected above.

## D7: Exactly one primary picture

**Decision**: A partial unique index on `(product_id)` where the row is the primary one guarantees
**at most one** primary per product at the storage layer. "**At least one** while any picture
exists" is maintained by the use case: the first picture added becomes primary, and removing the
primary promotes the next one by position.

**Rationale**: The two halves of FR-016 are different kinds of rule. "At most one" is a
catalogue-wide uniqueness property and belongs to the storage layer, exactly as the slug's
uniqueness does — no code path can create a second primary if the index refuses it. "At least one"
cannot be a constraint, because a product with no pictures has no primary and is a valid product
(the spec's edge case); it is a transition, and transitions live in the use case.

**Alternatives considered**: A `is_primary` flag with no index and an application check —
rejected: two concurrent promotions produce two primaries and a product whose main image is
whichever the client happens to read first. An ordered list where "first" means primary and no
flag exists — rejected: reordering would then silently change which picture is the main one, which
is a different act from promoting one.

## D8: The sell-state machine

**Decision**: Four states, stored as a constrained text column and named in the domain:

```
COMING_SOON ──► ACTIVE ◄──► OUT_OF_STOCK
     │            │              │
     └────────────┴──────────────┴──► DISCONTINUED   (terminal)
```

Exactly the transitions FR-021 lists: `COMING_SOON → ACTIVE`, `ACTIVE ↔ OUT_OF_STOCK`, and any
state → `DISCONTINUED`. Nothing else, and `DISCONTINUED` has no outgoing edge.

**Rationale**: Constitution III requires the transition set to be explicit in code and tested both
ways, and the spec settled the set by clarification rather than leaving it to a plan. The restock
edge (`OUT_OF_STOCK → ACTIVE`) is the one addition to the chain the module document draws, and the
spec records why: a one-way chain would make a sold-out product permanently unsellable, forcing a
duplicate product and breaking every saved link.

The state is a method-driven field, never a settable one: a caller asks the entity to transition,
and the entity refuses a move it does not have an edge for. A direct write to the column would be
the ad-hoc status write Constitution III forbids.

**Alternatives considered**: A one-way chain exactly as drawn — rejected by the operator for the
reason above. A free-form status string — rejected: it cannot be validated, so an invalid state
would be storable. Extra states (`ARCHIVED`, `DRAFT`) — rejected: the module document names four,
and a fifth would need a transition rule for every edge.

## D9: What a pre-order is

**Decision**: A boolean label plus an optional expected-availability date on an otherwise ordinary
product. A product may carry the label only while it is **not** on sale; transitioning to `ACTIVE`
clears it.

**Rationale**: The spec settled this by clarification. The label is what makes an unlaunched
product visible (FR-002) — which is the whole point of announcing one — while leaving it
unbuyable (FR-024). Clearing it on launch keeps the two facts from disagreeing: a product that is
on sale is not "coming soon", so it must not carry a label that says it is.

**Alternatives considered**: A fifth sell state — rejected: it would multiply the transition table
and the spec explicitly chose a label to avoid that. A per-release quantity limit — rejected by
the operator as a different feature with its own data model.

## D10: What a combo set is

**Decision**: A set is a **product** with `is_set = true`, its own name, slug, description,
pictures, price and sell state, plus a list of member products. The price is the operator's; the
members are recorded and never priced. Members are supplied as a list of product identifiers on
create and on update.

**Rationale**: The spec settled the pricing by clarification, and the shape follows from it: if
the price does not come from the members, the set is not a derived entity and needs no price
arithmetic. Recording the members anyway is what lets the operator see what they put in a set, and
what makes "removing a set does not touch its members" (US5) meaningful.

**Alternatives considered**: A set as a separate entity type — rejected: it would duplicate every
product field and every product rule for a difference of one price calculation that does not
happen. A set that hides its members from the catalogue — rejected by US5 scenario 2.

## D11: The routes

**Decision**:

| Method | Path | Who |
|---|---|---|
| GET | `/api/v1/products` | anyone |
| GET | `/api/v1/products/{slug}` | anyone |
| GET | `/api/v1/admin/products` | administrator |
| POST | `/api/v1/admin/products` | administrator |
| GET | `/api/v1/admin/products/{id}` | administrator |
| PATCH | `/api/v1/admin/products/{id}` | administrator |
| DELETE | `/api/v1/admin/products/{id}` | administrator |
| POST | `/api/v1/admin/products/{id}/state` | administrator |
| POST | `/api/v1/admin/products/{id}/images` | administrator |
| DELETE | `/api/v1/admin/products/{id}/images/{imageId}` | administrator |
| POST | `/api/v1/admin/products/{id}/images/{imageId}/primary` | administrator |

The public read is addressed by **slug** and the administrator surface by **identifier**, exactly
as module 03 does and for the same reasons: a customer link is built from the slug, and an
operator mid-edit must not have the address change under them. The public list takes an optional
`category` filter by category **slug**, because that is what a customer-facing category link
carries.

**The state change is its own endpoint.** Folding it into `PATCH` would make an invalid transition
look like a field that failed validation, and it would let one request both edit a product and
move it through its lifecycle — two operations with different rules and different failure modes.
A dedicated `POST .../state` says "this is a transition and it can be refused", which is what
Constitution III asks the surface to express.

**Alternatives considered**: One set of paths with the role deciding the response — rejected in
module 03 for the same reason and not revisited here. A `PATCH` with a `sellState` field —
rejected above. `PUT .../images/order` for reordering — rejected as YAGNI: order is insertion
order and the spec asks for an order, not for reordering.

## D12: Pagination

**Decision**: Both lists are paginated with the project's existing convention — a page selector
defaulting to 20, permitted between 1 and 100, answering the same metadata block the existing
paginated endpoints answer.

**Rationale**: The shop's product catalogue is created by the operator and has no natural bound,
so it takes the ordinary rule rather than the reference-data exception, exactly as module 03's
catalogue does.

**Alternatives considered**: Unpaginated — rejected: the catalogue grows without a bound.

## D13: What removing a product means

**Decision**: A **hard delete** of the product row, cascading to its pictures and its set
membership rows. The audit entry stays.

**Rationale**: The module document says "xóa mềm" (soft delete) in its MVP scope, and the spec's
FR-012 says only that an administrator can remove a product. A hard delete is chosen because
nothing reads a removed product: no order exists yet in this feature's scope, so there is no
history to preserve — and module 03 already established hard deletion for the same reason. A soft
delete would add a second invisible state to every read path.

**The obligation this leaves, stated plainly**: when module 07 Order exists, an order line will
have to keep reading a product it referenced. That is the point at which soft deletion or a
snapshot becomes necessary, and it belongs to the order module's design, not this one. Recorded in
`deferred.md`.

**Alternatives considered**: Soft deletion now, in anticipation — rejected: it is a state with no
reader, and the module that needs it does not exist. Removing the row but keeping the picture
assets — rejected: the assets would be unreachable and billable for nothing.

## D14: What happens to the stored pictures when a product or a picture is removed

**Decision**: Removing a picture releases the stored asset through the media port. Removing a
product releases every one of its pictures' assets, after the rows are gone.

**Rationale**: The spec's FR-020 requires a removed picture to release its asset, and a removed
product's pictures are removed by the cascade. Releasing **after** the database write is the order
module 02 already uses for an avatar and for the same reason: if the release fails, the reference
is already gone, so the worst case is an orphaned asset rather than a product pointing at
something that no longer exists.

**Alternatives considered**: Releasing before the write — rejected: a release that succeeds
followed by a write that fails leaves a product rendering a broken image. Leaking the assets —
rejected: unbounded provider storage with no owner.

## D15: The category reference, and the restricted delete

**Decision**: `products.category_id uuid NOT NULL REFERENCES categories(id) ON DELETE RESTRICT`,
created in this feature's migration. Module 03's delete path translates the resulting storage
refusal into `CATEGORY_IN_USE` (409).

**Rationale**: This is the debt feature 005 recorded and left here. Its `deferred.md` D1 states the
condition that closes it exactly: the reference lives on the product, so it must be created in
module 04's migration, and with a **restricting** delete so the rule holds at the storage layer
rather than in one code path. Doing it here is what makes 005's completion criterion true.

Translating the refusal is required by FR-036, and it is module 03's job because module 03 owns the
delete. The alternative — module 03 asking module 04 whether products exist — would be module 03
reading module 04's data, the same violation from the other side. The database is the one place
that can answer without either module reaching into the other, and it is already answering; the
change is to report its answer in words an operator can act on.

**Alternatives considered**: `ON DELETE CASCADE` — rejected outright: it would delete a shop's
products as a side effect of tidying categories, which is the opposite of what the module document
asks for. `ON DELETE SET NULL` — rejected: the spec requires exactly one category per product, and
a null category would be a product the catalogue cannot describe. Checking in module 03 — rejected
above.

## D16: The resize target for a product picture

**Decision**: A product picture is uploaded through the same port, asked to resize to a configured
maximum width (default **1600**), and the provider's reported dimensions are stored. No width
**guard** is applied, unlike the avatar's.

**Rationale**: The avatar's 512-pixel guard exists because the avatar contract declares a bound;
the product contract declares none, so a guard would refuse a reference the contract permits.
Asking for a resize is still right — a 2 MB phone photo is far wider than any storefront renders,
and downscaling at the provider is the same choice module 02 made.

**Alternatives considered**: No resize at all — rejected: it stores and serves images far larger
than any page displays. The same 512 width as an avatar — rejected: a product image is a primary
visual, not a thumbnail. A guard mirroring the avatar's — rejected above.

## D17: The ordering tie-break

**Decision**: `ORDER BY position, created_at, id` — ascending, all three, exactly as module 03
orders its catalogue.

**Rationale**: FR-009 requires the order to be stable across identical requests and the spec's
edge cases permit two products to share a position. The reasoning is module 03's, unchanged:
position alone leaves ties unordered, a tie-break that can change lets a new product shuffle its
neighbours, and the identifier breaks the remaining tie immutably.

**Alternatives considered**: Position then name — rejected: renaming a product would silently
reorder the shop's front page. Newest-first — rejected by the operator, who wants to control what
appears first.

## D18: Whether the public detail enumerates a set's members

**Decision**: No. The public product shape does not list the products inside a set. The
administrator shape does.

**Rationale**: The spec describes a set to a customer as "a single item with its own name, price
and picture" (US5) and does not ask the customer-facing shape to enumerate the contents, so adding
that field would be the plan inventing a contract the specification does not describe. The
operator, by contrast, needs to see what they put in a set, and the administrator shape is already
the one that shows what a customer cannot.

**Recorded as a deliberate gap**: showing a set's contents to a customer is a reasonable future
wish and is recorded in `deferred.md` rather than implemented against no requirement.

## Open questions carried into implementation

None. Every decision above is settled and each is testable.
