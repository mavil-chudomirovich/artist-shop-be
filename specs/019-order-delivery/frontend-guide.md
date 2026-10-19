# Frontend Integration Guide — Order Delivery & Completion

**Feature**: `019-order-delivery` (mở rộng module 07 Order, thay thế `010-order-confirmation`)
**Trạng thái**: **Mười bốn** endpoint của module 07 đã được ghi vào `docs/api-reference.md` §9.
**Đối tượng đọc**: Lập trình viên frontend (đơn hàng của khách đã đăng nhập, và bàn quản trị đơn của operator).
**Mục tiêu**: FE triển khai luồng giao hàng & hoàn tất chỉ với tài liệu này — không cần đọc code backend
hay Swagger.

> Nguồn authoritative là [`docs/api-reference.md`](../../docs/api-reference.md) §9. Guide này **phải
> khớp** với file đó; khi lệch, `docs/api-reference.md` thắng. Mọi tên trường dưới đây là **đúng**
> `json:"…"` mà DTO phát ra. Hợp đồng máy đọc được là
> [`contracts/openapi.yaml`](contracts/openapi.yaml).
>
> **Guide này thay thế [`specs/010-order-confirmation/frontend-guide.md`](../010-order-confirmation/frontend-guide.md).**
> Mọi thứ về enum trạng thái (`SHIPPED`), `POST /admin/orders/{orderId}/ship` (nay multipart) và
> `POST /admin/orders/{orderId}/complete` (đã xoá) trong guide cũ **không còn đúng**; đọc guide này.

---

## 0. Tóm tắt nhanh (TL;DR)

- **14 endpoint**: **7** của **KHÁCH đã đăng nhập** dưới `/orders`, **7** của **ADMIN** dưới `/admin/orders`.
  So với `010`: **3 MỚI**, **1 ĐỔI**, **1 XOÁ**, **10 giữ nguyên**.
- **Đổi enum phá vỡ hợp đồng**: `SHIPPED` **đổi tên** thành **`SHIPPING`** (đang giao) và thêm
  **`DELIVERED`** (giao thành công) — bộ trạng thái đơn còn **bảy** giá trị. FE **phải** cập nhật mọi
  chỗ so sánh/ánh xạ `SHIPPED`.
- **Dispatch nay là `multipart`**: `POST /admin/orders/{orderId}/ship` gửi kèm **0–5 ảnh** `photos`
  (JPEG/PNG/WebP ≤ 2 MB). Ảnh là **tuỳ chọn**.
- **Deliver là endpoint mới**: `POST /admin/orders/{orderId}/deliver` (`SHIPPING → DELIVERED`), cùng
  hình dạng multipart, ghi mốc `deliveredAt`.
- **Hoàn tất thuộc về khách**: `POST /orders/{orderId}/complete` (không body) đưa đơn `DELIVERED` của
  mình sang `COMPLETED`; hoặc đơn **tự hoàn tất sau 120 giờ** kể từ `deliveredAt` (FE thấy trạng thái,
  **không** nhận lỗi).
- **`POST /admin/orders/{orderId}/complete` đã bị XOÁ** — operator **không** còn hoàn tất thay khách.
- **Feedback cấp đơn**: `POST /orders/{orderId}/feedback` (**`multipart/form-data`**: `content` 1–2000 ký tự + **0–5** phần `photos`), tối đa **một lần** mỗi đơn, chỉ khi đơn `DELIVERED`/`COMPLETED`.
- **Detail đơn mang thêm bốn trường**: `deliveredAt`, `completedAt`, `deliveryPhotos` (mảng) và
  `feedback` (object hoặc `null`). **Danh sách không có** các trường này.
- **Không đụng tiền/tồn**: các bước giao và hoàn tất **không** đổi `total` và **không** tạo
  `inventory_transactions`.
- Tác nhân luôn lấy từ **session**; đơn của khách khác trả **cùng** `404 ORDER_NOT_FOUND`.

---

## 1. Quy ước

- Base path `/api/v1`; envelope `{data, meta}` / `{error}` như các guide trước.
- Mọi route yêu cầu **Bearer**. Thiếu/sai token → `401 UNAUTHENTICATED`. Route admin yêu cầu role
  `ADMIN`; `CUSTOMER` gọi route admin → `403 FORBIDDEN`.
- Tác nhân (chủ đơn khi `complete`/`feedback`; administrator khi `ship`/`deliver`) lấy từ **session**,
  không bao giờ từ body. Body có member lạ → `MALFORMED_REQUEST` (`additionalProperties: false`).
- `orderId` là UUID. Không phải UUID → `400 VALIDATION_ERROR` với `details[].field = "orderId"`.
- `page` mặc định `1` (tối thiểu `1`); `pageSize` mặc định `20`, khoảng `1..100`.
- Tiền là **số nguyên đơn vị nhỏ nhất + currency**, không bao giờ là số thực.
- **Upload**: ba route `ship`/`deliver`/`feedback` nhận `multipart/form-data` với trường `photos` (file);
  trường khác bị bỏ qua. Gửi JSON cho ba route này là **sai** (`400 VALIDATION_ERROR`/`MALFORMED_REQUEST`).

