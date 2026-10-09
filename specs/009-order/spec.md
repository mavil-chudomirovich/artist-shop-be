# Feature Specification: Order Checkout

**Feature Branch**: `009-order`

**Created**: 2026-10-09

**Status**: Draft

**Input**: User description: "làm module 07 Order" — module 07 Order (`docs/modules/07-order.md`), module thứ tư của V1.0: biến giỏ hàng thành đơn hàng, quản lý vòng đời đơn và tính tiền chính xác.

## Clarifications

### Session 2026-10-09

- Q: Ai được quyền chuyển nhượng một đơn **đã thanh toán** sang tài khoản khác → A: **Chỉ admin/operator**
  (không cho khách tự chuyển). Lý do: khớp US5 và mô hình quản trị admin-only của module; khách tự
  chuyển (kiểu tặng) là mở rộng cần luật riêng (chống lạm dụng, thông báo hai bên). Đánh đổi được chấp
  nhận: khách muốn đổi chủ phải qua operator.
- Q: Checkout lấy địa chỉ giao thế nào → A: **Nhận mã địa chỉ khách chọn**; nếu bỏ trống thì dùng **địa
  chỉ mặc định**; khách **chưa có địa chỉ** thì **từ chối**. Lý do: cho khách chọn địa chỉ (không đoán),
  tận dụng danh sách địa chỉ + địa chỉ mặc định đã có ở module 02. Đánh đổi được chấp nhận: khách phải
  có ít nhất một địa chỉ trước khi đặt hàng.
- Q: Trạng thái thanh toán có phải một trục riêng trên đơn ngay từ MVP không → A: **Không (gộp)** — đơn
  chỉ có **một trục trạng thái**; `paid` nghĩa là đã thanh toán. Module 08 (Payment) sẽ giữ trạng thái
  thanh toán riêng trong bảng của nó. Lý do: chưa có module 08, thêm trục bây giờ là dựng trước data
  model (Constitution VII, YAGNI), mà trạng thái đơn đã phản ánh đúng. Đánh đổi được chấp nhận: khi 08
  ra đời, việc liên kết đơn ↔ bản ghi thanh toán là việc của 08, không phải thêm cột ở đây.
- Q: Chuyển nhượng định danh tài khoản nhận bằng gì → A: **Bằng email** của tài khoản nhận. Operator nhập
  email; nếu không tài khoản nào mang email đó thì từ chối. Lý do: operator tự nhiên biết email khách
  hơn là mã tài khoản. Đánh đổi được chấp nhận: cần một đường tra email→tài khoản (việc của plan), và dựa
  vào email là duy nhất — điều module 01 đã bảo đảm.

## Scope boundary

`docs/modules/07-order.md` gives the MVP scope. This feature delivers the order itself and the short
hold that keeps its goods while the customer pays; it deliberately does **not** carry what the module
defers, and it pays three debts earlier features left it.

- **Deliverable**:
  1. **Checkout** — a signed-in customer turns their cart into an order; the order is created in one
     operation with a **snapshot** of each line's product as it was at that moment and of the delivery
     address, the cart is emptied, and the goods are **held** for the customer while they pay.
  2. **The order's selling life** — a small, explicit set of states the order moves through, refusing a
     move that makes no sense.
  3. **An unpaid order expires** — if the customer does not pay within the hold window, the order is
     cancelled and the goods go back, by itself.
  4. **A customer's own orders** — list and read the orders they placed, and cancel one that is still
     unpaid.
  5. **The operator's order desk** — an administrator lists and reads every order and advances its
     fulfilment (ship, complete), each act audited.
  6. **Transferring a started order** — a paid order can be handed to another existing account without
     changing what is in stock.
- **Deferred**: the payment **provider** and the confirmation that turns "awaiting payment" into
  "paid" is module 08 Payment (PayOS) — this feature leaves that transition for module 08 to drive;
  shipping fee and tracking is module 09; refunds, splitting an order, and discount codes are out of
  MVP.
- **Money and shipping, settled by ADR 015 §5**: an order carries only the **goods** total (the sum of
  its line snapshots); there is **no shipping fee** in the order — the customer pays shipping on
  delivery. Money is integer minor units with an explicit currency.
