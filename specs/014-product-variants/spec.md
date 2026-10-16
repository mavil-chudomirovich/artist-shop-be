# Feature Specification: Product Variants

> **ĐÃ BỊ THAY THẾ.** Feature 014 bị thay thế bởi
> [`specs/015-product-sku-model/`](../015-product-sku-model/spec.md) — xem
> [`superseded.md`](superseded.md). File này được giữ lại như bản ghi lịch sử, không còn là nguồn
> authoritative.

**Feature Branch**: `014-product-variants`

**Created**: 2026-10-14

**Status**: Draft

**Input**: User description: "mình muốn một sản phẩm có thể có nhiều loại — ví dụ Card Genshin khi bấm vào có thể chọn card Flins, card Dottore, giống Shopee khi bấm mua balo thì chọn màu hoặc chọn loại."

## Clarifications

### Session 2026-10-14

- Q: Một sản phẩm có thể có bao nhiêu chiều lựa chọn (ví dụ chỉ chọn loại, hay chọn cả màu × size)?
  → A: **Một chiều duy nhất**. Mỗi nhóm có đúng một chiều chọn (ví dụ "Card" hoặc "Màu"), khách chọn
  một giá trị trong chiều đó. Nhiều chiều kết hợp (màu × size) **hoãn**, vì cần một ma trận tổ hợp
  và cách tính giá/tồn theo tổ hợp — đó là data model khác, không phải cách diễn đạt khác. Lý do:
  yêu cầu nêu ra chỉ có một chiều ("chọn card nào", "chọn màu hoặc chọn loại").
- Q: Mỗi lựa chọn có phải là một sản phẩm đầy đủ (giá/tồn/kho/ảnh/trạng thái riêng), hay chỉ là một
  nhãn dùng chung giá và tồn của sản phẩm cha?
  → A: **Mỗi lựa chọn là một sản phẩm đầy đủ, độc lập**. Mỗi lựa chọn có giá riêng, tồn kho riêng,
  trạng thái bán riêng, ảnh riêng và liên kết riêng. Lý do: các lựa chọn khác nhau có thể khác giá và
  khác tình trạng còn hàng (một card hết hàng, card khác còn), và hệ thống đã bán theo từng đơn vị
  sản phẩm.
- Q: Bản thân nhóm có phải là một đơn vị bán không — nó có giá, tồn kho, trạng thái bán riêng?
  → A: **Không**. Nhóm là một "trang"/mục trưng bày gom các lựa chọn lại; nó **không** có giá bán,
  **không** có tồn kho, **không** có trạng thái bán riêng, và **không** thể được thêm thẳng vào giỏ.
  Mọi thứ mua được đều là một lựa chọn. Lý do: chỉ lựa chọn mới có giá và tồn; cho nhóm một giá sẽ
  tạo nguồn sự thật thứ hai mâu thuẫn với các lựa chọn.
- Q: Khi một sản phẩm đã là lựa chọn của một nhóm, khách thêm vào giỏ cái gì?
  → A: **Thêm đúng lựa chọn đã chọn**. Giỏ hàng và đơn hàng ghi nhận lựa chọn đó như một sản phẩm
  bình thường, với giá và định danh của chính nó; các lựa chọn cùng nhóm không trộn vào nhau. Lý do:
  đây là điều kiện để toàn bộ giỏ/đơn/kho hiện có tiếp tục hoạt động mà không phải đổi.
- Q: Một link segment có phải duy nhất trên **cả** nhóm lẫn sản phẩm, hay hai không gian tên riêng?
  → A: **Hai không gian tên riêng**. Link của nhóm duy nhất trong các nhóm; link của lựa chọn/sản phẩm
  duy nhất trong các sản phẩm; nhóm và sản phẩm được định địa chỉ qua hai route công khai khác nhau,
  nên cùng một giá trị có thể xuất hiện một lần ở mỗi loại. Lý do: dự án đã tách namespace theo loại
  (categories và products có slug độc lập, route riêng), nên tách namespace giữ nhất quán và không cần
  cơ chế chống trùng xuyên bảng.