### Kiểu dữ liệu *(mọi trường JSON của **mọi** DTO client thấy)*

`MoneyResponse` (2 member) — tiền:
```json
{ "amount": 150000, "currency": "VND" }
```

`OrderLineResponse` (8 member) — một dòng đơn, một **snapshot** lúc checkout/sửa:
```json
{
  "productId": "b2f1c0d4-5a6e-4b7c-8d9e-0f1a2b3c4d5e",
  "productName": "Tranh sơn dầu",
  "variantId": "c3a2b1e5-6f7a-4c8d-9e0f-1a2b3c4d5e6f",
  "variantName": "Bản lớn",
  "quantity": 2,
  "unitPrice": { "amount": 150000, "currency": "VND" },
  "lineTotal": { "amount": 300000, "currency": "VND" },
  "position": 0
}
```
- `productId`/`variantId`: UUID **đã ghi nhận** — **không** phải tham chiếu sống.
- `productName`/`variantName`: giá trị **tại lúc chụp**. SKU và mã sản phẩm **không** trả ra view khách.
- `position`: thứ tự dòng trong đơn.

`OrderAddressResponse` (7 member) — địa chỉ giao **chụp**:
```json
{
  "recipientName": "Nguyễn Thị An",
  "recipientPhone": "0912345678",
  "provinceCode": "01",
  "provinceName": "Hà Nội",
  "wardCode": "00004",
  "wardName": "Ba Đình",
  "streetAddress": "12 Ngõ 129 Dịch Vọng"
}
```
Cả 7 member là chuỗi, luôn có mặt.

**`DeliveryPhotoResponse` (6 member) — một ảnh bằng chứng giao hàng (MỚI ở feature 019):**
```json
{
  "id": "a1b2c3d4-5e6f-4a7b-8c9d-0e1f2a3b4c5d",
  "stage": "SHIPPING",
  "url": "https://res.cloudinary.com/.../dispatch-1.jpg",
  "width": 1600,
  "height": 1200,
  "position": 0
}
```
- `stage`: bước giao mà ảnh làm bằng chứng — `"SHIPPING"` (bàn giao cho vận chuyển) hoặc `"DELIVERED"`
  (giao tới khách).
- `url`: link hiển thị ảnh (HTTPS).
- `width`/`height`: kích thước pixel đã lưu.
- `position`: thứ tự ảnh trong cùng `stage` (bắt đầu 0). Ảnh đọc theo `stage` rồi `position`.

**`FeedbackPhotoResponse` (5 member) — một ảnh gắn vào feedback (MỚI, mở rộng 019, FR-025):**
```json
{ "id": "…", "url": "https://res.cloudinary.com/.../feedback-1.jpg", "width": 1600, "height": 1200, "position": 0 }
```
- `url`: link hiển thị ảnh (HTTPS). `width`/`height`: kích thước pixel đã lưu.
- `position`: thứ tự ảnh trong feedback (bắt đầu 0). Ảnh đọc theo `position`.
- **Không** có `stage` (khác `DeliveryPhotoResponse`): feedback là một tập ảnh, không chia bước.

**`FeedbackResponse` (3 member) — feedback cấp đơn (MỚI ở feature 019; thêm `photos` ở mở rộng FR-025):**
```json
{ "content": "Cảm ơn artist, tranh rất đẹp!", "createdAt": "2026-10-18T09:00:00Z", "photos": [] }
```
- `photos`: **luôn là mảng**; feedback không ảnh trả `[]`, **không** phải `null`.

`OrderSummaryResponse` (5 member) — một dòng của danh sách khách:
```json
{
  "id": "0f5c6e0c-1a44-4a1e-9b3d-9a1b2c3d4e5f",
  "status": "SHIPPING",
  "total": { "amount": 300000, "currency": "VND" },
  "itemCount": 2,
  "createdAt": "2026-10-09T08:15:04Z"
}
```
- `status`: một trong **7** giá trị (§4).
- **Danh sách KHÔNG mang** `deliveredAt`/`completedAt`/`deliveryPhotos`/`feedback`.

