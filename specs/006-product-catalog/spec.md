# Feature Specification: Product Catalogue

**Feature Branch**: `006-product-catalog`

**Created**: 2026-10-07

**Status**: Draft

**Input**: User description: "làm module tiếp theo" — module 04 Product (`docs/modules/04-product.md`): quản lý sản phẩm vật lý (acrylic stand, shikishi, badge, print, keychain, combo set) và trạng thái bán, làm nền cho giỏ hàng, đơn hàng và kho.

## Clarifications

### Session 2026-10-07

- Q: Sản phẩm có mang cột tồn kho ngay trong feature này không, hay "hết hàng" chỉ do operator
  đặt cho tới khi module 05 Inventory tồn tại → A: **Không có cột tồn kho**. `OUT_OF_STOCK` do
  operator đặt; toàn bộ việc theo dõi tồn kho và tự chuyển trạng thái thuộc module 05. Lý do:
  luồng "trạng thái tự chuyển khi hết/hồi kho" mà `docs/modules/04-product.md` mô tả là phối hợp
  với inventory, mà inventory là module 05 và **chưa tồn tại** — không có thực thể kho nào trong
  hệ thống đã giao, nên không có gì để quan sát. Thêm cột tồn kho bây giờ là dựng trước data model
  của module sau (Constitution VII, YAGNI), và kéo theo nghĩa vụ `inventory_transactions` của
  Constitution II. Đánh đổi được chấp nhận: trong lúc chờ module 05, `OUT_OF_STOCK` là trạng thái
  vận hành thủ công chứ không tự động — nghĩa vụ tự động hoá được ghi lại để module 05 đóng.
- Q: Combo set được tính giá thế nào → A: **Operator đặt giá cho cả set**. Set là một sản phẩm độc
  lập, có tên, link segment, mô tả, ảnh và **giá riêng do operator nhập**; các sản phẩm trong set
  được ghi nhận nhưng **không quyết định giá**. Lý do: `docs/modules/04-product.md` ghi đây là câu
  hỏi mở, và hai lựa chọn là hai data model khác nhau chứ không phải hai cách diễn đạt. Giá cố định
  không cần bảng tham chiếu sản phẩm-tới-sản-phẩm, và giá không lệch khi thành phần đổi giá.
  Đánh đổi được chấp nhận: operator có thể đặt giá set cao hơn hoặc thấp hơn tổng thành phần — đó là
  quyết định kinh doanh của họ, và hệ thống không tự tính thay.
- Q: Pre-order ở đây là gì → A: **Nhãn + ngày dự kiến trên sản phẩm bình thường**. Pre-order là một
  cờ trên sản phẩm, kèm **ngày dự kiến có hàng tuỳ chọn**; khách thấy đây là hàng sắp về và **chưa
  mua được** cho tới khi sản phẩm được mở bán. Lý do: `docs/modules/04-product.md` cũng ghi đây là
  câu hỏi mở, và pre-order trong module doc là một thông báo chứ không phải một cách bán. Thêm một
  trạng thái bán riêng sẽ đụng vào state machine ở FR-022 và kéo theo đếm số lượng theo đợt — độ
  phức tạp không có nhu cầu tương xứng ở MVP. Đánh đổi được chấp nhận: chưa giới hạn được số lượng
  nhận trước theo từng đợt; nếu cần, đó là một feature riêng vì nó đổi data model và state machine.
- Q: FR-002 nói danh sách công khai **chỉ** chứa sản phẩm đang bán và loại sản phẩm "đã công bố
  nhưng chưa mở bán", trong khi US6/FR-040 lại nói khách **thấy** được sản phẩm pre-order khi chưa
  mở bán — khách thấy những sản phẩm chưa mở bán nào → A: **Chỉ sản phẩm gắn nhãn pre-order**. Một
  sản phẩm đã công bố nhưng chưa mở bán và **không** gắn pre-order vẫn ẩn; pre-order là ngoại lệ
  duy nhất được hiện khi chưa mở bán. Lý do: đây chính là ý nghĩa của câu trả lời trước — pre-order
  là một thông báo cho khách, nên nếu nó cũng bị ẩn thì nó không thông báo được gì. Đánh đổi được
  chấp nhận: "hiện cho khách" không còn đồng nghĩa với "đang bán", nên spec phải nói rõ **hai** điều
  kiện hiện (đang bán **hoặc** đang pre-order) và tách nó khỏi điều kiện **mua được** (chỉ đang bán).
