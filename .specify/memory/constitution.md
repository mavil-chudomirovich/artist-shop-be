<!--
Sync Impact Report
==================
Version change: 1.2.0 → 1.3.0 (MINOR)
Bump rationale: Finalized the module layer names and internal layout agreed with
the project: layers `presentation / application / infrastructure / domain` with
sub-structure (`domain/{model,constant,error,repository}`,
`application/{interface,implement,dto,mapper}`,
`infrastructure/implement`, `presentation/{http,cli,worker,dto}`), a generic
repository in `internal/share/repository`, external-service ports and UnitOfWork
in `application/interface`, and a single mapper in `application`.

Modified principles:
- I. Modular Monolith & Clean Architecture (redefined layout)

Changed guidance:
- Layer renamed `controller` → `presentation` (contains http, cli, worker, dto).
- `domain` holds model/constant/error/repository; repository interfaces live here.
- `application` holds interface/implement/dto/mapper; external-service ports and
  `UnitOfWork` live in `application/interface`.
- `infrastructure` holds only `implement` (no interfaces).
- Generic repository interface + base struct live in `internal/share/repository`
  (CRUD + list + pagination, pgx; transactions via context).
- Presentation DTOs and application DTOs are separate; the single mapper lives in
  `application/mapper`.

Added sections: none
Removed sections: none

Templates requiring updates:
- ✅ .specify/memory/constitution.md (this file)
- ✅ doc/architecture.md (authoritative architecture reference)
- ✅ .specify/templates/plan-template.md (gate wording aligned)
- ⚠ .specify/templates/tasks-template.md (no change required)

Follow-up TODOs: refactor module 01 (auth) to the finalized layout.
-->

# Artist Shop Backend Constitution

## Core Principles

### I. Modular Monolith & Clean Architecture (NON-NEGOTIABLE)

The backend is a modular monolith. Each business capability lives in its own
module under `internal/modules/<module>` (auth, user, product, category,
inventory, cart, order, payment, shipping, commission, chat, notification,
review, wishlist, content, admin).

- Every module MUST be organized into exactly four layers, named `presentation`,
  `application`, `infrastructure`, and `domain`.
- The dependency rule is strict and one-directional:
  `presentation → application → domain`; `infrastructure → domain` and
  `infrastructure → application/interface`. `domain` MUST NOT import another
  layer or any framework/library other than the Go standard library and
  dependency-free shared value packages (`share/access`).
- `domain` MUST contain: `model` (entities/value objects), `constant` (business
  constants), `error` (business sentinel errors), and `repository` (repository
  interfaces).
- `application` MUST contain: `interface` (use-case interfaces, external-service
  ports, and the `UnitOfWork` port), `implement` (use cases), `dto`
  (input/output), and a single `mapper` (model ↔ dto). `application` MUST depend
  only on `application/interface` and `domain`, never on `infrastructure`.
- `infrastructure` MUST contain only `implement` (adapters: PostgreSQL, Redis,
  JWT/token, email, audit) and MUST NOT contain interfaces.
- `presentation` MUST contain `http`, `cli`, `worker`, and `dto` (HTTP
  request/response). It depends on `application` and MUST NOT contain business
  rules.
- Repository interfaces live in `domain/repository` and MAY embed the generic
  `share/repository.Repository[T, ID]` interface. Adapters embed
  `share/repository.Base[T, ID]` (CRUD + list + pagination, pgx).
- `application` owns transaction boundaries via the `UnitOfWork` port; repositories
  MUST NOT open transactions themselves.
- A module owns its data and MUST NOT allow other modules to read or write its
  tables directly.
- Code shared by more than one module MUST live in `internal/share/`.
- Cross-module communication MUST go through interfaces in `internal/contracts`;
  the providing module supplies an adapter at the composition root.
- Roles and permissions shared across modules MUST live in
  `internal/share/access`. Middleware MUST accept the required role as a
  parameter and MUST NOT define business roles itself.

Rationale: clean layering keeps business rules independent of frameworks and
storage, makes modules independently testable, and keeps a future extraction path
open while preventing the tangled coupling that makes monoliths unmaintainable.

### II. Transactional Integrity for Money & Inventory (NON-NEGOTIABLE)

