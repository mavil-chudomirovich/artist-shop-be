# Specification Quality Checklist: Product Catalogue

**Purpose**: Validate specification completeness and quality before proceeding to planning
**Created**: 2026-10-07
**Feature**: [spec.md](../spec.md)

## Content Quality

- [x] No implementation details (languages, frameworks, APIs)
- [x] Focused on user value and business needs
- [x] Written for non-technical stakeholders
- [x] All mandatory sections completed

## Requirement Completeness

- [x] No [NEEDS CLARIFICATION] markers remain
- [x] Requirements are testable and unambiguous
- [x] Success criteria are measurable
- [x] Success criteria are technology-agnostic (no implementation details)
- [x] All acceptance scenarios are defined
- [x] Edge cases are identified
- [x] Scope is clearly bounded
- [x] Dependencies and assumptions identified

## Feature Readiness

- [x] All functional requirements have clear acceptance criteria
- [x] User scenarios cover primary flows
- [x] Feature meets measurable outcomes defined in Success Criteria
- [x] No implementation details leak into specification

## Notes

- Items marked incomplete require spec updates before `/speckit.clarify` or `/speckit.plan`

### Validation run 2 — 2026-10-07

**All items pass.** The three markers were resolved by the operator on 2026-10-07 and the
answers are recorded in the specification's `Clarifications` section. The FR numbers below are
the current ones (the requirements shifted once when `/speckit.clarify` inserted the ordering
requirement as FR-009 — see run 3):

| Marker | Question | Answer chosen |
|---|---|---|
| FR-038 | Does a product carry a stock quantity? | No — out-of-stock is an operator's act; stock is module 05's. |
| FR-039 | How is a combo set priced? | A set is a product in its own right with a price the operator sets; members do not decide it. |
| FR-040 | What is a pre-order? | A label plus an optional expected date on an ordinary product. |

Each answer is reflected where it bites, not only in `Clarifications`: FR-038 to FR-040 carry the
settled rule, the Scope boundary no longer calls items 5 and 6 open, `Key Entities` describes set
membership as display-only, and `Assumptions` records that neither answer adds a sell state.

### Validation run 3 — 2026-10-07 (`/speckit.clarify`)

**All items still pass; no item changed state (16/16 → 16/16).** Five questions were asked and
answered, and every answer was integrated into the sections it affects rather than only logged:

| # | Question | Answer | Sections touched |
|---|---|---|---|
| Q1 | Which unlaunched products may a customer see? | Only those labelled as pre-orders | FR-002, FR-003, FR-004, FR-005, FR-006, FR-008, US1, SC-001, SC-003, Key Entities |
| Q2 | What are the allowed sell-state changes? | The module document's chain plus the restock direction; retired is terminal | FR-021, FR-025, Assumptions |
| Q3 | What are the length and picture bounds? | name 120, link segment 140, description 5000, at most 10 pictures | FR-019, FR-020, FR-030, Edge Cases |
| Q4 | How is the public list ordered? | By a position the operator sets, with a deterministic tie-break | New FR-009, FR-010, FR-012, SC-009, Key Entities, Edge Cases |
| Q5 | Does hiding a category hide its products? | Yes — a hidden category hides the products inside it | FR-002, FR-006, SC-003, Edge Cases, Assumptions |

Q1 resolved a real contradiction: FR-002 said the public list carried only on-sale products while
US6 said a pre-order is visible before launch. The two are now separated into *visible* (on sale
or a pre-order, in a visible category) and *buyable* (on sale only), and the whole spec uses those
two words consistently.

Q4 required inserting a requirement mid-list, so FR-009 is the ordering rule and every requirement
after it shifted by one; all internal FR references were renumbered with it and none dangle.

Q5 surfaced a module-boundary consequence that is recorded in `Assumptions`: product visibility now
depends on the category module's display state, so the plan must resolve how that fact is reached
without one module reading another's tables.

The spec is ready for `/speckit.plan`.
