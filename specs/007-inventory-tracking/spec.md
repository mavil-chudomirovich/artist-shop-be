# Feature Specification: Inventory Tracking

**Feature Branch**: `007-inventory-tracking`

**Created**: 2026-10-08

**Status**: Draft

**Input**: User description: "làm module này thôi" — module 05 Inventory (`docs/modules/05-inventory.md`): theo dõi tồn kho và lịch sử biến động kho một cách chính xác, an toàn giao dịch.

## Clarifications

### Session 2026-10-08

- Q: Đặt hàng có giữ chỗ tồn kho ngay khi khách vào bước thanh toán, hay tồn kho chỉ đổi khi
  thanh toán → A: **Giữ chỗ 15 phút**. Khi khách bắt đầu bước thanh toán của một đơn, số lượng
  của món được **giữ riêng cho đơn đó trong 15 phút**; hết hạn mà chưa thanh toán thì phần giữ
  **hoàn lại kho**; thanh toán thì phần giữ được **chốt thành đã bán**. Lý do: người dùng nêu rõ
  luồng này, và một con số "thực có" duy nhất không phân biệt được "đã bán" với "đang giữ" — trừ
  ngay khi vào thanh toán là coi như đã bán, không trừ thì món cuối bị hai khách cùng giữ. Đánh
  đổi được chấp nhận: thêm khái niệm giữ chỗ có hạn và một cơ chế hết hạn, đổi lấy việc món cuối
  không bị bán hai lần giữa checkout và thanh toán.
- Q: Khi bật bán một sản phẩm mà không có gì khả dụng (chưa restock, hoặc toàn bộ đang bị giữ),
  hệ thống xử lý thế nào → A: **Chỉ khoảnh khắc cắt qua 0 mới tự chuyển trạng thái.** Sản phẩm
  ACTIVE với khả dụng bằng 0 vẫn hiện cho khách tới biến động kế tiếp, và quy trình là restock
  trước khi bật bán. Lý do: roadmap quy định phụ thuộc một chiều `inventory → product`; nếu product
  phải hỏi inventory lúc bật bán thì phát sinh phụ thuộc ngược/vòng. Đánh đổi được chấp nhận: trong
  thời gian ngắn có thể có sản phẩm "đang bán nhưng khả dụng 0", nhưng giỏ/đơn vẫn từ chối vì kiểm
  tra khả dụng, nên không oversell.
- Q: Huỷ một đơn đã thanh toán có hoàn kho không → A: **Không có luồng huỷ đơn đã thanh toán**, nên
  không có việc hoàn kho cho đơn đã trả tiền. Thay vào đó hệ thống có luồng **chuyển nhượng một đơn
  đã thanh toán cho một tài khoản khác đã tồn tại**, và việc chuyển nhượng **không đổi tồn kho** vì
  hàng đã bán — nó chỉ đổi chủ sở hữu của đơn. Lý do: người dùng nêu rõ. Đánh đổi được chấp nhận:
  câu "đơn huỷ → hoàn kho" của module doc chỉ còn áp dụng cho đơn **chưa** thanh toán (nhả giữ chỗ),
  còn hoàn kho vật lý cho đơn đã trả tiền là một luồng không tồn tại.
- Q: Khi một thao tác vật lý (hư hỏng/điều chỉnh) sẽ đẩy khả dụng xuống âm, xử lý thế nào → A:
  **Từ chối thao tác.** Kho vật lý không bao giờ thấp hơn phần đang được hứa; operator được báo chờ
  các giữ chỗ hết hạn hoặc được thanh toán rồi làm lại. Lý do: đơn giản, không bao giờ hứa hàng
  không có, không thêm trạng thái "giữ chỗ bị phá", và giữ chỗ chỉ kéo dài 15 phút nên thời gian bị
  chặn ngắn (Constitution VII, YAGNI). Đánh đổi được chấp nhận: operator tạm thời không ghi được một
  hư hỏng cho tới khi giữ chỗ được giải quyết.

## Scope boundary

`docs/modules/05-inventory.md` lists four things under MVP scope. Three are deliverable now; the
fourth depends on modules that do not exist yet, and the reason is a fact about the repository
rather than a preference. The clarifications above add a holding behaviour, and this feature also
**inherits one obligation** that feature 006 deliberately left to it.