- Q: Bộ chuyển trạng thái bán được phép là gì → A: **Chuỗi của module doc cộng chiều hồi kho,
  retired là cuối**. Cụ thể: `đã công bố → đang bán`; `đang bán ↔ hết hàng` (hai chiều, để hồi kho
  đưa sản phẩm trở lại bán); `bất kỳ trạng thái → retired`; retired là trạng thái cuối và **không**
  có đường quay lại `đã công bố`. Lý do: `docs/modules/04-product.md` mô tả chuỗi một chiều
  `COMING_SOON → ACTIVE → OUT_OF_STOCK → DISCONTINUED`, nhưng một chiều thuần sẽ khiến sản phẩm hết
  hàng **không bao giờ** bán lại được — phải tạo sản phẩm mới và link cũ chết — trong khi hồi kho là
  việc bình thường của một cửa hàng. Đánh đổi được chấp nhận: chiều hồi kho là một bổ sung so với
  chuỗi vẽ trong module doc, nên nó được ghi rõ ở đây thay vì suy ra. Chiều "rút về chưa ra mắt"
  **không** được thêm: nó sẽ làm `đã công bố` không còn là trạng thái đầu, và module doc không mô tả
  nó.
- Q: Giới hạn cụ thể cho `name`, link segment, `description` và số ảnh mỗi sản phẩm → A: **`name` ≤
  120 ký tự, link segment ≤ 140, `description` ≤ 5000, tối đa 10 ảnh**, đếm theo **ký tự** trên giá
  trị đã trim. Lý do: `name` và link segment lấy **đúng** giới hạn mà module 03 đã dùng (120/140), để
  operator học luật một lần thay vì nhớ hai bộ số; `description` nới lên 5000 vì mô tả sản phẩm cần
  chi tiết hơn mô tả danh mục (chất liệu, kích thước, tình trạng), còn 2000 dễ chặt oan. Đếm theo ký
  tự chứ không theo byte là bắt buộc với tiếng Việt — 120 ký tự có dấu là khoảng 360 byte. 10 ảnh là
  mức đủ cho một bộ ảnh sản phẩm thông thường và vẫn chặn được việc dùng một sản phẩm làm nơi tải lên
  không giới hạn.
- Q: Danh sách sản phẩm công khai sắp theo gì → A: **Operator đặt thứ tự**, giống hệt module 03.
  Sản phẩm có một trường thứ tự do operator đặt; ties phá tất định bằng thời điểm tạo rồi định danh
  (FR-009). Lý do: spec ban đầu **không nói gì** về thứ tự, mà module 03 đã có hẳn một FR cho việc này
  và SC-002 của nó cam kết thứ tự tất định giữa hai request giống nhau — bỏ trống ở module 04 là một
  lỗ hổng, không phải một lựa chọn. Giữ cùng cách với module 03 để operator học luật một lần và để
  hai danh sách hành xử giống nhau. Đánh đổi được chấp nhận: thêm một cột thứ tự và một phần UI sắp
  xếp; đổi lại operator kiểm soát được cái gì lên đầu trang chủ.
- Q: Khi operator ẩn một danh mục (cờ `is_visible` của module 03), sản phẩm thuộc danh mục đó có bị
  ẩn theo không → A: **Có — ẩn danh mục ẩn luôn sản phẩm bên trong** (FR-002). Lý do: module 03 định
  nghĩa ẩn danh mục là "giấu đi trong lúc đang chuẩn bị"; nếu sản phẩm bên trong vẫn hiện thì ý định
  đó bị vô hiệu và khách thấy sản phẩm thuộc một mục đã bị giấu. Spec ban đầu không nói gì, tức mặc
  định ngầm là "không" — một lỗ hổng chứ không phải lựa chọn. Đánh đổi được chấp nhận: tính hiện của
  sản phẩm giờ phụ thuộc vào hai thứ (trạng thái bán của chính nó **và** tính hiện của danh mục), nên
  FR-002 phải nêu cả hai điều kiện; sản phẩm **không** bị sửa gì khi danh mục bị ẩn.

## Scope boundary

`docs/modules/04-product.md` lists six things under MVP scope. Five are deliverable now; the
sixth depends on a module that does not exist yet, and the reason is a fact about the repository
rather than a preference.

