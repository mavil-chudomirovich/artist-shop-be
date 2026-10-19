# Tasks: Order SSE Live Notifications

**Input**: Design documents from `specs/020-order-sse-stream/`

**Prerequisites**: plan.md (required), spec.md (3 user stories P1/P1/P2), research.md (D1–D9), data-model.md, contracts/ (sse-events.md, openapi.yaml)

**Tests**: Unit test cho Hub + use-case publish (fake publisher) + handler stream (guard, header, event, cô lập, dọn subscriber). State-transition logic không đổi nên không cần test-first cho transition; suite integration hiện có của order giữ xanh.

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2, US3)
- Include exact file paths in descriptions

## Path Conventions

- Single Go project: `internal/...` absolute from repository root
- Spec artifacts: `specs/020-order-sse-stream/...`

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Spec artifacts + Hub dùng chung

- [X] T001 Viết spec.md + checklists/requirements.md cho 020 (`specs/020-order-sse-stream/spec.md`)
- [X] T002 [P] Tạo `internal/share/sse/hub.go`: Hub in-process (Subscribe/Unsubscribe/Publish non-blocking drop + Stats), topic string
- [X] T003 [P] Tạo `internal/share/sse/stream.go` + `internal/share/sse/doc.go`: helper ServeStream (header SSE, heartbeat 25s, flush, dừng khi client đóng)
- [X] T004 Unit test `internal/share/sse/hub_test.go` + `internal/share/sse/stream_test.go`: sub/pub 2 topic, cô lập topic, drop khi đầy, unsubscribe dọn
- [X] T005 Viết plan.md + research.md (D1–D9) + data-model.md + contracts/sse-events.md + contracts/openapi.yaml + quickstart.md + deferred.md + frontend-guide.md skeleton (`specs/020-order-sse-stream/`)

**Checkpoint**: Hub dùng chung sẵn, chưa ai publish

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Port + adapter + helpers publish của order (chặn mọi story)

**⚠️ CRITICAL**: No user story work can begin until this phase is complete

- [X] T006 Thêm port `EventPublisher` + const topic/event + payload builders trong `internal/modules/order/application/interface/events.go`
- [X] T007 Adapter `internal/modules/order/infrastructure/implement/ssepublisher/publisher.go` (wrap `share/sse.Hub`, nil-safe) + unit test `internal/modules/order/infrastructure/implement/ssepublisher/publisher_test.go`
- [X] T008 Thêm field `Events` vào `Service` trong `internal/modules/order/application/implement/checkout.go` + helpers `internal/modules/order/application/implement/events.go` (publishNeedsConfirmation/publishStatusChanged/publishFeedbackReceived, nil-safe, gọi sau commit)
- [X] T009 Wire ở `cmd/api/main.go`: tạo `sse.Hub`, `ssepublisher.New`, gán vào `orderService.Events`, `orderHandler.SetStreams(orderHub)`

**Checkpoint**: Foundation ready - user story implementation can now begin

---

## Phase 3: User Story 1 - Admin báo đơn chờ xác nhận (Priority: P1) 🎯 MVP

**Goal**: Admin stream nhận `order.needs_confirmation` khi checkout/edit về PENDING

**Independent Test**: Mở `GET /admin/orders/stream` bằng admin, checkout đơn mới → nhận event đúng `orderId`; edit `PAYMENT_PENDING` về `PENDING` → nhận thêm event

### Tests for User Story 1

- [X] T010 [P] [US1] Use-case test checkout/edit publish `needs_confirmation` đúng topic `admins` (fake publisher) trong `internal/modules/order/application/implement/events_test.go`
- [X] T011 [P] [US1] Nil publisher không làm hỏng checkout/edit (đơn vẫn đúng) trong `internal/modules/order/application/implement/events_test.go`

### Implementation for User Story 1