1. **Stock per product.** Deliverable.
2. **Manual changes: restock, damage, adjustment.** Deliverable.
3. **Every physical change records an entry in the stock ledger.** Deliverable.
4. **Automatic changes: a paid order decreases stock; a cancelled order restores it.**
   **Not deliverable end to end in this feature:** the producer of those events does not exist.
   Order is module 07 and Payment is module 08, and neither has been delivered — nothing in the
   system can pay or cancel an order. The capability those modules will call is built and tested
   here directly, but no source event exists to drive it end to end, so the wiring is theirs to
   close when they exist. This mirrors exactly what feature 006 did when it left the automatic
   half of the sell state to this module. (The clarification on cancellation refines the module
   document's wording: cancellation applies to an order that is still held, and a paid order is
   never cancelled — it is transferred instead.)

**Holding stock during checkout is in scope.** The clarification settles that a payment attempt
holds its goods for 15 minutes. Because availability — not physical stock — is what a customer can
buy, this feature owns the hold: what is held, by which order, until when, and what remains
available. The triggers that create and resolve a hold (beginning checkout, paying, cancelling)
belong to Order (07) and Payment (08), which do not exist; the holding capability, its expiry, and
availability are delivered and tested here directly, and the end-to-end flow is those modules' to
wire and test.

This feature **inherits obligation D1 from feature 006**
([`specs/006-product-catalog/deferred.md`](../006-product-catalog/deferred.md) D1,
[`docs/modules/04-product.md`](../modules/04-product.md) "Hai điểm chưa giao"): the sell state
of a product MUST change by itself when stock runs out and when stock returns. Feature 006 could
not build it because no stock entity existed anywhere; it left the two transition edges in the
product state machine ready for this module to call. **Delivering that automatic transition is
in scope here**, now driven by what is available rather than by physical stock, and it is what
makes the module document's third flow true.

The module document's own completion criteria are in scope to the extent they are verifiable
now: increases and decreases with tests, oversell prevention, and duplicate handling. The
end-to-end order flow that would exercise them is deferred to module 07, and that is stated
plainly in Assumptions rather than left as a silent gap.

The module document leaves one open question — whether a low-stock threshold belongs in MVP —
and this feature answers it: **no**, it stays deferred with the rest of the advanced alerts
(`docs/modules/05-inventory.md` defers "cảnh báo tự động nâng cao").

## User Scenarios & Testing *(mandatory)*

### User Story 1 - An operator manages the stock of a product (Priority: P1)

An administrator records goods arriving, goods damaged, and the result of counting the shelf.
Each operation changes the quantity of one product, and every one of them leaves a record of
what was done, by whom, and why.

**Why this priority**: This is the only way stock ever changes by hand, and it is what an order
event changes when it exists, so without it the module has nothing to do. It is deliverable with
no dependency on any other module.

**Independent Test**: Restock a product, record a damage, and correct the quantity after a count,
through the administrator's operations, confirming after each step both the current quantity and
the ledger record it produced.

**Acceptance Scenarios**:

1. **Given** a product, **When** the administrator records a restock of a positive quantity,
   **Then** the product's physical stock rises by exactly that quantity and one ledger record is
   written naming the product, the kind, the amount, the resulting quantity, the administrator and
   the time.
2. **Given** a product with stock, **When** the administrator records damage smaller than the
   stock, **Then** the physical stock falls by exactly that amount and one ledger record is
   written.
3. **Given** a product whose stored quantity disagrees with the shelf, **When** the
   administrator corrects it to the counted quantity, **Then** the physical stock becomes the
   counted quantity and the difference is recorded as one adjustment.
4. **Given** a product, **When** the administrator records a change, **Then** an audit entry is
   written naming the product and the administrator.
5. **Given** a customer's session, **When** they attempt any stock operation, **Then** it is
   refused on role grounds.
6. **Given** a product that does not exist, **When** the administrator operates on it, **Then**
   the response is a not-found.

---

### User Story 2 - The shop can never oversell (Priority: P1)

An operator tries to remove more units than the shelf holds, or two operations reach the same
product at once. The system refuses anything that would take a product below zero, so the shop
never promises goods it does not have.

**Why this priority**: This is the guarantee that makes the quantity trustworthy; a stock figure
that can go negative is worse than no figure at all. Equally P1: tracking without it would record
a fiction.