Any operation that changes stock, payment state, order totals, or a commission
payment stage MUST execute inside a single database transaction with explicit
rollback on failure.

- All stock changes MUST create an `inventory_transactions` record. A paid
  order decreases stock; a cancelled order restores it; manual restock, damage,
  and adjustment are recorded the same way.
- Monetary values MUST be stored as integer minor units (e.g., VND) together
  with an explicit currency; floating point MUST NOT be used for money.
  Intermediate calculations use a decimal type.
- Payment provider callbacks (MoMo, VietQR) MUST be idempotent: replaying the
  same event MUST NOT double-apply payment or inventory effects. Idempotency
  keys/event IDs MUST be persisted.
- Manual VietQR verification is permitted for MVP but MUST follow the same
  transactional and audit rules as automated providers.

Rationale: money and stock are the highest-risk data in the system; partial
writes, double charges, or inventory drift are unacceptable defects.

### III. Explicit State Machines & Domain Invariants

Order, payment, and commission lifecycles MUST be modeled as explicit state
enums with the set of allowed transitions defined in code and covered by tests.

- Status MUST only change through the domain transition functions; ad-hoc
  status writes are forbidden.
- Invalid transitions MUST be rejected with a typed error and logged.
- Domain invariants (e.g., no work begins before deposit, revision limits,
  slot capacity, final payment before delivery) MUST be enforced in the domain
  layer, not only in HTTP handlers.
- A single canonical commission state machine is authoritative; documentation
  and code MUST NOT diverge.

Rationale: business processes are the product; encoding them explicitly keeps
behavior predictable, auditable, and safe to change.

### IV. Test-First for Critical Logic (NON-NEGOTIABLE)

Pricing, inventory calculation, order totals, commission payment calculation,
and state transitions MUST be developed test-first
(Red → Green → Refactor).

- Tests MUST be written and MUST fail before the corresponding implementation.
- Each state machine transition requires at least one positive and one
  negative (invalid-transition) test.
- API tests MUST cover authentication and authorization boundaries (guest /
  customer / admin, and resource ownership).
- Minimum coverage is enforced for critical packages; coverage percentage MUST
  not decrease on a change.

Rationale: these are the areas where a silent regression causes real financial
and reputation damage.

### V. Security, Privacy & Least Privilege (NON-NEGOTIABLE)

- Passwords MUST be hashed with Argon2id (bcrypt acceptable only as a
  documented fallback). Authentication uses short-lived JWT access tokens plus
  rotating refresh tokens; logout and revocation MUST invalidate refresh
  tokens server-side.
- Every admin endpoint MUST enforce the ADMIN role; every customer-owned
  resource MUST verify ownership, not merely authentication.
- Uploads MUST be validated and whitelisted (type, size, count); secrets MUST
  come from environment/secret storage and MUST NEVER be committed.
- Rate limiting MUST protect authentication and write-heavy endpoints.

Rationale: the platform handles accounts and payments; least privilege and
defense in depth are baseline requirements, not enhancements.

### VI. Observability & Auditability

- Logging MUST be structured (machine-parseable) and include a request/correlation
  ID; logs MUST NOT contain secrets, tokens, or unnecessary PII.
- Administrative mutations and all payment events MUST be recorded in
  `audit_logs` with actor, action, target, and timestamp.
- The service MUST expose health and readiness endpoints used by deployment
  and monitoring.

Rationale: when money or commissions go wrong, the first need is to reconstruct
exactly what happened and who did it.

### VII. Simplicity & Incremental Delivery (YAGNI)

- Start with the modular monolith, a REST API under `/api/v1`, and REST +
  polling for chat. Microservices, WebSocket/real-time transport, and other
  scalability machinery MUST NOT be introduced without measured need.
- Each feature MUST be independently deliverable and testable, with explicit
  contracts.
- Any deviation from these principles MUST be recorded and justified in the
  plan's Complexity Tracking section before implementation.

Rationale: disciplined simplicity keeps a solo/small-team project shipping.

## Technology & Architecture Constraints

