# Feature Specification: Hide Guard with Pending Orders

**Feature Branch**: `016-hide-guard`

**Created**: 2026-10-16

**Status**: Draft

**Input**: User description: "Chặn ẩn danh mục / sản phẩm / biến thể khi còn đơn chờ duyệt, và tự gỡ khỏi giỏ hàng. Khi ẩn, hệ thống phải kiểm tra có đơn 'đang chờ duyệt' nào chứa mặt hàng đó không — không có thì cho ẩn, có thì báo lỗi và từ chối. Nếu ẩn thành công, tự động gỡ các mặt hàng đó khỏi giỏ hàng của mọi người dùng."

## Clarifications

### Session 2026-10-16

- Q: Trạng thái đơn hàng nào được tính là "đang chờ duyệt" để chặn ẩn? → A: **PENDING và
  PAYMENT_PENDING**. PENDING là đơn chờ shop duyệt; PAYMENT_PENDING là đơn chờ khách thanh toán
  (đang giữ hàng). Các trạng thái đã thanh toán, đang giao, hoàn tất hoặc đã hủy không tính.
- Q: Hiện product không có cờ ẩn riêng (ẩn/hiện suy từ category + variant). Muốn "ẩn product" thì làm
  thế nào? → A: **Thêm cờ ẩn riêng ở product** (mặc định hiện), thay vì cascade đổi trạng thái variant.
  Lý do: đối xứng với cờ hiển thị của category, và bật lại đơn giản.
- Q: Chuyển variant sang trạng thái nào thì kích hoạt kiểm tra đơn chờ? → A: **Mọi chuyển rời trạng
  thái đang bán** — kể cả chuyển sang hết hàng do tồn tự động, và dù do operator hay hệ thống khởi
  xướng. Chỉ chuyển *về* đang bán mới không bao giờ bị kiểm tra.
- Q: Guard có áp dụng khi xoá cứng (hard delete) variant/product/category không? → A: **Không, chỉ khi
  ẩn.** Xoá giữ nguyên mọi luật hiện có của nó.

## Scope boundary

Trong phạm vi:

- Chặn ẩn category, product (cờ mới) và variant khi còn đơn PENDING hoặc PAYMENT_PENDING chứa mặt
  hàng đó, báo lỗi và từ chối.
- Tự gỡ các mặt hàng vừa bị ẩn khỏi giỏ hàng của mọi người dùng khi ẩn thành công.

Ngoài phạm vi (giữ hoãn, ghi rõ để không mất):

- Đổi luồng checkout/order khác.
- Thay đổi chính sách tồn kho.
- Thông báo cho khách khi giỏ của họ bị dọn.
- Ẩn product bằng cascade đổi trạng thái variant.
- Áp dụng guard cho xoá cứng (hard delete) variant/product/category.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Admin hides a category with pending orders (Priority: P1)

An administrator turns off the display of a category that currently has a PENDING or PAYMENT_PENDING
order containing one of its variants. The system refuses with an error naming the blocking orders, and
the category keeps its previous state. When no such order exists, hiding succeeds and every cart line
holding a variant of that category disappears from all customers' carts.

**Why this priority**: Hiding a category is the widest hide action (it affects many variants at once),
so guarding it delivers the core protection.

**Independent Test**: Seed a category with a variant, place that variant in a PENDING order, attempt to
hide the category, and confirm the refusal names the order and the category is unchanged; then move the
order out of the pending states and confirm hiding succeeds and the cart lines are gone.

**Acceptance Scenarios**:

1. **Given** a category with no pending order containing any of its variants, **When** an administrator
   hides it, **Then** it is hidden and every cart line holding its variants is removed.
2. **Given** a category with a PENDING order containing one of its variants, **When** an administrator
   hides it, **Then** it is refused with the blocking order named and the category is unchanged.
3. **Given** a category with only PAID, SHIPPED, COMPLETED or CANCELLED orders, **When** an
   administrator hides it, **Then** it succeeds — those states do not block.

---

### User Story 2 - Admin hides a variant with pending orders (Priority: P1)

An administrator moves a variant to a hidden sell state while a PENDING or PAYMENT_PENDING order holds
that exact variant. The system refuses with the blocking order named. With no such order, the state
change succeeds and the variant disappears from every cart. Moving a variant to an on-sale state is
never blocked.

**Why this priority**: The variant is the unit of sale, so this is the most frequent hide action.

**Independent Test**: Place a variant in a PAYMENT_PENDING order, attempt to hide the variant, confirm
the refusal; resolve the order, confirm the hide succeeds and the variant leaves all carts; confirm a
move back to on-sale is never blocked.

**Acceptance Scenarios**:

1. **Given** a variant with no pending order, **When** an administrator hides it, **Then** it is hidden
   and removed from every cart.
2. **Given** a variant in a PENDING or PAYMENT_PENDING order, **When** an administrator hides it,
   **Then** it is refused with the blocking order named and the variant is unchanged.
3. **Given** a hidden variant, **When** an administrator moves it back on sale, **Then** it is never
   refused for pending orders.

---

### User Story 3 - Admin hides a product with its own flag (Priority: P1)

An administrator turns off a product's own visibility flag. When a PENDING or PAYMENT_PENDING order
contains any variant of that product, the system refuses with the blocking orders named. Otherwise the
flag is turned off, the product leaves the public catalogue, and all of its variants leave every cart.