- Q: Nhóm có bộ ảnh riêng (dùng chung) tách khỏi ảnh của từng lựa chọn không?
  → A: **Có, và cả hai cùng tồn tại**. Nhóm có bộ ảnh dùng chung riêng; mỗi lựa chọn có ảnh riêng; khi
  khách chọn một lựa chọn, ảnh đang hiển thị **chuyển sang ảnh của lựa chọn đó**. Lý do: trang nhóm cần
  ảnh chung (banner) và cần phản ánh đúng lựa chọn đang xem, giống Shopee.
- Q: Card của nhóm hiển thị giá thế nào khi nhóm không có giá riêng?
  → A: **Một giá duy nhất, lấy từ một lựa chọn đang hiện** (lựa chọn thấp nhất đang hiện). Các vật phẩm
  được gom nhóm dự kiến cùng giá, nên chỉ cần lấy giá của một lựa chọn làm giá của card; **không** dùng
  khoảng giá. Lý do: nhu cầu thực tế hiện tại là gom các vật phẩm cùng giá.

## Scope boundary

`docs/modules/04-product.md` xếp "biến thể phức tạp (size/màu) nâng cao" vào mục **Không (hoãn)**,
và `specs/006-product-catalog/deferred.md` D7 ghi mỗi biến thể là một feature riêng khi có nhu cầu.
Feature này **đóng một phần** khoảng hoãn đó: biến thể **một chiều** được giao.

Trong phạm vi:

- Khách chọn một lựa chọn trong nhóm và mua đúng lựa chọn đó.
- Quản trị tạo/quản lý nhóm và gán sản phẩm làm lựa chọn.
- Mỗi lựa chọn độc lập về giá, tồn, trạng thái bán và ảnh.

Ngoài phạm vi (giữ hoãn, ghi rõ để không mất):

- Biến thể nhiều chiều kết hợp (màu × size) và ma trận tổ hợp.
- Giá/tồn được suy ra theo tổ hợp (SKU matrix đầy đủ kiểu Shopee).
- Ảnh/trạng thái dùng chung cấp nhóm cho các lựa chọn (mỗi lựa chọn giữ ảnh và trạng thái của riêng
  nó).
- Hiển thị số lượng còn lại theo từng lựa chọn trên trang nhóm (xem Assumptions).

## User Scenarios & Testing *(mandatory)*

### User Story 1 - A customer picks a variant and buys it (Priority: P1)

A visitor browses the shop and sees a grouped item — for example a character card set — as **one**
entry, not as one card per character. Opening it, they read the shared name, description and picture
gallery, see the choices offered (each with its own label, price and picture), pick one, and buy
that choice; choosing a choice switches the displayed picture to that choice's own picture. If they
go back and pick a different choice, it is a different purchasable item with its own price and its
own stock.

**Why this priority**: This is the reason the feature exists; without it a grouped product cannot
be chosen at all, and the whole feature delivers no value.

**Independent Test**: Create a group with two options that differ in price, open the group's entry
in the public catalogue, confirm the two choices appear with their own labels, prices and pictures,
add one to the cart, and confirm the cart holds exactly that choice at that price — then add the
other and confirm both are held separately.

**Acceptance Scenarios**:

1. **Given** a group with two options, **When** a visitor requests the public list, **Then** the
   group appears as a single entry (once, not once per option).
2. **Given** a group's entry, **When** a visitor opens it, **Then** they see the group's shared
   name, description and picture gallery together with every option's label, price and picture, and
   choosing an option switches the displayed picture to that option's own picture.
3. **Given** the option list, **When** the visitor chooses an option, **Then** that option — not the
   group and not another option — is what gets added to the cart, at that option's price.
4. **Given** two options of the same group, **When** both are added to the cart, **Then** they are
   held as two separate lines, each with its own price and quantity.
5. **Given** a group, **When** a visitor opens a link that points directly at one of its options,
   **Then** that option is shown with the group's shared content and the option's own content, and
   it is buyable exactly as if reached through the group.