- **Three debts paid here**:
  - Feature 007's `deferred.md` **D1** — the **reservation contract** module 05 withheld until the
    order flow exists. This feature is that consumer: checkout **holds** the goods, paying turns the
    hold into a sale, and cancelling or expiring returns it.
  - Feature 006's and 007's `deferred.md` **D3** — what an order line does when its product is
    removed. This feature **snapshots** the product into the line, so no order ever points at a
    product that is gone.
  - Feature 007's `deferred.md` **D2** — a paid order is never cancelled; it is **transferred**, and a
    transfer changes no stock.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - A customer checks out (Priority: P1)

A signed-in customer with a cart of buyable items presses "order"; the shop records what they are
buying at that moment, holds those goods for them, and shows them an order to pay. An empty or
unbuyable cart is refused rather than turned into a broken order.

**Why this priority**: This is the reason the order module exists and the only way a customer reaches
payment; without it nothing can be paid or shipped. It is deliverable on its own.

**Independent Test**: With a cart holding two buyable products, check out, and confirm an order was
created carrying a snapshot of both lines and the delivery address, that the goods are held (their
available quantity fell without the physical stock moving), and that the cart is now empty.

**Acceptance Scenarios**:

1. **Given** a signed-in customer with a cart of buyable items and a delivery address, **When** they
   check out, **Then** an order is created in "awaiting payment" carrying a snapshot of the name,
   price and currency of each line and of the delivery address, the goods are held for them, and the
   cart is emptied.
2. **Given** a cart with nothing in it, **When** the customer checks out, **Then** it is refused with
   an answer that says the cart is empty, and no order is created.
3. **Given** a cart holding something that has become unbuyable — sold out, taken off sale or removed
   — **When** the customer checks out, **Then** it is refused, the offending item is named, no order
   is created, and the cart is left as it was.
4. **Given** a line whose product's price changed after it was added, **When** the customer checks
   out, **Then** the checkout **refuses** and names the line, because the price the customer was shown
   is no longer the price; nothing is created, and a price they did not see is never charged.
5. **Given** a cart with more of a product than is available, **When** the customer checks out, **Then**
   it is refused and the available amount is named.
6. **Given** a customer who reads their orders after checking out, **Then** the new order appears with
   its lines, totals and "awaiting payment" state.
7. **Given** a customer with no delivery address, **When** they check out, **Then** it is refused,
   because an order has nowhere to go.

---

### User Story 2 - The order's selling life can be trusted (Priority: P1)

An order moves through a small, defined set of states — awaiting payment, paid, shipped, completed —
and a retired order is not reached again from completed; a move the rules forbid is refused and names
where the order is. An order left unpaid past its window cancels itself and returns the goods.

**Why this priority**: Equally P1, because the states are the product: the shop's promise to ship what
was paid for rests on them being enforced, and the hold must end by itself. This is a stated
completion criterion of the module.

**Independent Test**: Drive one order through every allowed move from "awaiting payment" to
"completed" and confirm each succeeds, then attempt a move the rules forbid and confirm it is refused
with the current state named and the order untouched; separately let an unpaid order pass its window
and confirm it cancels itself and its goods are available again.

**Acceptance Scenarios**:

1. **Given** an order awaiting payment, **When** the payment is confirmed, **Then** it becomes paid and
   the goods held for it become sold.
2. **Given** a paid order, **When** the operator marks it shipped and then completed, **Then** each
   succeeds and is recorded.
3. **Given** an order in any state, **When** a move the rules do not allow is attempted — completing an
   unpaid order, moving a completed order anywhere, shipping a cancelled one — **Then** it is refused
   and the response names the state that made the move invalid.
4. **Given** an unpaid order whose hold window passes, **When** nothing else happens, **Then** the
   order becomes cancelled by itself and its goods return to availability, each exactly once.
5. **Given** an order awaiting payment, **When** the customer cancels it, **Then** it becomes
   cancelled and its goods return to availability.
6. **Given** a paid order, **When** anyone attempts to cancel it, **Then** it is refused — a paid order
   is transferred instead, never cancelled (User Story 5).

---

### User Story 3 - A customer sees and manages only their own orders (Priority: P2)

A customer lists and reads the orders they placed, and no customer can see or change another's.

**Why this priority**: P2 because the feature works for one customer without it, but a shop with more
than one customer cannot be correct without it, and the project treats cross-account access as a defect.

