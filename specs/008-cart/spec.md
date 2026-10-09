# Feature Specification: Shopping Cart

**Feature Branch**: `008-cart`

**Created**: 2026-10-09

**Status**: Draft

**Input**: User description: "ok tiến hành cho v1" — module 06 Cart (`docs/modules/06-cart.md`), module đầu tiên của V1.0: cho khách tập hợp sản phẩm muốn mua trước khi thanh toán.

## Clarifications

### Session 2026-10-09

- Q: Khi khách **xem giỏ** (đọc), giỏ có đối chiếu lại tồn kho/trạng thái bán của từng dòng
  không, và dòng "thiếu một phần" (sản phẩm còn bán nhưng tồn hiện tại < số lượng trong dòng)
  hiển thị thế nào → A: **Đối chiếu lại mỗi lần đọc.** Mỗi dòng mang cờ **"còn mua được"** (sản
  phẩm đang bán **và** tồn khả dụng ≥ số lượng của dòng); khi dòng không mua được vì **còn bán
  nhưng thiếu** (tồn < số lượng, kể cả 0), giỏ kèm thêm **tồn khả dụng hiện tại** để khách giảm
  xuống; khi không mua được vì sản phẩm **ngừng bán hoặc đã xoá**, giỏ chỉ báo "không còn bán",
  không kèm số. Lý do: `FR-012` vốn đã yêu cầu báo dòng không mua được thay vì âm thầm bỏ, và cho
  khách biết hạn mức mới tốt hơn để họ tự chỉnh trước khi tới checkout. Đánh đổi được chấp nhận:
  đọc giỏ phải đọc tồn kho/trạng thái cho mọi dòng (hình dạng hợp đồng và cách gọi thuộc plan);
  đổi lại khách không bị chặn ở bước sau mà không hiểu vì sao.

## Scope boundary

`docs/modules/06-cart.md` gives the MVP scope. This feature delivers the cart itself and the
checks that keep it honest; it deliberately does **not** carry what the module defers, and it rides
on two modules already delivered.

- **Deliverable**: build a cart — add, change and remove lines — see an accurate subtotal, and have
  the cart refuse anything that cannot be bought: a product that is not on sale, or a quantity
  beyond what is available. One cart per signed-in customer.
- **Deferred (module doc)**: a cart for a customer who is not signed in, saved/later carts, discount
  codes, and upsell. Each is its own feature when a need is agreed.
- **Rides on two delivered modules**: the product module (`04`) says whether a product is on sale and
  what it costs; the inventory module (`05`) says what is available. The cart reads both facts but
  owns neither, and how those facts cross a module boundary is a plan concern (Assumptions).
- **The cart does not hold stock.** Holding stock is the inventory module's job and happens when a
  customer **begins paying** — settled by the inventory feature. The cart only *checks* what is
  available, and the checkout re-checks before an order is created. A cart line is therefore not a
  promise; nothing is reserved by putting something in a cart.
- **Price honesty is settled**: the unit price is captured when the product is added (the "snapshot"
  the module document names) and shown to the customer; the checkout re-checks the price before
  taking money, so a change between adding and paying is caught there, not silently charged here.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - A customer builds a cart (Priority: P1)

A signed-in customer opens a product, adds a few units to the cart, changes how many of one item
they want, removes an item they no longer want, and sees the running subtotal update each time.

**Why this priority**: This is the only part of the feature a customer uses, and the reason a cart
exists. It is deliverable on its own: a cart that the customer cannot add to is not a cart.

**Independent Test**: Add two products with quantities, change one quantity, remove one item, and
read the cart after each step, confirming the lines and the subtotal are what the actions imply.
Delivers value on its own.

**Acceptance Scenarios**:

1. **Given** a signed-in customer and a product on sale with stock, **When** they add N units,
   **Then** the cart contains one line for that product with quantity N and the unit price at the
   time it was added, and the subtotal rises by that price times N.
2. **Given** a cart, **When** the customer adds a product already in it, **Then** its quantity rises
   rather than a second line appearing for the same product.
3. **Given** a cart with a line, **When** the customer changes its quantity, **Then** the line and
   the subtotal reflect the new quantity.
4. **Given** a cart with a line, **When** the customer removes it, **Then** the line is gone and the
   subtotal falls by the amount it contributed.
5. **Given** a customer with no cart or an empty one, **When** they read it, **Then** they receive an
   empty cart rather than an error.
6. **Given** a cart, **When** the customer reads it, **Then** each line carries the product, the
   quantity, the unit price captured when it was added, the line total, and the cart's subtotal.

---

### User Story 2 - The cart refuses what cannot be bought (Priority: P1)

A customer tries to add a product that is no longer on sale, or more units than the shop has, and the
cart refuses and says which product and which limit failed — so the customer never fills a cart with
something that cannot be checked out.