**Why this priority**: The product flag is new in this feature; without it there is no single "hide
product" action at all.

**Independent Test**: Create a product with two variants, place one variant in a PENDING order, attempt
to hide the product, confirm the refusal; resolve the order, confirm the hide succeeds, the product
leaves the catalogue, and both variants leave every cart.

**Acceptance Scenarios**:

1. **Given** a product with no pending order on any of its variants, **When** an administrator hides
   it, **Then** it leaves the public catalogue and its variants leave every cart.
2. **Given** a product with a pending order on one of its variants, **When** an administrator hides
   it, **Then** it is refused with the blocking order named and nothing changes.
3. **Given** a hidden product, **When** a customer browses the catalogue, **Then** the product does not
   appear, regardless of its variants' states.

### Edge Cases

- **Two administrators race to hide the same item.** At most one refusal-or-success pair is recorded;
  the item ends hidden exactly once and no cart line is removed twice.
- **A new order containing the item is placed while the hide is being processed.** The hide check and
  the hide run atomically, so a checkout racing the hide either completes before the check (and then
  blocks it) or starts after the hide (and then cannot buy the hidden item).
- **A category with hundreds of variants.** The check and the cleanup each run as a bounded bulk
  operation, not one call per variant.
- **A cart that holds only hidden items.** After cleanup the cart is simply empty, which is a valid
  cart, not an error.
- **An order that leaves the pending states and re-enters.** Only orders currently in PENDING or
  PAYMENT_PENDING at check time block; history does not block.
- **Turning visibility back on.** Showing a category, product or variant again is never blocked and
  never restores removed cart lines.
- **A system stock-out transition blocked by a pending order.** The variant keeps its on-sale state and
  the blocked event is recorded, so the catalogue does not lie silently; sales stay safe because
  checkout always re-checks availability before an order is created.
- **Hard delete.** Removing a variant, product or category outright is outside this feature; existing
  deletion rules apply unchanged.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: "Pending approval" MUST mean an order whose status is PENDING or PAYMENT_PENDING, and
  nothing else.
- **FR-002**: The system MUST be able to answer, for any given set of variants, which pending orders
  contain at least one of them, in a single bounded read.
- **FR-003**: The system MUST be able to list the variants belonging to given categories without
  crossing module boundaries improperly.
- **FR-004**: Hiding a category (turning its display off) MUST first check its variants against
  pending orders in the same atomic unit of work, and MUST refuse with the blocking order identifiers
  when any is found.
- **FR-005**: Any move of a variant AWAY FROM its on-sale state — to any other sell state, whether
  initiated by an operator or by the system (for example an automatic stock-out transition) — MUST first
  check that variant against pending orders in the same atomic unit of work, and MUST refuse with the
  blocking order identifiers when found; moving a variant TO its on-sale state MUST never be checked.
  ("On-sale state" means the variant's ACTIVE sell state, so both ACTIVE→OUT_OF_STOCK and
  ACTIVE→DISCONTINUED are checked, while COMING_SOON→DISCONTINUED is not.)
- **FR-006**: A product MUST carry its own visibility flag, defaulting to visible; turning it off MUST
  check all of the product's variants against pending orders in the same atomic unit of work, and MUST
  refuse with the blocking order identifiers when found; the public catalogue MUST exclude a product
  whose flag is off.
- **FR-007**: After a successful hide, the system MUST remove every cart line holding any of the hidden
  variants from every cart, in the same atomic unit of work as the hide.
- **FR-008**: The check and the cleanup MUST behave the same when hiding a whole category (many
  variants at once) as for a single variant.

### Key Entities

- **Pending order**: an order in PENDING (awaiting shop confirmation) or PAYMENT_PENDING (awaiting
  customer payment). Only these two states block hiding.
- **Product visibility flag**: the new on/off switch owned by a product; off removes the product from
  the public catalogue independently of its variants' states.
- **Blocking order reference**: the identifier of a pending order that prevents a hide, returned in the
  refusal so an administrator can act on it.
- **Cart line**: a customer's intended purchase of one variant; removed automatically when its variant
  is hidden.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 100% of tested hide attempts against a category, product or variant covered by a pending
  order are refused, and the item is byte-for-byte unchanged afterwards.
- **SC-002**: 100% of tested successful hides leave zero cart lines holding a hidden variant across all
  carts.
- **SC-003**: A hidden product never appears in the public catalogue in any tested browse, filter or
  direct-link scenario.
- **SC-004**: Hiding a category holding hundreds of variants completes without one-per-variant round
  trips (verified by test observation of bounded reads/writes).

## Assumptions

- **Chosen defaults (decided with the product owner)**:
  - Pending = PENDING + PAYMENT_PENDING only.
  - Product hiding uses a new explicit flag, not a cascade of variant state changes.
  - Refusal carries the blocking order identifiers (admin-only surface, so no information leak to
    customers).
  - Check-then-hide and cart cleanup commit atomically in one transaction, so a racing checkout
    either completes before the check (and then blocks the hide) or starts after the hide (and then
    cannot buy the hidden item); checkout additionally re-checks sell state and availability at order
    time.
- **The existing foundation is reused as-is**: the administrator role, the audit writer, the request
  pipeline, the response envelope and the error catalogue are already in place.
- **Showing an item again is never blocked** and never restores removed cart lines.
- **No customer notification** is sent when a cart is cleaned; the cart simply no longer contains the
  line.