**Independent Test**: With two customers, place an order for one and confirm the other's list does not
contain it and that neither can read or change the other's order by any request.

**Acceptance Scenarios**:

1. **Given** a signed-in customer, **When** they list their orders, **Then** they see only the orders
   they placed, most recent first.
2. **Given** two customers, **When** each lists orders, **Then** neither sees the other's, and reading
   another's order by its identifier answers not-found.
3. **Given** no session, **When** anyone reads or changes an order, **Then** it is refused on
   authentication grounds.

---

### User Story 4 - The operator runs the order desk (Priority: P2)

An administrator lists every order, reads one in full, and advances its fulfilment — marks it shipped,
then completed — with each act recorded.

**Why this priority**: P2 because a customer can place and pay for an order without it, but nothing
would ever be marked shipped or completed, so the shop could not be run.

**Independent Test**: As an administrator, list orders, open one, mark it shipped and then completed,
and confirm the state changes and each act left an audit entry; confirm a customer's session is
refused.

**Acceptance Scenarios**:

1. **Given** an administrator, **When** they list orders, **Then** every order appears, newest first,
   with its customer, state and total.
2. **Given** a paid order, **When** the administrator marks it shipped and completed, **Then** each
   succeeds and is audited.
3. **Given** a customer's session, **When** they attempt any administrator order action, **Then** it is
   refused on role grounds.

---

### User Story 5 - A paid order can be handed to another account (Priority: P3)

An operator transfers a paid order to another existing account, so a purchase can change owner without
the goods changing hands and without touching stock.

**Why this priority**: P3 because the shop sells without it, and it exists only because the system
deliberately has no "cancel a paid order" — a transfer is what replaces it.

**Independent Test**: Transfer a paid order to a second account and confirm the order now belongs to
the second account, its lines and state are unchanged, and no stock moved.

**Acceptance Scenarios**:

1. **Given** a paid order and a second existing account, **When** an administrator transfers the order,
   **Then** the order belongs to the second account and its contents and state are unchanged.
2. **Given** a transfer, **When** it is done, **Then** no product's available or physical stock changes.
3. **Given** an email no account carries, **When** the order is transferred to it, **Then** it is
   refused.
4. **Given** an order that is not yet paid, **When** a transfer is attempted, **Then** it is refused —
   only a started (paid) order is transferred.

### Edge Cases

- **Checking out the same cart twice at once.** Exactly one order is created; the second attempt is
  refused because the cart is now empty, never producing two orders from one cart.
- **A product added to the cart then removed from the catalogue before checkout.** The checkout refuses
  and names it; an order is never created from a line that cannot be bought.
- **A price that changes between adding to the cart and checking out.** The checkout refuses and names
  the line, because the price the customer was shown is no longer the price; the customer reviews the
  cart (which re-checks the price) and checks out again. A price the customer did not see is never
  charged.
- **A product that sells out between checkout attempts.** The checkout that finds it short is refused
  and names the available amount; nothing is held for it.
- **The hold window passing the instant the customer pays.** Exactly one outcome: the order is either
  paid (and the goods sold) or expired (and the goods returned), never both, and never twice.
- **An order left unpaid a long time.** It does not sit forever: it cancels itself once its window
  passes, so a cart of held goods cannot be kept from other customers indefinitely.
- **An order whose customer's account is later disabled.** The order and its history survive; the shop
  does not lose what was sold.
- **A product removed after an order used it.** The order is unaffected: its lines carry a snapshot,
  so nothing an order line shows depends on a product still existing.
- **Marking a completed order shipped, or a cancelled order paid.** Refused, naming the state.
- **An operator advancing an order twice quickly.** The second move is refused because the first left
  the order in a state the second does not allow; the order never double-applies.

## Requirements *(mandatory)*

### Functional Requirements — checkout

- **FR-001**: System MUST let a signed-in customer turn their cart into an order in a single operation,
  with no order ever created from a partly-recorded cart.
- **FR-002**: An order MUST carry, for each cart line, a **snapshot** of the product as it was at
  checkout — its name, link segment, unit price and currency — and the quantity, so nothing an order
  shows depends on the product still existing or on a later price change.
- **FR-003**: An order MUST record the **delivery address** as a snapshot at checkout, taken from the
  address identifier the customer names, or from the customer's **default address** when none is named,
  so a later edit to the address does not change a placed order. A customer with **no** address MUST be
  refused, since an order has nowhere to go.
