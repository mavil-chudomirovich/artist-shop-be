# Phase 0 Research: Category Catalogue

All technical unknowns resolved here. No decision depends on an unanswered question.

## D1: Where the slug shape rule lives

**Decision**: A pure function in `domain/model`, so the rule has one home and every entry point
— HTTP today, a CLI or a seed command later — is bound by it.

**Rationale**: The rule is a business rule about what a category's public link may look like,
not a transport concern. Put in the handler it would be re-implemented by the next entry point
and the two would drift; the project learned this in module 02, where the avatar ceiling lives
in the domain precisely so a CLI cannot bypass it.

**Alternatives considered**: Validating in the handler, which is where the JSON arrives —
rejected for the reason above. Carrying the rule in the OpenAPI pattern alone — rejected: a
contract describes what a client should send, it does not enforce what the service accepts.

## D2: How case- and whitespace-insensitive uniqueness is enforced

**Decision**: Store a normalised form of both the name and the slug beside the operator's
values, and put the unique indexes on the normalised columns only.

**Rationale**: FR-016, FR-017 and FR-021 together require uniqueness that ignores letter case
and surrounding whitespace **and** holds catalogue-wide against any writer. An application-level
check cannot do that: two concurrent creates both read "no such name" and both insert. Feature
003's address module produced exactly that class of defect, and its fix was to let the storage
layer decide. A unique index over a normalised value is the only form the database can enforce
on its own.

**Alternatives considered**: A functional unique index over `lower(trim(name))` — workable, and
rejected because the normalisation then lives in a query while the domain also needs it for the
message it returns, giving the rule two homes. The `citext` type — rejected: it needs an
extension installed, and its case folding follows the database collation rather than the
Unicode standard, which is the subject of D3. An application-only check — rejected above: it
reports a duplicate only when the race does not happen.

## D3: Where the case folding happens, and why not in the database

**Decision**: The **application** computes the normalised value, using the Go standard library's
Unicode-aware case folding. The database stores it and enforces uniqueness on it, and carries a
**shape check** that the stored value is already trimmed and lowercase — but does not recompute
it.

**Rationale**: PostgreSQL's `lower()` folds according to the database collation. Under the `C`
collation it folds ASCII only, so a Vietnamese name beginning with an uppercase letter that is
not in ASCII would not fold at all — and the catalogue would silently accept two categories
whose names differ only by the case of one Vietnamese letter. That is the worst kind of failure:
it looks correct in every English test. Folding in the application, where the Unicode tables
are versioned with the language rather than with the database's locale settings, removes the
dependence entirely.

The shape check is the safety net: a future writer that forgets to normalise inserts a value the
database can recognise as un-normalised and refuse, without the database having to agree with Go
on how to fold.

**Verified, not assumed**: Go's `strings.ToLower` maps `Đ` (U+0110) to `đ` (U+0111) and folds
the Vietnamese diacritic set correctly, because it uses Unicode simple case mapping rather than
a locale. This is the reason the application owns the rule; a locale-driven fold would be the
one that fails.

**Alternatives considered**: Normalising in the database with `citext` or an ICU collation —
rejected: it makes correctness depend on how each deployment's database was created, and the
test database and production could disagree. Storing no normalised value and comparing in
queries — rejected in D2.

## D4: What a collision answers with

**Decision**: HTTP **409** with the shared `CONFLICT`-class answer, carrying a module code that
names which field collided, plus the field-level detail entry the project already uses.

**Rationale**: The input is well formed and the request is understandable — it simply conflicts
with what the catalogue already holds. That is a different situation from a malformed value, and
a client needs to tell them apart to decide whether to change the value or the field. The
project already documents a conflict status for exactly this shape of problem, so no new
concept is introduced.

