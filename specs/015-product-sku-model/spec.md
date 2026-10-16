# Feature Specification: Product SKU Model

**Feature Branch**: `015-product-sku-model`

**Created**: 2026-10-16

**Status**: Draft

**Input**: User description: "Tái cấu trúc sản phẩm thành mô hình sản phẩm cha + variant (SKU), thay thế cơ chế gom nhóm của feature 014. Sản phẩm có tên, mã sản phẩm, giá mặc định, danh mục, mô tả, slug, bộ ảnh; mỗi sản phẩm có mảng variant với tên, sku, giá riêng (nullable → giá mặc định), ảnh, thứ tự; tồn kho theo variant; giỏ/đơn theo variant; danh mục hiện một mục mỗi sản phẩm, giá thấp nhất; backfill dữ liệu cũ."

## Clarifications

### Session 2026-10-16

- Q: Tồn kho theo dõi theo sản phẩm hay theo từng variant? → A: **Theo từng variant (SKU)**. Mỗi
  variant có tồn riêng; đặt chỗ, bán, hủy đều theo variant. Lý do: sku là đơn vị bán thực tế, và khách
  chọn một variant để mua.
- Q: Dòng giỏ và dòng đơn định danh cái gì khi khách chọn một variant? → A: **Theo variant**. Dòng
  đơn lưu snapshot tên sản phẩm, mã sản phẩm, tên variant, sku, giá, số lượng; **mỗi cặp (mã sản phẩm,
  sku) là một dòng đơn**. Giao diện hiển thị "tên sản phẩm + tên variant". Lý do: cùng một sản phẩm
  phải có nhiều dòng (Flins và Dottore) và đơn phải giữ được sku đã mua.
- Q: Xử lý cơ chế gom nhóm của feature 014 (đã commit, chưa push) thế nào? → A: **Feature này thay
  thế 014**. Cơ chế gom nhóm cũ (nhóm biến thể) bị loại bỏ; mô hình mới là sản phẩm cha + mảng variant.
  Lý do: yêu cầu mới không khớp mô hình "mỗi lựa chọn là một sản phẩm riêng".
- Q: Trạng thái bán (sell state) đặt ở cấp sản phẩm hay cấp variant? → A: **Cấp từng variant**
  (COMING_SOON / ACTIVE / OUT_OF_STOCK / DISCONTINUED). Lý do: một variant có thể hết hàng trong khi
  variant khác còn bán.
- Q: Ảnh lưu ở đâu? → A: **Sản phẩm có bộ ảnh riêng (gallery); mỗi variant có một ảnh riêng**. Lý do:
  trang sản phẩm cần nội dung chung, và mỗi lựa chọn cần ảnh riêng.
- Q: Sản phẩm có bắt buộc ít nhất một variant không, và dữ liệu cũ xử lý thế nào? → A: **Bắt buộc ≥1
  variant**; backfill sản phẩm cũ thành mã sản phẩm = slug và một variant mặc định với sku = mã sản
  phẩm. Lý do: quy tắc "mặc định hiện variant đầu tiên" chỉ có nghĩa khi luôn có variant.
- Q: Nhãn preorder và cờ combo set (is_set) giữ hay bỏ? → A: **Bỏ cả hai** ở phiên bản này; sẽ thay
  bằng chức năng khác sau. Lý do: yêu cầu mới không nhắc tới chúng và chúng làm phức tạp thêm mô hình.
- Q: Giá hiển thị trên card danh mục khi sản phẩm có nhiều variant? → A: **Giá thấp nhất** trong các
  variant đang hiển thị. Lý do: thể hiện mức giá dễ tiếp cận nhất cho khách.
- Q: Trang sản phẩm có hiển thị tình trạng còn hàng theo từng variant? → A: **Có**. Lý do: khách cần
  biết variant nào còn hàng trước khi chọn.
- Q: Mã sản phẩm (code) và sku có bắt buộc và duy nhất không? → A: **Bắt buộc và duy nhất** (mã sản
  phẩm duy nhất trong toàn bộ sản phẩm; sku duy nhất toàn hệ). Backfill: mã = slug, variant mặc định
  sku = mã.
- Q: Thêm vào giỏ có bắt buộc chọn variant không? → A: **Bắt buộc chọn variantId**; không cho thêm ở
  cấp sản phẩm. Lý do: mỗi variant là một dòng giỏ và một đơn vị tồn riêng.
