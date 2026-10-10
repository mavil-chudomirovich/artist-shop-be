# Frontend Integration Guide — Order Confirmation & Editing

**Feature**: `010-order-confirmation` (mở rộng module 07 Order, thay thế `009-order`)
**Trạng thái**: **Mười hai** endpoint của module 07 đã được ghi vào `docs/api-reference.md` §9.
**Đối tượng đọc**: Lập trình viên frontend (đơn hàng của khách đã đăng nhập, và bàn quản trị đơn của operator).
**Mục tiêu**: FE triển khai đơn hàng chỉ với tài liệu này — không cần đọc code backend hay Swagger.

> Nguồn authoritative là [`docs/api-reference.md`](../../docs/api-reference.md) §9. Guide này **phải
> khớp** với file đó; khi lệch, `docs/api-reference.md` thắng. Mọi tên trường dưới đây là **đúng**
> `json:"…"` mà DTO phát ra. Hợp đồng máy đọc được là
> [`contracts/openapi.yaml`](contracts/openapi.yaml).
>
> **Guide này thay thế [`specs/009-order/frontend-guide.md`](../009-order/frontend-guide.md).** Mọi thứ
> về enum trạng thái, `POST /orders` và `POST /orders/{orderId}/cancel` trong guide cũ **không còn
> đúng**; đọc guide này.

---

## 0. Tóm tắt nhanh (TL;DR)

- **12 endpoint**: **5** của **KHÁCH đã đăng nhập** dưới `/orders`, **7** của **ADMIN** dưới `/admin/orders`.
  So với `009`: **3 MỚI**, **3 ĐỔI**, **6 giữ nguyên**.
- **Chủ sở hữu lấy từ session**, không bao giờ từ body/query; **không** route nào nêu id chủ. Đơn của
  khách khác trả **cùng** `404 ORDER_NOT_FOUND`.
- **Checkout `POST /orders` nay tạo đơn `PENDING` (chờ artist xác nhận) và KHÔNG giữ hàng**: chụp snapshot
  dòng + địa chỉ, **không** đụng tồn kho, **làm rỗng** giỏ. Trước đây nó tạo `PENDING_PAYMENT` và **giữ**
  hàng.
- **Artist xác nhận** (`confirm`) mới **giữ toàn bộ** hàng (all-or-nothing) và đưa đơn sang
  `PAYMENT_PENDING`; artist **từ chối** (`reject`) đưa đơn `PENDING` sang `CANCELLED`.
- **Đơn `PAYMENT_PENDING` tự huỷ sau 60 phút** (`payment_expires_at`); đơn `PENDING` **không** hết hạn
  theo thời gian. FE thấy `status: "CANCELLED"`, **không** nhận lỗi.
- **Khách sửa đơn** (`PUT /orders/{orderId}`) khi đơn `PENDING` hoặc `PAYMENT_PENDING`; sửa đơn
  `PAYMENT_PENDING` **trả hàng** và đưa đơn **quay lại `PENDING`** (artist phải xác nhận lại).
- **`total` = tổng `quantity × unitPrice`**, đơn vị nhỏ nhất + currency, **không** số thực, **không** làm
  tròn, **không** gồm phí ship.
- **Không có endpoint `pay`**: bước `PAID` do module 08 điều khiển, chưa có bề mặt HTTP.
- **Chuyển nhượng** (`transfer`) chỉ đổi **chủ sở hữu** (nêu bằng email), **không** đổi dòng/trạng
  thái/tổng, **không** đổi tồn kho; chỉ áp dụng cho đơn `PAID`.
- Module **không** có rate limit riêng; chỉ chịu hạn mức toàn cục.

---

## 1. Quy ước

- Base path `/api/v1`; envelope `{data, meta}` / `{error}` như các guide trước.
- Mọi route yêu cầu **Bearer**. Thiếu/sai token → `401 UNAUTHENTICATED`. Route admin yêu cầu role
  `ADMIN`; `CUSTOMER` gọi route admin → `403 FORBIDDEN`.
- Tác nhân (chủ đơn, khi checkout/sửa/huỷ; administrator, khi confirm/reject/ship/complete/transfer) lấy
  từ **session**, không bao giờ từ body. Body có member lạ → `MALFORMED_REQUEST` (`additionalProperties: false`).
