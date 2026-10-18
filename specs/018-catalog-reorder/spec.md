# Feature Specification: Catalog Reorder (Drag & Drop)

**Feature Branch**: `018-catalog-reorder`

**Created**: 2026-10-18

**Status**: Draft

**Input**: User description: "Reorder drag & drop for categories, products and product variants; remove manual position input on create/edit; new records appear first automatically; save an entire new order in one action."

## Clarifications

### Session 2026-10-18

- Q: When two administrators save different orders for the same list at nearly the same time, how should the service behave? → A: Last write wins — each save is atomic/all-or-nothing and the later save defines the stored order.
- Q: Should fetching the whole list (unpaginated) for reordering be bounded? → A: Yes — a documented safety maximum applies; a request beyond it is refused with a clear error.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Reorder catalogue records by drag & drop (Priority: P1)

An administrator opens a catalogue management list (categories, products, or the
variants of a product), grabs a row by its drag handle, drops it in a new place,
and saves. The whole list's new order is stored in one action, and that order is
what customers see next time they load the storefront.

**Why this priority**: This is the core of the feature. Today an operator edits a
number field per record, which is slow and error-prone; drag & drop removes the
number entirely and makes the intended order directly expressible.

**Independent Test**: Seed a list of several records, submit a full new order for
them, and confirm the stored order matches the submitted sequence and the
storefront renders in that sequence.

**Acceptance Scenarios**:

1. **Given** an administrator viewing an ordered catalogue list, **When** they drag a record to a new position and save, **Then** the list is stored in exactly that new order.
2. **Given** the saved order, **When** a customer loads the corresponding storefront list, **Then** the records appear in the saved order.
3. **Given** a product with several variants, **When** the administrator puts a different variant first and saves, **Then** that variant is the first one customers see and is the default variant used where a single representative image is needed.

---

### User Story 2 - New records appear first without manual ordering (Priority: P2)

An administrator creates a new category, product or variant and does not supply
any order number. The new record appears at the top of its list, so attention goes
to what was just added; the operator never has to guess a number.

**Why this priority**: It removes the last place where a human types an order
number, and it makes the "newest on top" behaviour the default. It is valuable on
its own even before drag & drop exists.

**Independent Test**: Create a record without an order value and confirm it is the
first entry of its list; then confirm a supplied order value is still honoured.

**Acceptance Scenarios**:

1. **Given** an existing ordered list, **When** a new record is created without an order value, **Then** the new record is the first entry of that list.
2. **Given** a create request that does supply an explicit order value, **When** it is processed, **Then** that explicit value is honoured as before.

---

### User Story 3 - Reorder across the whole list (Priority: P3)

An administrator who manages a large catalogue wants to reorder the list as a
whole, not only the twenty rows visible on the current page. They fetch the
complete list and drag records across the full set.

**Why this priority**: It makes reorder usable on catalogues larger than one page.
Without it, drag & drop would silently reorder only a fragment and produce
unexpected results.

**Independent Test**: With more records than fit on one page, fetch the complete
list, submit a full new order, and confirm every record's position is stored as
submitted.

**Acceptance Scenarios**:

1. **Given** a catalogue with more records than a single page holds, **When** the administrator fetches the complete ordered list, **Then** all records are returned in order.
2. **Given** the complete list, **When** the administrator saves a new order for it, **Then** the stored order exactly matches the submitted order.

---

### Edge Cases

