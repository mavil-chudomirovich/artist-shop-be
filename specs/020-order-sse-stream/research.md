# Research: Order SSE Live Notifications (020)

**Date**: 2026-10-19 | **Spec**: [spec.md](spec.md)

## D1: Vì sao SSE, không phải polling hay WebSocket

- **Decision**: SSE (`text/event-stream`) một chiều server→client; client dùng `fetch + ReadableStream` với header `Authorization` hiện có.
- **Rationale**: Nhu cầu chỉ là đẩy (không cần client→server qua socket); `EventSource` chuẩn không gắn header được nên phải dùng fetch-stream; WebSocket 2 chiều là thừa (YAGNI).
- **Alternatives**: polling 5-10s (đơn giản nhất nhưng trễ + tải DB cho hàng đợi admin); WebSocket (thừa, phải自 quản auth/room).

## D2: Hub đặt ở `internal/share/sse/`

- **Decision**: Hub generic in-process: `map[topic]map[subscriber]chan`, publish non-blocking + drop.
- **Rationale**: ≥2 consumer tương lai (chat, payment) → Constitution I bắt buộc share; order chỉ phụ thuộc port `EventPublisher`, test bằng fake.
- **Alternatives**: Hub trong module order (nhanh nhưng chat phải viết lại); Redis pub/sub ngay (cần cho multi-instance nhưng hiện 1 instance — hoãn).

## D3: Không lưu trữ, không replay

- **Decision**: Live-only; reconnect = mở lại stream + `GET` REST bù.
- **Rationale**: Đã chốt với dev; tránh bảng + migration + `Last-Event-ID`; đủ cho MVP vì mọi event đều có REST tương ứng để bù.
- **Alternatives**: bảng `notifications` + replay (đúng roadmap module 12, nhưng nặng cho yêu cầu hiện tại — deferred).

## D4: Auth qua session hiện có, topic suy từ session

- **Decision**: `GET /orders/stream` sau `RequireAuthentication`, topic = `user:<sessionID>`; `GET /admin/orders/stream` sau `RequireRole(ADMIN)`, topic = `admins`.
- **Rationale**: Chống cross-access theo FR-004 của feature này (topic suy từ id session, không từ query — nguyên tắc session của module 07 là FR-020); không token qua query nên không lộ log.
- **Alternatives**: query `?access_token=` cho `EventSource` chuẩn (lộ token vào log/proxy — loại).

## D5: Publish sau commit, best-effort, cùng điểm với email

- **Decision**: Thêm port `EventPublisher` riêng (không tái dùng `Notifier.Send(to,subject,body)` vì shape email không phải event); gọi ngay sau các `notify*` hiện có trong `notify.go` và các use case.
- **Publish points** (sau commit của transaction NGOÀI CÙNG — `WithTx` lồng nhau join cùng tx nên helper dùng chung không được publish; mỗi use-case có boundary riêng publish một lần sau commit của chính nó): checkout→admins; mọi edit được chấp nhận→admins (+ customer khi đổi trạng thái); confirm→customer; reject→customer; MarkPaid→customer; ship/deliver→customer; ConfirmReceived + AutoComplete→customer; Cancel/CancelMine/ExpireOrders→customer (qua helper `cancelLocked` dùng chung, mỗi đường publish sau commit của mình); feedback→admins.
- **Rationale**: Tái dùng bảo đảm "sau commit" đã được kiểm; nil publisher tolerated như nil notifier.
- **Alternatives**: Fan-out trong notifier email (trộn 2 kênh, khó test riêng); domain event/bus (design-pattern §10 đã loại — chưa cần).

## D6: Backpressure + heartbeat + proxy

- **Decision**: Channel subscriber 32, `select/default` drop + warn log; heartbeat `: ping\n\n` mỗi 25s; header `X-Accel-Buffering: no`.
- **Rationale**: Publish không bao giờ block use case; heartbeat giữ NAT/proxy không cắt idle.
- **Alternatives**: Channel unbuffered/blocking (treo đơn khi client chậm — cấm).

## D7: Rate limit + audit

- **Decision**: Không bucket riêng; SSE handshake chịu rate limit toàn cục như route order khác. Không ghi audit cho publish (mutation admin đã ghi sẵn).
- **Rationale**: Giống mọi route order (api-reference §1.5); tránh thêm luật không cần.

## D8: Transfer không phát event (clarify 2026-10-19)

- **Decision**: `POST /admin/orders/{orderId}/transfer` (đổi chủ đơn đã trả tiền) không publish gì; chủ mới tự refetch.
- **Rationale**: Transfer không phải đổi trạng thái mà là đổi chủ; email hiện cũng không báo. Hoãn cùng phạm vi chuyển nhượng tương lai thay vì phình scope feature này.
- **Alternatives**: `status_changed` cho chủ mới (nhiễu — from/to trùng nhau); event riêng `order.transferred` (thêm contract khi chưa có nhu cầu).

## D9: Kênh admin chỉ 2 loại event (clarify 2026-10-19)

- **Decision**: Kênh `admins` chỉ nhận `order.needs_confirmation` + `order.feedback_received`; không nhận `order.status_changed` của toàn shop.
- **Rationale**: Đúng yêu cầu gốc (đơn chờ xác nhận + feedback); fan-out mọi status change cho mọi admin vừa ồn vừa tốn tài nguyên Hub. Admin cần trạng thái thì poll `GET /admin/orders`.
- **Alternatives**: admin nhận mọi `status_changed` (mở rộng scope); endpoint subscribe từng đơn (thêm route khi chưa cần).