**Why this priority**: Equally P1, because a cart that accepted an unbuyable line would only move the
refusal later, at checkout, after the customer has invested effort. This is the cart's whole job
beyond listing.

**Independent Test**: Attempt to add a product that is not on sale, and a quantity beyond the
available stock; confirm each is refused with the product named and nothing added, then add a valid
quantity and confirm it succeeds.

**Acceptance Scenarios**:

1. **Given** a product that is not on sale (announced, out of stock, or retired), **When** a customer
   tries to add it, **Then** it is refused and nothing is added.
2. **Given** a product with three available, **When** the customer tries to add five, **Then** it is
   refused, the available amount is named, and the cart is unchanged.
3. **Given** a line for a product whose available quantity later falls, **When** the customer raises
   the line's quantity beyond what is now available, **Then** the change is refused and the previous
   quantity is kept.
4. **Given** a product with three available, **When** the customer adds exactly three, **Then** it
   succeeds — the whole available amount is reachable, only more than it is not.
5. **Given** a product that does not exist or was removed, **When** a customer tries to add it,
   **Then** it is refused as not found, the same `PRODUCT_NOT_FOUND` an unknown product answers.

---

### User Story 3 - The cart is the customer's own, and singular (Priority: P2)

Each signed-in customer has exactly one cart, and no customer can read or change another's.

**Why this priority**: P2 because the feature works for one customer without it, but a shop with more
than one customer cannot be correct without it, and the project treats cross-account access as a
defect rather than a nicety.

**Independent Test**: With two customers, fill one cart and confirm the other's cart is untouched and
that neither can reach the other's cart by any request.

**Acceptance Scenarios**:

1. **Given** a signed-in customer, **When** they add products across several sessions, **Then** all
   of them end up in the same single cart.
2. **Given** two customers, **When** each builds a cart, **Then** neither cart contains the other's
   products and neither can read or change the other's.
3. **Given** no signed-in session, **When** anyone tries to read or change a cart, **Then** it is
   refused on authentication grounds.

### Edge Cases

- **A quantity that is zero, negative, or fractional.** Adding or setting a non-positive or
  non-whole quantity is refused, naming the quantity; a customer empties a line by removing it, not
  by setting zero.
- **Adding more than is available.** It is refused with the product named and the cart unchanged; a
  quantity exactly equal to what is available is allowed.
- **A product added while on sale, then taken off sale or sold out before checkout.** The line is not
  silently dropped; the product's current availability is what the customer is told, and the checkout
  re-checks before an order is made, so an order is never created from a line that cannot be bought.
- **A line whose quantity now exceeds what is available, while the product is still on sale.** The
  cart reports the current available quantity for that line and marks it unbuyable as it stands, so
  the customer can reduce it; the quantity is never silently changed for them.
- **A product removed from the catalogue after being added.** The line can no longer be bought; the
  cart reports it as no longer available rather than presenting a product that does not exist.
- **Two updates to the same line arriving at once.** Both are applied against the current quantity,
  and neither can leave the line holding more than is available.
- **The same product added on two devices at once.** It converges to one line whose quantity is the
  sum the customer asked for, never two lines and never a quantity above what is available.
- **A very large but valid quantity.** Where it does not exceed availability, it is accepted and the
  subtotal is exact; money is never rounded.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: A signed-in customer MUST have exactly one cart; products they add across sessions all
  land in that one cart.
- **FR-002**: System MUST let a customer add a product with a positive whole quantity to their cart.
  Adding a product already in the cart MUST raise its quantity rather than create a second line for
  the same product.
- **FR-003**: System MUST let a customer change a line's quantity and remove a line entirely.
- **FR-004**: System MUST let a customer read their cart, returning each line's product identifier,
  the product's current name and slug (absent when the product is gone, so the customer can see and
  reach it), the quantity, the unit price captured when it was added, the line total, and the cart
  subtotal, plus whether each line can currently be bought (FR-012). An empty cart MUST answer an
  empty cart, with no subtotal, rather than an error.
- **FR-005**: A product MUST be addable, and a line MUST be changeable, only while the product is on
  sale. A product that is announced but not yet on sale, out of stock, retired, or removed MUST NOT
  be addable or buyable from the cart.
- **FR-006**: A line's quantity MUST be a positive whole number; zero, negative or fractional
  quantities MUST be refused naming the field. Emptying a line is done by removing it.
- **FR-007**: A line's quantity MUST NOT exceed the product's currently available quantity; a request
  that would exceed it MUST be refused, naming the quantity and the currently available amount, and
  leave the cart unchanged. The product is the one the request names, so it is identified by the
  request itself.
- **FR-008**: The unit price of a line MUST be captured when the product is added and MUST be what
  the customer is shown for that line, so the cart total is stable while the customer shops. The
  checkout re-checks the price before money is taken, so a price that changed after adding is caught
  there — the cart MUST NOT be the place where a stale price is silently charged.
