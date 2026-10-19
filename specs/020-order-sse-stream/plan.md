# Implementation Plan: Order SSE Live Notifications

**Branch**: `020-order-sse-stream` | **Date**: 2026-10-19 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/020-order-sse-stream/spec.md`

## Summary

Thêm 2 stream SSE live cho đơn hàng trên cùng các điểm đã có email best-effort: admin nhận `order.needs_confirmation` + `order.feedback_received`, khách nhận `order.status_changed` của chính mình. Hub generic trong-process ở `internal/share/sse/`; port `EventPublisher` mới ở order; publish sau commit, drop khi subscriber chậm.

## Technical Context

**Language/Version**: Go 1.26 (theo go.mod)

**Primary Dependencies**: chi router, `share/middleware` auth guards, `share/httpx` envelope/error, `slog` logging

**Storage**: N/A (không bảng/migration mới; event live-only trong bộ nhớ)

**Testing**: `go test` unit + `test-integration` (build tag `integration`, testcontainers PG + miniredis) theo `docs/testing.md`

**Target Platform**: Linux VPS (Docker), 1 instance API

**Performance Goals**: event tới client < 5s sau commit (SC-001/002); publish không cộng thêm latency p95 cho use case (non-blocking)

**Constraints**: SSE handshake chịu rate limit toàn cục; heartbeat 25s; subscriber buffer 32, drop thay vì block

**Scale/Scope**: hàng chục admin + hàng trăm khách đồng thời trên 1 instance; multi-instance hoãn (Redis pub/sub)

## Constitution Check

- [x] **Modular Monolith & Clean Architecture**: hub generic ở `internal/share/sse/`; order phụ thuộc port `EventPublisher` ở `application/interface`; adapter ở `infrastructure/implement/ssepublisher`; route ở `presentation/http`; `domain` không đụng.
- [x] **Transactional Integrity**: publish sau `WithinTx` commit, giống `notify*`; không mở transaction mới; tiền/tồn không đổi.
- [x] **State Machines & Invariants**: không thêm/sửa cạnh state machine; chỉ fan-out sau transition thành công.
- [x] **Test-First Critical Logic**: không đổi logic tiền/tồn; thêm test publish (positive + nil/failing publisher) cho các điểm đã có.
- [x] **Security & Least Privilege**: `RequireAuthentication` cho `/orders/stream`, `RequireRole(ADMIN)` cho `/admin/orders/stream`; topic suy từ session, không từ query; không PII trong payload/log.
- [x] **Observability**: log structured có correlation, không body/PII; mutation admin đã audit sẵn, publish không audit riêng.
- [x] **Simplicity (YAGNI)**: lệch có chủ đích — xem Complexity Tracking.
- [x] **Frontend Integration Guide**: feature đổi bề mặt client → ship `frontend-guide.md` + đồng bộ có đếm ở phase cuối.
- [x] **Swagger annotations**: 2 endpoint mới có annotation đầy đủ + `make swagger`.

## Project Structure

### Documentation (this feature)

```text
specs/020-order-sse-stream/
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/
│   ├── sse-events.md
│   └── openapi.yaml
├── frontend-guide.md
├── deferred.md
├── tasks.md
└── checklists/requirements.md
```

### Source Code (repository root)

```text
internal/share/sse/
├── hub.go          # Hub: Subscribe/Unsubscribe/Publish/Stats
├── hub_test.go
├── stream.go       # ServeStream helper (header, heartbeat, flush)
└── stream_test.go

internal/modules/order/
├── application/interface/events.go     # port EventPublisher + TopicForUser/Admins + Event type consts + payload builders
├── application/implement/events.go     # publish helpers (publishStatusChanged/publishNeedsConfirmation/publishFeedbackReceived)
├── application/implement/*.go          # gọi publish sau commit, cạnh notify* tương ứng (checkout/edit/lifecycle/admin/delivery/completion/feedback/orders)
├── infrastructure/implement/ssepublisher/publisher.go  # adapter Hub -> port
└── presentation/http/
    ├── stream.go        # StreamMine + StreamAdmin handlers + Swagger annotations
    ├── stream_test.go   # guard 401/403 + header SSE
    └── router.go        # mount GET /stream + GET /admin/orders/stream

cmd/api/main.go  # tạo Hub, wire ssepublisher vào orderService, mount routes (đã có)
```

**Structure Decision**: Single project layout theo `docs/architecture.md`; hub mới vào `internal/share/` vì dùng chung tương lai (chat/payment).

## Complexity Tracking

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| Constitution VII (REST+polling đủ, chưa cần realtime) + design-pattern §10 (no domain event/bus) | Admin cần biết đơn `PENDING` trong vài giây để kịp cửa sổ thanh toán 60 phút; khách cần biết trạng thái đổi kịp thời; polling toàn bộ hàng đợi mỗi 5-10s tốn DB và vẫn trễ | Polling đã đủ về mặt chức năng nhưng trễ cố hữu + tải list endpoint; SSE một chiều nhẹ hơn WebSocket và chỉ live (không lưu trữ/replay), là mức realtime tối thiểu |