- `orderId` là UUID. Không phải UUID → `400 VALIDATION_ERROR` với `details[].field = "orderId"`.
- `page` mặc định `1` (tối thiểu `1`); `pageSize` mặc định `20`, khoảng `1..100`. Ngoài khoảng →
  `400 VALIDATION_ERROR` với `details[].field` là `"page"` hoặc `"pageSize"`.
- Tiền là **số nguyên đơn vị nhỏ nhất + currency**, không bao giờ là số thực.
- Danh sách **mới nhất trước** (`createdAt`, rồi `id`) trừ khi `GET /admin/orders` được truyền `sort`.

### Kiểu dữ liệu *(mọi trường JSON của **mọi** DTO client thấy)*

`MoneyResponse` (2 member) — tiền:
```json
{ "amount": 150000, "currency": "VND" }
```
- `amount`: số nguyên, đơn vị nhỏ nhất của `currency`.
- `currency`: ba chữ in hoa.

`OrderLineResponse` (6 member) — một dòng đơn, một **snapshot** lúc checkout/sửa:
```json
{
  "productId": "b2f1c0d4-5a6e-4b7c-8d9e-0f1a2b3c4d5e",
  "name": "Tranh sơn dầu",
  "slug": "tranh-son-dau",
  "quantity": 2,
  "unitPrice": { "amount": 150000, "currency": "VND" },
  "lineTotal": { "amount": 300000, "currency": "VND" }
}
```
- `productId`: UUID sản phẩm **đã ghi nhận** — **không** phải tham chiếu sống; sản phẩm có thể đã bị xoá.
- `name` / `slug`: giá trị sản phẩm **tại lúc chụp**, không đọc lại.
- `quantity`: số nguyên **≥ 1**.
- `unitPrice`: giá **snapshot** (chụp lại mỗi lần sửa).
- `lineTotal`: `quantity × unitPrice`.

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
Cả 7 member là chuỗi, luôn có mặt. Một lần sửa địa chỉ sau đó **không** đổi đơn đã đặt; một lần sửa đơn
có thể thay địa chỉ giao của đơn (khi nêu `addressId`).

`OrderSummaryResponse` (5 member) — một dòng của danh sách khách:
```json
{
  "id": "0f5c6e0c-1a44-4a1e-9b3d-9a1b2c3d4e5f",
  "status": "PENDING",
  "total": { "amount": 300000, "currency": "VND" },
  "itemCount": 2,
  "createdAt": "2026-10-10T08:15:04Z"
}
```
- `id`: UUID đơn.
- `status`: một trong **6** giá trị (§3).
- `total`: tổng đơn (chỉ tiền hàng).
- `itemCount`: số dòng đơn.
- `createdAt`: mốc tạo đơn (ISO 8601).

`OrderResponse` (7 member) — chi tiết khách = summary + `address` + `lines`:
```json
{
  "id": "0f5c6e0c-1a44-4a1e-9b3d-9a1b2c3d4e5f",
  "status": "PAYMENT_PENDING",
  "total": { "amount": 300000, "currency": "VND" },
  "itemCount": 2,
  "createdAt": "2026-10-10T08:15:04Z",
  "address": { /* OrderAddressResponse */ },
  "lines": [ /* OrderLineResponse… */ ]
}
```
- `address`: `OrderAddressResponse`; luôn có.
- `lines`: mảng `OrderLineResponse`; luôn có (mảng, không bao giờ `null`).

`AdminOrderSummaryResponse` (6 member) — dòng danh sách admin = `OrderSummaryResponse` **+** `userId`:
```json
{
  "id": "0f5c6e0c-1a44-4a1e-9b3d-9a1b2c3d4e5f",
  "status": "PAYMENT_PENDING",
  "total": { "amount": 300000, "currency": "VND" },
  "itemCount": 2,
  "createdAt": "2026-10-10T08:15:04Z",
  "userId": "11111111-1111-1111-1111-111111111111"
}
```
- `userId`: tài khoản sở hữu đơn.

`AdminOrderResponse` (8 member) — chi tiết admin = `OrderResponse` **+** `userId`.

`CheckoutRequest` (1 member) — body của `POST /orders`:
```json
{ "addressId": "b2f1c0d4-5a6e-4b7c-8d9e-0f1a2b3c4d5e" }
```
- `addressId`: **tuỳ chọn**; bỏ trống hoặc `null` → dùng **địa chỉ mặc định** của khách. Không phải UUID
  hoặc không phải địa chỉ của khách → `400 VALIDATION_ERROR` (`details[].field = "addressId"`).