- **FR-009**: The subtotal MUST equal the sum of each line's quantity times its captured unit price,
  computed in the currency's minor unit; money MUST NOT be rounded or approximated. The cart's
  currency is that of the products in it, and the MVP shop is single-currency.
- **FR-010**: Every read or change of a cart MUST be refused unless the caller is signed in, and a
  customer MUST only ever see and change their own cart; the identity of the owner MUST come from the
  session, never from the request.
- **FR-011**: The cart MUST NOT reserve or hold stock. It checks availability; the checkout re-checks
  availability before an order is created, so an order is never created from a line that has become
  unbuyable.
- **FR-012**: Reading the cart MUST re-check every line against the product's current sell state and
  available quantity, so the cart reflects reality rather than the moment a line was added. Each
  line MUST state whether it can be bought as it stands — the product is on sale and at least its
  quantity is available. A line that cannot be bought because the product is still on sale but fewer
  units are available than the line asks for MUST report the currently available quantity, so the
  customer can reduce the line to a buyable amount; a line that cannot be bought because the product
  is off sale or removed MUST report that it is no longer available, without a quantity. A line is
  never silently removed and its quantity is never silently changed.

### Key Entities

- **Cart**: the one working basket of a signed-in customer. It belongs to exactly one account and is
  identified by that ownership; it carries lines and is the source of the subtotal.
- **Cart line**: one product the customer intends to buy, with the quantity they chose and the unit
  price captured when it was added. A product appears at most once as a line in a cart; adding it
  again raises the existing line. Its price is a record of what the customer was shown, not a
  promise, and the checkout re-checks it.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A customer can add, change and remove items and see an accurate subtotal, with the
  lines persisting across sessions and the subtotal equal to the sum of quantity times captured price
  — verified by automated test rather than by inspection.
- **SC-002**: No cart can ever hold a quantity above the product's available stock, including when
  two updates arrive at once; an attempt is refused, names the product, and leaves the cart
  unchanged. Verified by automated test, including a concurrent case.
- **SC-003**: A product that is not on sale, or removed, can never be added; a line whose product
  later becomes unbuyable is reported as unbuyable rather than silently kept or dropped, and a line
  that is still on sale but short of what is available is reported as unbuyable **with the current
  available quantity**, never silently reduced.
- **SC-004**: No customer can read or change another customer's cart through any route, and a request
  without a session is refused; verified by a test that attempts cross-account access.
- **SC-005**: A money total shown to a customer is exactly the sum its lines imply, with no rounding,
  and the price shown for a line is the price captured when the product was added.

## Assumptions

- **Signing in is required to use a cart**, as `docs/modules/06-cart.md` decides; a cart for a guest
  who has not signed in stays deferred. The checkout, and therefore the cart, is for signed-in
  customers.
- **One active cart per account.** The module document models `carts` as belonging to a user; a
  customer who abandons a cart and returns continues with the same one, since a second active cart
  would make "the cart" ambiguous.
- **The price is captured when a product is added (the module document's snapshot), and the checkout
  re-checks it.** The inventory feature's Clarifications settled the checkout flow; this feature
  stores and shows the captured price and leaves the re-check to the checkout, so the cart never
  silently charges a price the customer did not see.
- **The cart checks availability but does not hold it.** Stock is held by the inventory module when a
  customer begins paying, on a short, expiring hold; a cart is not a reservation, so the available
  quantity can legitimately change between adding to a cart and paying. This is why the checkout
  re-checks rather than trusting the cart.
- **The two facts the cart needs — whether a product is on sale and its price, and what is available
  — are read across a module boundary.** The product module owns the first; the inventory module owns
  the second. The cart may not read either module's tables, so how those facts are reached (the
  contract shape) belongs to the plan, and any contract this introduces follows the project's
  cross-module rule. This feature adds a **new availability read**; the **reservation** contract
  module 05 left unpublished until the order flow exists (its `deferred.md` D1) is a different
  contract and stays open — this feature neither uses nor closes it.
- **The MVP shop is single-currency.** Prices carry an explicit currency as the constitution
  requires, but the cart does not perform currency conversion and all products in one cart share a
  currency, so a cart never mixes currencies. An empty cart has no line to take a currency from and no
  money to express, so its `subtotal` is **absent** (null) rather than a zero in a currency the shop
  would have to invent; a client reads a missing subtotal as nothing owed.
- **No discount codes, upsell, or saved carts**, as the module document defers them; each is a
  separate feature.
- **No dedicated rate limit is added for this module.** The shared request limit applies. The cart is
  for signed-in customers and is not the abuse surface a module-specific limit exists to protect;
  revisit if it becomes one.
- **The foundation is reused as-is**: the session/role guard, the response envelope, the pagination
  convention if a list is ever needed, and the error catalogue already exist. This feature adds no new
  infrastructure.