1. **Customers browse a public list of products and read one product's detail.** Deliverable.
2. **An administrator creates, edits and removes products.** Deliverable.
3. **Product images, several per product with one primary, stored through the media provider.**
   Deliverable: the media capability already exists and is reused, not rebuilt.
4. **A sell state that follows rules, refusing an invalid change.** Deliverable.
5. **Combo sets and pre-orders.** Deliverable. The questions `docs/modules/04-product.md`
   leaves open — how a set is priced, what a pre-order is — are settled in Clarifications: a
   set is a product in its own right carrying a price the operator sets, and a pre-order is a
   label with an optional expected date on an otherwise ordinary product.
6. **The sell state changing by itself when stock runs out or returns.** **Not deliverable in
   this feature:** it is stated as a flow "in coordination with inventory", and inventory is
   module 05 — no stock entity exists anywhere in the delivered system. Nothing here can
   observe stock because nothing stores it, and Clarifications settles that this feature tracks
   no stock at all: leaving sale and returning to it are an operator's acts until module 05
   exists. The automatic half is module 05's to close.

This feature also **inherits two obligations** that feature 005 deliberately left to it, and
neither is optional. Feature 005's `deferred.md` records both with the exact condition that
closes them:

- **D1 — the category a product belongs to.** `products.category_id` references `categories(id)`
  with a restriction that refuses deleting a category while products still point at it. 005
  established the stable, unique identifier a product will reference (its FR-024) and defined
  what removing a category means; it could not add the reference, because the reference lives
  on the product. Until this feature adds it, "a category with products cannot be removed" is
  not enforced anywhere. Closing it here is what makes 005's completion criterion true.
- **D2 — "products belonging to a category".** 005 delivers the whole of categories; listing
  products by category needs a product table. 005's `deferred.md` D4 also settles how this is
  done: it is a query over the products table filtered by category identifier, **not** a call
  into module 03, so it needs no cross-module contract.

The module document's own completion criteria are all in scope: product CRUD with image
management and a public list, all with tests; the sell-state rules with tests including invalid
changes; and price and currency following the constitution.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - A customer browses the shop and inspects a product (Priority: P1)

A visitor opens the shop, sees the products on offer with a picture, a price and a short
description, narrows to one category, and opens a single product to read everything about it.
Products the shopkeeper has not put on sale, and products taken off sale, are not among them —
except one the shopkeeper has announced as a pre-order, which is shown marked as not yet
available.

**Why this priority**: This is the reason products exist and the only part a customer ever
touches; without it nothing can be bought. It also delivers value with no other module present.

**Independent Test**: With products created — some on sale, some not, one of the latter a
pre-order — request the public list, narrow it by a category, and open one product by its public
identifier, confirming exactly the on-sale ones and the pre-order appear with their price, picture
and description, that the pre-order is marked as not yet available, and that the others do not
appear.

**Acceptance Scenarios**:

1. **Given** products on sale, **When** a visitor requests the list, **Then** they receive them
   with the picture, price, name and description the shopkeeper set.
2. **Given** an ordinary product announced but not yet on sale, **When** a visitor requests the
   list, **Then** it does not appear and is not counted among the results.
3. **Given** a product taken off sale, **When** a visitor requests the list, **Then** it does
   not appear.
4. **Given** a hidden product or an unknown identifier, **When** a visitor requests that product
   by its public identifier, **Then** the response is a not-found, the same one an unknown
   identifier produces — the response must not confirm that a hidden product exists.
5. **Given** products spread across several categories, **When** a visitor narrows the list to
   one category, **Then** only that category's on-sale products appear.
6. **Given** the shop has nothing on sale, **When** a visitor requests the list, **Then** they
   receive an empty result rather than an error.
7. **Given** a product announced as a pre-order, **When** a visitor requests the list or that
   product by its public identifier, **Then** it appears, marked as not yet available, and its
   price is not presented as something that can be bought now.

---

### User Story 2 - An operator maintains the products (Priority: P1)

An administrator creates a product, gives it a name, a link segment, a description, a price, a
category and a state, adds several pictures and picks which one is the main one, corrects the
wording afterwards, and removes a product the shop no longer carries.

**Why this priority**: Without it the shop can never change, so US1 would only ever show
whatever was inserted by hand. Equally P1: the two halves ship together.

