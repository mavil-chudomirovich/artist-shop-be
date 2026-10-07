# Contract: Error codes — Category module

Authoritative for what this module can answer with. `specs/005-category-catalog/contracts/openapi.yaml`
describes where each appears; this file records what each means and, equally, which situations
deliberately reuse a shared code rather than inventing a module one.

## Codes this module adds

| Code | HTTP | Meaning | Retryable |
|---|---|---|---|
| `CATEGORY_NOT_FOUND` | 404 | No category carries that identifier, or it is withheld, or it was removed. The three are deliberately one answer | No |
| `CATEGORY_NAME_TAKEN` | 409 | Another category already uses that name, ignoring letter case and surrounding whitespace | No — change the name |
| `CATEGORY_SLUG_TAKEN` | 409 | Another category already uses that slug | No — change the slug |

### Why the collision is a conflict and not a validation failure

The name is well formed; it simply is not available. That is a different situation from a name
that is malformed, and the operator's next step is different — pick another value, rather than
fix this one. Answering both with one status would make the catalogue's two most common
rejections indistinguishable to whoever is reading the screen.

### Why two codes rather than one

A client that shows "that name is taken" needs to know which of the two it was, and deriving
that from a detail field makes the primary distinction live in a secondary field. Two codes cost
one extra row here and remove that coupling.

## Codes deliberately reused

| Situation | Code | Why not a module code |
|---|---|---|
| Slug is not URL-safe, or a field exceeds its bound | `VALIDATION_ERROR` (400) with `details[].field` | The value is invalid on its own, which is what the shared code means. The field detail is what the operator acts on, and the project already established this shape in module 02 |
| The path identifier is not a UUID | `VALIDATION_ERROR` (400) with `details[].field` | Same: a malformed path segment is request shape, not catalogue state |
| No token, or an expired one | `UNAUTHENTICATED` (401) | The foundation owns session validity for every module |
| A customer calls a maintenance route | `FORBIDDEN` (403) | The rule is role-based and the foundation established the code; the denial is audited by the calling module's own hook, as module 01 already does |
| Page or page size outside its permitted range | `VALIDATION_ERROR` (400) | Pagination validation is a foundation concern, established in module 02 |
| Something unexpected failed | `INTERNAL_ERROR` (500) | Never describe an internal failure in catalogue terms |
| Too many requests | `RATE_LIMITED` (429) with `Retry-After` | The shared limiter answers this, not the module |

## The one answer that is deliberately ambiguous

`CATEGORY_NOT_FOUND` covers three situations on purpose:

1. the identifier or slug was never used;
2. the category exists and is withheld from customers;
3. the category existed and was removed.

For the **public** surface this is required, not merely convenient: if a withheld category
answered differently from an unused identifier, the endpoint would confirm which categories the
operator has chosen not to publish. Two of those three situations are therefore indistinguishable
by design (FR-005).

For the **administrator** surface the same code is used for (1) and (3), because an administrator
reaching a deleted category is the same problem as reaching a mistyped one. Situation (2) never
arises there: the administrator surface reads withheld categories by design.

## What is not here

No code exists for "the category still has products". Nothing references a category yet, so the
situation cannot arise in this feature; when products exist the reference will restrict removal
at the storage layer, and the module document's rule becomes enforceable then. Recorded in
`deferred.md`.
