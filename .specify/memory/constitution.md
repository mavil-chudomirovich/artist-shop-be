<!--
Sync Impact Report
==================
Version change: 1.7.0 → 1.8.0 (MINOR)
Bump rationale: `/swagger` is generated from handler annotations, and the Definition
of Done named only `docs/api-reference.md`, so a module could ship every endpoint
with no annotations and still pass every gate. Feature 006 recorded exactly that gap
(its `deferred.md` D12), and modules 03, 04 and 05 had all drifted out of `/swagger`.
The gap was a missing MUST, not a missing tool: `make swagger-check` compares the
generated spec to the annotations that exist, so it cannot notice an endpoint that
carries none. This amendment makes the handler annotations and the regenerated
`docs/swagger/` part of the Definition of Done, so the rule lives where an agent
already looks.

Modified principles:
- VIII. API Documentation as a Contract — a new bullet requires handler Swagger
  annotations and a regenerated `docs/swagger/` in the same change.

Changed guidance:
- Development Workflow & Quality Gates → Definition of Done now names the
  annotations and the regenerated spec alongside `docs/api-reference.md`.

Added sections: none.
Added Governance rules: none (this extends an existing principle and the DoD).

Templates and agent files requiring updates:
- ✅ .specify/memory/constitution.md (this file)
- ✅ AGENTS.md §4 (endpoint → annotation + `make swagger`)
- ✅ .specify/templates/plan-template.md (Constitution Check gains the annotation gate)
- ✅ .specify/templates/tasks-template.md (a close-out task for annotations + `make swagger`)
- ✅ .opencode/skills/speckit-orchestrate/SKILL.md §3 (`make swagger` with any handler change)
- ✅ .opencode/agents/speckit-worker.md (a mandatory rule for endpoint annotations)

Follow-up TODOs: none.
-->

<!--
Sync Impact Report
==================
Version change: 1.6.0 → 1.7.0 (MINOR)
Bump rationale: Adds the frontend integration guide deliverable and the way it is
kept honest. `docs/api-reference.md` (Principle VIII) documents endpoints once, for
the whole service; it says nothing about what the frontend must do when a contract
changes, and it is written by hand to stay current. Two failure modes follow — a
client-facing change ships with no hand-off note, or the note is written once and
silently rots as implementation moves. The new Governance rules close both.

Added principles: none (Governance rules only).
Modified principles: none.
Added sections: none.

New Governance rules:
- Frontend integration guide: any feature that adds, removes or changes an HTTP
  endpoint, an event payload, or a client-facing DTO MUST ship
  `specs/<feature>/frontend-guide.md`, enumerating each affected endpoint/event with
  its method and path, marking it new/changed/removed, and giving before → after for
  each changed one. A feature with no client-facing change says so in `tasks.md`.
  The guide is the per-feature hand-off and MUST agree with `docs/api-reference.md`,
  which stays the authoritative cross-module reference (Principle VIII).
- Frontend guide sync: the guide is re-read and corrected in the feature's final
  phase and after every convergence pass, comparing against the source with counts
  (JSON fields, enum values, routes, HTTP statuses). A comparison that produces no
  counts did not happen.
- Technical debt found, not fixed: debt found while building a feature goes to
  `specs/<feature>/deferred.md` with what it is, why it is out of scope and what
  would unblock it — never an unchecked task of the feature that found it.

Backfill note: features 001–005 were created before this amendment and are exempt
from the frontend-guide rule. Their guides are backfilled deliberately in the same
change rather than left implicit; 005 is fully implemented, 004 has one task
(T028b) blocked by a mail-relay configuration outside the code.