**Independent Test**: Create a product, add and reorder its pictures, change its name and price,
remove a picture, and remove the product, through the administrator's endpoints, confirming
after each step what a customer would then see.

**Acceptance Scenarios**:

1. **Given** an administrator, **When** they create a product with a name, link segment,
   description, price, currency and category, **Then** it is created and receives a stable
   public identifier that never changes afterwards.
2. **Given** a product, **When** the administrator adds pictures, **Then** each is stored with
   the provider and only a reference to it is kept; the first one added is the main one unless
   the administrator picks another.
3. **Given** a product with several pictures, **When** the administrator removes one, **Then**
   it is gone from the product and the asset is released; the remaining pictures keep their
   order.
4. **Given** a product whose main picture is removed, **When** the removal is saved, **Then**
   the product still has a well-defined main picture among those left, or none if none are left.
5. **Given** a product, **When** the administrator changes its name, description, price or
   position, **Then** the customer-facing view reflects the change on the next request.
6. **Given** an administrator, **When** they supply a link segment containing accents, spaces or
   a trailing hyphen, **Then** it is refused with the field named and no product is created.
7. **Given** a product, **When** the administrator removes it, **Then** it no longer appears
   anywhere and the removal is recorded in the audit trail.
8. **Given** a customer's session, **When** they attempt any maintenance action, **Then** it is
   refused on role grounds.
9. **Given** a product that does not exist, **When** the administrator edits or removes it,
   **Then** the response is a not-found.

---

### User Story 3 - The sell state can be trusted (Priority: P2)

An operator moves a product through its selling life — announced, on sale, out of stock,
retired — and the system refuses a move that makes no sense, telling them why. A product only
becomes buyable when it is on sale, so a customer never sees a price for something that cannot
be bought.

**Why this priority**: P2 because US1 and US2 deliver a working catalogue without it; this adds
the guarantee that the catalogue's claim — "this is for sale" — is true. It must still be
enforced at the point of the change, and it is a stated completion criterion.

**Independent Test**: Drive a product through every allowed state change and confirm each
succeeds, then attempt a change the rules forbid and confirm it is refused with the state named
and the product left in its previous state.

**Acceptance Scenarios**:

1. **Given** a new product, **When** the administrator puts it on sale, **Then** it becomes
   visible to customers.
2. **Given** an on-sale product, **When** the administrator marks it out of stock, **Then** it
   disappears from the customer-facing list and a direct request for it answers not-found.
3. **Given** an out-of-stock product, **When** the administrator puts it back on sale, **Then**
   it reappears.
4. **Given** a product in any state, **When** the administrator retires it, **Then** it leaves
   the customer-facing catalogue for good.
5. **Given** a retired product, **When** the administrator attempts to put it back on sale,
   **Then** it is refused with the state named and the product is left retired.
6. **Given** a product, **When** the administrator attempts a change the rules do not allow,
   **Then** it is refused and the response names the state that made it invalid.

---

### User Story 4 - A category that still has products cannot vanish (Priority: P2)

An operator tries to remove a category that products still belong to. The system refuses, so no
product is ever left pointing at a category that no longer exists.

**Why this priority**: P2 because it protects data the catalogue already has rather than adding a
capability, and because it is the half of feature 005's completion criterion that could not be
built until products existed. It must be enforced where it cannot be bypassed, not only checked
in one code path.

**Independent Test**: Create a category and a product in it, attempt to remove the category, and
confirm it is refused and both survive; then move or remove the product and confirm the category
can then be removed.

**Acceptance Scenarios**:

1. **Given** a category with at least one product, **When** an administrator removes the
   category, **Then** it is refused, the category and its products are untouched, and the
   response explains that products still reference it.
2. **Given** a category whose products have all been moved to another category or removed,
   **When** an administrator removes the category, **Then** it succeeds.
3. **Given** the refusal in scenario 1, **When** it is enforced, **Then** it holds even if the
   application-level check is bypassed — the storage layer itself refuses the removal.

---

### User Story 5 - Combo sets (Priority: P3)

An operator offers several products together as one purchasable set, with a single price and a
single picture, so a customer can buy the bundle in one action.

**Why this priority**: P3 because the shop sells individual items without it, and because
`docs/modules/04-product.md` records its pricing model as an open question rather than a settled
one. It must not block the catalogue shipping.

**Independent Test**: Create a set from existing products, confirm it appears to customers as one
item with one price and one picture, and confirm the products inside it remain individually
listed.

