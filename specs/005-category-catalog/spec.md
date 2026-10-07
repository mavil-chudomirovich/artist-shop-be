# Feature Specification: Category Catalogue

**Feature Branch**: `005-category-catalog`

**Created**: 2026-10-07

**Status**: Draft

**Input**: User description: "Thực hiện module tiếp theo" — module 03 Category (`docs/modules/03-category.md`): phân loại sản phẩm để khách duyệt và tìm kiếm.

## Clarifications

### Session 2026-10-07

- Q: Nhãn công khai (slug) của danh mục do hệ thống tự sinh hay do người vận hành viết, và có
  sửa được không → A: **Do người vận hành viết và sửa được**, hệ thống kiểm tra duy nhất. Lý do:
  `slug` là một trường riêng trong data model của `docs/modules/03-category.md`, không phải trường
  phái sinh, và đây là thông lệ của các nền tảng thương mại. Hai lựa chọn còn lại đều có lỗi
  không sửa được (tự sinh rồi đóng băng: gõ sai một lần là sai mãi) hoặc tự phá link (tự sinh rồi
  đổi theo tên). Đánh đổi được chấp nhận: người vận hành **có thể** đổi nhãn và làm hỏng một link
  đã lưu — nhưng đó là lựa chọn có ý thức của họ, và hệ thống không tự làm điều đó sau lưng họ.
- Q: `slug` được phép chứa ký tự gì, và xử lý dấu tiếng Việt thế nào → A: **Bắt buộc mẫu an toàn
  URL**: chữ thường không dấu, chữ số, gạch nối đơn, không gạch nối đầu/cuối. Hệ thống có thể gợi ý
  slug từ tên nhưng giá trị lưu là do operator viết. Lý do: slug xuất hiện trong URL công khai nên
  phải an toàn cho URL; giữ dấu thì mọi ký tự có dấu phải mã hoá phần trăm, link dài và nhiều cạnh
  biên hơn. Đánh đổi được chấp nhận: slug khác tên về hình thức (đã được FR-017 cho phép), và
  operator phải tự gõ dạng không dấu — hệ thống chỉ gợi ý, không tự ghi đè.
- Q: Giới hạn độ dài của `name`, `slug`, `description` → A: **`name` ≤ 120 ký tự, `slug` ≤ 140,
  `description` ≤ 2000**, đếm theo **ký tự** trên giá trị đã trim. Lý do: tên danh mục tiếng Việt
  dài nhất cũng khoảng 40 ký tự nên 120 rất thoáng; 2000 đủ cho một đoạn mô tả dài mà vẫn chặn được
  nội dung nhét vào. Đếm theo ký tự chứ không theo byte là bắt buộc với tiếng Việt — 120 ký tự có
  dấu là khoảng 360 byte, đếm byte sẽ từ chối oan ở khoảng một phần ba độ dài cho phép.

## Scope boundary

`docs/modules/03-category.md` lists three things under MVP scope. Two are deliverable now; one
is not, and the reason is a fact about the repository rather than a preference:

1. **Admin creates, edits and removes categories.** Deliverable.
2. **Customers browse the list of categories.** Deliverable.
3. **Customers view products belonging to a category.** **Not deliverable in this feature:**
there is no product entity yet. `docs/product/backend-spec.md` lists a product entity among the
things the shop will hold, and nothing in the delivered system stores one — the only things
stored today are accounts, addresses and the audit trail. Module 04 Product owns that entity.
Delivering "products by category"
   here would mean inventing a product placeholder, which is a different feature's data model.

The same fact limits one of the module document's own completion criteria: "the constraint on
removing a category that still has products, with a test". A category cannot be checked against
products that do not exist. This feature therefore establishes the **remove rule**, and defines
the link a product will later use to belong to a category, so the check becomes enforceable the
moment products exist. Which half is verifiable now is stated plainly in Assumptions.

The module document records one open question — whether MVP categories are flat or nested — and
already answers it: *"MVP dùng danh mục phẳng (một cấp) → mặc định: phẳng"*. This feature takes
that answer as settled and does not re-open it.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - A customer browses the catalogue (Priority: P1)

A visitor opens the shop and sees the categories on offer, in the order the shopkeeper chose,
each with the name and description the shopkeeper wrote. Selecting one shows that category's
own page. Categories the shopkeeper has taken off display are nowhere to be seen.

**Why this priority**: This is the only part of the feature a customer ever touches, and it is
the reason categories exist — without it nothing can be browsed. It is also deliverable with no
dependency on any other module.