- [X] T012 [US1] Gọi `publishNeedsConfirmation` sau commit trong `Checkout` (`internal/modules/order/application/implement/checkout.go`) và edit (`internal/modules/order/application/implement/edit.go`)
- [X] T013 [US1] Handler `StreamAdmin` trong `internal/modules/order/presentation/http/stream.go` (topic `admins`) + mount `GET /stream` trong `AdminRouter` (`internal/modules/order/presentation/http/router.go`) + Swagger annotations
- [X] T014 [US1] Handler test 401/403/503 trong `internal/modules/order/presentation/http/stream_test.go`

**Checkpoint**: US1 chạy độc lập: admin stream báo đơn chờ xác nhận

---

## Phase 4: User Story 2 - Khách báo đổi trạng thái (Priority: P1)

**Goal**: Khách stream nhận `order.status_changed` của chính mình, không rò sang khách khác

**Independent Test**: Mở stream khách A, admin confirm/ship/deliver đơn A → A nhận từng event; stream khách B im lặng

### Tests for User Story 2

- [X] T015 [P] [US2] Use-case test confirm/cancel/ship/deliver publish `status_changed` đúng topic `user:<owner>` + đúng `from/to` (fake publisher) trong `internal/modules/order/application/implement/events_test.go`
- [X] T016 [P] [US2] Stream test mức handler trong `internal/modules/order/presentation/http/stream_test.go`: event tới đúng chủ, khách khác không nhận, subscriber dọn sau disconnect. Suite integration hiện có của order vẫn xanh

### Implementation for User Story 2

- [X] T017 [US2] Gọi `publishStatusChanged(from,to)` sau commit tại: `ConfirmByAdmin` + `adminMove` (reject) + `MarkPaid` + `Cancel` (cover cả `ExpireOrders`) trong `internal/modules/order/application/implement/lifecycle.go` + `internal/modules/order/application/implement/admin.go`; ship/deliver trong `internal/modules/order/application/implement/delivery.go`; `ConfirmReceived` + `AutoCompleteDelivered` trong `internal/modules/order/application/implement/completion.go`; edit trong `internal/modules/order/application/implement/edit.go`
- [X] T018 [US2] Handler `StreamMine` trong `internal/modules/order/presentation/http/stream.go` (topic từ session, guard auth) + mount `GET /stream` trong `Router` (`internal/modules/order/presentation/http/router.go`) + Swagger annotations. Thêm field `streams` + `SetStreams` (interface `streamsHub`) vào `Handler` (`internal/modules/order/presentation/http/handler.go`)

**Checkpoint**: US1 AND US2 chạy độc lập, cô lập theo chủ sở hữu

---

## Phase 5: User Story 3 - Admin báo feedback (Priority: P2)

**Goal**: Admin stream nhận `order.feedback_received` khi feedback lưu xong

**Independent Test**: Mở admin stream, khách submit feedback đơn DELIVERED → admin nhận event

- [X] T019 [US3] Use-case test `SubmitFeedback` publish `feedback_received` đúng topic `admins`; feedback bị từ chối thì không publish — trong `internal/modules/order/application/implement/events_test.go`
- [X] T020 [US3] Gọi `publishFeedbackReceived` sau commit trong `internal/modules/order/application/implement/feedback.go`

**Checkpoint**: Mọi user story chạy độc lập

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: Docs + gates + đồng bộ guide

- [X] T021 Cập nhật `docs/api-reference.md` (§9 intro 14→16 endpoint, `9.15`/`9.16`, bảng tổng hợp, change log) + `docs/modules/07-order.md` (ghi chú stream SSE)
- [X] T022 Chạy `make swagger`, kiểm `make swagger-check` (diff đúng 2 route mới, xanh sau commit)
- [X] T023 **Đồng bộ `frontend-guide.md`** ở phase cuối, SAU khi mã nguồn ổn định: đối chiếu có đếm — 2/2 route, 3/3 event, 5/3/2 member payload, 7/7 enum, HTTP status khớp annotation
- [X] T024 Chạy gate `make lint` (0 issues) + `go test ./...` (xanh) + `make test-integration` (package order xanh; full-repo timeout ở product, không liên quan) + `make build` (OK)
- [X] T025 **Ghi `deferred.md`**: Redis pub/sub, replay Last-Event-ID, bảng notifications, query-token EventSource, event transfer — KHÔNG để `- [ ]` cho việc ngoài phạm vi