- **Language/Runtime**: Go (latest stable minor), PostgreSQL, REST API,
  Docker. Deployment via VPS; frontend on Vercel (out of this repo's scope).
- **API**: base path `/api/v1`. Responses MUST use a consistent envelope and a
  single structured error format; list endpoints MUST support pagination and
  documented filtering/sorting. Backward-incompatible API changes require a
  version bump.
- **Data**: Primary keys MUST be stable and non-guessable (UUID) unless a plan
  documents otherwise. Timestamps are stored in UTC (`timestamptz`). Schema
  changes MUST be applied through versioned, reviewable migrations; destructive
  migrations require an explicit data-migration note.
- **Media**: Cloudinary is the media store. The database persists `public_id`,
  `secure_url`, and metadata; binary media MUST NOT be stored in the database.
- **Payments**: providers (MoMo, VietQR) MUST be accessed only through the
  `PaymentService` abstraction; no provider-specific types leak into domain or
  HTTP layers.
- **Module inventory**: `doc/modules.md` is the authoritative module list and
  roadmap, with one detail file per module under `doc/modules/`. Each module is
  specified and delivered as an independent Spec Kit feature. Adding or removing
  a module requires updating `doc/modules.md` and a constitution-compatible
  plan note; `doc/backend-spec.md` and `doc/project_overview.md` remain the
  original product sources.
- **Source layout**: executables live in `cmd/`; business modules in
  `internal/modules/<module>/{domain,application,infrastructure,presentation}`;
  cross-module interfaces in `internal/contracts`; cross-cutting code shared by
  modules in `internal/share/` (configuration, database, cache, HTTP
  envelope/error helpers, middleware, logging, health, audit, access, generic
  repository, testing support). Migrations are centralized under `migrations/`.
  The `vendor/` directory is not used. There is no `internal/platform` path.
- **Transactions**: `application` owns transaction boundaries through a
  `UnitOfWork` port in `application/interface`; repositories MUST NOT open
  transactions themselves. Multi-repository writes MUST be atomic within one use
  case.
- **Migrations**: a single centralized `migrations/` directory with one runner;
  file names follow `NNNNN_<module>_<description>.sql`.
- **Commands**: `cmd/*` are composition roots and MUST invoke application use
  cases for business behavior; they MUST NOT contain raw business SQL.
  `doc/architecture.md` is the authoritative architecture reference.

## Development Workflow & Quality Gates

- **Spec-driven flow**: constitution → `/speckit.specify` → `/speckit.plan` →
  `/speckit.tasks` → `/speckit.implement` → `/speckit.converge`. Requirements
  and plans MUST precede implementation.
- **Definition of Done**: `gofmt`/`go vet` clean, linter clean, tests passing,
  critical-logic tests present, migrations included, and API/contract docs
  updated for the change.
- **Gates before implementation**: the plan's Constitution Check MUST pass or
  violations MUST be justified in Complexity Tracking. `/speckit.analyze`
  SHOULD be run between `/speckit.tasks` and `/speckit.implement`.
- **Review**: changes require at least one review against this constitution;
  reviewers MUST flag violations rather than silently accepting them.
- **Runtime guidance**: `doc/modules.md` and `doc/modules/*.md` describe the
  module scope and delivery order; `doc/backend-spec.md` and
  `doc/project_overview.md` remain the original product sources. These MUST be
  updated when behavior or module scope changes.

## Governance

This constitution supersedes other development practices when conflicts arise.

- **Amendments**: proposed as a documented change to this file, with the
  rationale, the affected principles, and any migration/compatibility notes.
  Amendments take effect once merged.
- **Versioning policy** (semantic):
  - MAJOR: backward-incompatible governance change, or removal/redefinition of
    a principle.
  - MINOR: a new principle or section, or materially expanded guidance.
  - PATCH: clarifications, wording, or non-semantic refinements.
- **Compliance review**: every plan and PR MUST include a Constitution Check;
  complexity that violates a principle MUST be justified in the plan's
  Complexity Tracking table. Unjustified violations block merge.
- **Precedence**: where `doc/` guidance and this constitution conflict, this
  constitution wins, and the docs MUST be corrected.

**Version**: 1.3.0 | **Ratified**: 2026-09-18 | **Last Amended**: 2026-09-18