`EditLineRequest` (2 member) — một dòng trong body sửa đơn:
```json
{ "productId": "b2f1c0d4-5a6e-4b7c-8d9e-0f1a2b3c4d5e", "quantity": 3 }
```
- `productId`: **bắt buộc**, UUID sản phẩm.
- `quantity`: **bắt buộc**, số nguyên **≥ 1**.

`EditOrderRequest` (2 member) — body của `PUT /orders/{orderId}`:
```json
{
  "addressId": "b2f1c0d4-5a6e-4b7c-8d9e-0f1a2b3c4d5e",
  "lines": [
    { "productId": "b2f1c0d4-5a6e-4b7c-8d9e-0f1a2b3c4d5e", "quantity": 3 }
  ]
}
```
- `addressId`: **tuỳ chọn**; bỏ trống hoặc `null` → **giữ nguyên** địa chỉ hiện tại của đơn.
- `lines`: **bắt buộc**; **thay thế toàn bộ** tập dòng. Mảng rỗng (hoặc thiếu) → `409 ORDER_EMPTY`.

`TransferRequest` (1 member) — body của `POST /admin/orders/{orderId}/transfer`:
```json
{ "email": "recipient@example.com" }
```
- `email`: **bắt buộc**; email tài khoản nhận. Thiếu hoặc không phải email → `400 VALIDATION_ERROR`
  (`details[].field = "email"`).

---

## 2. Bảng endpoint

| # | Method | Path | Auth | Trạng thái | Trả về |
|---|---|---|---|---|---|
| 9.1 | POST | `/api/v1/orders` | Khách | **ĐỔI** | `201` `OrderResponse` |
| 9.2 | GET | `/api/v1/orders` | Khách | giữ nguyên | `200` `[OrderSummaryResponse]` (phân trang) |
| 9.3 | GET | `/api/v1/orders/{orderId}` | Khách | giữ nguyên | `200` `OrderResponse` |
| 9.4 | PUT | `/api/v1/orders/{orderId}` | Khách | **MỚI** | `200` `OrderResponse` |
| 9.5 | POST | `/api/v1/orders/{orderId}/cancel` | Khách | **ĐỔI** | `200` `OrderResponse` |
| 9.6 | GET | `/api/v1/admin/orders` | ADMIN | **ĐỔI** | `200` `[AdminOrderSummaryResponse]` (phân trang) |
| 9.7 | GET | `/api/v1/admin/orders/{orderId}` | ADMIN | giữ nguyên | `200` `AdminOrderResponse` |
| 9.8 | POST | `/api/v1/admin/orders/{orderId}/confirm` | ADMIN | **MỚI** | `200` `AdminOrderResponse` |
| 9.9 | POST | `/api/v1/admin/orders/{orderId}/reject` | ADMIN | **MỚI** | `200` `AdminOrderResponse` |
| 9.10 | POST | `/api/v1/admin/orders/{orderId}/ship` | ADMIN | giữ nguyên | `200` `AdminOrderResponse` |
| 9.11 | POST | `/api/v1/admin/orders/{orderId}/complete` | ADMIN | giữ nguyên | `200` `AdminOrderResponse` |
| 9.12 | POST | `/api/v1/admin/orders/{orderId}/transfer` | ADMIN | giữ nguyên | `200` `AdminOrderResponse` |

### Chi tiết endpoint **MỚI**/**ĐỔI**

**9.1 `POST /orders`** — checkout. **ĐỔI**: nay tạo đơn **`PENDING` (chờ artist xác nhận)** và **không**
giữ hàng (tồn khả dụng không đổi); trước đây tạo `PENDING_PAYMENT` và giữ hàng. Chụp snapshot từng dòng
và địa chỉ, làm rỗng giỏ, trong một thao tác. Body `CheckoutRequest` (có thể `{}`).
Lỗi: `VALIDATION_ERROR` 400 (`addressId`) · `MALFORMED_REQUEST` 400 · `UNAUTHENTICATED` 401 ·
`ORDER_CART_EMPTY` 409 · `ORDER_ITEM_NOT_PURCHASABLE` 409 · `ORDER_ITEM_PRICE_CHANGED` 409 ·
`ORDER_QUANTITY_EXCEEDS_AVAILABLE` 409 · `ORDER_NO_ADDRESS` 409 · `RATE_LIMITED` 429 · `INTERNAL_ERROR` 500.

