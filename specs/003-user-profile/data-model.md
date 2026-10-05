# Data Model: Customer Profile & Shipping Addresses

**Feature**: `specs/003-user-profile` | **Date**: 2026-10-05

Entities, storage layout and the rules the code must enforce. Source of truth for
field meanings is `spec.md`; rationale for each choice is in `research.md`.

---

## 1. Storage overview

| Storage | Contents | Notes |
|---|---|---|
| PostgreSQL `users` (existing table, extended) | Profile columns | Auth module created the table; this module adds the profile part |
| PostgreSQL `addresses` (new table) | Shipping addresses | Owned entirely by this module |
| Embedded JSON (`go:embed`) | Provinces and wards reference data | Read-only, in memory, never in the database (research D1) |

No new storage engine, no cache server, no object storage: media bytes live in the
external media service, the database keeps only the reference.

---

## 2. Entity: Profile

The profile is the customer-facing part of an account. It is **not a separate table**:
it is a column group on the existing `users` table created by the auth module, keyed
by that table's `users.id`.

**Columns added by this feature** (all nullable, so existing accounts stay valid):

| Column | Type | Null | Rules |
|---|---|---|---|
| `display_name` | `text` | yes | Trimmed; may be empty (a new account has none) |
| `phone` | `varchar(20)` | yes | Normalised to 10 digits starting with `0` (research D9) |
| `avatar_public_id` | `text` | yes | Opaque media identifier; null means no avatar |
| `avatar_secure_url` | `text` | yes | Displayable link; null means no avatar |
| `avatar_width` | `int` | yes | Pixel width of the stored image |
| `avatar_height` | `int` | yes | Pixel height of the stored image |

**Columns already present and NOT writable by this module** (owned by module 01 auth):

| Column | Type | Owner |
|---|---|---|
| `id` | `uuid` | auth — primary key |
| `email` | `text` | auth |
| `password_hash` | `text` | auth |
| `role` | `text` | auth |
| `status` | `text` | auth |
| `created_at`, `updated_at` | `timestamptz` | auth — `updated_at` is maintained by every write to the row |

No backfill is required: a profile is optional by design, and the auth module's
existing rows keep working with nulls.

### Validation rules

| Rule | Source | Enforced by |
|---|---|---|
| Display name is trimmed and may be empty | FR-002, FR-004 | `domain/model/profile.go` |
| Phone, when present, is a Vietnamese mobile number: ten digits starting with `0` | FR-003 | `domain/model/phone.go` |
| Phone is stored normalised, never raw | FR-003 | `domain/model/phone.go` |
| Avatar columns are either all null or all populated | FR-016 | `domain/model/avatar.go` |
| Stored avatar width never exceeds 512 px | FR-015 | media adapter + domain guard |
| Only the account in the session may read or write its profile | FR-006 | account id from session (research D5) |

### Lifecycle

| Transition | Trigger | Result |
|---|---|---|
| *no profile* → *profile with details* | first successful update | Row exists; avatar still null |
| *profile* → *avatar attached* | successful upload | All four avatar columns populated together, enforced by a CHECK constraint |
| *avatar attached* → *avatar replaced* | successful upload | All four columns overwritten atomically; old reference released |
| *avatar attached* → *no avatar* | explicit removal | All four columns cleared atomically |
| *any* → *email or role changed* | auth module | **Not this module's transition**; those columns are not writable here |

Rejections leave the profile untouched — a failed upload or invalid phone never
partially applies (FR-017).

---

## 3. Entity: Shipping Address

| Field | Type | Null | Rules |
|---|---|---|---|
| `id` | `uuid` | no | Primary key |
| `user_id` | `uuid` | no | Owning account; every query filters on it |
| `recipient_name` | `text` | no | Trimmed, non-empty |
| `recipient_phone` | `varchar(20)` | no | Normalised like `phone` (research D9) |
| `province_code` | `varchar(10)` | no | Must exist in the dataset |
| `province_name` | `text` | no | Captured at save time so history stays truthful (research D10) |
| `ward_code` | `varchar(15)` | no | Must exist **and belong to** `province_code` |
| `ward_name` | `text` | no | Captured at save time |
| `street_address` | `text` | no | Trimmed, non-empty, free text (house number, street) |
| `is_default` | `boolean` | no | Defaults to `false` |
| `deleted_at` | `timestamptz` | yes | Non-null means hidden (research D4) |
| `created_at` | `timestamptz` | no | UTC |
| `updated_at` | `timestamptz` | no | UTC |

### Constraints

| Constraint | Mechanism | Covers |
|---|---|---|
| **At most one default per account** | `CREATE UNIQUE INDEX addresses_one_default_per_user ON addresses (user_id) WHERE is_default AND deleted_at IS NULL` | FR-008, SC-004, research D3 |
| A hidden address can never be the default | The index predicate excludes hidden rows; the domain transition refuses it | FR-008 |
| Ward belongs to the chosen province | Use-case check in `application/implement` via the `Divisions` port, because `domain` may not import `share/administrative` (Constitution I) | FR-007b, SC-011 |
| Province and ward exist in the dataset | Use-case check through the same port; the shared package's own sentinel errors map to `USER_UNKNOWN_PROVINCE` / `USER_UNKNOWN_WARD` in presentation | FR-007a |
| An address belongs to exactly one account | `user_id` taken from the session | FR-006 |