**`OrderResponse` (11 member) — chi tiết khách = summary + `address` + `lines` + 4 trường giao hàng:**
```json
{
  "id": "0f5c6e0c-1a44-4a1e-9b3d-9a1b2c3d4e5f",
  "status": "DELIVERED",
  "total": { "amount": 300000, "currency": "VND" },
  "itemCount": 2,
  "createdAt": "2026-10-09T08:15:04Z",
  "address": { /* OrderAddressResponse */ },
  "lines": [ /* OrderLineResponse… */ ],
  "deliveredAt": "2026-10-18T08:00:00Z",
  "completedAt": null,
  "deliveryPhotos": [
    { "id": "…", "stage": "SHIPPING", "url": "…", "width": 1600, "height": 1200, "position": 0 },
    { "id": "…", "stage": "DELIVERED", "url": "…", "width": 1600, "height": 1200, "position": 0 }
  ],
  "feedback": null
}
```
- `deliveredAt`: mốc giao thành công (ISO 8601) hoặc `null` trước khi giao.
- `completedAt`: mốc hoàn tất (khách xác nhận hoặc tự động) hoặc `null`.
- `deliveryPhotos`: **luôn là mảng**; đơn chưa có ảnh trả `[]`, **không** phải `null`.
- `feedback`: object `{ content, createdAt }` hoặc `null`.
- **Chỉ có trên chi tiết**, không trên danh sách.

`AdminOrderSummaryResponse` (6 member) — dòng danh sách admin = `OrderSummaryResponse` **+** `userId`:
```json
{
  "id": "0f5c6e0c-1a44-4a1e-9b3d-9a1b2c3d4e5f",
  "status": "SHIPPING",
  "total": { "amount": 300000, "currency": "VND" },
  "itemCount": 2,
  "createdAt": "2026-10-09T08:15:04Z",
  "userId": "11111111-1111-1111-1111-111111111111"
}
```

`AdminOrderResponse` (12 member) — chi tiết admin = `OrderResponse` **+** `userId`. Admin đọc **cùng**
bộ `deliveredAt`/`completedAt`/`deliveryPhotos`/`feedback` như khách.

`CheckoutRequest` (1 member) — body `POST /orders`:
```json
{ "addressId": "b2f1c0d4-5a6e-4b7c-8d9e-0f1a2b3c4d5e" }
```

`EditLineRequest` (2 member) — một dòng trong body sửa đơn:
```json
{ "variantId": "c3a2b1e5-6f7a-4c8d-9e0f-1a2b3c4d5e6f", "quantity": 3 }
```

`EditOrderRequest` (2 member) — body `PUT /orders/{orderId}`:
```json
{
  "addressId": "b2f1c0d4-5a6e-4b7c-8d9e-0f1a2b3c4d5e",
  "lines": [ { "variantId": "c3a2b1e5-6f7a-4c8d-9e0f-1a2b3c4d5e6f", "quantity": 3 } ]
}
```

`TransferRequest` (1 member) — body `POST /admin/orders/{orderId}/transfer`:
```json
{ "email": "recipient@example.com" }
```

**Body `POST /orders/{orderId}/feedback` (MỚI ở feature 019; `multipart/form-data` ở mở rộng FR-025):**
hai trường — `content` (text) và `photos` (file, 0–5 phần).
```bash
curl -X POST "$BASE/orders/$O/feedback" -H "Authorization: Bearer $CUSTOMER" \
  -F 'content=Cảm ơn artist, tranh rất đẹp!' -F photos=@a.jpg
```
- `content`: **bắt buộc**, **1 đến 2000 ký tự** sau khi cắt khoảng trắng. Rỗng hoặc quá dài →
  `400 VALIDATION_ERROR`, `details[].field = "content"`.
- `photos`: **tuỳ chọn**, tối đa **5** ảnh, mỗi ảnh JPEG/PNG/WebP ≤ 2 MB; nhiều hơn 5 →
  `409 ORDER_IMAGE_LIMIT_REACHED`, sai định dạng → `400 ORDER_IMAGE_TYPE_UNSUPPORTED`, quá 2 MB →
  `413 ORDER_IMAGE_TOO_LARGE` (`details[].field = "photos"`).

---

## 2. Bảng endpoint

| # | Method | Path | Auth | Trạng thái | Trả về |
|---|---|---|---|---|---|
| 9.1 | POST | `/api/v1/orders` | Khách | giữ nguyên | `201` `OrderResponse` |
| 9.2 | GET | `/api/v1/orders` | Khách | giữ nguyên | `200` `[OrderSummaryResponse]` (phân trang) |
| 9.3 | GET | `/api/v1/orders/{orderId}` | Khách | giữ nguyên | `200` `OrderResponse` |
| 9.4 | PUT | `/api/v1/orders/{orderId}` | Khách | giữ nguyên | `200` `OrderResponse` |
| 9.5 | POST | `/api/v1/orders/{orderId}/cancel` | Khách | giữ nguyên | `200` `OrderResponse` |
| 9.6 | POST | `/api/v1/orders/{orderId}/complete` | Khách | **MỚI** | `200` `OrderResponse` |
| 9.7 | POST | `/api/v1/orders/{orderId}/feedback` | Khách | **MỚI** | `200` `OrderResponse` |
| 9.8 | GET | `/api/v1/admin/orders` | ADMIN | giữ nguyên | `200` `[AdminOrderSummaryResponse]` (phân trang) |
| 9.9 | GET | `/api/v1/admin/orders/{orderId}` | ADMIN | giữ nguyên | `200` `AdminOrderResponse` |
| 9.10 | POST | `/api/v1/admin/orders/{orderId}/confirm` | ADMIN | giữ nguyên | `200` `AdminOrderResponse` |
| 9.11 | POST | `/api/v1/admin/orders/{orderId}/reject` | ADMIN | giữ nguyên | `200` `AdminOrderResponse` |
| 9.12 | POST | `/api/v1/admin/orders/{orderId}/ship` | ADMIN | **ĐỔI** (multipart) | `200` `AdminOrderResponse` |
| 9.13 | POST | `/api/v1/admin/orders/{orderId}/deliver` | ADMIN | **MỚI** (multipart) | `200` `AdminOrderResponse` |
| ~~9.11 cũ~~ | POST | `/api/v1/admin/orders/{orderId}/complete` | — | **XOÁ** | — |