**Independent Test**: With categories created, some hidden, request the public list and confirm
each visible one appears in the configured order with its name and description, and that no
hidden one appears. Delivers value on its own.

**Acceptance Scenarios**:

1. **Given** several visible categories in a chosen order, **When** a visitor requests the
   list, **Then** they receive every visible category, ordered as configured.
2. **Given** a hidden category, **When** a visitor requests the list, **Then** it does not
   appear, and it is not counted among the results.
3. **Given** a hidden category, **When** a visitor requests that category by its public
   identifier, **Then** the response is a not-found, the same one an unknown identifier
   produces — the response must not confirm that a hidden category exists.
4. **Given** a category with a description, **When** a visitor browses, **Then** the name and
   description are returned as the shopkeeper wrote them.
5. **Given** the catalogue is empty or every category is hidden, **When** a visitor requests
   the list, **Then** they receive an empty result rather than an error.

---

### User Story 2 - An operator maintains the catalogue (Priority: P1)

An administrator creates a category, sets how it is described and where it sits in the order,
hides it from customers while it is being prepared, brings it back, corrects its wording, and
removes a category the shop no longer uses.

**Why this priority**: Without it the catalogue can never change, so US1 would only ever show
whatever was seeded by hand. It is equally P1: the two halves ship together.

**Independent Test**: Create, edit, hide, re-show and remove a category through the
administrator's endpoints, confirming after each step what a customer would then see. Delivers
value on its own.

**Acceptance Scenarios**:

1. **Given** an administrator, **When** they create a category with a name and a description,
   **Then** it is created, is visible to customers by default, and receives a stable public
   identifier that does not change afterwards.
2. **Given** an existing category, **When** the administrator changes its name, description or
   position, **Then** the customer-facing view reflects the change on the next request.
3. **Given** a visible category, **When** the administrator hides it, **Then** it disappears
   from the customer-facing list and from direct customer access.
4. **Given** a hidden category, **When** the administrator shows it again, **Then** it
   reappears in its configured position.
5. **Given** an administrator, **When** they supply a slug containing accents, spaces or a
   trailing hyphen, **Then** it is refused with the field named and no category is created.
6. **Given** a category, **When** the administrator removes it, **Then** it no longer appears
   anywhere, and the removal is recorded in the audit trail.
7. **Given** a customer's session, **When** they attempt any maintenance action, **Then** it is
   refused on role grounds.
8. **Given** a category that does not exist, **When** the administrator edits or removes it,
   **Then** the response is a not-found.

---

### User Story 3 - The catalogue cannot be made ambiguous (Priority: P2)

An operator tries to create or rename a category to a name or slug that another
category already uses. The system refuses, and explains which field collided, so the operator
can pick a different one instead of discovering later that two categories are
indistinguishable.

**Why this priority**: P2 because it protects the catalogue's integrity rather than adding a
capability, and because a duplicate can be corrected afterwards. It must still be enforced at
the point of the mistake, not merely audited.

**Independent Test**: Attempt to create and to rename into a collision, in both letter cases
and with surrounding whitespace, and confirm each is refused with the field named while the
existing category is untouched. Delivers value on its own.

**Acceptance Scenarios**:

1. **Given** an existing category named "Tranh sơn dầu", **When** an operator creates another
   one with the same name, **Then** it is refused and the field is named.
2. **Given** the same existing category, **When** an operator creates one whose name differs
   only in letter case or surrounding whitespace, **Then** it is refused.
3. **Given** two categories, **When** an operator renames one onto the other's name, **Then**
   it is refused and both categories are left unchanged.
4. **Given** an operator edits a category without changing its name, **When** the update is
   saved, **Then** it succeeds — a category does not collide with itself.

### Edge Cases

- **Two categories share a position.** The catalogue still returns a stable order: ties are
  broken by creation order and then by identifier, which the operator cannot change by
  accident and which never varies between requests, so inserting a category in the middle
  of a tie does not shuffle the rest.
- **A catalogue with nothing visible in it.** Empty and fully hidden both answer an empty list.
  Neither is an error, and the two are indistinguishable to a visitor.
- **A category with no description.** It is still a valid category and still listed; the
  description is simply absent. It must not be rejected for being incomplete.
- **Two operators create the same name at the same instant.** Whoever loses is told the name
  collided - not that the server failed. Neither creation may produce a duplicate, and the
  loser's request must not look like an outage to the operator who typed it.
