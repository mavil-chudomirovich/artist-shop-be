# Research: Customer Profile & Shipping Addresses

**Feature**: `specs/003-user-profile` | **Date**: 2026-10-05

Every decision below was resolved during planning; the plan contains no remaining
`NEEDS CLARIFICATION` markers.

---

## D1. Where does the Vietnamese administrative dataset live?

**Decision**: Bundle the dataset as a JSON file embedded into the binary with
`//go:embed`, load it into memory once at startup, and validate province/ward
references against it in the domain layer. Do **not** create `provinces` /
`wards` tables in PostgreSQL.

**Rationale**: The dataset is small (~35 provinces, ~700 wards, well under 100 KB
of JSON), changes a few times per year, and is needed by several modules for
reading. Keeping it in memory makes province/ward validation a pure domain
function with no database round trip, which also means validation cannot be
bypassed by a code path that forgets a foreign key. No synchronisation job, no
staleness window, no migration whenever a ward is renamed. A database table would
add a seed step, a refresh procedure and an extra failure mode for zero read
benefit at this scale.

**Alternatives considered**:
- *Reference tables in PostgreSQL with a seed migration.* Rejected: a ~700-row
  seed inside a SQL migration is large, reviewable-diff-hostile, and every dataset
  update becomes a new migration. Referential integrity would also live in the
  database while the business rule ("ward belongs to province") is a domain
  concern anyway.
- *Fetching from a public administrative API at runtime.* Rejected: introduces an
  external runtime dependency on the request path, makes profile saves fail when a
  third party is down, and contradicts the assumption recorded in the spec that the
  dataset ships with a release.
- *Free text.* Rejected during `/speckit.clarify`: the customer explicitly required
  addresses to be accurate national data selected from dropdowns.

**Consequence**: an address stores province **and ward codes** (stable strings)
alongside the display names it was created with, so a historical address still
renders if the dataset later renames a unit. `internal/share/administrative`
exposes `Provinces()` and `Wards(provinceCode)` for the cascading selects.

---

## D2. Does the profile live in a new table or extend `users`?

**Decision**: Extend the existing `users` table with `display_name`, `phone`,
`avatar_public_id`, `avatar_secure_url`, `avatar_width`, `avatar_height`. No
separate `profiles` table.

**Rationale**: The specification states the profile *is* an extension of the account
rather than a separate record, so there is exactly one customer identity. A
`profiles` table would add a join on the hottest read path (profile read on every
page load), a foreign key that can drift, and a second place to enforce "one
profile per account". Columns are nullable so existing accounts keep working and
the migration stays additive.

**Alternatives considered**:
- *Separate `profiles` table.* Rejected as above.
- *Generic key-value settings table.* Rejected: loses type safety, cannot express
  "at most one avatar", and makes the avatar lookup ambiguous.

**Consequence**: the module owns `users` alongside the auth module. This is the
documented exception already used by the platform (the auth feature created the
table and the constitution's "a module owns its data" rule is satisfied by the user
module owning the profile columns of its own customer). Ownership is documented in
`docs/modules/02-user.md`.

---

## D3. How is "at most one default address" guaranteed?

**Decision**: Enforce it in the database with a partial unique index plus an
application-level transaction, and treat the domain function as the only writer.

```sql
CREATE UNIQUE INDEX addresses_one_default_per_user
    ON addresses (user_id)
    WHERE is_default AND deleted_at IS NULL;
```

**Rationale**: A single default per account is an invariant that must hold under
concurrency — two simultaneous "set default" requests, or an add racing a delete,
cannot be serialised in application code alone. A partial unique index makes the
constraint atomic at the storage layer, so even a future code path or a manual
`psql` session cannot create two defaults. The domain function then guarantees the
*user-visible* behaviour (FR-010: clearing the previous default and setting the new
one is a single indivisible outcome) by running both writes in one transaction.

**Alternatives considered**:
- *Application check-then-update only.* Rejected: classic TOCTOU race; two
  concurrent requests both pass the check.
- *Serialising on the user row (`SELECT … FOR UPDATE`).* Rejected: heavier and
  requires a lock the generic repository does not provide; the index does the work
  without blocking readers.
- *Storing a `default_address_id` on `users`.* Rejected: adds a second source of
  truth that can disagree with `addresses.is_default`.

**Consequence**: the default-flag transition lives in `domain/model/address.go`,
the repository exposes an atomic `SetDefault`, and `application` wraps it in the
existing `UnitOfWork` port. FR-008's "exactly one default at all times" is
verified by a repository test that tries to create two defaults directly.

---

## D4. How are addresses deleted?

**Decision**: Soft delete via `deleted_at timestamptz`. A deleted address is
excluded from every customer-facing list and can never be the default, but its row
remains so historical orders that reference it stay reconstructable.

**Rationale**: The specification requires deleting an address that past orders
reference to be safe. Hard-deleting would either break order history or force
order rows to hold loose foreign keys. Soft delete also makes "hide this address"
a reversible operation and keeps the audit trail meaningful.

**Alternatives considered**:
- *Hard delete plus `ON DELETE SET NULL` on `orders`.* Rejected: loses the address a
  customer actually used, which is exactly what shipping disputes need.
- *Reject deletion while an order references it.* Rejected: it leaks order
  existence into the address screen and blocks a legitimate correction. The spec
  chose hiding, so we hide.

**Consequence**: every read query filters `deleted_at IS NULL`; the partial unique
index includes the same filter so a hidden address cannot hold the default flag.

---

## D5. How is the account for an operation identified?