> Số mục `9.x` của admin **dời** so với guide 010 (`ship` 9.10 cũ → **9.12**; `transfer` 9.12 cũ →
> **9.14**; admin list/detail/confirm/reject 9.6–9.9 cũ → **9.8–9.11**) vì hai mục của khách (`9.6`,
> `9.7`) được chèn lên trước.

### Chi tiết endpoint **MỚI**/**ĐỔI**/**XOÁ**

**9.6 `POST /orders/{orderId}/complete`** — **MỚI**. Khách xác nhận đã nhận đơn `DELIVERED` **của mình**:
đơn → `COMPLETED`, ghi `completedAt`. Không body. Đơn của khách khác → `404 ORDER_NOT_FOUND`; đơn không
ở `DELIVERED` → `409 ORDER_STATE_TRANSITION_INVALID` (message nêu trạng thái hiện tại).
Lỗi: `VALIDATION_ERROR` 400 · `UNAUTHENTICATED` 401 · `ORDER_NOT_FOUND` 404 ·
`ORDER_STATE_TRANSITION_INVALID` 409 · `RATE_LIMITED` 429 · `INTERNAL_ERROR` 500.

**9.7 `POST /orders/{orderId}/feedback`** — **MỚI**, body **`multipart/form-data`** (mở rộng FR-025).
Khách gửi **một** feedback cấp đơn cho artist: `content` (1–2000 ký tự) + 0–5 `photos`. Đơn phải
`DELIVERED`/`COMPLETED`; gửi lần hai bị từ chối. Response `200 OrderResponse` với `feedback` (gồm
`photos[]`).
Lỗi: `VALIDATION_ERROR` 400 (`content` rỗng/quá dài) · `MALFORMED_REQUEST` 400 ·
`ORDER_IMAGE_TYPE_UNSUPPORTED` 400 · `UNAUTHENTICATED` 401 · `ORDER_NOT_FOUND` 404 ·
`ORDER_FEEDBACK_NOT_ALLOWED` 409 · `ORDER_FEEDBACK_ALREADY_SENT` 409 · `ORDER_IMAGE_LIMIT_REACHED` 409 ·
`ORDER_IMAGE_TOO_LARGE` 413 · `RATE_LIMITED` 429 · `INTERNAL_ERROR` 500 · `ORDER_MEDIA_UNAVAILABLE` 503.

**9.12 `POST /admin/orders/{orderId}/ship`** — **ĐỔI sang `multipart/form-data`**. Đưa đơn `PAID` sang
`SHIPPING`, kèm **0–5 ảnh** `photos`. Không ảnh vẫn thành công.
Lỗi: `VALIDATION_ERROR` 400 · `MALFORMED_REQUEST` 400 · `ORDER_IMAGE_TYPE_UNSUPPORTED` 400 ·
`UNAUTHENTICATED` 401 · `FORBIDDEN` 403 · `ORDER_NOT_FOUND` 404 · `ORDER_STATE_TRANSITION_INVALID` 409 ·
`ORDER_IMAGE_LIMIT_REACHED` 409 · `ORDER_IMAGE_TOO_LARGE` 413 · `RATE_LIMITED` 429 ·
`INTERNAL_ERROR` 500 · `ORDER_MEDIA_UNAVAILABLE` 503.

**9.13 `POST /admin/orders/{orderId}/deliver`** — **MỚI**, cùng hình dạng multipart như 9.12. Đưa đơn
`SHIPPING` sang `DELIVERED`, ghi `deliveredAt`, kèm **0–5 ảnh** `photos`.
Lỗi: như 9.12, với `409 ORDER_STATE_TRANSITION_INVALID` khi đơn không ở `SHIPPING`.

**`POST /admin/orders/{orderId}/complete`** — **XOÁ** (không còn route). Gọi vào trả `404 NOT_FOUND`
(hoặc `405` nếu path khớp route khác). Thay bằng 9.6 (khách) và cơ chế tự hoàn tất 120 giờ.