**Acceptance Scenarios**:

1. **Given** products on sale, **When** an administrator creates a set containing them,
   **Then** the set appears to customers as a single item with its own name, price and picture.
2. **Given** a set, **When** a customer browses, **Then** the products inside it are still listed
   individually — a set does not hide its contents from the catalogue.
3. **Given** a set, **When** an administrator removes it, **Then** the products inside it are
   untouched.

---

### User Story 6 - Pre-orders (Priority: P3)

An operator announces a product that is not yet available and takes interest in it before it
exists, so customers can see what is coming.

**Why this priority**: P3 because it is an announcement rather than a way to sell, and because
`docs/modules/04-product.md` records its shape as an open question. The catalogue works without
it.

**Independent Test**: Create a product as a pre-order, confirm a customer sees it as an
announcement rather than as something buyable, and confirm it behaves as an ordinary product
once it goes on sale.

**Acceptance Scenarios**:

1. **Given** an administrator, **When** they mark a product as a pre-order, **Then** a customer
   can see it and can tell it is not yet available.
2. **Given** a pre-order product, **When** a customer attempts to buy it, **Then** it is not
   buyable, because only an on-sale product is.
3. **Given** a pre-order product, **When** it goes on sale, **Then** it becomes buyable and stops
   being announced as a pre-order.

### Edge Cases

- **Two products share a link segment.** It is refused, naming the field, and no duplicate is
  stored — the same rule categories already follow.
- **Two operators create the same link segment at the same instant.** Whoever loses is told the
  segment collided, not that the server failed; neither creation produces a duplicate.
- **Two products share a name.** This is allowed. Products legitimately repeat a name (two
  prints of the same title, a set and its headline item), so only the link segment is unique.
- **Two products share a position.** The list still returns a stable order: ties are broken by
  creation order and then by identifier, which the operator cannot change by accident and which
  never varies between requests, so inserting a product in the middle of a tie does not shuffle
  the rest.
- **A position that is zero, negative, or very large.** It is accepted and still orders
  deterministically; position is a preference, not a sequence with holes to fill.
- **A product with no pictures.** It is a valid product and still listed, with no picture; it
  must not be rejected for being incomplete.
- **A picture that is not an image, or is oversized.** It is refused with the field named,
  before any byte reaches the provider, exactly as an avatar is today.
- **A price that is zero or negative.** Refused, naming the field — nothing is given away by
  accident and a negative price is not a discount.
- **A price at the top of its range.** Accepted and stored exactly; money is never rounded or
  approximated.
- **A product pointing at a category that does not exist.** Refused, naming the field; a product
  must never be created in a category that was never made.
- **A product whose category is removed underneath it.** Cannot happen: the removal is refused
  while the product still references it (US4).
- **A product in a category the operator has hidden.** It leaves the customer-facing list and
  direct access along with the category, without its own sell state changing at all; showing the
  category again brings it back exactly as it was. The product is never modified by the category's
  display state — only its visibility is affected.
- **Filtering by a category the operator has hidden.** The answer is an empty list, identical to
  filtering by a category that does not exist, so the filter reveals nothing about hidden
  categories.
- **Two pictures uploaded at once for the same product.** Both are kept and both get a distinct
  position; neither overwrites the other's place.
- **An operator retires a product that is the only item in a category.** The category survives;
  only the product leaves the catalogue.
- **A product removed while a customer is reading it.** The customer's next request answers
  not-found; nothing is served from a product that no longer exists.
- **A name over 120 characters, a link segment over 140, or a description over 5000.** Each is
  refused with the field named. A Vietnamese description at the limit is accepted, because the
  bound counts characters and not bytes.
- **An eleventh picture on one product.** It is refused with the field named, before storage, so a
  single product cannot become an unbounded upload sink.

## Requirements *(mandatory)*

### Functional Requirements — customer-facing catalogue

- **FR-001**: System MUST expose a public list of products a visitor can read without
  authenticating.
- **FR-002**: A product MUST be visible to customers only when all of these hold: the category it
  belongs to is visible, and it is either on sale or announced as a pre-order. A product whose
  category is hidden, an ordinary product announced but not yet on sale, and a product that is out
  of stock or retired MUST NOT appear in the public list or be reachable through any customer-facing
  route.