### Frontend Integration Guide *(bắt buộc — hiến pháp §Governance)*

- [X] T023 **Có đổi bề mặt client** → viết `specs/020-order-sse-stream/frontend-guide.md`: 2 endpoint **mới**, 3 event **mới**, ví dụ parse fetch-stream, đối chiếu có đếm
- [X] T023 **Đồng bộ guide** ở phase cuối: 2/2 route vs router, 3/3 event vs const, payload member vs DTO, 7/7 enum vs `constant.Status`, HTTP status vs handler

### Swagger annotations *(bắt buộc nếu feature có endpoint)*

- [X] T022 **Có endpoint thêm** → annotation cho 2 handler stream (`@Summary`, `@Tags`, `@Produce`, `@Security`, `@Success`, `@Failure`, `@Router`) + `make swagger`

### Phát hiện ngoài phạm vi *(hiến pháp §Governance)*

- [X] T025 **Ghi vào `specs/020-order-sse-stream/deferred.md`**, KHÔNG để `- [ ]` ở file này

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies - can start immediately
- **Foundational (Phase 2)**: Depends on Setup completion - BLOCKS all user stories
- **User Stories (Phase 3+)**: All depend on Foundational phase completion
  - US1 + US2 can proceed in parallel (P1 first); US3 (P2) after
- **Polish (Final Phase)**: Depends on all desired user stories being complete

### User Story Dependencies

- **User Story 1 (P1)**: Can start after Foundational (Phase 2) - No dependencies on other stories
- **User Story 2 (P1)**: Can start after Foundational (Phase 2) - independent, shares Hub + port
- **User Story 3 (P2)**: Can start after Foundational (Phase 2) - independent, shares Hub + port

### Within Each User Story

- Tests (fake publisher / httptest stream) written alongside implementation, assert before/after behavior
- Port → adapter → helpers → publish call sites → handlers → routes → wiring
- Story complete before moving to next priority

### Parallel Opportunities

- T002 + T003 (different files: hub.go vs stream.go) can run in parallel
- T010 + T011 (same file, independent cases) can run in parallel
- T015 + T016 (different packages) can run in parallel
- US1 + US2 implementation can proceed in parallel after Phase 2 (different use-case files, shared Hub is done)

---

## Parallel Example: User Story 1

```bash
# Launch tests for User Story 1 together:
Task: "Use-case test checkout/edit publish needs_confirmation in internal/modules/order/application/implement/events_test.go"
Task: "Nil publisher test in internal/modules/order/application/implement/events_test.go"

# Hub files built in parallel:
Task: "Hub in internal/share/sse/hub.go"
Task: "ServeStream helper in internal/share/sse/stream.go"
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 1: Setup (Hub + spec artifacts)
2. Complete Phase 2: Foundational (port + adapter + wiring)
3. Complete Phase 3: User Story 1 (admin needs_confirmation stream)
4. **STOP and VALIDATE**: Open admin stream, checkout → event arrives
5. Deploy/demo if ready

### Incremental Delivery

1. Setup + Foundational → Foundation ready
2. Add US1 (admin needs_confirmation) → Test independently → Demo (MVP!)
3. Add US2 (customer status_changed) → Test independently → Demo
4. Add US3 (admin feedback_received) → Test independently → Demo
5. Polish (docs/swagger/guide/gates) → Close-out

---

## Notes

- Transfer (đổi chủ đơn) KHÔNG phát event — hoãn cùng phạm vi chuyển nhượng tương lai (clarify D8, `deferred.md`)
- Kênh `admins` chỉ 2 loại event, không fan-out `status_changed` toàn shop (clarify D9, FR-004)
- `[P]` tasks = different files, no dependencies
- `[Story]` label maps task to specific user story for traceability
- No vague tasks; every task names its file path