- **An operator saves a category without changing its name.** It succeeds. A category must
  never be treated as a duplicate of itself.
- **Names that differ only in letter case, or by surrounding whitespace.** They are the same
  name. This must hold for Vietnamese text as well as plain ASCII, including letters that
  change shape when uppercased.
- **Removing the last remaining category.** The catalogue becomes empty and the public list
  answers an empty list, exactly as it does for a catalogue that never had one.
- **A hidden category requested by a customer.** The answer is identical to the answer for an
  identifier that was never used, so the response cannot be used to find hidden categories.
- **A name over 120 characters, a slug over 140, or a description over 2000.** Each is refused
  with the field named, rather than stored and displayed broken. A Vietnamese name at the
  limit is accepted, because the bound counts characters and not bytes.
- **A position that is zero, negative, or very large.** It is accepted and still orders
  deterministically; position is a preference, not a sequence with holes to fill.
- **An operator changes the slug of a live category.** It succeeds and takes
  effect immediately; the catalogue then advertises the new slug. This is the one edit that
  can break a link a customer saved, so it is the operator's deliberate act, never a side
  effect of renaming the category.
- **Two categories are created with the same slug in the same instant.** Same as the name
  collision: one wins, the loser is told the field collided, and neither produces a duplicate.
- **A slug typed with accents or spaces.** It is refused, naming the field, and the category is
  not saved. The operator is offered a suggestion derived from the name so correcting it is one
  step rather than a guessing game.
- **A slug with a trailing hyphen, or with two hyphens in a row.** It is refused by the
  shape rule itself, so it never reaches the uniqueness check and a near-duplicate link
  cannot be created. The operator is told which field to correct.

## Requirements *(mandatory)*

### Functional Requirements — customer-facing catalogue

- **FR-001**: System MUST expose a public list of categories that a visitor can read without
  authenticating.
- **FR-002**: The public list MUST contain only categories the operator has left on display.
- **FR-003**: The public list MUST be ordered by the position the operator configured, and the
  order MUST be stable across requests — two categories never swap places between identical
  requests.
- **FR-004**: Each entry MUST carry the public identifier, the name, the slug and the
  description. The slug is what a customer-facing link is built from, so omitting it would
  make the list unusable for reaching a single category.
- **FR-005**: A request for a category that is hidden, removed or unknown MUST answer the same
  not-found, so the response never confirms that a hidden category exists.
- **FR-006**: An empty or fully hidden catalogue MUST answer an empty list, not an error.
- **FR-007**: A public entry MUST carry exactly the fields FR-004 names and nothing else. The
  display state and the position are how the operator manages the catalogue, not part of
  what a customer reads, and the response MUST NOT reveal whether any hidden category exists.

### Functional Requirements — operator maintenance

- **FR-008**: System MUST let an administrator create a category with a name, a description and
  a position.
- **FR-009**: System MUST let an administrator read a single category including its display
  state, so they can see what a customer cannot.
- **FR-010**: System MUST let an administrator change a category's name, description and
  position.
- **FR-011**: System MUST let an administrator take a category off display and put it back
  without destroying it.
- **FR-012**: System MUST let an administrator remove a category.
- **FR-013**: Every create, update, display change and removal MUST be written to the audit
  trail, identifying the category and the acting administrator.
- **FR-014**: Every maintenance action MUST be refused unless the caller holds the
  administrator role.
- **FR-015**: The public identifier of a category MUST be stable for its lifetime, so a link a
  customer saved keeps working.

### Functional Requirements — integrity

- **FR-016**: A category's name MUST be unique across the catalogue, compared after trimming
  surrounding whitespace and ignoring letter case.
- **FR-017**: A category's slug MUST be written by the operator, MUST be editable afterwards,
  and MUST be unique across the catalogue compared the same way. It MUST NOT be derived from
  the name, so the two may deliberately differ: an operator correcting a typo in the name while
  keeping a slug that customers already have is supported, not penalised.
- **FR-018**: A slug MUST NOT carry accented characters or spaces. The system MUST reject a
  slug that does not consist of lowercase unaccented letters, digits and single hyphens, with no
  leading or trailing hyphen, and MUST name the field so the operator can correct it. The
  system MAY offer a suggestion derived from the name, but the stored slug is always the
  operator's, never an automatic rewrite of it.