- **FR-003**: A request for a product that is hidden or unknown MUST answer the same not-found,
  so the response never confirms that a hidden product exists. A pre-order is visible, so its
  detail is served.
- **FR-004**: Each entry MUST carry the public identifier, the name, the link segment, the price
  with its currency, a picture, and whether the product is a pre-order that is not yet available,
  so the list is usable without a follow-up request.
- **FR-005**: System MUST expose a single product's full detail — the same fields plus the
  description and every picture — to a visitor who requests a visible product.
- **FR-006**: The public list MUST be filterable by category, and the filter MUST return only the
  visible products in that category. Filtering by a hidden or unknown category MUST answer an empty
  list — the same one a category with no visible products answers — so the filter cannot be used to
  learn whether a hidden category exists.
- **FR-007**: An empty or fully hidden catalogue MUST answer an empty list, not an error.
- **FR-008**: A public entry MUST NOT reveal whether any hidden product exists, nor the product's
  administrative state beyond what its visibility already implies.
- **FR-009**: The public list MUST be ordered by a position the operator sets, and that order MUST
  be stable across requests — two products never swap places between identical requests. Where two
  products share a position, the tie MUST be broken deterministically, as the catalogue already
  does for categories.

### Functional Requirements — operator maintenance

- **FR-010**: System MUST let an administrator create a product with a name, a link segment, a
  description, a price, a currency, a category and a position.
- **FR-011**: System MUST let an administrator read a single product including its sell state,
  so they can see what a customer cannot.
- **FR-012**: System MUST let an administrator change a product's name, link segment, description,
  price, category and position.
- **FR-013**: System MUST let an administrator remove a product.
- **FR-014**: Every create, update, picture change, state change and removal MUST be written to
  the audit trail, identifying the product and the acting administrator.
- **FR-015**: Every maintenance action MUST be refused unless the caller holds the administrator
  role.
- **FR-016**: The public identifier of a product MUST be stable for its lifetime, so a link a
  customer saved keeps working.

### Functional Requirements — pictures

- **FR-017**: System MUST let an administrator attach several pictures to a product, ordered,
  with exactly one of them designated the main picture while any picture exists.
- **FR-018**: A picture MUST be stored through the media provider and only a reference to it
  MUST be kept in the service's own storage; image bytes MUST NOT be stored in the database.
- **FR-019**: An uploaded picture MUST be validated before storage: only the supported image
  formats are accepted, decided by the content's own signature rather than by a file name or a
  client-declared type, and the payload size MUST be bounded to the same 2 MB ceiling an avatar
  upload already uses, so images have one size rule rather than two.
- **FR-020**: A product MUST carry at most 10 pictures, and the bound MUST be enforced before
  storage, so an eleventh is refused with the field named and nothing is uploaded.
- **FR-021**: Removing a picture MUST release the stored asset and MUST leave the product with a
  well-defined main picture, or none when no picture remains.

### Functional Requirements — sell state

- **FR-022**: A product's sell state MUST be one of: announced but not yet on sale, on sale, out
  of stock, retired. The allowed changes MUST be exactly: announced → on sale; on sale ↔ out of
  stock, in both directions, so a restock returns a sold-out product to sale; any state → retired;
  and nothing else. Every change MUST be covered by tests, one positive and one negative per
  change, and a change not on this list MUST be refused.
- **FR-023**: A state change MUST go through the defined transition rules; writing a state
  directly, bypassing them, MUST NOT be possible.
- **FR-024**: An invalid state change MUST be refused with an error that names the current state,
  and the product MUST be left in its previous state.
- **FR-025**: Only a product that is on sale MUST be buyable; every other state MUST NOT be,
  which is what keeps a pre-order unbuyable while it is announced.
- **FR-026**: Retiring a product MUST be terminal: a retired product MUST NOT return to sale or
  to any other state.
- **FR-027**: The state a customer-facing response implies MUST be enforced at the point of
  reading, not only at the point of writing, so no route can serve a product the state forbids.

### Functional Requirements — integrity

- **FR-028**: A product's link segment MUST be unique across the catalogue, compared after
  trimming surrounding whitespace and ignoring letter case, and MUST be refused with the field
  named on collision.
- **FR-029**: A link segment MUST NOT carry accented characters or spaces: it MUST consist of
  lowercase unaccented letters, digits and single hyphens, with no leading or trailing hyphen.
  It MUST be written by the operator and MUST be editable afterwards.
