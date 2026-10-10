# Backend Modules & Delivery Roadmap

Mục lục các module backend, tách từ `project_overview.md` và `backend-spec.md`.
Mỗi module có một file riêng trong `docs/modules/` để làm nguồn cho quy trình
Spec Kit (`/speckit.specify` → `/speckit.plan` → `/speckit.tasks` → `/speckit.implement`).

Tài liệu tra cứu:
- `docs/architecture.md` — kiến trúc chuẩn (authoritative).
- `docs/api-reference.md` — **toàn bộ endpoint hiện có** (authoritative, phải cập
  nhật khi thêm/sửa API theo Constitution VIII).

## Nguyên tắc

- Mỗi module sở hữu dữ liệu của mình; giao tiếp liên module qua interface/domain event.
- Phụ thuộc một chiều: module nền tảng trước, module nghiệp vụ sau.
- Một module = một feature Speckit, triển khai và kiểm thử độc lập.

## Danh sách module

| # | Module | File | Version | Phụ thuộc | Đã specify |
|---|--------|------|---------|-----------|------------|
| 00 | Cross-cutting foundation | [00-cross-cutting.md](modules/00-cross-cutting.md) | V0 | — | ✔ `specs/002-cross-cutting-foundation` |
| 01 | Auth | [01-auth.md](modules/01-auth.md) | V0 | cross-cutting | ✔ `specs/001-user-auth` |
| 02 | User | [02-user.md](modules/02-user.md) | V0 | auth | ✔ `specs/003-user-profile` |
| 03 | Category | [03-category.md](modules/03-category.md) | V0 | cross-cutting | ✔ `specs/005-category-catalog` |
| 04 | Product | [04-product.md](modules/04-product.md) | V1.0 | category | ✔ `specs/006-product-catalog` |
| 05 | Inventory | [05-inventory.md](modules/05-inventory.md) | V1.0 | product | ✔ `specs/007-inventory-tracking` |
| 06 | Cart | [06-cart.md](modules/06-cart.md) | V1.0 | product, inventory | ✔ `specs/008-cart` |
| 07 | Order | [07-order.md](modules/07-order.md) | V1.0 | cart, product, inventory, user, auth | ✔ `specs/009-order`, `specs/010-order-confirmation` |
| 08 | Payment | [08-payment.md](modules/08-payment.md) | V1.0 | order | ⬜ |
| 09 | Shipping | [09-shipping.md](modules/09-shipping.md) | V1.2 | order | ⬜ |
| 10 | Commission | [10-commission.md](modules/10-commission.md) | V1.1 | user, payment | ⬜ |
| 11 | Chat | [11-chat.md](modules/11-chat.md) | V1.1 | user | ⬜ |
| 12 | Notification | [12-notification.md](modules/12-notification.md) | V1.2 | user | ⬜ |
| 13 | Review | [13-review.md](modules/13-review.md) | V2.0 | product, order | ⬜ |
| 14 | Wishlist | [14-wishlist.md](modules/14-wishlist.md) | V2.0 | product, user | ⬜ |
| 15 | Content | [15-content.md](modules/15-content.md) | V2.0 | cross-cutting | ⬜ |
| 16 | Admin | [16-admin.md](modules/16-admin.md) | V1.2 | tất cả | ⬜ |

## Thứ tự triển khai

**V0 — Nền tảng (đã xong)**: 00 cross-cutting → 01 auth → 02 user → 03 category
**V1.0 — MVP bán hàng (Shop)**: 04 product (đã xong) → 05 inventory (đã xong) → 06 cart (đã xong) → 07 order (đã xong) → 08 payment (PayOS). Mục tiêu: khách mua được sản phẩm vật lý. Đơn **chỉ gồm tiền hàng** (khách trả online qua PayOS); **phí ship khách trả khi nhận hàng**; `09` (tracking) hoãn sang V1.2. Luồng đơn của `07` (feature `010-order-confirmation`) có thêm bước **artist xác nhận**: đơn mới ở `PENDING` và **chưa giữ hàng**, artist xác nhận mới **giữ toàn bộ** (all-or-nothing) và mở **hạn thanh toán 60 phút**, khách **sửa được** đơn trước khi trả tiền.
**V1.1 — Commission**: 10 commission → 11 chat (tái dùng 08). Mục tiêu: nhận yêu cầu vẽ, báo giá, đặt cọc, thanh toán theo giai đoạn, và trao đổi với artist.
**V1.2 — Vận hành & phụ trợ**: 09 shipping → 12 notification → 16 admin. Tracking giao hàng, thông báo, dashboard + audit.
**V2.0 — Mở rộng**: 13 review → 14 wishlist → 15 content.

> **Vì sao tái thứ tự so với bản "Giai đoạn" cũ**: `07/08` mở khoá thanh toán — vốn là
> prerequisite chung — nên làm trước; `10/11` (commission) chỉ cần `user` + `payment` nên
> đứng ngay sau; `09/12/16` là "làm dày" trải nghiệm, không chặn việc bán hàng, nên lùi lại.
> Quyết định và đánh đổi ghi ở
> [ADR 015](decisions/015-shop-first-version-plan-and-payos.md).

## Việc phải chốt trước khi specify

**Đã chốt cho V1.0** (`06`, `07`, `08`) — chi tiết ở
[ADR 015 §5](decisions/015-shop-first-version-plan-and-payos.md):

- `06-cart`: giá **snapshot lúc thêm + đối chiếu lại ở checkout**.
- `07-order`: trạng thái `PENDING → PAYMENT_PENDING → PAID → SHIPPED → COMPLETED` (cộng `CANCELLED`);
  đơn chỉ gồm tiền hàng, **không có phí ship**. (Feature `010` **đổi tên** `PENDING_PAYMENT` thành
  `PAYMENT_PENDING` và thêm `PENDING` — đơn **chờ artist xác nhận**, chưa giữ hàng; hàng chỉ giữ khi
  artist xác nhận, hạn thanh toán 60 phút.)
- `08-payment`: PayOS **tự viết bằng stdlib** (không dependency); `orderCode` số ngẫu nhiên
  int53 + thử lại khi trùng; **không hoàn tiền**, huỷ chỉ cho đơn chưa thanh toán.

**Còn để ngỏ**:

- `10-commission` (V1.1): thống nhất state machine giữa `project_overview.md` và
  `backend-spec.md` (một bên có `Delivered`, bên kia có `APPROVED/PAID_FULL`).
- `12-notification` (V1.2): nhà cung cấp email — đã chọn **Brevo** (`01-auth` đã dùng); địa
  chỉ/IP gửi cần được uỷ quyền ở console provider (xem `004` T028b).
- `09-shipping` (V1.2): đơn vị vận chuyển ngoài (ViettelPost) — chốt cơ chế tạo vận
  đơn/tracking khi làm V1.2 (không gộp vào V1.0).

## Trạng thái Spec Kit

- `⬜` chưa specify — `✅` đã có spec — `🟦` đã có plan — `🟩` đã có tasks — `✔` đã implement

## Tham chiếu gốc

- `docs/product/project_overview.md`, `docs/product/backend-spec.md`, `.specify/memory/constitution.md`