6. **Given** an option that is not on sale while a sibling is on sale, **When** the visitor tries to
   buy the off-sale option, **Then** it is refused, and the on-sale sibling remains buyable.

---

### User Story 2 - An operator maintains variant groups (Priority: P1)

An administrator creates a group with a name, a link, a description, a category, a position and a
single choice dimension name (for example "Card"), then attaches products to it as choices, each
with a short label ("Flins", "Dottore"). They rename the group, reorder it, change or add choices,
detach a choice, and eventually remove the group.

**Why this priority**: Without this, US1 can only ever show whatever was arranged by hand; the two
halves ship together, exactly as the catalogue itself did.

**Independent Test**: Create a group, attach two existing products as choices with labels, confirm
the customer-facing group shows them, detach one and confirm the group shows only the remaining
one, then remove the group and confirm the products remain.

**Acceptance Scenarios**:

1. **Given** an administrator, **When** they create a group with a name, link, description,
   category, position and one choice-dimension name, **Then** it is created and receives a stable
   identifier that never changes afterwards.
2. **Given** a group and an existing product, **When** the administrator attaches the product as a
   choice with a label, **Then** the product becomes an option of the group and keeps its own
   price, pictures, sell state and link.
3. **Given** a group with choices, **When** the administrator gives a new choice the label an
   existing choice already has, **Then** it is refused with the field named.
4. **Given** a grouped product, **When** the administrator detaches it, **Then** it becomes an
   ordinary product again and is listed on its own.
5. **Given** a group, **When** the administrator removes it, **Then** the products that were its
   choices are not removed.
6. **Given** a customer's session, **When** they attempt any variant-group maintenance action,
   **Then** it is refused on role grounds, and the attempt is audited.
7. **Given** a group that does not exist, **When** the administrator edits or removes it, **Then**
   the response is a not-found.

---

### User Story 3 - Each option is its own sellable unit (Priority: P2)

An operator prices and stocks each choice independently. One card can be priced higher than
another, one can be sold out while another is available, and each can carry its own picture. The
catalogue, the cart, the order and the stock all treat a choice as an ordinary product.

**Why this priority**: P2 because US1 and US2 deliver a usable grouped product without it, but
independent pricing and stock are what make variants realistic (a rare card costs and runs out
differently from a common one).

**Independent Test**: Give two options of a group different prices and different stock, confirm the
list/detail reflect each option's own price, confirm buying one stock-out option is refused while
the other succeeds, and confirm each option's stock moves only for that option.

**Acceptance Scenarios**:

1. **Given** two options of a group, **When** the administrator sets different prices, **Then**
   each option shows and charges its own price.
2. **Given** two options, **When** one is out of stock, **Then** only that option is unbuyable and
   the other remains buyable.
3. **Given** an option is bought, **When** stock changes, **Then** only that option's stock changes.
4. **Given** an option with its own pictures, **When** the group is shown, **Then** the option's
   picture is shown as its own distinct picture.

---

### User Story 4 - A group's presence follows its choices (Priority: P3)

A group whose choices are all hidden or removed does not linger in the customer-facing catalogue,
and a group comes back as soon as at least one choice is visible again.

**Why this priority**: P3 because it is a correctness guard on the list rather than a new
capability; the feature works without it, but leaving empty groups in the list would be wrong.

**Independent Test**: Hide or remove all of a group's choices and confirm the group disappears from
the list and its direct link answers not-found; make one choice visible again and confirm the group
reappears as a single entry.

**Acceptance Scenarios**:

1. **Given** a group whose every choice is hidden, **When** a visitor requests the list, **Then**
   the group does not appear and is not counted.
2. **Given** a group with at least one visible choice, **When** a visitor requests the list,
   **Then** the group appears once.
3. **Given** a group with no choice at all, **When** a visitor requests the list, **Then** it does
   not appear.

### Edge Cases

