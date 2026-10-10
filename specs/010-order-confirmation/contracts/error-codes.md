# Contract: Error codes — Order confirmation & editing

Authoritative for what this feature can answer with. `contracts/openapi.yaml` describes where each
appears; this file records what each means and which situations deliberately reuse a shared code.

## Codes this feature adds

| Code | HTTP | Meaning | Retryable |
|---|---|---|---|
| `ORDER_NOT_EDITABLE` | 409 | The order is not in a state the customer may change — it is paid or beyond. Only an order awaiting the artist or awaiting payment can be edited | No |
| `ORDER_EMPTY` | 409 | The edit would leave the order with no lines. An order always carries at least one line; the customer cancels instead | No — keep at least one line |

### Why these are conflicts and not validation failures

Each is a well-formed request the order's current state cannot satisfy — the order is already paid, or
the edit removes everything. Each leads to a different next step (stop editing, or cancel the order), so
they are conflicts the customer can act on, not malformed values. This is the same reasoning `009` and
the modules before it used.

## Codes deliberately reused

| Situation | Code | Why not a new code |
|---|---|---|
| Confirming or rejecting an order that is not awaiting confirmation | `ORDER_STATE_TRANSITION_INVALID` (409), naming the current state | The move is a transition and the state is what makes it invalid — the same situation `009` already named |
| Confirming an order when a line cannot be held (a competing hold took the last unit) | `ORDER_QUANTITY_EXCEEDS_AVAILABLE` (409), `details[].field = "productId"` with the available amount | It is the same shortage `009` names at checkout; a second code for "short at confirmation" would be the same fact |
| Editing a line whose product is off sale or removed | `ORDER_ITEM_NOT_PURCHASABLE` (409), `details[].field = "productId"` | Same situation as checkout |
| Editing a line whose price changed since it was snapshotted | `ORDER_ITEM_PRICE_CHANGED` (409), `details[].field = "productId"` | Same situation as checkout |
| Editing a line above what is available | `ORDER_QUANTITY_EXCEEDS_AVAILABLE` (409), with the available amount | Same situation as checkout |
| The order does not exist, or is another customer's | `ORDER_NOT_FOUND` (404) | The route never confirms another customer's order (`009`) |
| `addressId` is not a UUID, or names an address that is not the customer's | `VALIDATION_ERROR` (400), `details[].field = "addressId"` | Request shape, exactly as checkout |
| A line is missing `productId`, or a quantity is not positive | `VALIDATION_ERROR` (400), `details[].field` names the member | Request shape |
| The request body does not parse, or carries an unknown field | `MALFORMED_REQUEST` (400) | The shared decoder owns this |
| No token, or an expired one | `UNAUTHENTICATED` (401) | The foundation owns session validity |
| A customer's session reaches an administrator route | `FORBIDDEN` (403) | Role-based; the foundation established the code |
| Page, page size, `status` or `sort` outside its permitted values | `VALIDATION_ERROR` (400) | Pagination/filter validation is a foundation concern |
| Something unexpected failed | `INTERNAL_ERROR` (500) | Never describe an internal failure in order terms |
| Too many requests | `RATE_LIMITED` (429) with `Retry-After` | The shared limiter answers this |

## Codes carried over from `009` unchanged

`ORDER_NOT_FOUND`, `ORDER_CART_EMPTY`, `ORDER_ITEM_NOT_PURCHASABLE`, `ORDER_ITEM_PRICE_CHANGED`,
`ORDER_QUANTITY_EXCEEDS_AVAILABLE`, `ORDER_NO_ADDRESS`, `ORDER_STATE_TRANSITION_INVALID`,
`ORDER_NOT_TRANSFERABLE`, `ORDER_TRANSFER_TARGET_NOT_FOUND` — all still in use.

## Situations that deliberately have no code

| Situation | Why there is no code |
|---|---|
| An order awaiting the artist is left unconfirmed for a long time | It is not an error: the order holds nothing and has no deadline (research D5) |
| An awaiting-payment order passes its 60-minute window | It is not an error: the order cancels itself, which the customer sees on the order, not as a failed request |
| A payment callback arrives for an order edited since the payment attempt | Module 08 rejects it by comparing the order version; there is no HTTP surface for it here (research D7) |
| A customer edits an order to exactly its current content | It is a no-op edit; it still bumps the version and records history, but answers success, not an error |