- Q: Sản phẩm có hiển thị khi mọi variant đều hết hàng không? → A: **Không**. Sản phẩm chỉ hiển thị
  khi còn ít nhất một variant **đang bán**; một variant hết hàng vẫn được liệt kê (đánh dấu hết hàng)
  trên một sản phẩm đang hiển thị, nhưng nếu tất cả variant đều hết hàng thì sản phẩm rời danh mục.
  Lý do: bám sát yêu cầu "hiển thị khi còn ít nhất một variant đang bán".
- Q: Variant chuyển trạng thái bán theo bảng nào? → A: **Tái dùng bảng của feature 014 cho từng
  variant**: `COMING_SOON → ACTIVE`; `ACTIVE ↔ OUT_OF_STOCK` (operator đặt hoặc **tự động** khi tồn cắt
  qua 0 / hồi kho); bất kỳ trạng thái trừ `DISCONTINUED` → `DISCONTINUED`; `DISCONTINUED` là trạng thái
  cuối, không quay lại. Lý do: nhất quán state machine hiện có, và module 05 tự gọi transition đã tồn
  tại khi tồn cắt qua 0.

## Scope boundary

Trong phạm vi:

- Sản phẩm trở thành một thực thể **cha** có danh sách **variant** con.
- **Thay thế** cơ chế gom nhóm của feature 014.
- Tồn kho theo từng variant.
- Giỏ hàng và đơn hàng chọn/mua theo variant, có snapshot mã sản phẩm + sku.
- Tái cấu trúc danh mục công khai (một mục mỗi sản phẩm).
- Chuyển đổi (backfill) dữ liệu sản phẩm hiện có.

Ngoài phạm vi (giữ hoãn, ghi rõ để không mất):

- Combo set (nhóm sản phẩm bán chung) — dự kiến làm bằng chức năng khác.
- Pre-order (nhãn đặt trước) — dự kiến làm bằng chức năng khác.
- Biến thể nhiều chiều kết hợp (màu × size) và ma trận tổ hợp.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - A customer picks a variant and buys it (Priority: P1)

A visitor browses the catalogue and sees each product as **one** entry, showing the lowest price among
its variants and the image of its default (first) variant. Opening the product, they read the shared
content — the product name, description and picture gallery — and see the list of variants, each with
its own name, price and stock status. They pick a variant and add it to the cart; the exact variant
chosen (the pair of product code and SKU) is what gets purchased.

**Why this priority**: This is the reason the feature exists; without it a multi-SKU product cannot be
chosen or bought and the restructure delivers no value.

**Independent Test**: Create a product with two variants that differ in price, stock and image (activate
at least one so the product is on the catalogue); confirm the catalogue shows one entry with the lowest
on-sale price and the first variant's image; open it and confirm both variants appear with their own
price and stock status; add an in-stock variant to the cart at its own price; confirm adding a sold-out
variant is refused and adding without a variant is refused.

**Acceptance Scenarios**:

1. **Given** a product with several variants, **When** a visitor requests the catalogue, **Then** the
   product appears as a single entry.
2. **Given** a product, **When** a visitor opens it, **Then** they see the shared name, description and
   picture gallery together with every visible variant's name, price and stock status.
3. **Given** the variant list, **When** a visitor chooses a variant and adds it to the cart, **Then**
   that variant is held in the cart at its own price.
4. **Given** a variant that is sold out, **When** a visitor tries to add it, **Then** it is refused,
   and a sibling variant that is on sale still adds.
5. **Given** a visitor, **When** they try to add a product without choosing a variant, **Then** it is
   refused.

---

### User Story 2 - An operator maintains a product and its variants (Priority: P1)

An administrator creates a product (name, product code, default price, category, description, slug,
picture gallery), then manages its list of variants: adding, editing and removing variants, setting
each variant's name, SKU, price, image, order and sell state.

**Why this priority**: Without it nothing can be arranged; US1 and US2 ship together.

**Independent Test**: Create a product with two variants, change a variant's name, price, image and
order and move its sell state, remove one variant leaving one, confirm removing the last variant is
refused, and confirm a duplicate product code or SKU is refused.

**Acceptance Scenarios**:

1. **Given** an administrator, **When** they create a product, **Then** it requires a name, a product
   code, a default price, a category, a description and a slug, and at least one variant; a new variant
   starts not yet on sale.