**Independent Test**: Attempt to remove more than the available stock, and drive two competing
decreases at the same product simultaneously, confirming the quantity never goes below zero and
every refusal leaves both the quantity and the ledger untouched.

**Acceptance Scenarios**:

1. **Given** a product with three units, **When** the administrator records damage of five,
   **Then** it is refused, the field is named, the physical stock stays at three, and no ledger
   record is written.
2. **Given** a product with one unit, **When** two decreases of one arrive at the same instant,
   **Then** exactly one succeeds, the stock ends at zero, and the other is refused rather than
   making the stock negative.
3. **Given** a product with one unit, **When** an action would take it to exactly zero, **Then**
   it succeeds — zero is reachable, only below zero is not.
4. **Given** a refused operation, **When** the operator lists the history, **Then** the refused
   change has left no record, because nothing changed.

---

### User Story 3 - Stock is held while a customer pays (Priority: P2)

A customer starts paying for an item, and for the next fifteen minutes that item is set aside for
them — no one else can take it — while the money is not yet paid. If the payment does not happen
in time, the item goes back on the shelf for anyone to buy. If it is paid, the item is gone; if
the order is cancelled, it is back.

**Why this priority**: P2 because the manual ledger works without it, but without a hold the last
unit can be sold twice between checkout and payment — the exact oversell US2 promises not to
allow. It must be built here because availability is what customers can buy, and the moment a
customer can buy is settled by this feature.

**Independent Test**: With a product holding several units, hold some for an order and confirm
the rest remains available to another; let the hold expire and confirm the quantity returns;
hold again, pay, and confirm physical stock falls once; hold, cancel, and confirm the quantity
returns — no step ever doubling on retry.

**Acceptance Scenarios**:

1. **Given** a product with three available, **When** a customer begins paying for two, **Then**
   two are held for that order, one remains available to everyone else, and physical stock is
   unchanged.
2. **Given** a hold, **When** fifteen minutes pass with no payment, **Then** the held quantity
   returns to availability.
3. **Given** a hold, **When** the order is paid, **Then** the physical stock falls by the held
   quantity, the hold closes, and availability is unchanged.
4. **Given** a hold, **When** the order is cancelled, **Then** the held quantity returns to
   availability and physical stock is unchanged.
5. **Given** a customer holds the last available unit, **When** another customer tries to take
   it, **Then** the second is refused because nothing is available.
6. **Given** a request to hold more than is available, **When** it is made, **Then** it is
   refused and nothing is held.

---

### User Story 4 - Availability follows the stock by itself (Priority: P2)

When the last unit of an on-sale product is taken — sold, or held for a customer who is paying —
the product stops being offered; when it becomes available again — restocked, or a hold released
— it is offered again. The operator no longer has to remember to mark a product out of stock by
hand, and a customer never sees a price for something the shop cannot ship.

**Why this priority**: P2 because US1 and US2 deliver a working stock ledger without it, but this
is the inherited obligation from feature 006 and a stated flow of the module document. It must be
automatic, so it cannot live only in an operator's habit.

**Independent Test**: Make an on-sale product's availability reach zero and confirm it becomes
out of stock, then make it available again and confirm it returns to sale; separately confirm
that an announced product and a retired product are untouched by a stock change.

**Acceptance Scenarios**:

1. **Given** an on-sale product with five available, **When** the available quantity reaches
   zero, **Then** the product becomes out of stock in the same operation.
2. **Given** an out-of-stock product with zero available, **When** the available quantity rises
   above zero — by a restock or by a hold being released — **Then** the product returns to sale
   in the same operation.
3. **Given** a product announced but not yet on sale, **When** its stock changes, **Then** its
   sell state is untouched.
4. **Given** a retired product, **When** it is restocked, **Then** it stays retired and never
   returns to sale.
5. **Given** an on-sale product marked out of stock by the operator while it still has stock,
   **When** it is restocked without crossing zero, **Then** its sell state is untouched — the
   operator's deliberate marking is preserved.
6. **Given** a change that makes a product's availability reach zero, **When** a customer requests
   the product in the same instant, **Then** the product is never served as buyable with nothing
   available.

---

### User Story 5 - A repeated event changes stock exactly once (Priority: P2)

