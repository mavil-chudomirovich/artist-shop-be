# Feature Specification: Cross-Cutting Foundation

**Feature Branch**: `002-cross-cutting-foundation`

**Created**: 2026-09-18

**Status**: Draft

**Input**: User description: "Thực hiện module Cross-Cutting Foundation theo doc/modules/00-cross-cutting.md: cấu hình, kết nối dữ liệu, migration, định dạng phản hồi/lỗi, logging, middleware, audit, health, kiểm thử"

## Clarifications

### Session 2026-09-18

- Q: Ranh giới middleware xác thực/phân quyền giữa foundation và module auth? → A: Foundation cung cấp interface + hook cưỡng chế; module auth (01) cài đặt logic xác thực/phân quyền thực tế.
- Q: Thời điểm chạy migration? → A: Tự động áp khi service khởi động (có lock chống chạy trùng), vẫn có lệnh chạy thủ công; kiến trúc là modular monolith — một service triển khai duy nhất.
- Q: Bảo đảm ghi audit? → A: Bất đồng bộ/best-effort có retry; audit lỗi không làm thao tác nghiệp vụ thất bại, nhưng phải retry và quan sát được.
- Q: Cơ chế rate limiting? → A: Trong tiến trình (per-instance), ngưỡng cấu hình theo nhóm route; không có store ngoài.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Bootstrap the service from a clean checkout (Priority: P1)

A developer clones the repository on a fresh machine and gets the backend running
locally by following the documented steps, without undocumented manual setup or
hard-coded values.

**Why this priority**: Every later module will be built and tested against this
service; if it cannot be started reliably, no other work can proceed.

**Independent Test**: From a clean checkout, follow the documented setup and
confirm the service starts and answers a basic request.

**Acceptance Scenarios**:

1. **Given** a clean checkout and the documented prerequisites, **When** a
   developer follows the setup steps, **Then** the service starts successfully.
2. **Given** the service is starting with a required configuration value missing
   or invalid, **When** startup runs, **Then** it stops quickly with a clear
   message naming the offending configuration.
3. **Given** a running service, **When** a developer sends a request to the
   versioned base path, **Then** a response is returned in the standard format.

---

### User Story 2 - Deploy and operate per environment (Priority: P1)

An operator deploys the same build to different environments (development,
staging, production) by supplying configuration only, and can verify the
service is alive and ready before routing traffic to it.

**Why this priority**: Without environment-driven configuration and reliable
health signalling, deployment is unsafe and unattended.

**Independent Test**: Start the service in two environments with different
configuration and confirm behavior differs only by configuration; poll the
health and readiness signals before and after the service is ready.

**Acceptance Scenarios**:

1. **Given** a build artifact, **When** it runs in an environment with that
   environment's configuration, **Then** it uses that configuration and no
   environment-specific values are baked into the artifact.
2. **Given** the service is starting and dependencies are not yet usable,
   **When** readiness is queried, **Then** it reports not ready; once usable, it
   reports ready.
3. **Given** the service is running, **When** liveness is queried, **Then** it
   reports alive.

---

### User Story 3 - Consume a predictable API contract (Priority: P2)

A client integrating with the API receives every response in a consistent
envelope and every error in a single format with a stable, machine-readable code
and a correlation identifier, so failures can be handled and traced
deterministically.

**Why this priority**: Consistent contracts reduce client-side rework and make
support and debugging tractable across all modules.

**Independent Test**: Trigger success, client error, and server error responses
and confirm each uses the same envelope/error format and carries a correlation
identifier that also appears in the logs.

**Acceptance Scenarios**:

1. **Given** any successful request, **When** the response is returned, **Then**
   it follows the standard success envelope.
2. **Given** an invalid request or an unavailable resource, **When** it fails,
   **Then** the response follows the standard error format with a stable code.
3. **Given** an unexpected internal failure, **When** it occurs, **Then** the
   client receives a standard error without internal details or stack traces.