---

## 3. Trước → sau

### Endpoint **MỚI** (3)

| # | Method | Path | Trước | Sau |
|---|---|---|---|---|
| 9.6 | POST | `/api/v1/orders/{orderId}/complete` | *chưa tồn tại* | `200` `OrderResponse` (`DELIVERED → COMPLETED`, khách xác nhận) |
| 9.7 | POST | `/api/v1/orders/{orderId}/feedback` | *chưa tồn tại* | `200` `OrderResponse` (ghi feedback cấp đơn, một lần mỗi đơn) |
| 9.13 | POST | `/api/v1/admin/orders/{orderId}/deliver` | *chưa tồn tại* | `200` `AdminOrderResponse` (`SHIPPING → DELIVERED`, ghi `deliveredAt`, 0–5 ảnh) |

### Endpoint **ĐỔI** (1)

| # | Method | Path | Trước (010) | Sau (019) |
|---|---|---|---|---|
| 9.12 | POST | `/api/v1/admin/orders/{orderId}/ship` | Không body JSON; trả `status: "SHIPPED"` | **`multipart/form-data`** với 0–5 `photos`; trả `status: "SHIPPING"` |

### Endpoint **XOÁ** (1)

| # | Method | Path | Trước (010) | Sau (019) |
|---|---|---|---|---|
| — | POST | `/api/v1/admin/orders/{orderId}/complete` | Admin hoàn tất đơn đã giao (`-> COMPLETED`) | **XOÁ**: hoàn tất do khách (9.6) hoặc tự động 120 giờ |

### Không phải endpoint nhưng **ĐỔI bề mặt client** (3)

| Bề mặt | Trước (010) | Sau (019) |
|---|---|---|
| Enum `OrderStatus` trong **mọi** response có `status` | **6** giá trị, gồm `SHIPPED` | **7** giá trị; `SHIPPED` **đổi tên** `SHIPPING`, thêm `DELIVERED` |
| Detail đơn (`OrderResponse`/`AdminOrderResponse`) | 7/8 member, không có sự kiện giao hàng | **11/12** member; thêm `deliveredAt`, `completedAt`, `deliveryPhotos[]`, `feedback` |
| `FeedbackResponse` trong detail | 2 member (`content`, `createdAt`) | **3** member; thêm `photos[]` (`FeedbackPhotoResponse`, 5 member) |

**Tổng: 3 mới, 1 endpoint đổi, 1 endpoint xoá, 1 enum đổi (breaking), 1 hình dạng detail đổi, 10 endpoint
giữ nguyên.** Không endpoint nào của module khác bị đổi.

---

## 4. Bảng giá trị enum `OrderStatus`

`status` là một trong **7** giá trị:

| Giá trị | Ý nghĩa | Vào bằng | Ra được không |
|---|---|---|---|
| `PENDING` | **Chờ artist xác nhận** — chưa giữ hàng | Checkout tạo đơn | → `PAYMENT_PENDING` hoặc → `CANCELLED` |
| `PAYMENT_PENDING` | **Chờ thanh toán** — hàng đang **giữ** | Artist `confirm` | → `PAID`, → `PENDING` (khách sửa), hoặc → `CANCELLED` |
| `PAID` | Đã trả tiền — giữ chỗ đã thành **bán** | Module 08 xác nhận thanh toán (không có HTTP) | → `SHIPPING` |
| `SHIPPING` | **Đang giao** — đã giao cho bên vận chuyển (**đổi tên từ `SHIPPED`**) | Operator `ship` (9.12) | → `DELIVERED` |
| `DELIVERED` | **Giao thành công** — khách đã nhận (**MỚI**) | Operator `deliver` (9.13) | → `COMPLETED` (khách xác nhận hoặc tự động 120h) |
| `COMPLETED` | Hoàn tất | Khách `complete` (9.6) hoặc sweeper tự hoàn tất | **Không** (cuối) |
| `CANCELLED` | Đã huỷ/từ chối/hết hạn | Khách `cancel`, admin `reject`, hoặc sweeper | **Không** (cuối) |

> **`SHIPPED` của `010` đã bị ĐỔI TÊN thành `SHIPPING`; `DELIVERED` là giá trị MỚI.** FE **phải** cập
> nhật mọi chỗ so sánh/ánh xạ `SHIPPED` và bổ sung `DELIVERED`. Đây là **thay đổi phá vỡ hợp đồng công
> khai** — phát hành đồng bộ với backend (xem ADR 023).
>
> `COMPLETED` vẫn là cuối. Operator **không** còn đánh dấu hoàn tất trực tiếp; đừng hiển thị nút
> "Hoàn tất" cho admin nữa.

---

## 5. Bảng mã lỗi

**Riêng module order** (**17** mã; ▲ = mới trong feature 019):

