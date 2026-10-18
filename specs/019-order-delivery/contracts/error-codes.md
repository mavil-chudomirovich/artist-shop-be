# Contract: Error codes — Order delivery & completion

Authoritative for what this feature can answer with. `contracts/openapi.yaml` describes where each
appears; this file records what each means and which situations deliberately reuse a shared code.

## Codes this feature adds

| Code | HTTP | Meaning | Retryable |
|---|---|---|---|
| `ORDER_IMAGE_TYPE_UNSUPPORTED` | 400 | An uploaded photo is not JPEG, PNG or WebP (the format is decided by sniffing the bytes) | No — choose a supported file |
| `ORDER_IMAGE_TOO_LARGE` | 413 | An uploaded photo exceeds the size ceiling; also returned when a request near the route's body ceiling is refused before the count check | No — compress the photo / send fewer |
| `ORDER_IMAGE_LIMIT_REACHED` | 409 | More than five photos were attached to one delivery step (reachable when the total request body stays under the route ceiling) | No — send at most five |
| `ORDER_FEEDBACK_NOT_ALLOWED` | 409 | Feedback was submitted while the order is not `DELIVERED` or `COMPLETED` | No — wait until the order is delivered |
| `ORDER_FEEDBACK_ALREADY_SENT` | 409 | A second feedback was submitted for the same order | No — one feedback per order |

### Why the photo codes mirror the product codes

The three photo codes carry the same HTTP statuses as module 04's `PRODUCT_IMAGE_*` codes
(type 400, size 413, count 409), so a client handles an upload refusal the same way everywhere, while
the `ORDER_*` prefix names the resource the client is actually touching (an order, not a product).

### Why the feedback refusals are conflicts and not validation failures

Each is a well-formed request the order's current state (or its existing feedback) cannot satisfy, and
each leads to a different next step — wait until the order is delivered, or stop (you already sent one).
That is the same reasoning `009` and `010` used for their conflicts.

## Codes deliberately reused

| Situation | Code | Why not a new code |
|---|---|---|
| Dispatching an order that is not `PAID`, delivering one that is not `SHIPPING`, or confirming receipt of one that is not `DELIVERED` | `ORDER_STATE_TRANSITION_INVALID` (409), naming the current state | It is a transition and the state is what makes it invalid — the same situation the project already names |
| The order does not exist, or is another customer's | `ORDER_NOT_FOUND` (404) | The route never confirms another customer's order |
| A missing or empty `content`, or a wrong multipart part | `VALIDATION_ERROR` (400), `details[].field` names the member | Request shape |
| The request body does not parse, or carries an unknown field | `MALFORMED_REQUEST` (400) | The shared decoder owns this |
| No token, or an expired one | `UNAUTHENTICATED` (401) | The foundation owns session validity |
| A customer's session reaches an administrator route | `FORBIDDEN` (403) | Role-based; the foundation established the code |
| Something unexpected failed | `INTERNAL_ERROR` (500) | Never describe an internal failure in order terms |
| Too many requests | `RATE_LIMITED` (429) with `Retry-After` | The shared limiter answers this |

## Codes carried over from `010` unchanged

All of `010`'s remain in use: `ORDER_NOT_FOUND`, `ORDER_CART_EMPTY`, `ORDER_ITEM_NOT_PURCHASABLE`,
`ORDER_ITEM_PRICE_CHANGED`, `ORDER_QUANTITY_EXCEEDS_AVAILABLE`, `ORDER_NO_ADDRESS`,
`ORDER_STATE_TRANSITION_INVALID`, `ORDER_NOT_TRANSFERABLE`, `ORDER_TRANSFER_TARGET_NOT_FOUND`,
`ORDER_NOT_EDITABLE`, `ORDER_EMPTY`.

## Situations that deliberately have no code

| Situation | Why there is no code |
|---|---|
| A delivered order passes its five-day window without the customer confirming | It is not an error: the order completes itself, which the customer sees on the order, not as a failed request |
| The customer confirms receipt at the same moment the sweep completes the order | It is not an error: the second act finds the order already completed; the order simply shows `COMPLETED` |
| A photo upload succeeds at the media provider but the order write fails | It is not a client error: the request fails as it would have, and the orphaned asset is an accepted cost (the product module's rule) |
| The `SHIPPED` → `SHIPPING` rename for an existing order | It is not an error: the migration backfills the value; a client reading `SHIPPING` sees the same order |
| An order rests at `SHIPPING`/`DELIVERED` because a delivery failed | It is not an error but a **deferred** capability: there is no failed/returned state yet (`deferred.md`) |