2. **Given** a product, **When** a variant's price is left blank, **Then** the variant uses the
   product's default price; when a price is given, it overrides the default.
3. **Given** a SKU another variant already uses, **When** the administrator assigns it, **Then** it is
   refused with the field named.
4. **Given** a product code already in use, **When** the administrator creates another product,
   **Then** it is refused with the field named.
5. **Given** a product with exactly one variant, **When** the administrator removes that variant,
   **Then** it is refused.
6. **Given** a customer's session, **When** they attempt any product or variant maintenance action,
   **Then** it is refused on role grounds, and the attempt is audited.

---

### User Story 3 - Each variant is its own stock and selling unit (Priority: P2)

An operator gives each variant its own stock and sell state. One variant can be out of stock while a
sibling is on sale, and the catalogue, cart, order and stock all treat a variant as the unit of sale.

**Why this priority**: P2 because US1 and US2 deliver a usable multi-SKU product without it, but
per-variant stock and state are what make SKUs realistic.

**Independent Test**: Give two variants different stock and sell states, confirm each shows and charges
its own, sell one out, and confirm only that variant changes while the other stays on sale.

**Acceptance Scenarios**:

1. **Given** two variants, **When** one runs out of stock, **Then** only that variant becomes
   unbuyable while the sibling stays on sale.
2. **Given** a variant on sale, **When** its stock reaches zero, **Then** it is marked out of stock
   (not buyable) but is still shown to customers marked as out of stock.
3. **Given** a variant is bought, **When** stock changes, **Then** only that variant's stock changes.

---

### User Story 4 - Existing products are migrated (Priority: P2)

Every product that already exists is converted into a product with a code and one default variant, so
the "at least one variant" rule holds and the product keeps selling as before.

**Why this priority**: P2 because it protects existing data rather than adding a capability, and it must
be true before the new model can serve the catalogue.

**Independent Test**: On a catalogue that predates the change, confirm every product has a product code
and exactly one default variant, and that buying it behaves as before.

**Acceptance Scenarios**:

1. **Given** an existing product, **When** the migration runs, **Then** it receives a product code
   equal to its slug and one default variant whose SKU equals the code.
2. **Given** a migrated product, **When** a customer buys it, **Then** the purchase flow works as
   before.

### Edge Cases

- **A variant's price is blank.** The variant falls back to the product's default price; a variant
  with an explicit price uses that price.
- **A duplicate product code or a duplicate SKU.** Refused, naming the field; the value cannot be
  stored twice.
- **Removing the last variant of a product.** Refused; every product keeps at least one variant.
- **A variant in COMING_SOON or DISCONTINUED.** Hidden from customers; a variant that is on sale or
  out of stock is shown.
- **Every variant of a product is hidden, or every variant is out of stock.** The product leaves the
  customer-facing catalogue, because a product is shown only while at least one variant is on sale.
- **A variant runs out of stock.** It still appears on the product page, marked as out of stock, and
  cannot be bought.
- **A variant's price changes after it is in a cart.** The price is re-checked at checkout, so the
  customer never silently pays a price they did not see.
- **A product is removed.** Its variants are removed with it; other products are untouched.
- **A product's category is hidden.** The product leaves the catalogue without its variants' sell
  states changing.
- **A product with exactly one variant.** Valid and shown like any other product.

## Requirements *(mandatory)*

### Functional Requirements — customer-facing catalogue

- **FR-001**: System MUST list one catalogue entry per product, with all of a product's variants
  collapsed into that single entry.
- **FR-002**: A product's catalogue entry MUST show the product name, the lowest effective price among
  its **on-sale** variants, and the image of its default (first) variant (falling back to the product's
  primary gallery picture). Only a buyable price is shown on the card, so an out-of-stock variant's
  cheaper price never headlines a product.
- **FR-003**: A product's detail MUST show the shared content — name, description and picture gallery,
  plus a summary of the lowest on-sale variant price and the default variant's image — together with
  every visible variant's name, effective price and stock status (a variant is visible when it is on
  sale or out of stock; one not yet on sale or discontinued is not).
- **FR-004**: A product MUST be visible to customers only when its category is visible and at least one
  of its variants is **on sale**. A product whose variants are all out of stock, not yet on sale or
  retired MUST NOT appear in the customer-facing catalogue.