**9.4 `PUT /orders/{orderId}`** — **MỚI**. Sửa đơn chưa trả tiền: thay **toàn bộ** tập dòng và (khi nêu)
địa chỉ. Body `EditOrderRequest`. Trả `200` `OrderResponse` với `status: "PENDING"` (đơn vốn
`PAYMENT_PENDING` cũng về `PENDING`).
Lỗi: `VALIDATION_ERROR` 400 (`addressId`/`productId`/`quantity`) · `MALFORMED_REQUEST` 400 ·
`UNAUTHENTICATED` 401 · `ORDER_NOT_FOUND` 404 · `ORDER_NOT_EDITABLE` 409 (đã trả tiền) · `ORDER_EMPTY` 409
· `ORDER_ITEM_NOT_PURCHASABLE` 409 · `ORDER_QUANTITY_EXCEEDS_AVAILABLE` 409 · `RATE_LIMITED` 429 ·
`INTERNAL_ERROR` 500.

**9.5 `POST /orders/{orderId}/cancel`** — **ĐỔI**. Nay huỷ được đơn ở **cả `PENDING` và
`PAYMENT_PENDING`** (trước đây chỉ `PENDING_PAYMENT`); đơn `PAYMENT_PENDING` trả hàng đã giữ, đơn
`PENDING` không có gì để trả. Đơn đã trả tiền → `409 ORDER_STATE_TRANSITION_INVALID`.
Lỗi: `VALIDATION_ERROR` 400 (`orderId`) · `UNAUTHENTICATED` 401 · `ORDER_NOT_FOUND` 404 ·
`ORDER_STATE_TRANSITION_INVALID` 409 · `RATE_LIMITED` 429 · `INTERNAL_ERROR` 500.

**9.6 `GET /admin/orders`** — **ĐỔI**. Thêm hai query **tuỳ chọn**: `status` (một trong 6 trạng thái) và
`sort` (`newest` mặc định, `oldest`). `status=PENDING&sort=oldest` = **hàng đợi xác nhận FIFO**.
Lỗi: `VALIDATION_ERROR` 400 (`page`/`pageSize`/`status`/`sort`) · `UNAUTHENTICATED` 401 · `FORBIDDEN` 403 ·
`RATE_LIMITED` 429 · `INTERNAL_ERROR` 500.

**9.8 `POST /admin/orders/{orderId}/confirm`** — **MỚI**. Artist xác nhận đơn `PENDING`: giữ **toàn bộ**
hàng all-or-nothing, mở hạn thanh toán 60 phút, đưa đơn sang `PAYMENT_PENDING`. Ghi audit
`ORDER_CONFIRMED`, thông báo khách.
Lỗi: `VALIDATION_ERROR` 400 (`orderId`) · `UNAUTHENTICATED` 401 · `FORBIDDEN` 403 · `ORDER_NOT_FOUND` 404 ·
`ORDER_STATE_TRANSITION_INVALID` 409 (không ở `PENDING`) · `ORDER_QUANTITY_EXCEEDS_AVAILABLE` 409 (một
dòng không giữ được, `details[].field = "productId"`) · `RATE_LIMITED` 429 · `INTERNAL_ERROR` 500.

**9.9 `POST /admin/orders/{orderId}/reject`** — **MỚI**. Artist từ chối đơn `PENDING` → `CANCELLED`; không
hàng nào bị giữ nên không cần trả. Ghi audit `ORDER_REJECTED`, thông báo khách.
Lỗi: `VALIDATION_ERROR` 400 (`orderId`) · `UNAUTHENTICATED` 401 · `FORBIDDEN` 403 · `ORDER_NOT_FOUND` 404 ·
`ORDER_STATE_TRANSITION_INVALID` 409 (không ở `PENDING`) · `RATE_LIMITED` 429 · `INTERNAL_ERROR` 500.

> **9.7/9.10/9.11/9.12** (`GET /admin/orders/{orderId}`, `ship`, `complete`, `transfer`) **giữ nguyên**
> hợp đồng của `009`; xem [`docs/api-reference.md`](../../docs/api-reference.md) §9.