| Code | HTTP | FE nên làm |
|---|---|---|
| `ORDER_NOT_FOUND` | 404 | Đơn không tồn tại **hoặc** của khách khác. Hiện "không tìm thấy đơn", tải lại danh sách. `details` không có |
| `ORDER_CART_EMPTY` | 409 | Giỏ rỗng khi checkout. Báo khách giỏ trống; **không** tự thử lại |
| `ORDER_ITEM_NOT_PURCHASABLE` | 409 | Một dòng không còn bán/đã xoá. `details[].field = "variantId"` |
| `ORDER_ITEM_PRICE_CHANGED` | 409 | Giá một dòng đã đổi so với lúc checkout. `details[].field = "variantId"` |
| `ORDER_QUANTITY_EXCEEDS_AVAILABLE` | 409 | Một dòng vượt tồn, hoặc không giữ được hàng. `details[].field = "variantId"`, `issue` nêu số khả dụng |
| `ORDER_NO_ADDRESS` | 409 | Khách chưa có địa chỉ giao. Mời thêm địa chỉ (module 02) |
| `ORDER_STATE_TRANSITION_INVALID` | 409 | Bước chuyển không hợp lệ với trạng thái hiện tại; `message` nêu trạng thái. Tải lại đơn |
| `ORDER_NOT_TRANSFERABLE` | 409 | Đơn chưa `PAID` nên không chuyển nhượng được. Ẩn/disable nút transfer |
| `ORDER_TRANSFER_TARGET_NOT_FOUND` | 404 | Email không ứng với tài khoản nào |
| `ORDER_NOT_EDITABLE` | 409 | Đơn **đã trả tiền** (hoặc xa hơn) nên không sửa được |
| `ORDER_EMPTY` | 409 | Một lần sửa để đơn **còn 0 dòng**. Giữ **≥ 1 dòng**, hoặc **huỷ đơn** |
| ▲ `ORDER_IMAGE_TYPE_UNSUPPORTED` | 400 | Ảnh gửi không phải JPEG/PNG/WebP. `details[].field = "photos"`. Chọn lại ảnh |
| ▲ `ORDER_IMAGE_TOO_LARGE` | 413 | Ảnh vượt 2 MB, **hoặc** request chạm trần body. `details[].field = "photos"`. Nén/giảm ảnh |
| ▲ `ORDER_IMAGE_LIMIT_REACHED` | 409 | Gửi **hơn 5 ảnh** cho một bước. `details[].field = "photos"`. Gửi tối đa 5 |
| ▲ `ORDER_MEDIA_UNAVAILABLE` | 503 | Dịch vụ media từ chối/không tới được; **đơn không đổi**. Thử lại sau |
| ▲ `ORDER_FEEDBACK_NOT_ALLOWED` | 409 | Gửi feedback khi đơn chưa `DELIVERED`/`COMPLETED`; `message` nêu trạng thái. Chờ đơn được giao |
| ▲ `ORDER_FEEDBACK_ALREADY_SENT` | 409 | Đơn đã có feedback. Ẩn/disable form feedback |

**Dùng chung / tái sử dụng**: `VALIDATION_ERROR` 400 (kèm `details[].field`), `MALFORMED_REQUEST` 400,
`UNAUTHENTICATED` 401, `FORBIDDEN` 403, `RATE_LIMITED` 429, `INTERNAL_ERROR` 500.

---

## 6. Luồng giao hàng (nhắc lại cho FE)

- **Hai bước giao**: admin gửi **ảnh bàn giao** khi `ship` (`PAID → SHIPPING`), rồi **ảnh giao tới
  khách** khi `deliver` (`SHIPPING → DELIVERED`). Mỗi bước **0–5 ảnh**, ảnh là **tuỳ chọn**.
- **Upload multipart**: dùng `FormData` với các phần cùng tên `photos` (file). FE tự chọn giới hạn
  2 MB/ảnh và tối đa 5 ảnh để tránh lỗi `413`/`409`.
- **`deliveredAt`** được đặt ở bước `deliver`; nó **mở cửa sổ 120 giờ** để khách xác nhận hoặc hệ thống
  tự hoàn tất. FE nên hiển thị đếm ngược nếu muốn, nhưng **không** giả định trạng thái bất biến — hãy
  đọc lại đơn.
- **Khách xác nhận** (`9.6`) chỉ khi đơn `DELIVERED`; gọi ở trạng thái khác → `409` nêu trạng thái.
- **Feedback** (`9.7`): `multipart/form-data`, một lần mỗi đơn, chỉ khi `DELIVERED`/`COMPLETED`; `content`
  bắt buộc, kèm 0–5 `photos`. Sau khi gửi, `feedback` (kèm `photos[]`) xuất hiện trong detail.
