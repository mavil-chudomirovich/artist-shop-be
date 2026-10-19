# Feature Specification: Order SSE Live Notifications

**Feature Branch**: `020-order-sse-stream`

**Created**: 2026-10-19

**Status**: Draft

**Input**: User description: "Mình muốn setup sse để phục vụ cho việc sau - noti cho admin nếu có đơn hàng cần xác nhận và khi người dùng feedback - noti cho khách hàng mỗi lần admin cập nhật trạng thái đơn hàng vd như pending sang payment pending hoặc shipping hoặc delivered"

## Clarifications

### Session 2026-10-19 (plan mode)

- Q: Phạm vi lưu trữ? → A: **Chỉ SSE live**, không bảng notifications; rớt mạng thì client tự refetch REST.
- Q: Client dùng EventSource chuẩn hay fetch-stream? → A: **fetch + ReadableStream**, giữ `Authorization: Bearer` header như API hiện tại.
- Q: Hub đặt ở đâu? → A: **`internal/share/sse/` dùng chung**, order chỉ publish; tái dùng cho chat/payment sau.
- Q: Reconnect/replay? → A: **Chỉ live**, không `Last-Event-ID`; reconnect = mở lại stream + refetch `GET orders`.

### Session 2026-10-19 (clarify)

- Q: Khi admin transfer (đổi chủ) đơn đã trả tiền, có phát SSE cho chủ mới không? → A: **Không phát; hoãn** cùng phạm vi chuyển nhượng tương lai (chủ mới tự refetch).
- Q: Admin stream có cần nhận mọi `order.status_changed` của toàn shop không? → A: **Không — chỉ 2 loại** (`order.needs_confirmation`, `order.feedback_received`) như spec.

## Scope boundary

Kế thừa module 07 (features 009/010/019). Feature này **không đổi** state machine, tiền, tồn kho, email hiện có; nó **thêm kênh đẩy live** cho các sự kiện đã có email best-effort.

- **Deliverable**:
  1. Admin nhận event live khi có đơn cần xác nhận (`PENDING` mới, hoặc edit đưa đơn về `PENDING`) và khi khách gửi feedback.
  2. Khách nhận event live mỗi lần trạng thái đơn của chính mình đổi (confirm, reject, ship, deliver, complete incl. sweeper, cancel/expire, paid).
  3. Hai stream HTTP SSE: khách (`/orders/stream`) và admin (`/admin/orders/stream`), xác thực bằng session hiện có.
- **Deferred**: bảng `notifications`/in-app + đếm chưa đọc (module 12 đầy đủ); replay `Last-Event-ID`; Redis pub/sub multi-instance; push/SMS; **event khi transfer** (đổi chủ đơn đã trả tiền không phát event; chủ mới tự refetch) — hoãn cùng phạm vi chuyển nhượng tương lai.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Admin được báo live khi có đơn chờ xác nhận (Priority: P1)

Admin đang mở dashboard/stream thì một đơn `PENDING` mới được tạo (checkout) hoặc một đơn được sửa về `PENDING`; admin thấy event trong vài giây mà không cần F5/polling.

**Why this priority**: Đây là nỗi đau chính — đơn chờ xác nhận mở cửa sổ thanh toán 60 phút; báo càng sớm, artist xác nhận càng kịp.

**Independent Test**: Mở `GET /admin/orders/stream` bằng session admin, rồi checkout một đơn mới → stream nhận `order.needs_confirmation` mang `orderId` đó; edit đơn `PAYMENT_PENDING` về `PENDING` → nhận thêm một event.

**Acceptance Scenarios**:

1. **Given** admin đã mở stream, **When** khách checkout tạo đơn `PENDING`, **Then** admin nhận `order.needs_confirmation` với `orderId` đúng trong vài giây.
2. **Given** admin đã mở stream, **When** khách sửa đơn `PAYMENT_PENDING` khiến đơn về `PENDING`, **Then** admin nhận `order.needs_confirmation` cho đơn đó.
3. **Given** request không có session admin, **When** mở `GET /admin/orders/stream`, **Then** bị từ chối `401 UNAUTHENTICATED` (không session) hoặc `403 FORBIDDEN` (không phải admin), và không nhận event nào.

---

### User Story 2 - Khách được báo live khi trạng thái đơn đổi (Priority: P1)

Khách đang mở trang đơn hàng/stream thì admin đổi trạng thái đơn của chính mình (confirm → `PAYMENT_PENDING`, reject/cancel → `CANCELLED`, ship → `SHIPPING`, deliver → `DELIVERED`, complete → `COMPLETED`, paid → `PAID`); khách thấy event mà không cần F5/polling.

**Why this priority**: Đây là nửa còn lại của yêu cầu — kháchale biết đơn đi tới đâu theo thời gian thực.

**Independent Test**: Mở `GET /orders/stream` bằng session khách A, rồi admin confirm/ship/deliver đơn của A → stream của A nhận `order.status_changed` tương ứng; stream của khách B không nhận gì.

**Acceptance Scenarios**:

1. **Given** khách A mở stream, **When** admin confirm đơn của A (`PENDING → PAYMENT_PENDING`), **Then** A nhận `order.status_changed` với `from/to` đúng.
2. **Given** khách A mở stream, **When** admin ship/deliver đơn của A, **Then** A nhận `order.status_changed` cho từng bước.
3. **Given** khách A và B cùng mở stream, **When** trạng thái đơn của A đổi, **Then** chỉ A nhận event, B không nhận (chống cross-access).
4. **Given** request không có session, **When** mở `GET /orders/stream`, **Then** bị từ chối `401 UNAUTHENTICATED`.