A payment or a cancellation may reach the shop twice — a retried callback, a repeated message.
If the same event arrives twice, the stock changes once, not twice.

**Why this priority**: P2 because no order exists yet to send such an event, but the constitution
requires that replaying the same event never doubles an inventory effect, and this is the only
place that guarantee can live. It must be built here and be provably correct before module 07 can
rely on it.

**Independent Test**: Apply a stock change carrying an outside event's identity, then apply the
identical event again, confirming the quantity changed exactly once and only one ledger record
exists; repeat with the two applications arriving simultaneously.

**Acceptance Scenarios**:

1. **Given** an outside event that has already been applied, **When** the same event is applied
   again, **Then** the quantity is unchanged, no second ledger record is written, and the repeat
   is accepted rather than treated as a fault.
2. **Given** an outside event that would take physical stock below zero, **When** it is applied,
   **Then** it is refused, no ledger record is written, and nothing is applied.
3. **Given** the same outside event applied twice at the same instant, **When** both arrive,
   **Then** the quantity changes exactly once.

---

### User Story 6 - The operator can account for the stock (Priority: P3)

An administrator opens a product's stock history and sees every change in order — what happened,
how much, when, by whom — so a figure that looks wrong can be explained without guessing.

**Why this priority**: P3 because the shop runs without a history view, but "every change is
traceable" is a stated requirement of the module document and the history is the evidence that
makes every other guarantee auditable.

**Independent Test**: Make several changes to one product, read its history, and confirm every
change appears once, in order, with its kind, amount, resulting quantity and actor; confirm a
product with no changes answers an empty history.

**Acceptance Scenarios**:

1. **Given** a product with several changes, **When** the administrator reads its history,
   **Then** every change appears once, in the order it happened, with its kind, amount, resulting
   quantity, actor and time.
2. **Given** a product with no changes, **When** the administrator reads its history, **Then**
   the result is empty rather than an error.
3. **Given** a product with many changes, **When** the administrator reads its history, **Then**
   it is paginated under the project's usual list convention.

### Edge Cases

- **Two operations reach the same product at once** (a restock and a damage, a hold and a damage,
  or two holds). Each is applied against the current quantities in a defined order, and neither
  the physical stock nor the available quantity ever goes below zero; whichever cannot be
  satisfied is refused rather than silently rounded.
- **A damage larger than the physical stock.** It is refused, naming the field, and leaves both
  the quantity and the ledger untouched.
- **A physical decrease that would leave less physical stock than is currently held.** It is
  refused rather than allowed to break a hold; the operator is told to wait for the holds to
  expire or be paid, and the physical stock never falls below what has been promised.
- **A hold larger than what is available.** It is refused and nothing is held; the physical stock
  is untouched.
- **An adjustment to the quantity already recorded.** Nothing changed, so nothing is written;
  recounting and finding the same number is not an error.
- **A quantity that is zero, negative, or fractional.** Where a positive whole amount is
  required, it is refused naming the field; stock is a whole number of units and is stored
  exactly, however large.
- **A hold that expires at the same instant the order is paid.** Exactly one of the two takes
  effect: the goods are either sold or returned, never both, and never twice.
- **A paid order transferred to another existing account.** Nothing about stock changes: the goods
  stay sold, the physical quantity stays down, and no hold is involved because a paid order has
  none. Only who owns the order changes, which is the order module's concern.
- **A product removed from the catalogue while it still has stock, holds, or history.** It leaves
  stock management with the product; it cannot be restocked or sold afterwards. How much movement
  history survives a removal is a storage decision for the plan, and the audit trail keeps the
  fact of the removal regardless. Preserving inventory history beyond a product's life is not
  required for MVP and is revisited together with module 07, which feature 006 named as the owner
  of the product-removal decision.
- **A retired product that still holds stock.** Restocking it is allowed, but it never returns to
  sale; the goods exist but are not offered.
- **An on-sale product whose availability is zero but which the operator has not yet changed.** A
  change that crosses zero fixes the state automatically (US4); a product put on sale with
  nothing available is corrected by the first change, and the operator is expected to restock
  before offering a product.
- **A product with no stock ever recorded.** Its quantity is understood as zero, it is not
  offered as buyable, and restocking it is the ordinary way to make it available.