- **Chỉ detail** mang `deliveredAt`/`completedAt`/`deliveryPhotos`/`feedback`; **danh sách không** — FE
  phải gọi `GET /orders/{orderId}` (hoặc admin tương ứng) để hiển thị bằng chứng.

---

## 7. Checklist FE

- [ ] **Cập nhật enum**: thay `SHIPPED` bằng `SHIPPING`, thêm `DELIVERED`; mọi `switch`/map theo
      `status` phải xử lý **7** giá trị.
- [ ] Bỏ mọi lời gọi `POST /admin/orders/{id}/complete` — route đã xoá; thay bằng luồng khách xác nhận.
- [ ] Đổi `POST /admin/orders/{id}/ship` sang **multipart** (`FormData` với `photos`); thêm màn
      `POST /admin/orders/{id}/deliver` cùng hình dạng.
- [ ] Hiển thị/đọc `deliveredAt`, `completedAt`, `deliveryPhotos[]`, `feedback` **chỉ ở detail**.
- [ ] Ảnh: chỉ JPEG/PNG/WebP, tối đa 2 MB mỗi ảnh, tối đa 5 ảnh mỗi bước; xử lý `ORDER_IMAGE_*` và
      `ORDER_MEDIA_UNAVAILABLE`.
- [ ] Màn khách: nút "Đã nhận hàng" chỉ khi `status == "DELIVERED"`; form feedback chỉ khi
      `DELIVERED`/`COMPLETED` và chưa có `feedback`.
- [ ] Form feedback gửi **`multipart/form-data`**: trường `content` + 0–5 file `photos` (JPEG/PNG/WebP
      ≤ 2 MB); đọc `feedback.photos[]` để hiển thị.
- [ ] Xử lý `ORDER_FEEDBACK_ALREADY_SENT`/`ORDER_FEEDBACK_NOT_ALLOWED` bằng ẩn/disable form và giải thích.
- [ ] Không gửi `userId`/`ownerId` ở body/query; tác nhân lấy từ phiên.
- [ ] Hiển thị tiền bằng `amount` (số nguyên) + `currency`; `total` **không** gồm phí ship.
- [ ] Trang admin: `status=PENDING&sort=oldest` vẫn là hàng đợi xác nhận FIFO; giữ `status` filter theo
      **7** giá trị.

---

## 8. Đồng bộ guide với mã nguồn (có đếm)

Lần đồng bộ này đọc lại source ngày **2026-10-19** và đếm theo quy tắc frontend-guide sync: **mọi** route
trong `router.go`, **mọi** thẻ `json:"…"` của DTO presentation và application, **mọi** giá trị enum
`OrderStatus`, **mọi** mã HTTP trong `errors.go`. Mọi con số dưới đây **đếm lại từ chính nguồn**, không
chép từ tài liệu khác.

### 8.1 Route — **14** (nguồn: `internal/modules/order/presentation/http/router.go`)

| Hướng | Kết quả |
|---|---|
| Guide → code | §2 liệt kê **14** mục (7 khách + 7 admin). `router.go` khai **14** route: `Router` có **7** (`GET /`, `POST /`, `GET /{orderId}`, `PUT /{orderId}`, `POST /{orderId}/cancel`, `POST /{orderId}/complete`, `POST /{orderId}/feedback`) và `AdminRouter` có **7** (`GET /`, `GET /{orderId}`, `POST /{orderId}/confirm`, `POST /{orderId}/reject`, `POST /{orderId}/ship`, `POST /{orderId}/deliver`, `POST /{orderId}/transfer`) |
| Code → guide | Cả **14** route đều có mặt ở §2. **Không** còn route `POST /admin/orders/{orderId}/complete` |

Khớp `contracts/openapi.yaml` (**4** operation dưới `paths:` cho các endpoint 019 chạm) và
`docs/api-reference.md` §9 (**14** mục `9.1`–`9.14`). **3 mới, 1 đổi, 1 xoá.**

### 8.2 Trường JSON — **55** thẻ `json:"…"` trên **14** kiểu (nguồn: `presentation/dto/dto.go`)

`application/dto/dto.go` có **0** thẻ `json` — chúng là type nội bộ của use case, **không** ra dây.