- **FR-030**: A name MUST be at most 120 characters, a link segment at most 140, and a description
  at most 5000, each counted in characters rather than bytes on the trimmed value, so Vietnamese
  text is not refused at a fraction of its allowance. A value over its bound MUST be refused with
  the field named.
- **FR-031**: A price MUST be stored as an integer amount in the currency's minor unit together
  with an explicit currency; floating point MUST NOT be used for money, and the stored amount
  MUST equal the amount supplied exactly.
- **FR-032**: A price MUST be positive; zero and negative amounts MUST be refused with the field
  named.
- **FR-033**: A product MUST belong to exactly one category, and creating or editing a product
  with a category that does not exist MUST be refused with the field named.
- **FR-034**: System MUST enforce the link-segment uniqueness, the price rule and the category
  reference at the storage layer, so a defect in any single code path cannot produce a product
  that violates them.

### Functional Requirements — obligations inherited from the category feature

- **FR-035**: A product MUST reference the stable identifier a category exposes, and the
  reference MUST be enforced by the storage layer so a product cannot point at a category that
  was never created.
- **FR-036**: Removing a category that still has products MUST be refused, and the refusal MUST
  hold at the storage layer rather than only in the category module's own code, so no code path
  can leave a product pointing at a removed category. This closes the half of feature 005's
  FR-024 that feature 005 could not enforce.
- **FR-037**: The refusal in FR-036 MUST be reported to the operator in terms they can act on —
  that products still reference the category — rather than as an unexplained server failure.

### Functional Requirements — sets, pre-orders and stock scope

- **FR-038**: A product MUST NOT carry a stock quantity in this feature. Leaving sale and
  returning to it MUST be an administrator's act, because no stock is tracked until module 05
  Inventory exists, and a state derived from a quantity nothing stores could not be true.
- **FR-039**: System MUST let an administrator offer several products together as one purchasable
  set. A set MUST be a product in its own right — its own name, link segment, description,
  pictures and sell state — carrying a price the administrator sets, and that price MUST NOT be
  derived from the products inside it. The products inside a set MUST remain listed
  individually, and removing a set MUST NOT touch them.
- **FR-040**: System MUST let an administrator mark a product as a pre-order. A pre-order MUST
  be an otherwise ordinary product carrying a label and an optional expected-availability date;
  it MUST NOT be buyable while it is announced, and going on sale MUST clear the label.

### Key Entities

- **Product**: one thing the shop sells. Carries the name the operator writes, the description
  customers read, a link segment used in links, a price with its currency, the category it
  belongs to, a position that decides the order customers see, its sell state, whether it is a
  set, and whether it is a pre-order together with an optional expected-availability date. Its
  public identifier never changes once created.
- **Picture**: an image attached to a product, held outside the service by the media provider.
  Carries the reference that identifies the stored asset, its position on the product, and
  whether it is the main one. The service keeps the reference, never the bytes.
- **Sell state**: where a product is in its selling life — announced, on sale, out of stock,
  retired. Being on sale is what makes a product both visible and buyable; every other state
  leaves it unbuyable, and it stays visible only while the pre-order label is set. It is not a
  deletion, and leaving the customer's view is a consequence of it rather than a separate flag.
- **Set membership**: which products make up a set. A set is a product in its own right carrying
  a price the operator sets, so this records what is inside one for display and does not decide
  that price. A product inside a set remains an ordinary product and stays listed on its own.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A visitor can discover every visible product — every on-sale one and every
  pre-order — in a single request, with no hidden product present, and can narrow that request to
  one category without a follow-up request.
- **SC-002**: Every product in the public list carries a price, a currency, a picture and its
  availability, and the price a customer sees is exactly the amount the operator entered —
  verified by automated test rather than by inspection.
- **SC-003**: Every product that is not visible — because it is neither on sale nor a pre-order, or
  because its category is hidden — is unreachable by a visitor through any customer-facing route,
  and a request for it is indistinguishable from a request for a product that never existed; a
  pre-order is reachable and marked as not yet available.
- **SC-004**: Every invalid sell-state change is refused, with the current state named, and the
  product is left unchanged; every allowed change succeeds. Verified by automated test, one
  positive and one negative per change.
- **SC-005**: A category that products still belong to cannot be removed through any code path,
  including one that bypasses the application's own check — verified by a test that attempts the
  removal directly against storage.