---

## 3. Bảng giá trị enum `OrderStatus`

`status` là một trong **6** giá trị:

| Giá trị | Ý nghĩa | Vào bằng | Ra được không |
|---|---|---|---|
| `PENDING` | **Chờ artist xác nhận** — chưa giữ hàng | Checkout tạo đơn; sửa đơn `PAYMENT_PENDING` hoặc artist xác nhận lại | → `PAYMENT_PENDING` (artist `confirm`) hoặc → `CANCELLED` |
| `PAYMENT_PENDING` | **Chờ thanh toán** — hàng đang **giữ** | Artist `confirm` | → `PAID` (module 08), → `PENDING` (khách sửa), hoặc → `CANCELLED` (huỷ/hết hạn) |
| `PAID` | Đã trả tiền — giữ chỗ đã thành **bán** | Module 08 xác nhận thanh toán (không có HTTP) | → `SHIPPED` |
| `SHIPPED` | Đã giao | Admin `ship` | → `COMPLETED` |
| `COMPLETED` | Hoàn tất | Admin `complete` | **Không** (cuối) |
| `CANCELLED` | Đã huỷ/từ chối/hết hạn | Khách `cancel`, admin `reject`, hoặc sweeper | **Không** (cuối) |

> **`PENDING_PAYMENT` của `009` đã bị ĐỔI TÊN thành `PAYMENT_PENDING`.** Không còn giá trị enum nào tên
> `PENDING_PAYMENT`; FE **phải** cập nhật mọi chỗ so sánh/ánh xạ giá trị này. Đơn vẫn **không** có trường
> trạng thái thanh toán riêng; `PAID` chính là đã xác nhận thanh toán.

---

## 4. Trước → sau

### Endpoint **MỚI** (3)

| # | Method | Path | Trước | Sau |
|---|---|---|---|---|
| 9.4 | PUT | `/api/v1/orders/{orderId}` | *chưa tồn tại* | `200` `OrderResponse` (sửa đơn chưa trả tiền) |
| 9.8 | POST | `/api/v1/admin/orders/{orderId}/confirm` | *chưa tồn tại* | `200` `AdminOrderResponse` (`PENDING → PAYMENT_PENDING`, giữ hàng) |
| 9.9 | POST | `/api/v1/admin/orders/{orderId}/reject` | *chưa tồn tại* | `200` `AdminOrderResponse` (`PENDING → CANCELLED`) |

### Endpoint **ĐỔI** (3)

| # | Method | Path | Trước (009) | Sau (010) |
|---|---|---|---|---|
| 9.1 | POST | `/api/v1/orders` | Tạo đơn `PENDING_PAYMENT`, **giữ** hàng, làm rỗng giỏ | Tạo đơn **`PENDING`** (chờ artist), **không** giữ hàng, làm rỗng giỏ; artist được báo |
| 9.5 | POST | `/api/v1/orders/{orderId}/cancel` | Chỉ huỷ được `PENDING_PAYMENT` | Huỷ được **cả `PENDING` và `PAYMENT_PENDING`** (đơn `PAYMENT_PENDING` trả hàng) |
| 9.6 | GET | `/api/v1/admin/orders` | Chỉ phân trang, mới nhất trước | Thêm `status` + `sort` (tuỳ chọn); `status=PENDING&sort=oldest` = hàng đợi FIFO |

### Không phải endpoint nhưng **ĐỔI bề mặt client** (1)

| Bề mặt | Trước (009) | Sau (010) |
|---|---|---|
| Enum `OrderStatus` trong **mọi** response có `status` | 5 giá trị, gồm `PENDING_PAYMENT` | **6** giá trị; `PENDING_PAYMENT` **đổi tên** `PAYMENT_PENDING`, thêm `PENDING` |

**Tổng: 3 mới, 3 endpoint đổi, 1 enum đổi, 6 endpoint giữ nguyên.** Không endpoint nào bị **xoá**; không
endpoint nào của module khác bị đổi.

---

## 5. Bảng mã lỗi

**Riêng module order** (**11** mã; ▲ = mới trong feature 010):