| Kiểu | Số member | Member |
|---|---|---|
| `CheckoutRequest` | 1 | `addressId` |
| `EditLineRequest` | 2 | `variantId`, `quantity` |
| `EditOrderRequest` | 2 | `addressId`, `lines` |
| `TransferRequest` | 1 | `email` |
| `FeedbackPhotoResponse` ▲ | 5 | `id`, `url`, `width`, `height`, `position` |
| `MoneyResponse` | 2 | `amount`, `currency` |
| `OrderLineResponse` | 8 | `productId`, `productName`, `variantId`, `variantName`, `quantity`, `unitPrice`, `lineTotal`, `position` |
| `OrderAddressResponse` | 7 | `recipientName`, `recipientPhone`, `provinceCode`, `provinceName`, `wardCode`, `wardName`, `streetAddress` |
| `DeliveryPhotoResponse` ▲ | 6 | `id`, `stage`, `url`, `width`, `height`, `position` |
| `FeedbackResponse` ▲ | 3 | `content`, `createdAt`, `photos` ▲ |
| `OrderSummaryResponse` | 5 | `id`, `status`, `total`, `itemCount`, `createdAt` |
| `OrderResponse` | 11 | `id`, `status`, `total`, `itemCount`, `createdAt`, `address`, `lines`, `deliveredAt` ▲, `completedAt` ▲, `deliveryPhotos` ▲, `feedback` ▲ |
| `AdminOrderSummaryResponse` | 1 (+5 nhúng) | `userId` |
| `AdminOrderResponse` | 1 (+11 nhúng) | `userId` |

Tổng: 1+2+2+1+5+2+8+7+6+3+5+11+1+1 = **55** thẻ trên **14** kiểu, thành **37** tên member phân biệt;
**37/37** đều có mặt ở §1. Năm tên **mới** trong 019 (kể cả mở rộng FR-025): `deliveredAt`, `completedAt`,
`deliveryPhotos`, `feedback`, `photos` (cộng ba kiểu mới `DeliveryPhotoResponse`, `FeedbackResponse`,
`FeedbackPhotoResponse`).

### 8.3 Enum `OrderStatus` — **7** giá trị (nguồn: `domain/constant/codes.go`)

`PENDING`, `PAYMENT_PENDING`, `PAID`, **`SHIPPING`** (đổi tên từ `SHIPPED`), **`DELIVERED`** (mới),
`COMPLETED`, `CANCELLED`. §4 liệt kê **đủ 7**. `SHIPPED` **không** còn là giá trị hợp lệ (khớp
`orders_status_ck` bảy giá trị của `migrations/00018_order_delivery.sql`).

### 8.4 Mã lỗi — **17** mã riêng của module + **6** mã tái sử dụng

Nguồn: `domain/constant/codes.go` (**17** hằng `Code*`) và `presentation/http/errors.go` (ánh xạ).
**6** mã mới trong 019: `ORDER_IMAGE_TYPE_UNSUPPORTED`, `ORDER_IMAGE_TOO_LARGE`,
`ORDER_IMAGE_LIMIT_REACHED`, `ORDER_MEDIA_UNAVAILABLE`, `ORDER_FEEDBACK_NOT_ALLOWED`,
`ORDER_FEEDBACK_ALREADY_SENT`. §5 liệt kê **đủ 17** (▲ đánh **đủ 6** mã mới). Ba mã ảnh
(`ORDER_IMAGE_*`) được **dùng chung** cho cả ảnh bằng chứng giao hàng **và** ảnh feedback (FR-025) —
mở rộng này **không** thêm mã mới.

### 8.5 HTTP status — **5** do `errors.go` ánh xạ, **11** trên bề mặt module

`presentation/http/errors.go` gọi `coded(...)` với **5** status khác nhau: `400` (`StatusBadRequest`),
`404` (`StatusNotFound`), `409` (`StatusConflict`), `413` (`StatusRequestEntityTooLarge`), `503`
(`StatusServiceUnavailable`); cộng `500` từ nhánh mặc định `httpx.Wrap(err, httpx.CodeInternal)` → **6**
status do `mapError` sinh ra. Bề mặt HTTP của module (gồm handler và middleware) là **11** status:
`200`, `201`, `400`, `401`, `403`, `404`, `409`, `413`, `429`, `500`, `503`. §5 chỉ dùng **11** status này;
module **không** dùng `204`, `405`, `415`.

### 8.6 Mismatch đã tìm và sửa

- Guide 010 ghi enum **6** giá trị và `PG /admin/orders/{id}/complete` giữ nguyên — **sai** sau 019; đã
  đánh dấu guide 010 **bị thay thế**.
- Guide 017 (admin dashboard) ghi `recentOrders[].status` là **6** giá trị và doanh thu chỉ gồm
  `PAID`/`SHIPPED`/`COMPLETED` — **sai** sau 019; đã sửa thành **7** giá trị và
  `PAID`/`SHIPPING`/`DELIVERED`/`COMPLETED`.
- Bản thân guide này ghi `POST /orders/{orderId}/feedback` là body JSON `{ "content": string }` và
  `FeedbackResponse` có **2** member — **sai** sau mở rộng FR-025; đã sửa thành **`multipart/form-data`**
  (`content` + 0–5 `photos`), `FeedbackResponse` **3** member (thêm `photos`) và thêm kiểu
  `FeedbackPhotoResponse` (5 member).
- Không còn mismatch nào khác giữa guide này và source tại thời điểm đồng bộ: **14** route, **55** thẻ
  `json`, **7** giá trị enum, **17+6** mã lỗi, **11** HTTP status — tất cả khớp.