- **An outside event repeated after its effect was already applied.** It is a no-op accepted as
  success, so a retrying caller stops retrying instead of being told it failed.
- **An outside event replayed with a different identity but the same intent.** The system cannot
  know two different events mean the same thing; deduplication is by event identity, so callers
  must supply a stable identity — recorded here so the obligation on module 07 is explicit.

## Requirements *(mandatory)*

### Functional Requirements — operator maintenance

- **FR-001**: System MUST let an administrator increase a product's physical stock by a positive
  quantity (restock).
- **FR-002**: System MUST let an administrator decrease a product's physical stock by a positive
  quantity (damage), refusing any decrease that would take the physical stock below zero.
- **FR-003**: System MUST let an administrator correct a product's physical stock to a recounted
  absolute quantity (adjustment), recording the signed difference from the previous quantity.
- **FR-004**: Every physical stock change MUST write exactly one immutable ledger record naming
  the product, the kind of change, the signed amount, the resulting quantity, the acting operator,
  the time and — for a manual change — an optional free-text note the operator supplies. A ledger
  record MUST NOT be edited or removed. A hold, a hold's release and a hold's expiry are not
  physical changes and are recorded as holds, not in the ledger; only the point a hold is turned
  into a sale changes physical stock and writes a ledger record.
- **FR-005**: Every **manual** stock operation — restock, damage and adjustment — MUST be refused
  unless the caller holds the administrator role, and the acting administrator MUST come from the
  session rather than the request. A stock change caused by a system event (a payment turning a hold
  into a sale, or another outside event) carries no session and no administrator; it is applied by
  the module's own use case and is governed by FR-014 to FR-023 instead.
- **FR-006**: Every manual stock operation MUST be written to the audit trail, identifying the
  product and the acting administrator. A stock change applied from an outside event MUST also be
  written to the audit trail, naming that event's source reference with no human actor, so a
  payment event's inventory effect is auditable (Constitution VI).
- **FR-007**: System MUST let an administrator read, for a product, its physical stock, the
  quantity currently held, and what remains available. A request naming a product that does not
  exist MUST answer not-found, so the administrator is never shown a fabricated zero for a product
  that was never created; a product that exists but has never been stocked answers a physical
  quantity of zero.
- **FR-008**: System MUST let an administrator read a product's movement history, in the order
  the changes happened, and the history MUST be paginated.

### Functional Requirements — integrity

- **FR-009**: A product's physical stock MUST never be negative, and neither MUST its available
  quantity. An operation that would take either below zero MUST be refused with the field named,
  and nothing MUST be changed. This includes a physical decrease that would leave less physical
  stock than is currently held: it is refused rather than allowed to break a hold, and the operator
  is told to wait for the holds to resolve.
- **FR-010**: These non-negative rules MUST hold at the storage layer, so they stand even when two
  operations reach the same product concurrently and no single code path can violate them.
- **FR-011**: A physical change, its ledger record, and any availability consequence it causes
  MUST be applied inside a single transaction, so the quantities and the ledger can never
  disagree.
- **FR-012**: A stock quantity MUST be a whole number and MUST be stored exactly, equal to the
  quantity the operation produced; a fractional, zero or negative amount where a positive one is
  required MUST be refused with the field named.
- **FR-013**: A product's available quantity MUST be derived, at the moment it is asked, from its
  physical stock and the quantities its active holds have set aside, rather than stored as a
  separate copy that can drift. Exposing that quantity across the module boundary to a future cart
  or order — whose modules do not exist — is deferred, and is recorded in Assumptions rather than
  promised here.

### Functional Requirements — holding stock during checkout

- **FR-014**: When a customer begins the payment step of an order, the system MUST hold the
  ordered quantity for that order, reducing the product's available quantity while leaving its
  physical stock unchanged, for a fixed window of 15 minutes.
- **FR-015**: A hold MUST expire 15 minutes after it is created. A hold that expires unpaid MUST
  return its quantity to availability without changing physical stock.
- **FR-016**: Paying a held order MUST consume its hold: the physical stock MUST fall by the held
  quantity and the hold MUST close in the same operation, leaving availability unchanged. This
  consumption is the physical change FR-004 requires to be recorded in the ledger.