| Code | HTTP | FE nên làm |
|---|---|---|
| `ORDER_NOT_FOUND` | 404 | Đơn không tồn tại **hoặc** của khách khác. Hiện "không tìm thấy đơn", tải lại danh sách. `details` không có |
| `ORDER_CART_EMPTY` | 409 | Giỏ rỗng khi checkout. Báo khách giỏ trống, mời thêm sản phẩm; **không** tự thử lại |
| `ORDER_ITEM_NOT_PURCHASABLE` | 409 | Một dòng không còn bán/đã xoá. `details[].field = "productId"`. Hiện món gây lỗi, đưa khách về giỏ hoặc màn sửa đơn |
| `ORDER_ITEM_PRICE_CHANGED` | 409 | Giá một dòng đã đổi so với giá khách thấy **lúc checkout**. `details[].field = "productId"`. Báo khách xem lại giỏ rồi checkout lại |
| `ORDER_QUANTITY_EXCEEDS_AVAILABLE` | 409 | Một dòng vượt tồn, hoặc không giữ được hàng (checkout/confirm/sửa). `details[].field = "productId"`, `issue` nêu số khả dụng. Gợi ý giảm còn số đó |
| `ORDER_NO_ADDRESS` | 409 | Khách chưa có địa chỉ giao. Mời khách thêm địa chỉ (module 02) rồi checkout lại |
| `ORDER_STATE_TRANSITION_INVALID` | 409 | Bước chuyển không hợp lệ với trạng thái hiện tại; `message` nêu trạng thái. Tải lại đơn để thấy trạng thái thật |
| `ORDER_NOT_TRANSFERABLE` | 409 | Đơn chưa `PAID` nên không chuyển nhượng được. Ẩn/disable nút transfer cho đơn chưa trả tiền |
| `ORDER_TRANSFER_TARGET_NOT_FOUND` | 404 | Email không ứng với tài khoản nào. Báo operator kiểm tra email |
| ▲ `ORDER_NOT_EDITABLE` | 409 | Đơn **đã trả tiền** (hoặc xa hơn) nên không sửa được. Ẩn/disable nút sửa cho đơn không ở `PENDING`/`PAYMENT_PENDING` |
| ▲ `ORDER_EMPTY` | 409 | Một lần sửa để đơn **còn 0 dòng**. Yêu cầu khách giữ **≥ 1 dòng**, hoặc **huỷ đơn** nếu muốn bỏ hết |

**Dùng chung / tái sử dụng**: `VALIDATION_ERROR` 400 (kèm `details[].field`), `MALFORMED_REQUEST` 400,
`UNAUTHENTICATED` 401, `FORBIDDEN` 403, `RATE_LIMITED` 429, `INTERNAL_ERROR` 500.

Bốn mã nêu món (`ORDER_ITEM_NOT_PURCHASABLE`, `ORDER_ITEM_PRICE_CHANGED`,
`ORDER_QUANTITY_EXCEEDS_AVAILABLE`, và mã `ORDER_EMPTY` nêu ở cấp đơn chứ không theo dòng) mang
`error.details[].field = "productId"` khi nêu một dòng; riêng mã số lượng, `issue` nêu số còn khả dụng —
FE gắn lỗi vào đúng dòng.

---

## 6. Trạng thái, giữ chỗ, sửa đơn và chuyển nhượng (nhắc lại cho FE)

- **Đặt hàng KHÔNG giữ hàng**: sau `POST /orders`, tồn khả dụng **không đổi**; chỉ khi artist `confirm`
  thì hàng mới bị giữ (vật lý không đổi, khả dụng **giảm**). FE **không** cần gọi API tồn kho.
- **Đơn `PAYMENT_PENDING` tự huỷ**: quá 60 phút (`payment_expires_at`, tính từ lúc xác nhận) tự thành
  `CANCELLED` và trả hàng. FE nên hiển thị đếm ngược nếu muốn, nhưng **không** giả định trạng thái bất
  biến — hãy đọc lại đơn. Đơn `PENDING` **không** hết hạn theo thời gian.
- **Trạng thái qua endpoint riêng**: để đổi trạng thái, gọi `confirm`/`reject`/`cancel`/`ship`/`complete`,
  **không** gửi `status` trong body. Một bước sai trả `409` kèm trạng thái hiện tại.
- **Sửa đơn**: chỉ khi đơn ở `PENDING`/`PAYMENT_PENDING`. Sửa đơn `PAYMENT_PENDING` **trả hàng** và đưa
  đơn về `PENDING`; sau đó artist **phải** xác nhận lại. FE phải tải lại đơn sau khi sửa.
