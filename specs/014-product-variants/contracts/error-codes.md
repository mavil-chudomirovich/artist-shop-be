# Contract: Error codes — Product Variants (014)

Authoritative for what the variant-group surface of **module 04 (product)** can answer with.
`specs/014-product-variants/contracts/openapi.yaml` describes where each appears. Codes already
defined by feature 006 are reused, not redefined; this file records the ones this feature adds and
the situations that deliberately reuse a 006 or shared code.

## Codes this feature adds

| Code | HTTP | Meaning | Retryable |
|---|---|---|---|
| `VARIANT_GROUP_NOT_FOUND` | 404 | No variant group carries that identifier or slug, or it is hidden — its category is hidden, none of its options is visible, or it was removed. The situations are one answer on purpose | No |
| `VARIANT_GROUP_SLUG_TAKEN` | 409 | Another variant group already uses that slug, ignoring letter case and surrounding whitespace | No — change the slug |
| `VARIANT_GROUP_HAS_OPTIONS` | 409 | The group still has options; it cannot be removed until they are detached | No — detach the options first |
| `VARIANT_LABEL_TAKEN` | 409 | Another option of the same group already uses that label | No — choose a different label |
| `PRODUCT_ALREADY_IN_VARIANT_GROUP` | 409 | The product is already an option of a group; attaching it to another group is refused (a product belongs to at most one group) | No — detach it first |

### Why the group not-found is one answer

`VARIANT_GROUP_NOT_FOUND` covers: the slug/identifier was never used; the group exists but its
category is hidden; the group exists but none of its options is visible; the group was removed. For
the **public** surface this is required, not convenient — a group hidden by its category and one
hidden because it has no visible option must be indistinguishable from an unknown one, or the
endpoint would confirm which groups the operator has not published (FR-006, FR-008). For the
**administrator** surface the group is read regardless of category/options, so only "unknown" and
"removed" arise.

It is also answered by the product create/edit routes when `variantGroupId` names no group (the
`products_variant_group_fk` refusal), so attaching a product to a group that does not exist is a
`404 VARIANT_GROUP_NOT_FOUND` rather than a `VALIDATION_ERROR` — the referenced group is a missing
resource, not a malformed value.

### Why the removal and attachment refusals are conflicts

Removing a group with options and attaching a product that already has a group are well-formed
requests the current data does not allow — the same shape as module 04's existing
`PRODUCT_STATE_TRANSITION_INVALID` and `PRODUCT_IMAGE_LIMIT_REACHED`: the request is understandable,
the state forbids it. A `400` would wrongly say the request was malformed.

## Codes deliberately reused

| Situation | Code | Why not a new code |
|---|---|---|
| A product attached as an option does not exist | `PRODUCT_NOT_FOUND` (404) | It is the existing "no product carries that id" answer; module 04 already owns it |
| A product is both a combo set and an option, or an option is made a set | `VALIDATION_ERROR` (400) with `details[].field` | The offending value is a field on one row (FR-024); the shared code with a field detail is the established shape |
| Group name/slug/description over bound; slug not URL-safe; category does not exist; an option label empty while grouped | `VALIDATION_ERROR` (400) with `details[].field` | The value is invalid on its own, exactly as the catalogue answers for a product |
| Path identifier is not a UUID | `VALIDATION_ERROR` (400) with `details[].field` | Malformed path segment is request shape |
| Body does not parse, or carries an unknown field | `MALFORMED_REQUEST` (400) | The shared decoder owns this |
| No token / expired | `UNAUTHENTICATED` (401) | The foundation owns session validity |
| A customer calls a group maintenance route | `FORBIDDEN` (403) | Role-based; the foundation established the code |
| Page or page size out of range | `VALIDATION_ERROR` (400) | Pagination validation is a foundation concern |
| Something unexpected failed | `INTERNAL_ERROR` (500) | Never describe an internal failure in catalogue terms |
| Too many requests | `RATE_LIMITED` (429) with `Retry-After` | The shared limiter answers this |

## Picture codes reused for the group gallery

The group gallery reuses module 04's existing picture codes unchanged, because it is the same media
capability and the same rules (formats, 2 MB ceiling, ten-picture count, one primary):

| Code | HTTP | Situation on the group gallery |
|---|---|---|
| `PRODUCT_IMAGE_TYPE_UNSUPPORTED` | 400 | The uploaded bytes are not JPEG, PNG or WebP |
| `PRODUCT_IMAGE_TOO_LARGE` | 413 | The upload exceeds the 2 MB ceiling |
| `PRODUCT_IMAGE_LIMIT_REACHED` | 409 | The group already carries ten pictures |
| `PRODUCT_MEDIA_UNAVAILABLE` | 503 | The media provider refused or is unconfigured (the only retryable code) |

They keep their `PRODUCT_` prefix deliberately: they are module 04's codes and re-prefixing them per
entity would make clients branch on two spellings of one condition.

## What is not here

No code exists for "a group has no options": an empty group is a valid group (it simply is invisible
to customers), not an error. No code exists for "the group's price is missing": the group carries no
price by design (Clarifications). No availability code exists here: this feature adds no
cross-module stock read (Assumptions).