**Decision**: Always derive the account id from the authenticated session (the
`sub` claim), and for administrator lookups take the id from the path. Never accept
an owner id in a request body or query string.

**Rationale**: Ownership bugs come from trusting a client-supplied owner id. Taking
it from the session makes "can this customer read this address?" a question the
handler cannot get wrong. For the administrator route the id *is* the subject of
the request, so it comes from the path — and the route is role-gated and audited.

**Alternatives considered**:
- *Client-supplied `userId` field.* Rejected: horizontal privilege escalation for
  free.
- *Path parameter for self-service routes too (`/users/{id}/addresses`).* Rejected:
  it invites the same confusion and duplicates the "is this me?" check.

**Consequence**: FR-006 and SC-003 are enforced by construction, and the ownership
test asserts that a second customer's address id yields a not-found response rather
than someone else's data.

---

## D6. How is the avatar resized without adding an imaging library?

**Decision**: Upload the original bytes to the media provider and ask the provider
to apply the resize transformation at upload time (target width 512 px). Add no Go
imaging dependency.

**Rationale**: The requirement is that stored photos are 512 px wide, and media
services do this natively as part of the upload call. Pulling in an image-decoding
library would add a large dependency (with its own CVE surface) to the service
binary to re-implement a capability the provider already offers, and it would slow
every container build. The server still validates the bytes before uploading, so
validation does not depend on the provider.

**Alternatives considered**:
- *Decode and resize in Go.* Rejected: new dependency, more CPU per request, and
  two places that can disagree with the provider about output format.
- *Client resizes before upload.* Rejected: the specification requires the server to
  validate type and size, which means the server cannot trust client-produced
  dimensions, and a hostile client could ignore the resize entirely.

**Consequence**: the `MediaStore` port takes the original bytes plus a target width
and returns an identifier, URL and the resulting dimensions, which are stored so the
client never has to guess the rendered size.

---

## D7. How is upload type and size validated?

**Decision**: Validate on the server by (a) a hard size ceiling enforced while
reading the request body, (b) content sniffing on the first bytes of the payload
rather than trusting the filename or the client-declared `Content-Type`, and
(c) an allowlist of JPEG, PNG and WebP.

**Rationale**: A filename extension and a client-supplied content type are both
attacker-controlled. Sniffing the actual bytes is what makes FR-014 testable as a
security property rather than a suggestion. Buffering with a ceiling also prevents
a memory-exhaustion upload, which a post-hoc size check cannot do.

**Alternatives considered**:
- *Trust the declared content type.* Rejected: trivially bypassed.
- *Only check the extension.* Rejected: same problem, worse.
- *Virus scanning.* Deferred (YAGNI): no requirement for it, and it would add an
  external dependency to every upload.

**Consequence**: the avatar route declares its own body limit, because the global
`MAX_BODY_BYTES` (1 MiB by default) is below the 2 MB avatar ceiling and the shared
pipeline applies globally.

---

## D8. What does an administrator get, and how is it reused?

**Decision**: Ship a read-only route `GET /api/v1/users/{id}` in this module,
restricted to the `ADMIN` role, audited on every call, and expose the same
capability as an interface in `internal/contracts` so later modules depend on the
contract rather than on this module's tables.

**Rationale**: Order and commission modules both need customer contact details and
a delivery address. Settling the access rule now — read-only, admin-only, audited —
avoids each module inventing its own path and then refactoring twice. The
`internal/contracts` interface keeps the dependency direction legal: modules depend
on an interface, and the composition root wires the user module as the provider.

**Alternatives considered**:
- *Deferring to module 16 Admin.* Rejected during `/speckit.clarify`: modules 07 and
  10 would either block or grow a duplicate contract.
- *Letting administrators edit customer profiles.* Rejected: out of scope for this
  feature and not required by any flow.

**Consequence**: one additional route and one audit action; the contract file is the
single place later modules import.

---

## D9. How is the phone number normalised?

**Decision**: A `domain/model/phone.go` value object that trims whitespace, strips
spaces, dots, dashes and parentheses, converts a leading `+84` to `0`, requires
exactly ten digits, and stores the normalised form.

**Rationale**: Customers type `0912 345 678`, `0912-345-678` and `+84912345678` and
mean the same number. Normalising at the edge means one representation in the
database, so equality checks and future SMS integration do not have to handle four
spellings. Keeping it in the domain means the rule is enforced no matter which
entry point calls it.

**Alternatives considered**:
- *Store the raw string.* Rejected: duplicate records for one person.
- *Validate in the handler only.* Rejected: bypassable from any other entry point
  (CLI, future worker).

**Consequence**: FR-003 becomes a single domain function with unit tests, and the
stored value is always the normalised one.

---

## D10. What happens when the dataset changes?

**Decision**: Stored addresses keep their province and ward **codes** plus the
display names captured when the address was created. When a lookup finds a code that
is no longer in the dataset, the address is still readable and the response marks it
as needing attention rather than failing the whole list.

**Rationale**: Administrative units get merged and renamed. Silently dropping an
address would lose delivery information a customer already gave us, and failing the
whole address list would block a customer's checkout because a government dataset
changed. Retaining the captured names means the historical text stays truthful while
the codes stay joinable to the new dataset.

**Alternatives considered**:
- *Deleting addresses whose ward disappeared.* Rejected: data loss.
- *Blocking saves once a dataset version is older than a stored address.* Rejected:
  needs a version field everywhere for a rare event.

**Consequence**: the address response carries the province/ward names and an
optional "needs review" marker; the edge case is covered by a test that loads a
retired code.

---

## Open questions carried into implementation

None. Every decision above is settled; the tasks phase can proceed without asking.