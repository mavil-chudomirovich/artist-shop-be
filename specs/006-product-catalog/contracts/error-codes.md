# Contract: Error codes — Product module

Authoritative for what this module can answer with. `specs/006-product-catalog/contracts/openapi.yaml`
describes where each appears; this file records what each means and, equally, which situations
deliberately reuse a shared code rather than inventing a module one.

## Codes this module adds

| Code | HTTP | Meaning | Retryable |
|---|---|---|---|
| `PRODUCT_NOT_FOUND` | 404 | No product carries that identifier or slug, or it is hidden — because it is not on sale and not a pre-order, because it is retired, or because its category is hidden — or it was removed. The situations are deliberately one answer | No |
| `PRODUCT_SLUG_TAKEN` | 409 | Another product already uses that slug, ignoring letter case and surrounding whitespace | No — change the slug |
| `PRODUCT_STATE_TRANSITION_INVALID` | 409 | The requested sell-state change is not one the current state allows. The message names the current state | No — the product must be moved through a legal state first |
| `PRODUCT_IMAGE_LIMIT_REACHED` | 409 | The product already carries the maximum of ten pictures | No — remove one first |
| `PRODUCT_IMAGE_TYPE_UNSUPPORTED` | 400 | The uploaded bytes are not JPEG, PNG or WebP, decided by the content's own signature | No — choose a different file |
| `PRODUCT_IMAGE_TOO_LARGE` | 413 | The upload exceeds the 2 MB ceiling | No — send a smaller file |
| `PRODUCT_MEDIA_UNAVAILABLE` | 503 | The media service refused or could not store the upload, or its credentials are not configured. The only retryable code of this module: nothing was changed | Yes |

### Why the state refusal is a conflict and not a validation failure

The requested state is a well-formed value and the request is understandable; it simply is not
reachable from where the product is. That is a different situation from a malformed value, and the
operator's next step is different — move the product through a legal state, rather than fix the
value they sent. It is also what makes an invalid transition distinguishable from a typo, which is
the whole point of FR-023.

### Why the picture ceiling is a conflict

Same reasoning: adding an eleventh picture is a well-formed request that the product's current
contents do not allow. A `400` would say the request was wrong, when what is wrong is the product's
state.

### Why the picture errors are two codes and the type one is not `413`

`PRODUCT_IMAGE_TYPE_UNSUPPORTED` and `PRODUCT_IMAGE_TOO_LARGE` are the two refusals a customer can
act on by choosing a different file, and they lead to different actions — pick another format
versus pick a smaller file — so they are separate. The size one is `413` rather than `400` because
the platform already answers an oversized upload that way for the avatar, and a second status for
the same condition would be a distinction without a difference.

### Why `PRODUCT_MEDIA_UNAVAILABLE` is separate from the avatar's code

The avatar's `USER_MEDIA_UNAVAILABLE` and this one describe the same upstream condition, and they
are deliberately **not** merged: each is a module's own code, and merging them would make one
module's contract depend on another's. A client that handles both already branches on the prefix,
which is what the codes are shaped for.

## A code this feature makes reachable in another module

| Code | HTTP | Module | Meaning |
|---|---|---|---|
| `CATEGORY_IN_USE` | 409 | **Module 03 Category** | A category cannot be removed while products still belong to it |

This is module 03's code, added to its surface by this feature, and it is listed here because this
feature is what makes it reachable. Before the restricting foreign key existed the situation could
not arise; now it can, and FR-036 requires the operator to be told what happened rather than shown
an unexplained server failure. The refusal itself comes from the storage layer — module 03
translates it rather than checking for products, because checking would mean one module reading
another's table.

## Codes deliberately reused

| Situation | Code | Why not a module code |
|---|---|---|
| Slug is not URL-safe, a field exceeds its bound, the price is not positive, the currency is not three uppercase letters, the referenced category does not exist, a member identifier does not exist | `VALIDATION_ERROR` (400) with `details[].field` | The value is invalid on its own, which is what the shared code means. The field detail is what the operator acts on, and the project established this shape in module 02 |
| The path identifier is not a UUID | `VALIDATION_ERROR` (400) with `details[].field` | A malformed path segment is request shape, not catalogue state |
| The request body does not parse, or carries an unknown field | `MALFORMED_REQUEST` (400) | The shared decoder owns this, and the project established it in module 01 |
| No token, or an expired one | `UNAUTHENTICATED` (401) | The foundation owns session validity for every module |
| A customer calls a maintenance route | `FORBIDDEN` (403) | The rule is role-based and the foundation established the code |
| Page or page size outside its permitted range | `VALIDATION_ERROR` (400) | Pagination validation is a foundation concern |
| Something unexpected failed | `INTERNAL_ERROR` (500) | Never describe an internal failure in catalogue terms |
| Too many requests | `RATE_LIMITED` (429) with `Retry-After` | The shared limiter answers this, not the module |

## The one answer that is deliberately ambiguous

`PRODUCT_NOT_FOUND` covers four situations on purpose:

1. the identifier or slug was never used;
2. the product exists and is not on sale and is not a pre-order;
3. the product is retired;
4. the product is on sale but its **category** is hidden.

For the **public** surface this is required, not merely convenient: if a product hidden by its own
state answered differently from one hidden by its category, or from an unused slug, the endpoint
would confirm which products and which categories the operator has chosen not to publish (FR-003,
FR-006). Three of those four are therefore indistinguishable by design.

For the **administrator** surface the same code is used for (1) and (4), because an administrator
reaching a deleted product, or one whose category was removed, is the same problem as reaching a
mistyped one. Situations (2) and (3) never arise there: the administrator surface reads every
product regardless of its state.

## What is not here

No code exists for "the product is a set with no members", or "the product is not a set". Neither
is an error the spec describes: a set is a flag on a product and the spec does not require it to
carry members, so no refusal is defined for either. No code exists for "stock is insufficient":
this feature stores no stock, by the decision recorded in the spec's FR-038, so the situation
cannot arise here. Both are noted in `deferred.md` where they belong to a later module.