**Alternatives considered**: A validation failure, which is what module 02 uses for a rejected
field — rejected: module 02's case is a value that is invalid on its own (`an empty recipient
name`), and here the value is valid and only its existence elsewhere is the problem. Collapsing
both into one status would make "that name is taken" indistinguishable from "that name is
malformed", which are different fixes for the operator.

## D5: What removal means

**Decision**: Removal is a **hard delete**. The row goes; the audit entry stays.

**Rationale**: The module document's own words are "do not hard-delete a category that still has
products", which takes hard deletion as the default and blocks it only when something references
the category. Nothing references a category yet. Soft deletion would add a second, invisible
state to every read path — the public list would have to exclude hidden rows *and* removed
rows, and the two would be distinguishable to a future reader only by remembering which is
which.

**The obligation this leaves, stated plainly**: once products exist, `products.category_id`
must reference `categories.id` with a **restricting** delete, so the module document's blocked
removal becomes a storage-layer guarantee rather than an application check. That obligation is
recorded in `deferred.md` and belongs to module 04, because the product entity is where the
reference lives. What this feature can and does guarantee now is the other half: the identifier
a product will reference is immutable and unique for the category's lifetime.

**Alternatives considered**: Soft deletion, which module 02 used for addresses — rejected here:
addresses were soft-deleted because orders had to keep reading them, and no order has to read a
category. A `deleted_at` column with no reader is cost with no benefit.

## D6: The ordering tie-break

**Decision**: `ORDER BY position, created_at, id` — ascending, all three.

**Rationale**: FR-003 requires the order to be stable across identical requests, and the spec's
Assumptions permit two categories to share a position. Two of the three keys are needed because
one alone is insufficient: position alone leaves ties unordered, and a tie-break that can change
would let a new category shuffle the ones around it. Insertion order is not stable enough on its
own because two rows created in the same transaction share a timestamp, so the identifier breaks
the remaining tie — and it is immutable, which is what makes the order reproducible.

**Alternatives considered**: Position then name — rejected: renaming a category would silently
reorder the catalogue, which is surprising and hard to explain. Randomising ties — rejected: it
breaks FR-003 outright.

## D7: The routes, and why the administrator surface is prefixed

**Decision**:

| Method | Path | Who |
|---|---|---|
| GET | `/api/v1/categories` | anyone |
| GET | `/api/v1/categories/{slug}` | anyone |
| GET | `/api/v1/admin/categories` | administrator |
| POST | `/api/v1/admin/categories` | administrator |
| GET | `/api/v1/admin/categories/{id}` | administrator |
| PATCH | `/api/v1/admin/categories/{id}` | administrator |
| DELETE | `/api/v1/admin/categories/{id}` | administrator |

The public read is addressed by **slug**, because the slug is what a customer-facing link is
built from — addressing it by identifier instead would make the slug decorative. The
administrator surface is addressed by **identifier**, because the operator reached it from the
administrator list and an identifier cannot change under a client that is mid-edit.

**Rationale for the `/admin/` segment**: module 02 deliberately avoided an administrator prefix
for its customer lookup, and that decision stands — there the resource being read was a
*customer*, addressed by the customer's own identifier, so no separate surface existed. Here the
resource is the catalogue, which has two audiences with genuinely different views of the same
rows: one sees hidden categories, one must never know they exist. Two paths keep the two
response shapes from being one shape with a conditional, which is the failure mode this project
already paid for once. The auth module already exposes an administrator path under `/admin/`,
so the segment is not new either.

**Alternatives considered**: One set of paths with the role deciding the response — rejected for
the reason above. `/categories/manage` — rejected as a less obvious convention than the one the
project already uses.

## D8: Pagination

**Decision**: Both lists are paginated with the project's existing convention — a page selector
defaulting to 20, permitted between 1 and 100, answering the same metadata block the existing
paginated endpoints answer.

**Rationale**: The project's list convention is ordinary pagination, with a documented exception
for reference data that is bounded and embedded in the binary. The catalogue is neither: the
operator creates it and could create thousands. Taking the ordinary rule rather than the
exception keeps the exception meaningful — an exception that is not needed is how a convention
stops being informative.

**Alternatives considered**: Unpaginated with a documented exception, as the divisions endpoints
did — rejected: those are bounded by a fixed dataset, and the catalogue is not.

## D9: How the display state is represented

**Decision**: A single boolean on the category, with the transitions expressed as methods on the
entity rather than as a settable field.

**Rationale**: There are exactly two states and no third is foreseen — removal is a delete, not
a state (D5). A boolean with no setter keeps the transition explicit (Constitution III) while
staying the smallest thing that can be true. An enumeration would invite a third value that
nothing handles.

**Alternatives considered**: A status string with `visible`, `hidden` and `archived` — rejected:
`archived` would be a second name for deletion, and two ways to remove a category is one too
many. A nullable timestamp meaning "hidden since" — rejected: nothing reads it.

## D10: Whether this module needs a cross-module contract

**Decision**: No entry in `internal/contracts/`.

**Rationale**: A contract exists so one module can depend on another without depending on its
internals. Nothing consumes categories yet — module 04 will, and it will need products-by-
category, which is a query over the product table filtered by a category identifier rather than
a call into this module. Writing a contract now would be an interface with no implementer and no
consumer, and the project's own guidance is that contracts appear when a consumer appears.

**Alternatives considered**: Publishing a `CategoryLookupService` now, so module 04 finds it
ready — rejected: it would be designed against a guessed requirement, and module 04's actual
need is a join, not a service call.

## D11: The internal whitespace question, decided narrowly

**Decision**: Uniqueness normalises by **trimming the ends and folding case only**. Internal
runs of whitespace are **not** collapsed, so "Tranh sơn dầu" and "Tranh  sơn  dầu" are two
different names.

**Rationale**: This is what FR-016 and the spec's edge cases say, literally — they scope sameness
to letter case and *surrounding* whitespace. Quietly making the rule stronger would be the plan
changing the specification, which is the thing this whole workflow exists to prevent.

**Recorded as a known sharp edge rather than a defect**: an operator who types a double space
can create what looks to a human like a duplicate. The impact is cosmetic — the operator can
rename one — and the fix, if the product owner wants it, is a one-line change to the
normalisation rule plus a spec amendment. Recorded in `deferred.md`.

**Alternatives considered**: Collapsing internal runs now — rejected for the reason above. It is
the better outcome and the wrong process.

## Open questions carried into implementation

None. Every decision above is settled and each is testable.