- **FR-005**: A variant MUST be visible to customers when it is on sale or out of stock; a variant that
  is not yet on sale or retired MUST NOT be visible.
- **FR-006**: A customer MUST choose a variant to add an item to the cart; adding a product without
  choosing a variant MUST be refused.
- **FR-007**: Adding a variant that is not buyable — not on sale, or out of stock — MUST be refused,
  independently of its sibling variants.
- **FR-008**: A hidden or unknown product, and a hidden or unknown variant, MUST answer the same
  not-found on the customer-facing **catalogue** reads, so a response never confirms that a hidden
  product or variant exists. (A cart add is different: a variant that exists but is not buyable is the
  cart's not-purchasable refusal, and only a variant no product carries is a not-found.)

### Functional Requirements — operator maintenance

- **FR-009**: System MUST let an administrator create, edit and remove a product with a name, a
  product code, a default price, a category, a description, a link segment and a picture gallery.
- **FR-010**: System MUST let an administrator manage a product's variants: add, edit and remove a
  variant, setting its name, SKU, price, image and display order, and moving its sell state through the
  transition endpoint.
- **FR-011**: A product code MUST be unique across products, and a SKU MUST be unique across all
  variants; both MUST be required, and a violation MUST be refused naming the field.
- **FR-012**: Every product MUST have at least one variant; removing a product's last variant MUST be
  refused, and removing a product MUST remove its variants without touching other products.
- **FR-013**: Every product and variant maintenance action MUST be refused unless the caller holds the
  administrator role, and MUST be written to the audit trail identifying the target and the acting
  administrator.

### Functional Requirements — variant independence

- **FR-014**: Each variant MUST carry its own stock, changed only by operations on that variant.
- **FR-015**: Each variant MUST carry its own sell state (`COMING_SOON`, `ACTIVE`, `OUT_OF_STOCK`,
  `DISCONTINUED`), independent of its siblings: one variant MAY be `OUT_OF_STOCK` while another is
  `ACTIVE`, and a change to one MUST NOT change another. The allowed changes MUST be exactly:
  `COMING_SOON → ACTIVE`; `ACTIVE ↔ OUT_OF_STOCK`, in both directions (set by the operator, or
  automatically as stock crosses zero and as stock is restocked, in the same transaction as the stock
  change); any state except `DISCONTINUED` → `DISCONTINUED`; and nothing else. `DISCONTINUED` MUST be
  terminal and MUST NOT return to sale. Every change MUST have a positive and a negative test, and a
  change not on this list MUST be refused naming the current state.
- **FR-016**: Each variant MUST carry its own image, stored through the existing media capability (only
  a reference to the stored asset is kept, never the bytes); a variant has at most one image, which may
  be replaced or removed.
- **FR-017**: A variant's price MUST be optional; when absent the variant MUST use the product's
  default price, and when present the variant's price MUST override it.

### Functional Requirements — cart, order and stock

- **FR-018**: A cart line MUST identify the chosen variant; a cart MUST hold at most one line per
  variant, and MUST capture the variant's price when it is added.
- **FR-019**: An order line MUST record a snapshot at purchase time of the product name, product code,
  variant name, SKU, unit price, currency and quantity. The snapshot is kept for the record; the
  customer-facing order view shows the product name and the variant name (the SKU is not shown to the
  customer, per the SKU assumption below).
- **FR-020**: Each (product code, SKU) pair MUST correspond to one order line.
- **FR-021**: Checkout MUST re-check that each ordered variant may be bought, that enough is available,
  and that the captured price still matches the variant's current effective price; if any of these
  changed, the whole checkout MUST be refused and nothing created, so a price or availability change
  after a cart was filled never sells something it should not.
- **FR-022**: Stock reservation, sale and release MUST operate per variant and MUST record every
  physical change as required by the constitution.

### Functional Requirements — data integrity and migration

- **FR-023**: The uniqueness of a product code and of a SKU, and the "at least one variant" rule where
  enforceable, MUST be enforced at the storage layer, not only in application code.
- **FR-024**: Money MUST be stored as an integer amount in the currency's minor unit with an explicit
  currency; floating point MUST NOT be used, and a variant's optional price MUST NOT introduce a second
  currency for the same product.
- **FR-025**: System MUST replace feature 014's variant-grouping mechanism: the grouping structure and
  every route and response it introduced MUST be removed or superseded by the product-and-variants
  model.
- **FR-026**: System MUST migrate every existing product into the new model — assigning a product code
  equal to its slug and creating one default variant whose SKU equals the code — so that no product is
  left without a variant.
- **FR-027**: A migrated product MUST keep selling as it did before the change.

### Functional Requirements — documentation

- **FR-028**: Every added, changed or removed endpoint or client-facing response MUST update the
  authoritative API reference and the client hand-off in the same change.

### Key Entities

- **Product**: the parent a customer browses and describes. Carries a name, a unique product code, a
  default price, the category it belongs to, a description, a link segment, and its own picture
  gallery. It groups one or more variants and is never sold on its own.
- **Variant**: one sellable choice within a product. Carries a display name, a globally unique SKU, an
  optional price (falling back to the product's default price), its own image, a display order, its own
  sell state, and its own stock. It is the unit a customer selects, adds to a cart and buys.
- **Cart line**: one chosen variant with its quantity and the price captured when it was added.
- **Order line**: a snapshot at purchase time of one (product code, SKU) pair — product name, product
  code, variant name, SKU, unit price, currency and quantity.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A customer can discover a product, choose among its variants and buy the chosen variant
  in under 3 minutes without support intervention.
- **SC-002**: 100% of tested products appear exactly once in the catalogue, and 0% of variants appear
  as separate entries.
- **SC-003**: 100% of tested orders store the full (product code, SKU) pair and the price at purchase
  for every line.
- **SC-004**: Changing one variant's stock, price or sell state changes only that variant — verified by
  automated test.
- **SC-005**: 100% of tested existing products, after migration, carry a product code and at least one
  variant, and the existing catalogue, cart, order and inventory suites still pass once updated.
- **SC-006**: No duplicate product code or SKU exists under any write path — verified by attempting the
  violation directly against storage.

## Assumptions

- **Chosen defaults (decided with the product owner in this conversation)**:
  - Stock is tracked per variant; the cart and order act on a variant, and the order keeps the product
    code and SKU it was sold under.
  - Sell state lives on the variant; the product has no sell state of its own.
  - Every product has at least one variant; existing products are migrated to satisfy this.
  - The product keeps its own picture gallery, and each variant has one image.
  - Pre-order and combo-set are dropped for now and will return as a separate capability.
- **The default variant is the one with the smallest display order**, ties broken by creation time and
  then identifier, so "the first variant" is deterministic.
- **A product keeps a link segment** (as today) used for its public link; the product code is a
  separate, operator-facing identifier and is not the link segment.
- **Money is a single currency per product**; a variant carries only an amount and inherits the
  product's currency.
- **Showing per-variant stock status crosses a module boundary** (the product surface needs to know the
  inventory's availability). How that read is provided belongs to the plan; this specification states
  only that the product page shows each variant's status.
- **The migration is additive for existing data**: a product's existing price becomes its default
  price, and its existing sell state becomes the default variant's sell state.
- **The existing foundation is reused as-is**: the administrator role, the audit writer, the media
  capability, the request pipeline, the response envelope, pagination and the error catalogue are
  already in place.
- **Feature 014's grouping artefacts are superseded**, including its data structures, routes and
  documentation; any of its deferred items that still apply (multi-dimension options, per-variant
  availability) carry forward.
- **The SKU is an internal identifier.** It is used by the backend and captured on an order line, but
  it is not shown in customer-facing responses: a customer sees a variant by its display name, and the
  client identifies the chosen variant for the cart by the variant's own identifier, not its SKU. The
  order snapshot keeps the SKU for the record; the customer's order view shows the product name and the
  variant name.
- **A product's position is an optional ordering preference** at creation (defaulting to zero, like the
  catalogue's existing position rule); the catalogue order still comes from `position, created_at, id`.
- **The product gallery is managed through the catalogue's existing product-picture routes**
  (feature 006's image endpoints), which this feature does not change; a variant's single image is set
  on the variant, not in the gallery.
- **The catalogue card image follows the default variant, then falls back.** It shows the default
  (first) variant's image; when that variant has no image it falls back to the product's primary
  gallery picture, and to no image when neither exists.
- **Product code and SKU are operator-written and bounded in length**, and uniqueness is compared after
  trimming surrounding whitespace and ignoring letter case, following the catalogue's existing
  slug rule, so "ABC" and "abc" cannot coexist.