- **FR-017**: Cancelling an order that is still held — one that has not been paid — MUST release its
  hold: the held quantity MUST return to availability with no change to physical stock. A paid
  order is never cancelled, so inventory never restores physical stock for one; a paid order may
  instead be transferred to another account, which does not change stock at all.
- **FR-018**: A product's available quantity MUST be its physical stock minus the quantities held
  by its active holds, and it MUST never be negative; a hold that would exceed availability MUST
  be refused.
- **FR-019**: Creating, releasing and consuming a hold MUST each be applied at most once per
  order, so a retried or duplicated checkout, payment or cancellation never holds, releases or
  consumes twice.

### Functional Requirements — outside events and exactly-once application

- **FR-020**: A physical stock change caused by an outside event MUST carry the identity of that
  event as a source reference, and applying the same source reference more than once MUST change
  stock at most once. The application MUST be recorded in the audit trail naming the source
  reference with no human actor, so the event's inventory effect is auditable (Constitution VI).
- **FR-021**: A repeated application of an already-applied source reference MUST be accepted as a
  no-op — no second ledger record, no second change — so a retrying caller is not told it failed.
- **FR-022**: The source reference MUST be unique at the storage layer, so a duplicate cannot be
  recorded even when two identical events arrive at the same instant.
- **FR-023**: An outside event that would take a product's physical stock below zero MUST be
  refused and MUST NOT record a movement, leaving the event unapplied.

### Functional Requirements — availability follows stock (inherited from feature 006, D1)

- **FR-024**: When a product's available quantity reaches zero and the product is on sale, the
  system MUST move the product to out of stock.
- **FR-025**: When a product's available quantity rises from zero to a positive value and the
  product is out of stock, the system MUST move the product back on sale.
- **FR-026**: The availability change MUST happen in the same transaction as the change that
  caused it — a physical change or a hold — so a product is never served as buyable while nothing
  is available.
- **FR-027**: The automatic availability change MUST affect only the on-sale and out-of-stock
  states. A product announced but not yet on sale MUST NOT be changed by a stock change, and a
  retired product MUST NEVER return to sale through stock.
- **FR-028**: A change that does not cross zero MUST NOT change a product's sell state, so an
  operator's deliberate out-of-stock marking is preserved.

### Functional Requirements — out of scope

- **FR-029**: System MUST NOT track stock at more than one location, and MUST NOT warn on a
  low-stock threshold; both stay deferred, as `docs/modules/05-inventory.md` records.

### Key Entities

- **Stock level**: the physical number of units of one product the shop holds. It belongs to a
  product and is a whole number that is never negative. The quantity a customer can take is the
  **available** quantity — the physical stock minus what active holds have set aside — and it too
  is never negative.
- **Stock movement**: one immutable record of a single change to a product's physical stock.
  Carries the product, the kind of change (restock, damage, adjustment, or a sale), the signed
  amount, the resulting quantity, the optional identity of an outside event that caused it, the
  acting operator, the time, and an optional free-text note for a manual change. Movements are
  append-only and are the traceable history of how a physical quantity came to be. A hold is not a
  movement; only turning a hold into a sale is.
- **Hold**: a quantity of one product set aside for one order while that order is being paid.
  Carries the product, the order it belongs to, the quantity, when it was created, and when it
  expires. While a hold is active the quantity is unavailable to anyone else; it ends by being
  paid (which turns it into a sale), cancelled (which returns it), or expiring (which returns it).
- **Source reference**: the identity of an outside event that caused a physical change. It is what
  lets the same event be applied at most once, and it is unique across movements.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Every change to a product's physical stock leaves exactly one ledger record, and the
  records of a product reconcile to its physical quantity — verified by automated test rather than
  by inspection.
- **SC-002**: No operation, alone or concurrent with another — a physical change or a hold — can
  drive a product's physical stock or available quantity below zero; an attempted oversell is
  refused, names the field, and leaves the quantities and the ledger unchanged. Verified by
  automated test including a concurrent case.
- **SC-003**: Applying the same outside event more than once, including simultaneously, changes
  stock exactly once and produces exactly one ledger record; a repeated application is accepted
  as a no-op rather than reported as a failure.
- **SC-004**: A hold placed as payment begins makes its quantity unavailable to others within the
  same operation while leaving physical stock unchanged; if it is unpaid for fifteen minutes its
  quantity is available again; paying consumes it (physical stock falls exactly once) and
  cancelling returns it — verified by automated test, including the two customers contesting the
  last unit.