- **FR-019**: A name, a slug and a description MUST each be bounded: 120, 140 and 2000 characters
  respectively, counted in characters rather than bytes on the trimmed value, so a Vietnamese name
  that occupies three bytes per character is not refused at a third of its allowance. A value over
  its bound MUST be refused with the field named.
- **FR-020**: A collision MUST be refused and MUST name the field that collided, so the operator
  can correct it in one step.
- **FR-021**: System MUST enforce both uniques at the storage layer, so a defect in any single
  code path cannot produce a duplicate.
- **FR-022**: Editing a category without changing the colliding field MUST succeed — a category
  never collides with itself.
- **FR-023**: The identifier a product will later use to belong to a category MUST be unique and
  MUST never change or be reused for the lifetime of the catalogue, so that a reference a product
  stores can never come to mean a different category. The removal rule MUST be defined such that
  a product referencing a category will be able to block that category's removal at the storage
  layer. The reference itself belongs on the product, so making the block enforceable is module
  04's work; this requirement guarantees the half that lives here.

### Functional Requirements — provider-verifiable behaviour

- **FR-024**: Removing a category MUST leave no trace of it in the customer-facing catalogue,
  including in the ordering of what remains.

### Key Entities

- **Category**: a way of grouping what the shop sells. Carries the name the operator writes,
  the description shown to customers, a slug used in links, a position that decides the
  order customers see, and whether it is currently on display. Its public identifier never
  changes once created.
- **Display state**: whether a category is on display or withheld. Withholding preserves
  everything about the category and only removes it from the customer's view; it is not a
  deletion.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A visitor can discover the full visible catalogue in a single request, with no
  hidden category present and no follow-up request needed to learn the order.
- **SC-002**: Two identical requests for the catalogue return the same order, every time,
  including immediately after another category is added or moved.
- **SC-003**: Every attempt to create a duplicate name or slug is refused, across letter case
  and surrounding whitespace, and every slug that is not URL-safe is refused before it is stored;
  in both cases the operator is told which field to change. Verified by automated test rather
  than by inspection.
- **SC-004**: A hidden category is unreachable by a visitor through any customer-facing route;
  a request for it is indistinguishable from a request for a category that never existed.
- **SC-005**: Every maintenance action leaves an audit entry naming the category and the
  operator, and no audit entry contains a customer's personal data.
- **SC-006**: The identifier the system assigns to a category never changes, and editing a
  category's name, description or position never changes its slug — so a link
  that resolved before such an edit still resolves after it. The slug changes only when the
  operator changes it deliberately, and that is the one edit that can invalidate a saved link.

## Assumptions

- **Both lists are paginated**, following the project's existing list convention: a page
  selector with a default size of 20 and a permitted range of 1 to 100, answering the same
  metadata the existing paginated endpoints answer. Unlike the divisions reference data, the
  catalogue is created by the operator and has no natural bound, so it takes the ordinary rule
  rather than the documented exception.
- **No dedicated rate limit is added for this module.** The shared request limit applies. The
  write endpoints are administrator-only, so the abuse surface the module-specific limits of
  feature 003 exist to protect — any authenticated customer hammering a write endpoint — does
  not exist here. Revisit if the maintenance endpoints ever become reachable by more than the
  administrator role.
- **Ordering ties break deterministically** on creation order and then on the identifier. The
  operator sets a position and nothing else; the tie-break exists so the order is never left to
  chance, and is not something the operator needs to understand or configure.

- **MVP categories are flat**, as `docs/modules/03-category.md` already decided. No parent or
  child relationship is modelled. Nested categories stay deferred until a concrete need is
  agreed, at which point they are a new specification.
- **"Products belonging to a category" is delivered by module 04 Product**, not here, because
  no product entity exists. This feature delivers everything about categories that does not
  require a product, and records what it cannot yet verify.
- **The removal rule is established now, its product-dependent half is not verifiable now.**
  This feature defines what removing a category means and gives a future product row the
  reference it will need (FR-021). The check "refuse while products reference this category"
  becomes testable only once products exist, and is carried forward to module 04's scope rather
  than asserted here against nothing.
- **Advanced attribute filtering and tagging stay deferred**, as the module document states.
- The existing foundation is reused as-is: the administrator role already exists, the audit
  writer already exists, and the request pipeline, response envelope and error catalogue are
  already in place. This feature adds no new infrastructure.
- Authentication follows the existing scheme; no new credential handling is introduced.
- A category's position is an operator-chosen whole number. Where two categories share a
  position, the catalogue still returns a stable order — the tie is broken deterministically
  rather than left to chance.
