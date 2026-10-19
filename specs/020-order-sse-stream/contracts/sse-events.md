# Contracts: SSE Events (020)

Wire format: Server-Sent Events, một event = 2 dòng + dòng trống:

```text
event: order.status_changed
data: {"orderId":"...","from":"PAID","to":"SHIPPING","version":3,"at":"2026-10-19T08:00:00Z"}

```

- `event:` một trong `order.needs_confirmation`, `order.status_changed`, `order.feedback_received`.
- `data:` JSON một dòng, UTF-8, không PII (xem data-model.md).
- Heartbeat: dòng comment `: ping` mỗi 25s (client bỏ qua).
- Headers: `Content-Type: text/event-stream`, `Cache-Control: no-cache`, `Connection: keep-alive`, `X-Accel-Buffering: no`.
- Auth: `Authorization: Bearer <accessToken>` (fetch-stream); không hỗ trợ query token.
- Lỗi guard: `401 UNAUTHENTICATED` (thiếu/sai session), `403 FORBIDDEN` (customer vào admin stream), `429 RATE_LIMITED` (rate limit toàn cục), `503 SERVICE_UNAVAILABLE` (hub chưa wire) — trả envelope JSON thường, không phải SSE.
- Mở đầu stream server gửi `: connected` một lần, rồi heartbeat `: ping` mỗi 25s (client bỏ qua mọi dòng `:`).
