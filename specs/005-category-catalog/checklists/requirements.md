# Specification Quality Checklist: Category Catalogue

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

Two validation iterations were run. All items now pass.

**Iteration 1 — one item failed; three corrections made in the same iteration.**

- *Requirement Completeness → No `[NEEDS CLARIFICATION]` markers remain*: **FAIL**. One marker
  remained on FR-017, covering where a category's customer-facing label comes from and whether
  it may change. Three defensible answers existed with materially different consequences for a
  link a customer has already saved, and neither `docs/modules/03-category.md` nor the backend
  specification said who owns the label. Raised as Q1 and settled by the product owner: the
  operator writes it and may edit it, and the system enforces uniqueness.

  Other corrections in the same iteration:

  - *Content Quality → No implementation details*: the Scope boundary explained the missing
    product entity in terms of a database table and a migration that does not exist. Rewritten
    as what the shop does and does not store, which makes the same point to a reader who does
    not work in the repository.
  - *Requirement Completeness → Edge cases are identified*: this section did not exist while
    the item was marked as passing. Twelve edge cases were written, including the two that
    matter most here — a duplicate label created concurrently must tell the loser the field
    collided rather than reporting a failure, and changing the label is the one edit that can
    invalidate a saved link and must be the operator's deliberate act rather than a side effect
    of renaming.
  - *Feature Readiness → Success criteria measurable*: SC-006 claimed a public identifier never
    changes while the answer to Q1 allows the operator to change the label. Rewritten to
    distinguish the identifier the system assigns, which never changes, from the label the
    operator owns, which changes only when they change it.

**Iteration 2 — all items pass.**

## Open Questions

None. Every decision this specification depended on is settled and recorded.

## Deliberate scope decisions, recorded rather than asked

Each had only one answer the repository permits, so offering choices would have been theatre:

- **Products by category** belongs to module 04: no product entity exists, so the requirement
  cannot be built here without inventing another feature's data model.
- **The product-dependent half of the removal constraint** cannot be verified now, for the same
  reason. The rule and the link a product will use are both defined here, so module 04 inherits
  a complete constraint rather than an unfinished one.
- **Flat rather than nested categories** is already answered by the module document
  (*"mặc định: phẳng"*). Re-opening an answered question wastes the reader's attention.
