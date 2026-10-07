# Implementation Plan: Category Catalogue

**Branch**: `005-category-catalog` | **Date**: 2026-10-07 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/005-category-catalog/spec.md`

## Summary

Module 03 Category: an operator keeps a flat catalogue of categories, and a visitor reads the
ones on display. Two thirds of the module document's MVP scope is deliverable here; the third
— products belonging to a category — has no entity to build against and belongs to module 04.

This is the first module built entirely on the foundation and module conventions without
inheriting a partially-built feature, so it establishes the pattern for the ten modules after
it: a new module under `internal/modules/category`, one migration, one set of routes mounted
beside the existing three, and no new dependency.

## Technical Context

**Language/Version**: Go 1.26.0

**Primary Dependencies**: Standard library plus what the foundation already provides — the chi
router, pgx, the shared response/error envelope, the shared audit writer and the shared
generic repository. **No new dependency.** `go.mod` and `go.sum` stay untouched.

**Storage**: PostgreSQL 16, one additive migration `migrations/00005_category.sql`. Redis is
not involved: nothing here is cached, rate-limited per module, or short-lived.

**Testing**: `testing` with `net/http/httptest` for the handler and router layers; in-memory
fakes for the use cases; `testcontainers-go` behind the `integration` build tag for the
adapter, including the storage-level uniqueness and ordering guarantees that a fake cannot
prove.

**Target Platform**: Linux server (Docker) in production, local development on Windows via
Docker Desktop.

**Project Type**: web-service

**Performance Goals**: The public catalogue answers inside the platform's existing response
target. The catalogue is small by nature — tens of categories, not thousands — so the goal is
the platform's standing one rather than a new number invented for this module.

**Constraints**: Categories are **flat**; no parent, no nesting. No attributes, tags or
advanced filtering. Writes are administrator-only; the public read is unauthenticated. Both
lists are paginated per the project's list convention. No dedicated module rate limit.

**Scale/Scope**: One new table, seven endpoints (two public, five administrator), one module.
Estimated surface is a new module directory, one migration, one mount line, and the
documentation that must move with them.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-checked after Phase 1 design.*

- [x] **I. Modular Monolith & Clean Architecture**: A new module at `internal/modules/category`
      with the four layers the project uses (`domain`, `application`, `infrastructure`,
      `presentation`), mirroring module 02. `domain` imports only the standard library and
      `internal/share/access`; `application` depends on `domain` types and its own ports;
      `infrastructure` implements those ports; `presentation` translates HTTP and nothing else.
      Cross-module needs go through `internal/contracts/` — and for this module there are none
      yet: nothing consumes categories until module 04 arrives.
- [x] **II. Transactional Integrity**: Not applicable. Nothing here is money, inventory or a
      payment stage, and no operation spans more than one write. The single place a transaction
      could be argued for — making one category the only one at a given position — does not
      exist, because positions are a preference and ties are permitted (FR-003 and the
      ordering tie-break in Assumptions).
- [x] **III. State Machines & Invariants**: A category has one lifecycle with an explicit
      transition — created on display, taken off display, put back, removed — and the model
      holds that transition rather than letting a caller set the flag. Uniqueness of name and
      slug are invariants of the whole catalogue, not of one row, so they are enforced where
      catalogue-wide things can be: the storage layer (FR-021), with the domain rule as the
      message a human reads. Every transition gets a positive and a negative test.
- [x] **IV. Test-First for Critical Logic**: The critical logic here is the invariants and the
      transitions: case- and whitespace-insensitive uniqueness including Vietnamese text, the
      slug shape rule, the not-found equivalence for hidden categories, the ordering
      tie-break, and the display transitions. Each is written as a failing test first. The
      storage-level constraints are additionally proven against real PostgreSQL, because a
      fake that enforces a unique index proves only that the fake was written twice.
- [x] **V. Security & Least Privilege**: Writes are refused unless the caller holds the
      administrator role (FR-014), and the role comes from the session, never from the request.
      The public surface reveals nothing about hidden categories — a hidden category answers
      exactly the not-found an unused identifier answers (FR-005), so the endpoint cannot be
      used to enumerate what the operator has not published. The slug is constrained to
      URL-safe characters (FR-018), which keeps an operator-supplied value from being pasted
      into a link, a path or a log line as something else. No new credential, no new secret.
- [x] **VI. Observability**: Every create, edit, display change and removal writes an audit
      entry naming the category and the administrator (FR-013), reusing the shared audit
      writer rather than a second one. The public read is not audited: it is an unauthenticated
      browse, and recording every visitor would create a privacy problem the module does not
      need to solve.
- [x] **VII. Simplicity (YAGNI)**: Flat rather than nested, because the module document already
      decided it. No bulk reorder endpoint: an operator edits one position at a time, and the
      deterministic tie-break means the order never depends on the order of edits. No
      attributes or tags. No caching layer: a list of tens of rows read from an indexed table
      does not need one, and adding it would give the module a second source of truth for
      nothing.
│   ├── openapi.yaml     # The seven endpoints
      the refusal semantics change `docs/api-reference.md`, this feature's OpenAPI contract and
      the module document's status in the same change as the code, never after it.

**Post-design re-check**: all eight still hold. Nothing in Phase 0 or Phase 1 forced a violation,
so Complexity Tracking records the two deliberate design choices below rather than a violation —
neither breaks a principle, and each is recorded because it is the kind of choice a later reader
would otherwise question.

## Project Structure

### Documentation (this feature)

```text
specs/005-category-catalog/
├── plan.md              # This file
├── spec.md              # Feature specification
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/
│   ├── openapi.yaml     # The seven endpoints
│   └── error-codes.md   # Every code this module can answer with
├── deferred.md          # What this feature deliberately does not carry
└── checklists/
    └── requirements.md  # Spec quality checklist