- **Incomplete order**: a submitted order that omits one or more existing records of the list is rejected and the stored order is unchanged.
- **Duplicate entries**: a submitted order that names the same record twice is rejected with its own distinct error and nothing changes.
- **Unknown or foreign record**: a submitted order naming a record that does not exist, or (for variants) a variant that belongs to a different product, is rejected and nothing changes.
- **Empty order**: a submitted order with no records is rejected as invalid.
- **Concurrent edits**: two administrators reordering the same list at once must not leave a partially applied order; each save applies as a whole, and the save that completes last defines the stored order (last write wins).
- **Mixed explicit and default order**: a create that supplies an order value while other records were positioned automatically must still record the explicit value where asked.
- **Reorder of a page fragment**: submitting only the visible rows of a paginated list is treated as an incomplete order and rejected, so a fragment can never corrupt the full order.
- **List larger than the safety maximum**: a complete-list request whose size exceeds the documented maximum is refused with a clear error instead of returning an unbounded result.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: Administrators MUST be able to submit the complete new display order for all categories in a single action.
- **FR-002**: Administrators MUST be able to submit the complete new display order for all products in a single action.
- **FR-003**: Administrators MUST be able to submit the complete new display order for all variants of one product in a single action.
- **FR-004**: A reorder MUST be all-or-nothing: on any rejection, the previously stored order is left unchanged.
- **FR-005**: A reorder that omits any existing sibling record MUST be rejected as a validation error.
- **FR-006**: A reorder that repeats a record identifier MUST be rejected with a distinct, dedicated error code.
- **FR-007**: A reorder naming a record that does not exist MUST be rejected with the corresponding not-found error.
- **FR-008**: A reorder of a product's variants MUST reject any variant that does not belong to that product, with a dedicated error code.
- **FR-009**: A reorder MUST be reachable only by an administrator.
- **FR-010**: A reorder MUST be recorded in the audit trail with the acting administrator, the affected records and the time.
- **FR-011**: When a record is created without an explicit order value, the order value MUST NOT be required and the record MUST default to the front of its list. An edit that omits the order value keeps the record's current order.
- **FR-012**: When a create or edit does supply an explicit order value, that value MUST be honoured (existing behaviour preserved).
- **FR-013**: The customer-facing order MUST reflect the stored order after a reorder.
- **FR-014**: For a product's variants, the chosen order MUST also determine which variant is treated as the default (the first one).
- **FR-015**: Administrators MUST be able to retrieve the complete ordered list of a resource, without being limited to a single page, so that reorder can operate on the whole list. The complete-list retrieval MUST be bounded by a documented maximum; a request that would exceed it MUST be refused with a clear error rather than returning an unbounded result.
- **FR-016**: Product and category display orders MUST each be a single catalogue-wide order (not a separate order per category).

### Key Entities *(include if feature involves data)*

- **Category**: a catalogue grouping; carries one catalogue-wide display order.
- **Product**: a sellable parent; carries one catalogue-wide display order.
- **Variant**: a sellable choice within a product; carries a display order scoped to its parent product; the first variant is the product's default.
- **Reorder request**: the complete, ordered set of identifiers that replaces the current order of one list.
- **Administrator actor**: the authenticated operator performing the reorder; taken from the session, never from the request.
- **Audit entry**: the durable record of a reorder (actor, affected records, time).

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: An administrator can change the order of any catalogue list without typing any number, in a single save action.
- **SC-002**: A newly created record appears at the top of its list with no manual ordering step, 100% of the time.
- **SC-003**: 100% of reorder attempts that omit a record, repeat a record, or name a record of another list are rejected, and the stored order is provably unchanged.
- **SC-004**: After saving, the customer-facing order matches the administrator's submitted order on the next load in 100% of cases.
- **SC-005**: Reordering a list of several hundred records is applied in a single request that does not issue one write per record (no per-record round trips).
- **SC-006**: Every reorder is attributable to a specific administrator in the audit trail.

## Assumptions

- Product ordering is a single catalogue-wide order; it is not re-ordered separately per category.
- Category ordering is a single catalogue-wide order.
- Variant ordering is scoped within its parent product.
- A reorder always carries the complete set of sibling records for the list being ordered; partial orders are rejected by design.
- Explicit order values on create/edit remain supported for backward compatibility, even though the administrator interface no longer presents an order input.
- Administrator list views can be fetched in full (beyond the default page) to support reordering the whole list.
- The administrator interface provides drag handles and saves the full order to the service.
- The order field continues to accept any whole number (including zero and negative) where supplied explicitly.
- The existing customer-facing visibility rules (on-sale variants, visible products/categories) are unchanged; only ordering is affected.