---

### User Story 3 - Admin được báo live khi có feedback (Priority: P2)

Khách gửi feedback cho đơn `DELIVERED`/`COMPLETED`; admin đang mở stream nhận `order.feedback_received` để đọc kịp thời.

**Why this priority**: P2 — feedback không có deadline 60 phút như xác nhận, nhưng vẫn là yêu cầu rõ ràng của người dùng.

**Independent Test**: Mở admin stream, rồi khách submit feedback cho đơn đã giao → admin nhận `order.feedback_received` mang `orderId` đó.

**Acceptance Scenarios**:

1. **Given** admin mở stream, **When** khách gửi feedback hợp lệ cho đơn `DELIVERED`, **Then** admin nhận `order.feedback_received`.
2. **Given** feedback bị từ chối (đơn chưa giao / đã có feedback), **When** khách gửi, **Then** admin không nhận event mới và đơn nguyên vẹn.

### Edge Cases

- Client ngắt kết nối giữa chừng: server dọn subscriber, không leak goroutine; client mở lại stream + tự refetch `GET /orders` để bù (không replay từ server).
- Subscriber đọc chậm: publish bỏ qua (drop) thay vì block use case; có log warn + đếm drop, đơn vẫn đúng.
- Publish khi không ai nghe: no-op, không lỗi, không ảnh hưởng đơn.
- Sweeper tự hoàn tất/hết hạn cũng phát event như đường khách/admin (vì đi qua cùng use case).
- Proxy/VPS cắt idle connection: heartbeat comment mỗi ~25s giữ kết nối.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: Hệ thống MUST phát event `order.needs_confirmation` cho admin sau khi một đơn vào `PENDING` (checkout mới) và sau mọi lần edit được chấp nhận trên đơn `PENDING`/`PAYMENT_PENDING` (nội dung đổi nên cần artist xác nhận lại — giống email `notifyArtistConfirmationNeeded`).
- **FR-002**: Hệ thống MUST phát event `order.feedback_received` cho admin sau khi feedback được lưu thành công.
- **FR-003**: Hệ thống MUST phát event `order.status_changed` cho **chủ đơn** sau mỗi lần đổi trạng thái thành công (confirm, reject, ship, deliver, complete incl. sweeper, cancel/expire, paid).
- **FR-004**: Stream khách MUST chỉ phát event của đơn thuộc session đang mở; stream admin MUST yêu cầu quyền `ADMIN` và MUST chỉ nhận 2 loại event (`order.needs_confirmation`, `order.feedback_received` — không nhận `order.status_changed` của toàn shop).
- **FR-005**: Phát event MUST best-effort sau khi transaction commit; lỗi publish MUST không làm hỏng hay rollback nghiệp vụ đơn.
- **FR-006**: Stream MUST dùng SSE (`Content-Type: text/event-stream`), hỗ trợ `fetch + ReadableStream` với header `Authorization` hiện có; heartbeat giữ kết nối.
- **FR-007**: Ngắt kết nối client MUST được dọn dẹp phía server (không leak subscriber/goroutine).
- **FR-008**: Payload event MUST tối thiểu (`orderId`, `from`/`to` khi đổi trạng thái, `version`, `at`) và MUST không chứa PII thừa (tên/SĐT/địa chỉ); chi tiết xem bằng REST.

### Key Entities

- **SSE Stream**: kết nối HTTP dài của một session; thuộc về đúng một chủ (khách: `user:<id>`; admin: kênh `admins`).
- **Order Event**: sự kiện live dẫn xuất từ đơn — `order.needs_confirmation`, `order.status_changed`, `order.feedback_received`; không phải bảng lưu trữ, không replay.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Admin mở stream nhận được `needs_confirmation` trong 5 giây sau khi đơn `PENDING` được tạo (đo thủ công/E2E với 1 checkout).
- **SC-002**: Khách mở stream nhận được `status_changed` trong 5 giây sau mỗi bước admin (confirm/ship/deliver) trên đơn của mình.
- **SC-003**: Stream của khách khác không nhận event của đơn không thuộc về mình (0 event rò rỉ trong test SC-002).
- **SC-004**: Lỗi publish (hub đóng/drop) không làm hỏng nghiệp vụ đơn — 100% ca use-case test với publisher hỏng vẫn giữ đơn/feedback đúng.
- **SC-005**: Ngắt 50 kết nối stream liên tiếp không làm tăng goroutine/subscriber tồn đọng (không leak, kiểm bằng unit/integration).

## Assumptions

- Một instance API (theo `docker-compose.yml`); multi-instance cần Redis pub/sub — đã hoãn, ghi `deferred.md`.
- FE dùng `fetch + ReadableStream` (không dùng `EventSource` chuẩn nên không cần token qua query).
- Email hiện có giữ nguyên; SSE là kênh cộng thêm, không thay email.
- Không thêm bảng/migration; không replay `Last-Event-ID`.
- `GET /orders/stream` và `GET /admin/orders/stream` chịu rate limit toàn cục như các route order khác (không bucket riêng).