- **A group whose options differ in price.** The grouped entry must still present one coherent price
  to the customer (see Assumptions); opening the group reveals each option's own price.
- **Two options in one group with the same label.** Refused, naming the field; labels must be
  distinguishable because they are what the customer picks between.
- **A product already an option of a group is attached to another group.** Refused: a product
  belongs to at most one group at a time.
- **A group's link and a product's link hold the same value.** This is allowed: group links and
  product links are two separate namespaces addressed by distinct customer-facing routes (see
  Clarifications). A collision *within* one namespace — two groups, or two products — is refused
  with the field named, following the catalogue's existing uniqueness rule.
- **A group still has options when it is removed.** The removal must not delete or orphan the
  options; either it is refused while options remain, or the options are detached first (see
  Assumptions).
- **A group with one option.** Valid: a group may offer a single choice.
- **A group's option is also a combo set, or a set is made an option.** Refused: a combo set is a
  product in its own right, not a choice among alternatives, so the two roles must not mix.
- **An option is removed while a customer is reading the group.** The group's next read reflects the
  removal; a direct request for the removed option answers not-found.
- **The last visible option of a group goes out of stock.** The group leaves the customer-facing
  list (its only choice is no longer visible), exactly as a single product would.
- **A group in a category the operator hides.** The whole group leaves the list and direct access
  along with the category, without any option's sell state changing; showing the category again
  brings the group back as it was.
- **A visitor opens a group link while every option is off sale.** The group answers the same
  not-found a nonexistent group answers, so the response never confirms a hidden group exists.

## Requirements *(mandatory)*

### Functional Requirements — customer-facing catalogue

- **FR-001**: System MUST let a customer browse a public catalogue in which a variant group appears
  as exactly one entry, never as one entry per option.
- **FR-002**: System MUST let a customer open a group's entry and see the group's shared name,
  description and its own picture gallery together with every one of its visible options, each
  carrying its own label, price and picture. Selecting an option MUST switch the displayed picture to
  that option's own picture, so the page reflects the choice being viewed.
- **FR-003**: Choosing an option MUST be how a grouped product is bought: the chosen option MUST be
  added to the cart and recorded in an order as an ordinary product, with its own identifier and its
  own price, distinct from its sibling options.
- **FR-004**: An option that is not on sale MUST NOT be addable to a cart — through its group or
  through a direct link — independently of its siblings, and a group containing an on-sale option
  MUST remain buyable for that option. "Buyable" means on sale, exactly as it does for any product,
  so an off-sale option (out of stock, retired, or not yet on sale) is refused while a sibling that
  is on sale is not (FR-027).
- **FR-005**: A group's catalogue entry MUST present a single price taken from one of its currently
  visible options (the lowest-priced visible option), together with the group's own main picture, so
  the list is usable without opening the group.
- **FR-006**: A group MUST be visible to customers only when at least one of its options is visible;
  a group whose options are all hidden, removed, or nonexistent MUST NOT appear in the list or be
  reachable through any customer-facing route.
- **FR-007**: A link that points at one option of a group MUST resolve and present the group's
  shared content plus that option's own content, so a shared link to an option keeps working.
- **FR-008**: A hidden or unknown group, and a hidden or unknown option, MUST answer the same
  not-found the catalogue already answers, so a response never confirms that a hidden group or
  option exists.

### Functional Requirements — operator maintenance

- **FR-009**: System MUST let an administrator create a variant group with a name, a link segment,
  a description, a category, a position, exactly one choice-dimension name and its own picture
  gallery.
- **FR-010**: System MUST let an administrator attach an existing product to a group as an option by
  giving it an option label, without changing the product's own price, pictures, sell state or link.
- **FR-011**: System MUST let an administrator edit a group's name, link segment, description,
  category, position, choice-dimension name and its own picture gallery, and relabel or detach an
  option. Moving an option to another group is done by detaching it and attaching it again, because a
  product belongs to at most one group at a time (FR-021) and attaching an already-grouped product is
  refused.