- **Không có `pay`**: một đơn ở `PAYMENT_PENDING` chưa có cách trả tiền qua HTTP. Đừng giả định có luồng
  thanh toán cho tới khi module 08 ra đời.
- **Chuyển nhượng** chỉ đổi chủ; dòng/trạng thái/tổng giữ nguyên, tồn kho không đổi. FE cập nhật `userId`
  sau khi thành công; không tải lại dòng.

---

## 7. Checklist FE

- [ ] Cập nhật enum: thay `PENDING_PAYMENT` bằng `PAYMENT_PENDING`, xử lý thêm giá trị `PENDING`.
- [ ] Mọi request đơn gọi `/api/v1/orders*` (khách) hoặc `/api/v1/admin/orders*` (admin) với Bearer;
      **không** gửi `userId`/`ownerId` ở body/query.
- [ ] Hiển thị tiền bằng `amount` (số nguyên) + `currency`; **không** tự quy đổi hay làm tròn.
- [ ] Xử lý `total` là **chỉ tiền hàng** (không phí ship); không cộng phí ship vào tổng.
- [ ] Hiển thị `status` theo bảng §3; ẩn/disable nút `cancel`/sửa cho đơn **không** ở
      `PENDING`/`PAYMENT_PENDING`, nút `transfer` cho đơn không `PAID`.
- [ ] Trang admin: gọi `GET /admin/orders?status=PENDING&sort=oldest` cho màn hình xác nhận hàng đợi (FIFO).
- [ ] Màn sửa đơn gửi `PUT /orders/{id}` với **toàn bộ** `lines`; giữ **≥ 1 dòng**; xử lý `ORDER_EMPTY`
      bằng cách mời khách giữ ít nhất một dòng hoặc huỷ đơn.
- [ ] Xử lý `409 ORDER_NOT_EDITABLE` bằng cách ẩn/disable nút sửa.
- [ ] Xử lý `409 ORDER_STATE_TRANSITION_INVALID` bằng cách tải lại đơn (trạng thái đã đổi), dùng
      `message` để giải thích.