Templates requiring updates:
- ✅ .specify/memory/constitution.md (this file)
- ✅ .specify/templates/tasks-template.md (Frontend Integration Guide + out-of-scope blocks)
- ✅ .specify/templates/deferred-template.md (created)
- ✅ .specify/templates/plan-template.md (Constitution Check gains the guide gate; layer name corrected to `presentation`)
- ⚠ .specify/templates/spec-template.md (no change — the guide is a close-out artifact, not a specification artefact)
- ✅ .opencode/agents/speckit-worker.md (created, so the orchestrate skill's delegation resolves)
- ✅ .opencode/skills/speckit-orchestrate/SKILL.md (make-target drift corrected for this repo)

Docs requiring manual follow-up:
- specs/001..005/*/frontend-guide.md (created in this change)
- docs/development/agent-workflow.md (frontend guide noted in the task and implement rules)

Follow-up TODOs: none.
-->

<!--
Sync Impact Report
==================
Version change: 1.3.0 → 1.5.0 (MINOR)
Bump rationale: Added the API Documentation principle, the container workflow
section, documentation-location and decision-record rules, and Agent & Commit
Governance. Endpoint drift and unreviewed history rewrites are the two failure
modes this repository cannot detect on its own: Spec Kit keeps one OpenAPI file
per feature, and an automated agent can silently rewrite history unless the rules
forbid it explicitly.

Added principles:
- VIII. API Documentation as a Contract (NON-NEGOTIABLE)

Added sections:
- Container & Local Development Workflow (Dockerfiles, compose stacks, .env)
- Documentation location rule (see Technology & Architecture Constraints)
- Agent & Commit Governance (git authorization, commit format, precedence)
- Decision records (ADR in docs/decisions/)

Structural changes:
- The former `doc/` directory was consolidated into `docs/`, split by role:
  operational guides at the root of `docs/`, process guides in
  `docs/development/`, design references in `docs/system-design/` and at the
  root of `docs/`, decision records in `docs/decisions/`, and the original
  requirement sources moved to `docs/product/`.
- `AGENTS.md` at the repository root is the navigation and gate source of truth
  for automated agents.

Templates requiring updates:
- ✅ .specify/memory/constitution.md (this file)
- ✅ AGENTS.md (agent rules, gate tiers, git policy)
- ✅ docs/README.md (documentation index and rules)
- ✅ docs/api-reference.md (created as the authoritative reference)
- ✅ docs/development/* (process guides)
- ✅ docs/system-design/* (patterns, contract purity)
- ✅ docs/decisions/* (ADR log seeded with 7 recorded decisions)
- ⚠ .specify/templates/plan-template.md (no change required)
- ⚠ .specify/templates/tasks-template.md (no change required)

Follow-up TODOs: none.
-->

<!--
Sync Impact Report
==================
Version change: 1.5.0 → 1.6.0 (MINOR)
Bump rationale: Principle I's domain-import rule was unenforceable as written:
`domain/model` and `domain/repository` must express the UUID primary-key type,
and inventing a second ID type per module would duplicate the concept without
any behavioural gain. The rule now names `github.com/google/uuid` as the one
allowed value-type library and requires the owning feature to record the entry
in Complexity Tracking instead of leaving it implicit.

Modified principles:
- I. Modular Monolith & Clean Architecture (domain import allowlist)

Changed guidance:
- `domain` may import stdlib, `share/access`, and `github.com/google/uuid`.

Structural changes:
- none

Templates requiring updates:
- ✅ .specify/memory/constitution.md (this file)
- ✅ docs/architecture.md (§2 dependency rules)
- ⚠ .specify/templates/plan-template.md (no change required)
- ⚠ .specify/templates/tasks-template.md (no change required)

Follow-up TODOs: none.
-->

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
- ✅ docs/architecture.md (authoritative architecture reference)
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
  layer or any framework/library other than the Go standard library,
  dependency-free shared value packages (`share/access`), and the UUID value
  type library `github.com/google/uuid` (primary keys are UUIDs, so the ID type
  is part of the domain value objects; recorded as a Complexity Tracking entry
  in the owning feature's plan).
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

### VIII. API Documentation as a Contract (NON-NEGOTIABLE)

`docs/api-reference.md` is the authoritative, cross-module reference for every HTTP
endpoint the service exposes. It is a deliverable, not an afterthought.

- Adding, changing, or removing an endpoint MUST update `docs/api-reference.md` in
  the same change. An endpoint whose documentation is missing or stale is an
  incomplete implementation.
- Every HTTP handler MUST carry the Swagger annotations the generator reads
  (`@Summary`, `@Tags`, `@Param`, `@Success`, `@Failure`, `@Router`). Adding,
  changing or removing an endpoint MUST regenerate `docs/swagger/` with `make
  swagger` in the same change, and `make swagger-check` (part of `make check`) MUST
  pass. An endpoint without annotations is an incomplete implementation, exactly as
  an undocumented one is.
- Each endpoint entry MUST state: method and path, authentication requirement,
  rate limit, request body, response body with an example, and the error codes it
  can return. The shared envelope, error catalogue, and conventions MUST be
  documented once, not repeated per endpoint.
- New machine-readable error codes MUST be added to the reference and to the
  feature's `contracts/error-codes.md`.
- Per-feature `specs/<feature>/contracts/openapi.yaml` files remain the
  machine-readable artifacts; when they disagree with `docs/api-reference.md`, the
  reference wins and the OpenAPI file MUST be corrected.
- Breaking changes to an existing endpoint MUST be called out in the reference's
  change log, in addition to the version bump required below.

Rationale: the frontend team and future contributors integrate against this file
rather than against the source. Silent drift between code and documentation costs
more than the few minutes it takes to update a table.

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
- **Module inventory**: `docs/modules.md` is the authoritative module list and
  roadmap, with one detail file per module under `docs/modules/`. Each module is
  specified and delivered as an independent Spec Kit feature. Adding or removing
  a module requires updating `docs/modules.md` and a constitution-compatible
  plan note; `docs/product/backend-spec.md` and `docs/product/project_overview.md` remain the
  original product sources.
- **Documentation**: all project documentation lives under `docs/`; there is no
  other documentation directory. Operational guides (`getting-started`,
  `configuration`, `docker`, `makefile`, `testing`, `troubleshooting`) MUST be
  updated in the same change as the behavior they describe. Design references
  (`docs/api-reference.md`, `docs/architecture.md`, `docs/modules.md`) are
  authoritative. `docs/product/` holds the original requirement sources and MUST
  NOT be edited to make the code look compliant. Documentation MUST use relative
  links so every file renders correctly in GitHub and in editors.
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
  `docs/architecture.md` is the authoritative architecture reference.

## Container & Local Development Workflow

- **Docker**: `Dockerfile` is multi-stage (build → minimal runtime), builds all
  three commands (`api`, `migrate`, `seed`) into one image, and MUST NOT bake
  secrets into layers. Commands run as a non-root user; `migrate` and `seed` run
  by overriding the entrypoint of that same image.
- **Compose**: `docker-compose.yml` is the minimal default stack (PostgreSQL,
  Redis, API) and MUST NOT publish backing-service ports on the host, so it cannot
  collide with a developer's local installs. `docker-compose.dev.yml` is an
  optional override that publishes ports on `127.0.0.1` and adds Mailpit for
  local email. One-shot jobs (`migrate`, `seed`) sit behind the `tools` profile
  and are invoked explicitly.
- **Configuration**: `.env` is the single source for local settings, git-ignored,
  and generated from `.env.example` (`make env`). Inside containers the compose
  file overrides host-oriented values (`DATABASE_URL`, `REDIS_ADDR`) to the
  service names. Secrets MUST come from the environment; `.env` MUST NOT be
  committed, and `.gitattributes` MUST keep LF endings so `gofmt`/`make
  fmt-check` behave the same on every platform.
- **Workflow entry point**: all common commands go through `make` targets so
  Windows and Linux developers run identical commands.

## Development Workflow & Quality Gates

- **Spec-driven flow**: constitution → `/speckit.specify` → `/speckit.plan` →
  `/speckit.tasks` → `/speckit.implement` → `/speckit.converge`. Requirements
  and plans MUST precede implementation.
- **Definition of Done**: `gofmt`/`go vet` clean, linter clean, tests passing,
  critical-logic tests present, migrations included, and API/contract docs
  updated for the change — specifically `docs/api-reference.md` (Principle VIII)
  whenever an endpoint was added, changed, or removed. An endpoint change also MUST
  carry the handler's Swagger annotations and a regenerated `docs/swagger/`
  (`make swagger`), so `/swagger` cannot drift behind the reference.
- **Gates are tiered by change size**; the tiers in `AGENTS.md` §3 are the single
  source of truth and MUST NOT be duplicated elsewhere. A typo does not require
  the close-out gate, and a feature phase MUST NOT skip the integration suite.
- **Commands**: work goes through `Makefile` targets rather than raw commands, so
  Windows and Linux developers run the same thing.
- **Gates before implementation**: the plan's Constitution Check MUST pass or
  violations MUST be justified in Complexity Tracking. `/speckit.analyze`
  SHOULD be run between `/speckit.tasks` and `/speckit.implement`.
- **Review**: changes require at least one review against this constitution;
  reviewers MUST flag violations rather than silently accepting them.
- **Runtime guidance**: `docs/modules.md` and `docs/modules/*.md` describe the
  module scope and delivery order; `docs/product/backend-spec.md` and
  `docs/product/project_overview.md` remain the original product sources. These MUST be
  updated when behavior or module scope changes.
- **Decision records**: an architecture decision with long-lived consequences MUST
  be recorded as an ADR in `docs/decisions/`. Editing a decision after the fact
  is not allowed; supersede it with a new ADR. Decisions scoped to one feature
  belong in that feature's `research.md` instead.

## Agent & Commit Governance

Automation MUST behave as follows, because these rules protect history rather than
describe code:

- An agent MUST NOT mutate git history — no `commit`, `push`, `tag`, `branch`,
  `checkout`, `merge`, `rebase`, `reset`, `amend`, `restore`, or `clean` — without
  explicit human authorization for that specific action. Read-only git commands
  are always allowed. Approving a plan that contains several commits does not
  authorize any of them; the general-purpose skill MAY declare a standing
  exception for a single run, but the skill's policy is what counts and it MUST
  list the commits up front.
- Commit messages MUST be written in English and follow Conventional Commits,
  scoped to the narrowest meaningful unit; see `docs/development/git-workflow.md`.
- An agent MUST stage only the files belonging to the current change.
- When documents and code disagree, the document wins and the code MUST be
  corrected. When two documents disagree, precedence is
  `constitution > docs/system-design > docs/development > other docs > code`.
- When a decision is missing, the agent MUST ask the human with concrete options
  (marking the recommended one) instead of guessing, and MUST record the chosen
  default in the feature's `Assumptions` when the ambiguity is low-risk.
- Documentation and code MUST use **relative** paths; absolute machine paths such
  as `file:///C:/...` are forbidden.
- Code comments and `.env` files MUST be written in English; `docs/` content and
  human conversation MAY be in Vietnamese.

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
- **Precedence**: where `docs/` guidance and this constitution conflict, this
  constitution wins, and the docs MUST be corrected.
- **Frontend integration guide**: any feature that adds, removes, or changes an
  HTTP endpoint, an event payload, or a client-facing DTO MUST ship
  `specs/<feature>/frontend-guide.md`. The guide MUST enumerate every affected
  endpoint or event with its method and path, mark each as new, changed or
  removed, and for each changed one state the behaviour before and after.
  Documenting a shape without showing the transition does not satisfy this rule.
  A feature with no client-facing change MUST say so in `tasks.md` rather than
  leave the question open. The guide is the per-feature hand-off to the frontend
  team and MUST agree with `docs/api-reference.md`, which remains the
  authoritative cross-module reference (Principle VIII). Applies to features
  created after this amendment; features 001–005 predate it and are exempt, but
  their guides are backfilled deliberately (see the Sync Impact Report).
- **Frontend guide sync**: the guide MUST be re-read and corrected in the
  feature's final phase and again after any convergence pass, because
  implementation routinely moves the contract after the guide is written. The
  re-read MUST compare against the source rather than skim: every client-facing
  field the DTOs emit, every value of every enum the guide documents, every route
  it lists, every HTTP status it claims. A comparison that produces no counts did
  not happen.
- **Technical debt found, not fixed**: debt discovered while building a feature
  and left unfixed MUST be recorded in `specs/<feature>/deferred.md` with what it
  is, why it is out of scope, and what would unblock it. It MUST NOT be left as an
  unchecked task of the feature that found it, because an unchecked task claims
  the feature is unfinished — which is both a different claim and a false one. An
  entry that undermines an already-ticked task's evidence MUST name that task, or
  `[X]` will be read as unconditional. Deferring is not parking: work that turns
  out to belong to the feature moves back into `tasks.md` as a real task.

**Version**: 1.8.0 | **Ratified**: 2026-09-18 | **Last Amended**: 2026-10-09