- **FR-012**: System MUST let an administrator remove a variant group, and removing a group MUST NOT
  remove the products that were its options.
- **FR-013**: System MUST let an administrator read a group including the choices a customer cannot
  see, so the operator can manage a group that is not currently visible.
- **FR-014**: Every variant-group create, edit, attach, detach and removal MUST be written to the
  audit trail, identifying the group and the acting administrator.
- **FR-015**: Every variant-group maintenance action MUST be refused unless the caller holds the
  administrator role.

### Functional Requirements — independence, money and stock

- **FR-016**: Each option MUST carry its own price and currency, stored as an integer minor-unit
  amount exactly as an ordinary product's price is; an option's price MUST NOT be derived from its
  group or its siblings, and floating point MUST NOT be used.
- **FR-017**: Each option MUST carry its own availability and sell state, independently of its
  siblings: one option MAY be out of stock while another is on sale, and a change to one option's
  stock or state MUST NOT change another's.
- **FR-018**: A variant group MUST carry its own picture gallery, managed exactly as a product's
  pictures are (several pictures, one main, the existing format, size and count rules), and each
  option MUST carry its own pictures with its own main picture. An option's pictures MUST NOT be
  shared with or inherited from its group or siblings, and the group's gallery MUST NOT be replaced
  by an option's pictures.

### Functional Requirements — integrity and constraints

- **FR-019**: A variant group's link segment MUST be unique among variant groups, and an option's
  (product's) link segment MUST remain unique among products, both compared after trimming
  whitespace and ignoring letter case and both refused with the field named on collision — the same
  rule the catalogue already enforces. The two are separate namespaces addressed by distinct
  customer-facing routes, so the same value MAY appear once as a group's link and once as a
  product's link.
- **FR-020**: A variant group's name, link segment and description MUST be bounded exactly as a
  product's are (the catalogue's existing character bounds); a value over its bound MUST be refused
  with the field named.
- **FR-021**: An option label MUST be unique within its group, and a product MUST belong to at most
  one group at a time; both MUST be refused with the field named when violated.
- **FR-022**: The group→option relationship, the option-label uniqueness, and the
  one-group-per-product rule MUST be enforced at the storage layer, so no code path can produce a
  group or option that violates them.
- **FR-023**: A group MUST belong to exactly one category, and creating or editing a group with a
  category that does not exist MUST be refused with the field named.
- **FR-024**: A combo set MUST NOT be an option of a variant group, and a product that is an option
  of a variant group MUST NOT be a combo set; the two roles MUST NOT combine.
- **FR-025**: Removing a group MUST NOT leave any option pointing at a group that no longer exists,
  and MUST NOT delete any option (the specific mechanism is settled in Assumptions).

### Functional Requirements — consistency with the existing catalogue

- **FR-026**: An ordinary product that belongs to no group MUST behave exactly as it does today —
  same visibility, same price, same cart and order behaviour — with no change to any existing
  catalogue, cart, order or inventory behaviour.
- **FR-027**: The visibility rules a group and its options follow MUST reuse the catalogue's
  existing rules (the category must be visible, and a product must be on sale or an announced
  pre-order), so "what a customer sees" keeps one definition across the module.
- **FR-028**: Adding, changing or removing any variant-group route or response field MUST update the
  API documentation and the client hand-off, as the project requires for any client-facing change.

### Key Entities

- **Variant group**: a catalogue entry that offers one product in several interchangeable choices. It
  carries the shared content a customer reads — a name, a link segment, a description, a category, a
  position, the name of its single choice dimension (for example "Card" or "Colour") and its own
  picture gallery. It is a display container: it has no price, no stock and no sell state of its own,
  and it is never bought directly. It is visible to customers only while at least one of its choices
  is visible.
- **Option**: one interchangeable choice within a variant group — for example one specific card. It
  is a product in its own right: it carries its own price and currency, its own availability and
  sell state, its own pictures, and its own link segment, in addition to a short label that is unique
  within its group and is what the customer picks between. It belongs to at most one group at a time,
  and it is what is actually added to a cart and recorded in an order.
