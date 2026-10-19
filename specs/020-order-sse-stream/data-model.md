# Data Model: Order SSE Live Notifications (020)

Không có bảng/migration mới. Mọi sự kiện là dẫn xuất live từ `orders` đã có.

## In-memory shapes (không lưu trữ)

### Topic

- `user:<uuid>` — stream của đúng một khách (uuid = session subject).
- `admins` — mọi session có role ADMIN.

### SSE Event (wire: `event:` + `data:` JSON một dòng)

| Event | Topic | Payload JSON | Phát khi |
|---|---|---|---|
| `order.needs_confirmation` | `admins` | `{"orderId":"<uuid>","version":<n>,"at":"<rfc3339>"}` | checkout tạo `PENDING`; edit đưa về `PENDING` |
| `order.status_changed` | `user:<owner>` | `{"orderId":"<uuid>","from":"<ST>","to":"<ST>","version":<n>,"at":"<rfc3339>"}` | mọi transition thành công |
| `order.feedback_received` | `admins` | `{"orderId":"<uuid>","at":"<rfc3339>"}` | feedback lưu thành công |

`from/to` là giá trị `status` CHECK hiện có (`PENDING`, `PAYMENT_PENDING`, `PAID`, `SHIPPING`, `DELIVERED`, `COMPLETED`, `CANCELLED`).

## Ràng buộc

- Không PII trong payload (không tên/SĐT/địa chỉ/email/nội dung feedback); chi tiết qua REST đã guard.
- Không log body event; log chỉ `topic`, `type`, `orderId`.
- Subscriber buffer 32; drop + đếm khi đầy; ngắt kết nối dọn subscriber.