4. **Given** any request, **When** it is handled, **Then** a correlation
   identifier is returned and appears in the corresponding log entry.

---

### User Story 4 - Trace privileged and payment activity (Priority: P2)

A security reviewer can reconstruct who performed privileged (admin) actions and
what happened to payments, using append-only audit records with actor, action,
target, and time.

**Why this priority**: Money and privileged actions are the highest-risk
activity and must be reconstructable.

**Independent Test**: Perform a privileged action and a payment event, then
confirm exactly one audit record exists for each with the required fields.

**Acceptance Scenarios**:

1. **Given** an authenticated admin performer, **When** a privileged action
   succeeds or fails, **Then** an audit record captures actor, action, target,
   outcome, and timestamp.
2. **Given** a payment event is processed, **When** it completes, **Then** an
   audit record is written.
3. **Given** existing audit records, **When** new activity occurs, **Then**
   earlier records are not modified (append-only).
4. **Given** an audit write fails, **When** the originating business operation
   has already succeeded, **Then** the operation is not rolled back, the audit
   event is retried, and the failure remains observable until persisted.

---

### User Story 5 - Evolve the schema safely (Priority: P3)

A developer adds or changes database structure through versioned, reviewable
migrations that can be applied consistently across environments and can be
rolled back, without destroying existing data silently.

**Why this priority**: Safe schema evolution is essential once the schema grows,
but it can follow the first deployable foundation.

**Independent Test**: Apply a new migration to an empty database and to an
existing database, confirm it applies and can be rolled back.

**Acceptance Scenarios**:

1. **Given** a new versioned migration, **When** it is applied, **Then** the
   schema reaches the expected version and the change can be rolled back.
2. **Given** a migration that would destroy data, **When** it is proposed,
   **Then** it requires an explicit accompanying data-migration note.
3. **Given** a database at an older schema version, **When** the service starts,
   **Then** it reaches the expected version consistently.

---

### Edge Cases

- Required configuration missing or malformed at startup: fail fast with a
  clear, non-secret-revealing message; never start in a partially configured
  state.
- Database unavailable or not yet accepting connections during startup: readiness
  stays not-ready; the service does not serve traffic as if healthy.
- Migration fails midway: the schema is left in a known, recoverable state; the
  service does not report ready.
- A recovered internal failure (panic): a standard error is returned, no internal
  details leak, and the event is logged with the correlation identifier.
- Request body exceeds the allowed size: rejected before processing with a
  standard error.
- Correlation identifier absent from an incoming request: one is generated; if
  present but malformed, a new one is generated.
- Unsupported route or method: standard error response, no internal details.
- A secret appears in a request: it must never be written to logs.
- Concurrent requests: correlation identifiers never collide or leak across
  requests.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The system MUST load all configuration and secrets from the
  environment; no environment-specific value may be baked into the build.
- **FR-002**: The system MUST validate required configuration at startup and
  fail fast with a clear message that does not reveal secret values.
- **FR-003**: The system MUST establish and manage a pooled connection to the
  primary relational database.
- **FR-004**: The system MUST apply schema changes only through versioned,
  reviewable migrations that include a rollback path. Migrations MUST be applied
  automatically when the service starts, guarded by a lock that prevents
  concurrent execution, and MUST also be runnable as an explicit manual command.
- **FR-005**: The system MUST NOT apply a schema change that destroys data
  without an explicit accompanying data-migration note.
- **FR-006**: The system MUST return all API responses in a single consistent
  envelope.
- **FR-007**: The system MUST return all errors in a single format with a
  stable, machine-readable error code and a human-readable message.
- **FR-008**: The system MUST NOT expose internal details (stack traces,
  queries, secrets) in API responses.
- **FR-009**: The system MUST assign or propagate a correlation identifier for
  every request, include it in the response, and emit it in logs.
- **FR-010**: The system MUST emit structured, machine-parseable logs.
- **FR-011**: The system MUST exclude secrets, credentials, tokens, and
  unnecessary personal data from logs.