- **SC-005**: When a product's availability crosses zero, its sell state changes accordingly
  within the same operation, and a retired or announced product is never affected; a change that
  does not cross zero leaves the sell state exactly as it was. Verified by automated test, one
  positive and one negative per crossing.
- **SC-006**: Every manual operation leaves an audit entry naming the product, the operator, the
  kind and the amount, and no audit entry contains a customer's personal data.
- **SC-007**: A product's history shows every physical change once, in order, with its actor and
  time, and an operation that changed nothing leaves no history entry.

## Assumptions

- **Each product has one physical stock level and any number of holds.** `docs/modules/05-inventory.md`
  tracks stock per product and defers multiple locations, so there is one physical quantity per
  product; the Clarifications add holds on top of it, which is what makes the "available" quantity
  a derived fact rather than a second physical number.
- **The hold window is a single fixed business constant of 15 minutes** for MVP. It is stated once
  rather than per product or per order, and it is the one value the whole holding behaviour turns
  on; whether it should be configurable is a later concern, not a reason to leave the behaviour
  open.
- **A combo set is a product in its own right** (feature 006, FR-039), so it has its own stock
  level like any other product. This feature does not derive a set's stock from its members, and
  whether selling a set also consumes member stock is an order-level concern, not this feature's.
- **Availability is expressed through the product's sell state, not a second flag.** Customers
  already read availability from the sell state (feature 006), so the stock module drives that
  state between on-sale and out-of-stock and introduces no parallel notion of in-stock. This is
  the obligation inherited from feature 006 (D1), and it is the one place this feature's behaviour
  reaches across a module boundary.
- **Crossing zero is the only trigger, and it is crossing zero of the available quantity**, not of
  the physical quantity. A product goes out of stock the moment nothing is available — sold or
  held — and comes back the moment something is. Putting a product on sale for the first time
  remains the operator's act (feature 006), and a product put on sale with nothing available is
  corrected by the next change; the operator is expected to restock before offering a product.
- **The checkout, payment and cancellation triggers belong to their modules and are deferred.**
  Order (07) and Payment (08) do not exist, so nothing can begin a checkout, pay or cancel yet.
  The holding capability, its expiry, and the exactly-once application of an outside event are
  delivered and tested here directly, but the end-to-end flow is theirs to wire and test. This is
  the same arrangement feature 006 used when it left the automatic sell-state half to this module.
- **A paid order is never cancelled, and transferring one does not change stock.** The system has no
  flow that cancels an order after payment, so inventory never restores physical stock for a paid
  order; instead an order may be transferred to another existing account, which changes who owns
  the order and not what is in stock. The module document's "a cancelled order restores stock"
  therefore applies only to an order that was still held, whose hold is released — its physical
  quantity was never decreased.
- **Deduplication is by identity, and a hold is keyed by its order.** The system can only apply an
  outside event at most once if the caller supplies a stable identity for it, and it can only
  avoid a double hold if a hold is keyed by the order it belongs to; supplying the same event
  under two different identities is indistinguishable from two genuine events and is the caller's
  obligation. This is recorded so module 07 knows what it must provide.
- **The foundation is reused as-is**: the administrator role, the audit writer, the request
  pipeline, the response envelope, the pagination metadata and the error catalogue are already in
  place. This feature adds no new infrastructure beyond what expiring a hold requires, and how
  expiry is driven is a plan decision.
- **No dedicated rate limit is added for this module.** The shared request limit applies. The
  operations are administrator-only, so the abuse surface a module-specific limit exists to
  protect — any authenticated customer hammering a write endpoint — does not exist here. Revisit
  if the stock operations ever become reachable by more than the administrator role.
- **The product module currently removes a product outright** (feature 006 chose hard delete).
  This feature does not change that: a removed product leaves stock management with the product.
  Whether inventory history must outlive a product is revisited together with module 07, which
  feature 006 named as the owner of the product-removal decision, rather than decided here.
- **Advanced forecasting and low-stock alerts stay deferred**, as `docs/modules/05-inventory.md`
  states; this feature answers the module document's open question about a low-stock threshold by
  leaving it out of MVP.