- [ ] Xử lý `404 ORDER_NOT_FOUND` bằng **một** thông báo chung (không phân biệt "không tồn tại" và "của
      người khác"), rồi tải lại danh sách.
- [ ] Gắn lỗi `ORDER_QUANTITY_EXCEEDS_AVAILABLE`/`ORDER_ITEM_NOT_PURCHASABLE` vào dòng qua
      `details[].field = "productId"`, và hiện số khả dụng khi có.
- [ ] Khi checkout báo `ORDER_NO_ADDRESS`, đưa khách sang màn hình thêm địa chỉ (module 02).
- [ ] Dùng `addressId` **tuỳ chọn** ở checkout; ở sửa đơn, bỏ trống để **giữ nguyên** địa chỉ hiện tại.
- [ ] Trang admin: chỉ gọi `/admin/orders*` khi token có role `ADMIN`; xử lý `403 FORBIDDEN` riêng.

---

## 8. Changelog & đối chiếu

**2026-10-10** — Viết guide cho feature 010; đánh dấu guide của feature 009 đã bị thay thế.

Đối chiếu với implementation, **có đếm** — mọi con số dưới đây được đếm lại từ chính nguồn, không chép từ
tài liệu khác.

**Endpoint và route** — **12**, đối chiếu hai chiều:

| Hướng | Nguồn | Kết quả |
|---|---|---|
| Guide → code | §2 liệt kê **12** mục | `internal/modules/order/presentation/http/router.go` khai đúng **12** route (5 trong `Router` + 7 trong `AdminRouter`) |
| Code → guide | router **12 route** | cả 12 đều có mặt ở §2 |

Cùng khớp `specs/010-order-confirmation/contracts/openapi.yaml` (**12** operation dưới `paths:`) và
`docs/api-reference.md` §9 (**12** mục `9.1`–`9.12`). **3 mới, 3 đổi, 6 giữ nguyên.**

**Trường JSON** — `internal/modules/order/presentation/dto/dto.go` có **35** tag `json:"…"` trên **11**
kiểu, thành **25** tên member phân biệt:

| Kiểu | Số member | Member |
|---|---|---|
| `CheckoutRequest` | 1 | `addressId` |
| `EditLineRequest` | 2 | `productId`, `quantity` |
| `EditOrderRequest` | 2 | `addressId`, `lines` |
| `TransferRequest` | 1 | `email` |
| `MoneyResponse` | 2 | `amount`, `currency` |
| `OrderLineResponse` | 6 | `productId`, `name`, `slug`, `quantity`, `unitPrice`, `lineTotal` |
| `OrderAddressResponse` | 7 | `recipientName`, `recipientPhone`, `provinceCode`, `provinceName`, `wardCode`, `wardName`, `streetAddress` |
| `OrderSummaryResponse` | 5 | `id`, `status`, `total`, `itemCount`, `createdAt` |
| `OrderResponse` | 7 | `id`, `status`, `total`, `itemCount`, `createdAt`, `address`, `lines` |
| `AdminOrderSummaryResponse` | 1 (+5 nhúng) | `userId` (+ `OrderSummaryResponse`) |
| `AdminOrderResponse` | 1 (+7 nhúng) | `userId` (+ `OrderResponse`) |

**25/25** tên phân biệt đều có mặt trong guide §1. Tổng **35** tag (đếm theo khai báo `json:` trong DTO) →
**25** tên vì nhiều tên lặp ở nhiều kiểu (`id`/`status`/`total`/`itemCount`/`createdAt` xuất hiện ở cả
hai hình dạng summary/detail; `addressId` ở `CheckoutRequest` và `EditOrderRequest`; `productId`/`quantity`
ở `OrderLineResponse` và `EditLineRequest`; `userId` ở hai kiểu admin). So với 009 (**31** tag trên **9**
kiểu) feature thêm **2** kiểu (`EditLineRequest`, `EditOrderRequest`) và **4** tag. Không tag nào ngoài
guide.

**Giá trị enum** — **6**: `OrderStatus` = `PENDING`, `PAYMENT_PENDING`, `PAID`, `SHIPPED`, `COMPLETED`,
`CANCELLED` (khớp `internal/modules/order/domain/constant/codes.go`, `contracts/openapi.yaml`
`OrderStatus`, và cột `orders_status_ck` trong `migrations/00011_order_confirmation.sql`). §3 liệt kê đủ
6; `PENDING_PAYMENT` được đánh dấu **đổi tên**.

**Mã lỗi** — **11** mã riêng của module (`ORDER_NOT_FOUND`, `ORDER_CART_EMPTY`,
`ORDER_ITEM_NOT_PURCHASABLE`, `ORDER_ITEM_PRICE_CHANGED`, `ORDER_QUANTITY_EXCEEDS_AVAILABLE`,
`ORDER_NO_ADDRESS`, `ORDER_STATE_TRANSITION_INVALID`, `ORDER_NOT_TRANSFERABLE`,
`ORDER_TRANSFER_TARGET_NOT_FOUND`, `ORDER_NOT_EDITABLE`, `ORDER_EMPTY` — khớp
`domain/constant/codes.go` và `contracts/error-codes.md`; hai mã cuối **mới** trong 010) cộng **6** mã
**tái sử dụng** (`VALIDATION_ERROR`, `MALFORMED_REQUEST`, `UNAUTHENTICATED`, `FORBIDDEN`, `RATE_LIMITED`,
`INTERNAL_ERROR`).

**HTTP status khẳng định** — **9**: `200`, `201`, `400`, `401`, `403`, `404`, `409`, `429`, `500`. Mỗi
status là một câu trả lời thật: `200` do các handler đọc/đổi; `201` do checkout; `400`
(`VALIDATION_ERROR`/`MALFORMED_REQUEST`); `401` (`UNAUTHENTICATED`); `403` (`FORBIDDEN` do role guard của
admin); `404` (`ORDER_NOT_FOUND`, `ORDER_TRANSFER_TARGET_NOT_FOUND`); `409` (các mã `ORDER_*` trong
`mapError`); `429` (`RATE_LIMITED`); `500` (`INTERNAL_ERROR` mặc định của `mapError`). Module **không**
dùng `204`, `405`, `413`, `503` nên các status đó vắng mặt có chủ ý.

**Annotation Swagger** — cả **12** handler trong `handler.go` mang đủ `@Summary`, `@Tags`, `@Param`
(đường dẫn và/hoặc body), `@Success`, `@Failure`, `@Router`. Không handler nào thiếu.