### Lifecycle

| Transition | Trigger | Result |
|---|---|---|
| *none* → *address* | create | `is_default = true` if the account had no default, else `false` (FR-009) |
| *address* → *default* | set default | Previous default cleared and new one set **in one transaction** (FR-010) |
| *default* → *non-default* | another address becomes default | Exactly one default remains |
| *address* → *edited* | update | `is_default` preserved (FR-011) |
| *address* → *hidden* | delete | Row kept, excluded from lists, cannot be default (FR-012) |
| *last default* → *none* | delete the only default | No default remains; the next created address becomes default (US2 AC6) |

Invalid transitions are rejected with a typed domain error and recorded; the
previous state stays intact (constitution III).

---

## 4. Entity: Province and Ward (reference data)

Read-only, bundled with the binary, never persisted.

| Field | Type | Rules |
|---|---|---|
| `code` | `string` | Stable identifier; the only value stored on an address |
| `name` | `string` | Display name shown in the select |
| `wards` | `Province` → `[]Ward` | Every ward belongs to exactly one province |

### Rules

| Rule | Source |
|---|---|
| Clients retrieve provinces, then the wards of one province — never the whole list at once | FR-007c |
| A ward list is always scoped to a single province | FR-007c |
| A code that is no longer in the dataset does not invalidate a stored address; the response flags it | research D10, edge case |
| The dataset is read-only at runtime | spec assumption |

---

## 5. Relationships

```text
users (auth module)
  1 ──── 0..1  profile columns (this module, same row)
  1 ──── *    addresses (this module)
orders (module 07, future)
  * ──── 0..1  addresses   → stores its own copy of the address text at order time
```

- One account has **zero or more** addresses and **at most one** default among the
  non-hidden ones.
- One province has **one or more** wards; one ward belongs to **exactly one**
  province.
- Future order rows keep their **own snapshot** of recipient, phone, province, ward
  and street, so a later address edit or deletion never rewrites order history
  (FR-012, research D4).

---

## 6. Audit events

Every event below is written to `audit_logs` with actor, action, target, outcome and
timestamp (FR-019, FR-022a).

| Action | Actor | Recorded when |
|---|---|---|
| `USER_PROFILE_UPDATED` | customer | Display name or phone changed, including cleared fields |
| `USER_AVATASET` / `USER_AVATAR_REMOVED` | customer | Avatar attached/replaced or removed |
| `USER_ADDRESS_CREATED` | customer | Address created |
| `USER_ADDRESS_UPDATED` | customer | Address edited |
| `USER_ADDRESS_DELETED` | customer | Address hidden |
| `USER_ADDRESS_DEFAULT_SET` | customer | Default address changed |
| `USER_PROFILE_VIEWED_BY_ADMIN` | admin | Administrator opened a customer's contact details |

The administrator read event is privacy-relevant: customer contact data must leave
the customer's control only with a trace.

---

## 7. Validation summary (testable rules)

| # | Rule | Test level |
|---|---|---|
| 1 | Phone normalises to ten digits and rejects anything else | unit |
| 2 | Display name may be cleared and is trimmed | unit |
| 3 | Province must exist in the dataset | unit |
| 4 | Ward must exist and belong to the chosen province | unit |
| 5 | Address create/delete/mark-default keeps exactly one default at all times | unit + integration |
| 6 | A second default cannot be created even by writing the column directly | integration (index) |
| 7 | Deleting the default address leaves zero defaults and never two | integration |
| 8 | Another customer's address id yields a not-found response, not their data | unit + HTTP |
| 9 | Uploaded bytes must sniff as JPEG, PNG or WebP | unit |
| 10 | Upload larger than 2 MB is rejected before reaching the media service | unit + HTTP |
| 11 | Rejected upload leaves the previous avatar intact | unit + integration |
| 12 | Administrator read is refused for customers and audited for admins | HTTP + integration |
| 13 | Address list is paginated and returns a stable order plus total | integration |
| 14 | A stored code missing from the dataset still renders the saved names | unit |
| 15 | The stored avatar reference reports a width of at most 512 px | unit + integration |

---

## 8. Migration

One migration file, `migrations/00004_user.sql`, following the existing naming rule
`NNNNN_<module>_<description>.sql` and the centralized runner:

1. `ALTER TABLE users ADD COLUMN` for the profile columns (additive only; existing
   accounts stay valid with nulls).
2. `CREATE TABLE addresses` with the foreign key to `users(id)`, an index on
   `(user_id) WHERE deleted_at IS NULL` for list queries, and the partial unique
   index for the default invariant.
3. No data backfill: the profile is optional by design.

Both parts must be reversible (`-- +goose Down`), because the repository requires
every migration to provide Up and Down sections.