- **FR-012**: The system MUST provide the request middleware pipeline and the
  enforcement mechanism for authentication context handling, authorization
  enforcement, rate limiting, panic recovery, cross-origin policy, and
  request-size limits. The foundation defines the middleware contract and
  enforcement points only; it MUST NOT implement credential or session logic,
  which is supplied by the auth module (module 01).
- **FR-013**: The system MUST expose liveness and readiness signals; readiness
  MUST reflect whether dependencies and schema are usable.
- **FR-014**: The system MUST persist append-only audit records for privileged
  (admin) actions and payment events, capturing actor, action, target, outcome,
  metadata, and timestamp. Audit writing MUST be asynchronous and MUST NOT block
  or fail the underlying business operation. Failed audit writes MUST be retried
  idempotently (no duplicate records) and MUST be observable until persisted.
- **FR-015**: The system MUST provide an isolated test database and a test
  harness covering both unit-level and API-level tests.
- **FR-016**: The API MUST be served under a versioned base path.
- **FR-017**: The system MUST reject unsupported routes and methods with the
  standard error format.
- **FR-018**: The system MUST recover from unexpected internal failures, return
  a standard error, and log the event with the correlation identifier.
- **FR-019**: The system MUST enforce a maximum request body size and reject
  oversized requests with a standard error.
- **FR-020**: The system MUST support at least three environments (development,
  staging, production) using configuration only, with no code changes.
- **FR-021**: The system MUST enforce rate limiting within the service process
  (per instance), keyed by client identity and route class, with thresholds
  configurable per environment. No external shared store is required for rate
  limiting in the modular monolith.

### Key Entities *(include if data involved)*

- **Audit Record**: An append-only record of a privileged action or payment
  event. Attributes: actor (who), action (what), target (on what), outcome,
  metadata (context), timestamp. Never updated or deleted by application logic.
- **Configuration Set**: The environment-provided values that drive runtime
  behavior, separated into non-secret configuration and secrets. Validated at
  startup.
- **Error Catalogue**: The set of stable, machine-readable error codes and their
  meanings shared across all modules.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A developer can go from a clean checkout to a running service in
  under 10 minutes by following the documentation.
- **SC-002**: 100% of runtime configuration is environment-provided; startup
  fails within 5 seconds when a required value is missing.
- **SC-003**: 100% of tested API errors — including client errors, server errors,
  and recovered internal failures — conform to the single error format.
- **SC-004**: 100% of tested responses carry a correlation identifier that also
  appears in the corresponding log entry.
- **SC-005**: The readiness signal reports not-ready while dependencies/schema
  are unusable and ready within 30 seconds of becoming usable.
- **SC-006**: In tested privileged and payment flows, exactly one audit record is
  eventually produced per event (retries do not create duplicates), with all
  required fields present.
- **SC-007**: Across tested flows, zero secrets are present in emitted logs.
- **SC-008**: The full test suite runs from a clean checkout and completes in
  under 10 minutes.

## Assumptions

- The backend is a modular monolith: a single deployable service containing
  internally bounded modules, using the project's primary relational database.
  It is explicitly not a microservice architecture; there is no service mesh,
  inter-service transport, or independently deployed backend service.
- Service library and tooling choices (HTTP framework, database driver,
  migration tool, logger) are implementation concerns resolved during planning,
  not in this specification.
- Distributed tracing, advanced metrics, and APM are out of scope; structured
  logging and correlation identifiers are the observability baseline.
- Multi-tenancy is out of scope.
- Audit records are persisted durably on a best-effort, asynchronous basis with
  idempotent retry. A failed audit write does not fail the business operation,
  but the failure is retried and made observable until persisted.
- Rate limiting is enforced in-process per service instance; exact thresholds
  are configurable per environment and are an implementation concern. A shared
  external store is out of scope unless the service is scaled to multiple
  instances.