- **Option label**: the short text distinguishing one option from its siblings within a group (for
  example "Flins"). It is not the whole product name; it is the value the customer selects in the
  group's single choice dimension.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A customer can discover a grouped product, choose among its options, and buy the
  chosen option without the shop needing one catalogue entry per option — verified by automated test
  on the public list and the buy flow.
- **SC-002**: 100% of tested groups with at least one visible option appear exactly once in the
  public list, and 0% of their options appear as separate entries; a group with no visible option
  appears 0 times.
- **SC-003**: Changing one option's price or stock changes only that option: sibling options keep
  their own price and their own availability, verified by automated test.
- **SC-004**: No tested path can create a group/option combination that violates option-label
  uniqueness, one-group-per-product, or uniqueness of a group's link segment — verified by a test
  that attempts the violation directly against storage.
- **SC-005**: Every ordinary product that belongs to no group behaves exactly as before the change:
  the existing catalogue, cart, order and inventory test suites continue to pass unchanged.
- **SC-006**: Every variant-group maintenance action leaves an audit entry naming the group and the
  operator, and no audit entry contains a customer's personal data.

## Assumptions

- **Chosen defaults (decided with the product owner in this conversation)**:
  - **One choice dimension.** A group offers exactly one dimension of choice (a card, a colour, a
    type). Multi-dimension combinations are deferred and are a separate feature.
  - **Each option is a full, independent product.** Prices, availability and sell state live on the
    option, never on the group; the group is a display container with no price, stock or state.
  - **The customer buys the option.** Cart, order and stock continue to operate on the option as an
    ordinary product, so no existing module's contract changes.
- **The group's catalogue price is taken from one of its options.** Because the group holds no price
  of its own, its list entry shows the price of the lowest-priced currently visible option; grouped
  options are expected to share a price, so this is simply the group's price. A price range is
  deliberately not used.
- **Shared content is not duplicated onto options.** A group's name, description, category, position,
  choice-dimension name and its own picture gallery live on the group; each option's price, stock,
  sell state, pictures and link live on the option. The option's own product name remains whichever
  name the operator gives it, so an operator who wants a fully-qualified order line names the option
  accordingly.
- **Options of a group are listed under the group's category.** The group's category governs where
  the group appears and whether it is visible; the option is reached through the group's shared
  content and does not introduce a second placement for the same group.
- **Removing a group with options is refused.** A group that still has options cannot be removed
  until they are detached, so the removal never deletes or orphans a product; the operator detaches
  the options first. Detaching makes each product an ordinary product again.
- **Group links and product links are two separate namespaces.** A group's link is unique among
  groups and a product's link is unique among products, and the two are addressed by distinct
  customer-facing routes, so the same value may exist once in each. A customer can share either the
  group's link (and choose) or one option's link (and reach it preselected).
- **Text bounds reuse the catalogue's existing numbers**: a group's name at most 120 characters, its
  link segment at most 140, and its description at most 5000, counted in characters on the trimmed
  value, exactly as a product's are, so an operator learns the rule once.
- **Availability per option is not shown on the group page in this feature.** The product module
  cannot read stock, and this feature introduces no new cross-module read; the customer learns a
  chosen option's availability when adding it to the cart, exactly as today. Surfacing per-option
  availability is deferred to a later feature.
- **The existing foundation is reused as-is**: the administrator role, the audit writer, the request
  pipeline, the response envelope, pagination metadata, the media capability and the error catalogue
  are already in place. This feature adds no new infrastructure and no new cross-module contract.
- **Media, money and stock rules are unchanged.** Pictures — for a group and for an option alike — go
  through the existing media capability with the existing format, size and per-owner count rules;
  prices stay integer minor units with an explicit currency; stock stays module 05's concern, reached
  for an option exactly as for any product.
- **The catalogue's existing one-group-per-product scope holds for v1.** A product is an option of at
  most one group; "one product offered under several different groups" is not supported and is
  deferred until a concrete need is agreed.