- **SC-006**: Every maintenance action leaves an audit entry naming the product and the operator,
  and no audit entry contains a customer's personal data.
- **SC-007**: An image that is not a supported format, or is oversized, or would exceed the
  per-product count, is refused before it is stored, and the operator is told which field to
  change.
- **SC-008**: The identifier the system assigns to a product never changes, so a link that
  resolved before any edit still resolves after it.
- **SC-009**: Two identical requests for the public list return the same order, every time,
  including immediately after another product is added or moved, and narrowing the request to a
  category does not change that.

## Assumptions

- **Both lists are paginated**, following the project's existing list convention: a page selector
  with a default size of 20 and a permitted range of 1 to 100, answering the same metadata the
  existing paginated endpoints answer. The catalogue is created by the operator and has no
  natural bound, so it takes the ordinary rule.
- **A product belongs to exactly one category.** `docs/modules/04-product.md` names "danh mục"
  in the singular among a product's fields, and feature 005 modelled a flat, one-level
  catalogue. Multiple categories per product is a data-model change and stays deferred until a
  concrete need is agreed.
- **Only the link segment is unique; the name is not.** `docs/modules/04-product.md` requires a
  unique slug and says nothing about the name, and product names legitimately repeat. The link
  segment follows exactly the rule categories already use (FR-028, FR-029), so the two modules
  behave the same way and an operator learns the rule once.
- **The image rules are the ones the avatar already enforces**: the same supported formats,
  decided by content signature, and the same 2 MB size ceiling (FR-019), because a second,
  different rule for the same capability would be two rules to keep in step. The per-product count
  is new and is bounded at 10 (FR-020).
- **No dedicated rate limit is added for this module.** The shared request limit applies. The
  write endpoints are administrator-only, so the abuse surface a module-specific limit exists to
  protect — any authenticated customer hammering a write endpoint — does not exist here.
  Revisit if the maintenance endpoints ever become reachable by more than the administrator role.
- **The media capability is reused rather than rebuilt.** It exists today and is owned by another
  module; the constitution requires code shared by more than one module to live in the shared
  area, so extracting it there is a plan-level concern, not a behaviour this specification
  introduces. No new provider integration is written.
- **Product visibility now depends on a fact the category module owns**, and that crosses a module
  boundary. FR-002 makes the category's display state a condition on a product being visible, so
  the products module must learn something the category module stores. The constitution forbids one
  module reading another's tables, and feature 005's `deferred.md` D4 assumed this would be a plain
  join — this answer shows it is not free. How it is resolved (a cross-module contract, or a value
  the products module can hold without asking) belongs to the plan, and a deviation from the
  dependency rule must be justified there rather than assumed. It is recorded here because it is
  the one place this feature's behaviour reaches across a module boundary.
- **The sell state's allowed changes are settled by Clarifications** and stated in full in FR-022:
  the module document's chain plus the restock direction, with retired terminal and no return to
  announced. It is a clarification rather than an assumption because Constitution III requires the
  transition set to be explicit and tested, and a default nobody had confirmed would be exactly
  the ambiguity that rule exists to prevent.
- **Currency is stored per product** as the constitution requires, even though the shop's MVP
  trades in one currency. Storing it explicitly is what makes the price unambiguous and is not a
  multi-currency feature.
- **The existing foundation is reused as-is**: the administrator role, the audit writer, the
  request pipeline, the response envelope, the pagination metadata and the error catalogue are
  already in place. This feature adds no new infrastructure beyond the media extraction noted
  above.
- **Advanced variants (size, colour) and multi-language product content stay deferred**, as
  `docs/modules/04-product.md` states.
- **Stock-driven automatic state changes are out of scope** until module 05 Inventory exists.
  The state is operator-set in this feature and no stock quantity is stored (FR-038); the
  automatic half is module 05's to close, and closing it is what makes the module document's
  third flow true.
- **Sets and pre-orders are settled by Clarifications rather than left open.** A set is a product
  in its own right priced by the operator (FR-039), and a pre-order is a label with an optional
  expected date on an ordinary product (FR-040). Neither adds a state to the sell-state machine,
  so FR-022 is unaffected by them; the pre-order label does change one thing outside that machine
  — it is what makes an unlaunched product visible to customers (FR-002), which is why "visible"
  and "buyable" are stated as two separate conditions rather than one.