- **FR-004**: Checkout MUST re-read the current state and price of every cart line and **refuse** the
  whole checkout if any line is no longer buyable — sold out, off sale or removed — **or its price has
  changed** since the customer was shown it, naming the offending item; nothing is created. The
  customer reviews the cart and checks out again, so no price they did not see is ever charged (the
  cart feature's Clarifications settled this).
- **FR-005**: Checkout MUST re-read what is available for every line and refuse the whole checkout if a
  line asks for more than is available, naming the available amount; nothing is created.
- **FR-006**: An empty cart MUST be refused at checkout with an answer that says the cart is empty, and
  no order is created.
- **FR-007**: On a successful checkout, the customer's cart MUST be emptied and MUST NOT be reusable to
  create a second order for the same contents.
- **FR-008**: The order total MUST equal the sum of its line snapshots (quantity times the snapshotted
  unit price) in the currency's minor unit, and this feature MUST NOT add a shipping fee to the order
  (ADR 015 §5). Money MUST NOT be rounded.

### Functional Requirements — the order's selling life

- **FR-009**: An order's state MUST be one of: awaiting payment, paid, shipped, completed, cancelled.
  The allowed changes MUST be exactly: awaiting payment → paid; paid → shipped; shipped → completed;
  awaiting payment → cancelled (by the customer or by expiry); and nothing else. Every move MUST be
  covered by tests, one positive and one negative per move.
- **FR-010**: The state MUST change only through the defined transition rules; writing a state directly,
  bypassing them, MUST NOT be possible.
- **FR-011**: An invalid move MUST be refused with an error naming the current state, and the order
  MUST be left as it was.
- **FR-012**: An order awaiting payment MUST stop being payable once its hold window passes: it MUST
  become cancelled by itself and its goods MUST return to availability, applied **exactly once**, so a
  shop's held goods cannot be withheld indefinitely and a replay cannot free them twice.

### Functional Requirements — the goods are held while the customer pays

- **FR-013**: Checkout MUST **hold** the ordered quantity of every line for the order, so the goods are
  set aside for that customer and no one else can take them while the customer pays; the physical
  stock MUST NOT move at checkout, only what is available to others.
- **FR-014**: Confirming payment for an order MUST turn its hold into a **sale**: the physical stock
  falls by the held quantity and the hold closes, and this MUST happen **exactly once** even if the
  confirmation arrives twice.
- **FR-015**: Cancelling an order that is still awaiting payment, and an expired order, MUST **return**
  the held quantity to availability with no change to physical stock, exactly once.
- **FR-016**: The hold's window and the way it expires MUST be the inventory module's; this feature
  consumes that capability rather than defining its own, so there is one place that decides when a hold
  is over.
- **FR-017**: An order MUST NOT be created if the goods cannot be held — for example when another
  customer's hold has taken the last unit — and the refusal MUST name the item that could not be held.

### Functional Requirements — a customer's own orders

- **FR-018**: System MUST let a customer list their own orders, most recent first, and read one in full.
- **FR-019**: System MUST let a customer cancel an order that is **still awaiting payment**, returning
  its goods; a **paid** order MUST NOT be cancellable (it is transferred instead, FR-024).
- **FR-020**: Every read or change of an order MUST be refused unless the caller is signed in, and a
  customer MUST only ever see and change their own orders; the owner MUST come from the session, never
  the request.

### Functional Requirements — the operator's desk

- **FR-021**: System MUST let an administrator list every order and read one in full, including its
  customer, its lines and its state.
- **FR-022**: System MUST let an administrator move an order through its fulfilment — mark it shipped,
  then completed — through the transition rules, refusing a move the current state does not allow.
- **FR-023**: Every administrator action on an order MUST be refused unless the caller holds the
  administrator role, and MUST be written to the audit trail, naming the order and the administrator.

### Functional Requirements — transfer

- **FR-024**: System MUST let an administrator transfer a **paid** order to another **existing**
  account, named by its **email**, changing only who owns the order; the order's lines, state and totals
  MUST be unchanged, and no product's physical or available stock MUST change. An unpaid order MUST NOT
  be transferable, and an email no account carries MUST be refused.

### Key Entities

- **Order**: a purchase a customer has committed to: the account that owns it, the delivery address as
  captured at checkout, the total in the currency's minor unit, where it is in its life, and the moment
  it was placed and last changed. Its identifier is stable; who owns it can change by transfer.
- **Order line**: one item of an order, holding a **snapshot** of the product as it was at checkout —
  its name, link segment, unit price and currency — and the quantity. It does not point at the product,
  so it survives the product's removal and never changes when the product does.
- **Order state**: where the order is in its life — awaiting payment, paid, shipped, completed,
  cancelled. Paid is reached only by a confirmed payment; cancelled is reached by the customer or by
  expiry; completed is terminal.
- **Hold**: the goods a live order has set aside while the customer pays, owned by the inventory module
  and identified by the order. It is what makes checkout reserve rather than promise.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A customer with a cart can place an order in a single action, and afterwards sees it with
  its lines, its address and its total equal to the sum of its line snapshots — verified by automated
  test rather than by inspection.
- **SC-002**: An order is never created from an empty or unbuyable cart, from a line above what is
  available, or when the goods cannot be held; each refusal names what failed and leaves the cart
  unchanged. Verified by automated test, including two checkouts of one cart at once producing exactly
  one order.
- **SC-003**: Every illegal state move is refused, naming the current state, and leaves the order
  unchanged; every legal move succeeds. Verified by automated test, one positive and one negative per
  move.
- **SC-004**: An unpaid order past its window cancels itself and its goods are available again, applied
  exactly once; a confirmed payment turns the hold into a sale exactly once even when replayed.
- **SC-005**: No customer can read or change another customer's order through any route; a customer's
  session is refused every administrator action; and each successful administrator action leaves an
  audit entry. Verified by tests that attempt cross-account and cross-role access.
- **SC-006**: A paid order an operator transfers to another account changes owner with its lines, state
  and totals unchanged and no stock moved.

## Assumptions

- **Signing in is required to order**, as the cart requires a session; there is no order for a guest.
- **An order is created when the customer begins to pay**, so "checkout" and "the payment step" are the
  same moment: the order is created awaiting payment and its goods are held for the hold window the
  inventory feature settled (fifteen minutes). This is the moment the customer pressed "order" — the
  hold the inventory feature's Clarifications describe.
- **The goods are held, not promised, and the hold is the inventory module's.** This feature consumes
  the reservation capability module 05 delivered and tested; it neither re-implements holding nor
  stores a quantity of its own, so when module 05 decides a hold is over, there is one answer.
- **The paid transition is driven by the payment module (08), which does not exist yet.** This feature
  defines the transition and the sale it produces (FR-014) and tests it directly, but nothing yet
  confirms a real payment; module 08 is where that confirmation and the PayOS provider live. Until
  then, the paid move is reachable only in tests, exactly as feature 006 left the sell state to 007.
- **The order has a single state axis.** `paid` means a payment was confirmed, and there is no separate
  payment-status field; module 08 owns the payment record and its own status, and linking it to the
  order is module 08's concern (clarification, Session 2026-10-09).
- **Money is integer minor units with an explicit currency**, carried per line; the MVP shop is
  single-currency. Money is never a float.
- **Shipping is not this feature's and is not priced online.** The customer pays shipping on delivery
  (ADR 015 §5), so the order total is goods only, and shipping fee and tracking belong to module 09.
- **A paid order is never cancelled; it is transferred.** The system has no refund flow (ADR 015 §5),
  and a transfer changes no stock (feature 007's `deferred.md` D2). A refund flow, if one is ever
  wanted, is a separate feature.
- **An order line snapshots its product**, so an order never points at a product that is gone and is
  unaffected by a product's removal, which module 04 may do. This resolves the decision features 006
  and 007 left to this module (`deferred.md` D3 in both).
- **Orders are paginated** for both the customer's list and the administrator's, following the
  project's existing list convention.
- **No dedicated rate limit is added for this module.** The shared request limit applies; checkout and
  cancellation are customer actions behind a session, and the operator's endpoints are
  administrator-only, so the abuse surface a module-specific limit exists to protect does not exist
  here.
- **The foundation is reused as-is**: the session/role guards, the audit writer, the response envelope,
  the pagination metadata and the error catalogue already exist. This feature adds no new
  infrastructure.
