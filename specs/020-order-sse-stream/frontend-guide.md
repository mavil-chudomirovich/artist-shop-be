# Frontend Integration Guide: Order SSE Live Notifications (020)

> Đồng bộ lần cuối: 2026-10-19 (đối chiếu có đếm với mã nguồn sau implement — xem "Đối chiếu" cuối file).

## Endpoints mới

| Method + path | Đánh dấu | Dùng khi nào | Auth |
|---|---|---|---|
| `GET /api/v1/orders/stream` | **mới** | Khách subscribe trạng thái đơn của chính mình | `Authorization: Bearer` |
| `GET /api/v1/admin/orders/stream` | **mới** | Admin subscribe đơn chờ xác nhận + feedback | `Authorization: Bearer` + role ADMIN |

**Không dùng `EventSource` chuẩn** (không gắn header được). Dùng `fetch + ReadableStream`:

```js
const res = await fetch('/api/v1/orders/stream', { headers: { Authorization: `Bearer ${token}` } });
const reader = res.body.getReader();
const dec = new TextDecoder();
let buf = '';
for (;;) {
  const { done, value } = await reader.read();
  if (done) break;
  buf += dec.decode(value, { stream: true });
  // parse các khối kết thúc bằng \n\n: dòng `event:`, `data:`, bỏ dòng `:`
}
// reconnect: fetch lại + GET /orders bù (server không replay)
```

## Events mới

| `event:` | Kênh | `data:` JSON | Khi nào |
|---|---|---|---|
| `order.needs_confirmation` | admin | `{"orderId","version","at"}` | đơn mới `PENDING` / edit về `PENDING` |
| `order.status_changed` | khách (chủ đơn) | `{"orderId","from","to","version","at"}` | mọi đổi trạng thái của đơn mình |
| `order.feedback_received` | admin | `{"orderId","at"}` | feedback lưu xong |

`from/to` ∈ `PENDING|PAYMENT_PENDING|PAID|SHIPPING|DELIVERED|COMPLETED|CANCELLED`. Không có PII trong event; chi tiết gọi REST (`GET /orders/{id}`, `GET /admin/orders/{id}`).

## Lỗi

- `401 UNAUTHENTICATED`: thiếu/sai token (cả 2 stream).
- `403 FORBIDDEN`: customer gọi admin stream.
- Rớt mạng: mở lại stream + refetch REST; server không gửi bù.

## Đối chiếu (sync có đếm, 2026-10-19)

- Route: 2/2 khớp — `GET /stream` trong `Router()` + `GET /stream` trong `AdminRouter()` (`presentation/http/router.go`), mount tại `/orders` và `/admin/orders` (`cmd/api/main.go`).
- Event: 3/3 khớp — `order.needs_confirmation`, `order.status_changed`, `order.feedback_received` (`application/interface/events.go`).
- Payload JSON: `status_changed` 5 member (`orderId,from,to,version,at`), `needs_confirmation` 3 member (`orderId,version,at`), `feedback_received` 2 member (`orderId,at`) — khớp `StatusChangedPayload`/`NeedsConfirmationPayload`/`FeedbackReceivedPayload`.
- Enum `from/to`: 7/7 giá trị của `constant.Status` (`PENDING, PAYMENT_PENDING, PAID, SHIPPING, DELIVERED, COMPLETED, CANCELLED`).
- HTTP status: `StreamMine` 200/401/429/503, `StreamAdmin` 200/401/403/429/503 — khớp annotation `@Failure` trong `presentation/http/stream.go` (429 từ rate limit toàn cục).
- Publish points: 12 commit boundaries (13 call sites — edit gọi 2: `publishNeedsConfirmation` + `publishStatusChanged` có điều kiện) — checkout, edit, confirm, reject (`adminMove`), MarkPaid, Cancel, CancelMine, ship, deliver, ConfirmReceived, AutoComplete, feedback. `ExpireOrders` không phải boundary riêng (tái dùng boundary của `Cancel` qua helper `cancelLocked`); `CancelMine` và `Cancel` mỗi đường publish một lần sau commit của mình. Mỗi điểm cạnh `notify*` tương ứng.