```

### Source Code (repository root)

```text
internal/modules/category/
├── domain/
│   ├── constant/
│   │   ├── audit.go          # the audit actions this module records
│   │   └── codes.go          # the CATEGORY_* machine codes
│   ├── error/errors.go       # the module's business sentinel errors
│   ├── model/
│   │   ├── category.go     # the entity, its transitions and its invariants
│   │   └── slug.go         # the slug shape, the bounds and the folding key
│   └── repository/category.go # the persistence contract, no transaction inside it
├── application/
│   ├── dto/dto.go            # use-case input and output types
│   ├── interface/ports.go    # the use-case surface and what it depends on
│   ├── implement/
│   │   ├── catalogue.go      # the public browse use cases
│   │   └── maintenance.go    # the administrator use cases
│   └── mapper/mapper.go      # the single place model becomes dto
├── infrastructure/implement/
│   ├── postgres/category.go  # the adapter, explicit Columns projection
│   └── auditor/auditor.go    # the audit adapter over the shared writer
└── presentation/
    ├── dto/dto.go            # the two HTTP shapes: public and administrator
    └── http/
        ├── handler.go        # request to use case, use case to response
        ├── router.go         # the two route groups
        └── errors.go         # sentinel to status, code and field detail

migrations/00005_category.sql  # the table, its constraints and its indexes
cmd/api/main.go                # one mount line beside the existing three
docs/api-reference.md          # the seven endpoints in the existing format
docs/modules/03-category.md    # status, spec pointer, completion criteria
```

**Structure Decision**: A new module directory mirroring module 02, because module 02 is the
most recently completed module and therefore the cheapest pattern to follow and the easiest to
review against. No `internal/contracts/` entry is added: nothing consumes categories yet, and
inventing a contract before a consumer exists is the premature abstraction Constitution VII
exists to prevent. Module 04 will add it when it has a reason.

## Complexity Tracking

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| A stored, normalised form of the name and the slug, alongside the values the operator typed | FR-016 and FR-017 require comparison that ignores letter case and surrounding whitespace, and only the storage layer can enforce that catalogue-wide (FR-021). Comparing the raw column with `lower(trim(...))` in a query index works, but leaves the rule split across a query and the domain, where the next writer can miss it | Comparing case-insensitively in the application only is the simpler option and was rejected: two concurrent creates both pass an application-level check and both insert, which is exactly the class of defect that produced a stranded-write bug in feature 003. A unique index over a normalised value is the only form the database can enforce |
| The public list and the administrator list are served by two different handlers over one repository | The administrator needs the display state and the position; a customer must not receive either (FR-007). Two response shapes over one read model is less code than one shape with a conditional that has to be right in two directions | A single handler with a flag that decides which fields to emit was rejected: the module already learned in feature 004 what happens when one response shape tries to serve two audiences honestly — the flag silently answers a question one of them cannot know |
