# Frontend Integration Guide — Đơn hàng (Order Checkout)

> ⚠️ **ĐÃ BỊ THAY THẾ (superseded) — 2026-10-10.** Feature [`010-order-confirmation`](../010-order-confirmation/spec.md)
> thay thế guide này. Guide mới: [`specs/010-order-confirmation/frontend-guide.md`](../010-order-confirmation/frontend-guide.md).
> Đặc biệt, guide này **sai** ở: enum `OrderStatus` (nay **6** giá trị; `PENDING_PAYMENT` **đổi tên**
> `PAYMENT_PENDING`, thêm `PENDING`), `POST /orders` (nay tạo đơn `PENDING` và **không** giữ hàng), và
> `POST /orders/{orderId}/cancel` (nay huỷ được cả `PENDING` lẫn `PAYMENT_PENDING`). Không sửa file này;
> dùng guide mới.

**Feature**: `009-order`
**Trạng thái**: Chín endpoint của module 07 đã được ghi vào `docs/api-reference.md` §9.
**Đối tượng đọc**: Lập trình viên frontend (đơn hàng của khách đã đăng nhập, và bàn quản trị đơn của operator).
**Mục tiêu**: FE triển khai đơn hàng chỉ với tài liệu này — không cần đọc code backend hay Swagger.

> Nguồn authoritative là [`docs/api-reference.md`](../../docs/api-reference.md) §9. Guide này **phải
> khớp** với file đó; khi lệch, `docs/api-reference.md` thắng. Mọi tên trường dưới đây là **đúng**
> `json:"…"` mà DTO phát ra. Hợp đồng máy đọc được là
> [`contracts/openapi.yaml`](contracts/openapi.yaml).

---

## 0. Tóm tắt nhanh (TL;DR)

- **9 endpoint**: **4** của **KHÁCH đã đăng nhập** dưới `/orders`, **5** của **ADMIN** dưới `/admin/orders`.
  Tất cả là **MỚI** trong feature này.
- **Chủ sở hữu lấy từ session**, không bao giờ từ body/query; **không** route nào nêu id chủ. Đơn của
  khách khác trả **cùng** `404 ORDER_NOT_FOUND`.
- **Checkout `POST /orders` biến giỏ thành đơn `PENDING_PAYMENT`**: chụp snapshot từng dòng + địa chỉ,
  **giữ** hàng (số vật lý **không** đổi, khả dụng **giảm**), **làm rỗng** giỏ, trong **một** thao tác.
- **`total` = tổng `quantity × unitPrice`**, đơn vị nhỏ nhất + currency, **không** số thực, **không**
  làm tròn, **không** gồm phí ship.
- Đơn chưa trả tiền **tự huỷ sau 15 phút** (`expires_at`); FE thấy `status: "CANCELLED"`, **không**
  nhận lỗi.
- Trạng thái đi qua **endpoint riêng** (`cancel`/`ship`/`complete`); một bước chuyển sai trả `409
  ORDER_STATE_TRANSITION_INVALID` kèm **trạng thái hiện tại**.
- **Không có endpoint `pay`**: bước `PAID` do module 08 điều khiển, chưa có bề mặt HTTP.
- **Chuyển nhượng** (`transfer`) chỉ đổi **chủ sở hữu** (nêu bằng email), **không** đổi dòng/trạng
  thái/tổng, **không** đổi tồn kho; chỉ áp dụng cho đơn `PAID`.
- Module **không** có rate limit riêng; chỉ chịu hạn mức toàn cục.

---

## 1. Quy ước

- Base path `/api/v1`; envelope `{data, meta}` / `{error}` như các guide trước.
- Mọi route yêu cầu **Bearer**. Thiếu/sai token → `401 UNAUTHENTICATED`. Route admin yêu cầu role
  `ADMIN`; `CUSTOMER` gọi route admin → `403 FORBIDDEN`.
- Tác nhân (chủ đơn, khi checkout/huỷ; administrator, khi ship/complete/transfer) lấy từ **session**,
  không bao giờ từ body. Body có member lạ → `MALFORMED_REQUEST` (`additionalProperties: false`).
- `orderId` là UUID. Không phải UUID → `400 VALIDATION_ERROR` với `details[].field = "orderId"`.
- `page` mặc định `1` (tối thiểu `1`); `pageSize` mặc định `20`, khoảng `1..100`. Ngoài khoảng →
  `400 VALIDATION_ERROR` với `details[].field` là `"page"` hoặc `"pageSize"`.
- Tiền là **số nguyên đơn vị nhỏ nhất + currency**, không bao giờ là số thực.
- Danh sách **mới nhất trước** (`createdAt`, rồi `id`).

### Kiểu dữ liệu

`MoneyResponse` (2 member) — tiền:
```json
{ "amount": 150000, "currency": "VND" }
```
- `amount`: số nguyên, đơn vị nhỏ nhất của `currency`.
- `currency`: ba chữ in hoa.

`OrderLineResponse` (6 member) — một dòng đơn, một **snapshot** lúc checkout:
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
- `productId`: UUID sản phẩm **đã ghi nhận** lúc mua — **không** phải tham chiếu sống; sản phẩm có thể
  đã bị xoá mà dòng vẫn còn.
- `name` / `slug`: giá trị sản phẩm **tại lúc checkout**, không đọc lại.
- `quantity`: số nguyên **≥ 1**.
- `unitPrice`: giá **snapshot** lúc checkout.
- `lineTotal`: `quantity × unitPrice`.

`OrderAddressResponse` (7 member) — địa chỉ giao **chụp tại checkout**:
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
Cả 7 member là chuỗi, luôn có mặt; một lần sửa địa chỉ sau đó **không** đổi đơn đã đặt.

`OrderSummaryResponse` (5 member) — một dòng của danh sách khách:
```json
{
  "id": "0f5c6e0c-1a44-4a1e-9b3d-9a1b2c3d4e5f",
  "status": "PENDING_PAYMENT",
  "total": { "amount": 300000, "currency": "VND" },
  "itemCount": 2,
  "createdAt": "2026-10-09T08:15:04Z"
}
```
- `id`: UUID đơn.
- `status`: một trong **5** giá trị (§3).
- `total`: tổng đơn (chỉ tiền hàng).
- `itemCount`: số dòng đơn.
- `createdAt`: mốc tạo đơn (ISO 8601).

`OrderResponse` (7 member) — chi tiết khách = summary + `address` + `lines`:
```json
{
  "id": "0f5c6e0c-1a44-4a1e-9b3d-9a1b2c3d4e5f",
  "status": "PENDING_PAYMENT",
  "total": { "amount": 300000, "currency": "VND" },
  "itemCount": 2,
  "createdAt": "2026-10-09T08:15:04Z",
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
  "status": "PENDING_PAYMENT",
  "total": { "amount": 300000, "currency": "VND" },
  "itemCount": 2,
  "createdAt": "2026-10-09T08:15:04Z",
  "userId": "11111111-1111-1111-1111-111111111111"
}
```
- `userId`: tài khoản sở hữu đơn.

`AdminOrderResponse` (8 member) — chi tiết admin = `OrderResponse` **+** `userId`.

`CheckoutRequest` (1 member) — body của `POST /orders`:
```json
{ "addressId": "b2f1c0d4-5a6e-4b7c-8d9e-0f1a2b3c4d5e" }
```
- `addressId`: **tuỳ chọn**; bỏ trống hoặc `null` → dùng **địa chỉ mặc định** của khách. Không phải
  UUID hoặc không phải địa chỉ của khách → `400 VALIDATION_ERROR` (`details[].field = "addressId"`).

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
| 9.1 | POST | `/api/v1/orders` | Khách | MỚI | `201` `OrderResponse` |
| 9.2 | GET | `/api/v1/orders` | Khách | MỚI | `200` `[OrderSummaryResponse]` (phân trang) |
| 9.3 | GET | `/api/v1/orders/{orderId}` | Khách | MỚI | `200` `OrderResponse` |
| 9.4 | POST | `/api/v1/orders/{orderId}/cancel` | Khách | MỚI | `200` `OrderResponse` |
| 9.5 | GET | `/api/v1/admin/orders` | ADMIN | MỚI | `200` `[AdminOrderSummaryResponse]` (phân trang) |
| 9.6 | GET | `/api/v1/admin/orders/{orderId}` | ADMIN | MỚI | `200` `AdminOrderResponse` |
| 9.7 | POST | `/api/v1/admin/orders/{orderId}/ship` | ADMIN | MỚI | `200` `AdminOrderResponse` |
| 9.8 | POST | `/api/v1/admin/orders/{orderId}/complete` | ADMIN | MỚI | `200` `AdminOrderResponse` |
| 9.9 | POST | `/api/v1/admin/orders/{orderId}/transfer` | ADMIN | MỚI | `200` `AdminOrderResponse` |

### Chi tiết

**9.1 `POST /orders`** — checkout. Chụp snapshot từng dòng và địa chỉ, giữ hàng, làm rỗng giỏ, trong
một thao tác. Body `CheckoutRequest` (có thể `{}` để dùng địa chỉ mặc định). `201` trả cả đơn.
Lỗi: `VALIDATION_ERROR` 400 (`addressId`) · `MALFORMED_REQUEST` 400 · `UNAUTHENTICATED` 401 ·
`ORDER_CART_EMPTY` 409 · `ORDER_ITEM_NOT_PURCHASABLE` 409 · `ORDER_ITEM_PRICE_CHANGED` 409 ·
`ORDER_QUANTITY_EXCEEDS_AVAILABLE` 409 · `ORDER_NO_ADDRESS` 409 · `RATE_LIMITED` 429 · `INTERNAL_ERROR`
500.

**9.2 `GET /orders`** — danh sách đơn của chính khách, mới nhất trước, phân trang. Khách chưa có đơn
nào trả `data: []`, không phải lỗi.
Lỗi: `VALIDATION_ERROR` 400 (`page`/`pageSize`) · `UNAUTHENTICATED` 401 · `RATE_LIMITED` 429 ·
`INTERNAL_ERROR` 500.

**9.3 `GET /orders/{orderId}`** — chi tiết một đơn của chính khách. Đơn không tồn tại **hoặc** của
khách khác → `404 ORDER_NOT_FOUND` (cùng câu trả lời).
Lỗi: `VALIDATION_ERROR` 400 (`orderId`) · `UNAUTHENTICATED` 401 · `ORDER_NOT_FOUND` 404 ·
`RATE_LIMITED` 429 · `INTERNAL_ERROR` 500.

**9.4 `POST /orders/{orderId}/cancel`** — huỷ đơn còn `PENDING_PAYMENT`, trả hàng về khả dụng. Đơn
đã trả tiền → `409 ORDER_STATE_TRANSITION_INVALID` (nêu trạng thái hiện tại). Huỷ lần hai cũng `409`.
Lỗi: `VALIDATION_ERROR` 400 (`orderId`) · `UNAUTHENTICATED` 401 · `ORDER_NOT_FOUND` 404 ·
`ORDER_STATE_TRANSITION_INVALID` 409 · `RATE_LIMITED` 429 · `INTERNAL_ERROR` 500.

**9.5 `GET /admin/orders`** — mọi đơn, mới nhất trước, kèm `userId`, phân trang.
Lỗi: `VALIDATION_ERROR` 400 · `UNAUTHENTICATED` 401 · `FORBIDDEN` 403 · `RATE_LIMITED` 429 ·
`INTERNAL_ERROR` 500.

**9.6 `GET /admin/orders/{orderId}`** — chi tiết một đơn bất kỳ, kèm `userId`.
Lỗi: `VALIDATION_ERROR` 400 · `UNAUTHENTICATED` 401 · `FORBIDDEN` 403 · `ORDER_NOT_FOUND` 404 ·
`RATE_LIMITED` 429 · `INTERNAL_ERROR` 500.

**9.7 `POST /admin/orders/{orderId}/ship`** — `PAID → SHIPPED`. Bước sai → `409
ORDER_STATE_TRANSITION_INVALID`. Ghi audit `ORDER_SHIPPED`.
Lỗi: `VALIDATION_ERROR` 400 · `UNAUTHENTICATED` 401 · `FORBIDDEN` 403 · `ORDER_NOT_FOUND` 404 ·
`ORDER_STATE_TRANSITION_INVALID` 409 · `RATE_LIMITED` 429 · `INTERNAL_ERROR` 500.

**9.8 `POST /admin/orders/{orderId}/complete`** — `SHIPPED → COMPLETED` (cuối). Bước sai → `409
ORDER_STATE_TRANSITION_INVALID`. Ghi audit `ORDER_COMPLETED`.
Lỗi: `VALIDATION_ERROR` 400 · `UNAUTHENTICATED` 401 · `FORBIDDEN` 403 · `ORDER_NOT_FOUND` 404 ·
`ORDER_STATE_TRANSITION_INVALID` 409 · `RATE_LIMITED` 429 · `INTERNAL_ERROR` 500.

**9.9 `POST /admin/orders/{orderId}/transfer`** — đổi chủ một đơn `PAID` sang tài khoản mang `email`,
chỉ đổi chủ, không đổi tồn kho. Body `TransferRequest`. Ghi audit `ORDER_TRANSFERRED`.
Lỗi: `VALIDATION_ERROR` 400 (`email`) · `MALFORMED_REQUEST` 400 · `UNAUTHENTICATED` 401 ·
`FORBIDDEN` 403 · `ORDER_NOT_FOUND` 404 (đơn) · `ORDER_TRANSFER_TARGET_NOT_FOUND` 404 (email) ·
`ORDER_NOT_TRANSFERABLE` 409 · `RATE_LIMITED` 429 · `INTERNAL_ERROR` 500.

---

## 3. Bảng giá trị enum `OrderStatus`

`status` là một trong **5** giá trị:

| Giá trị | Ý nghĩa | Vào bằng | Ra được không |
|---|---|---|---|
| `PENDING_PAYMENT` | Chờ thanh toán — hàng đang được **giữ** cho khách | Checkout tạo đơn | → `PAID` hoặc → `CANCELLED` |
| `PAID` | Đã trả tiền — giữ chỗ đã thành **bán** | Module 08 xác nhận thanh toán (không có HTTP) | → `SHIPPED` |
| `SHIPPED` | Đã giao | Admin `ship` | → `COMPLETED` |
| `COMPLETED` | Hoàn tất | Admin `complete` | **Không** (cuối) |
| `CANCELLED` | Đã huỷ — khách huỷ hoặc hết hạn | Khách `cancel` / sweeper | **Không** (cuối) |

Đơn **không** có trường trạng thái thanh toán riêng; `PAID` chính là đã xác nhận thanh toán.

---

## 4. Trước → sau

### Endpoint MỚI (9)

Toàn bộ endpoint ở §2 là **mới** trong feature này. Module 07 chưa từng có endpoint nào trước đó.

| # | Method | Path | Trước | Sau |
|---|---|---|---|---|
| 9.1 | POST | `/api/v1/orders` | *chưa tồn tại* | `201` `OrderResponse` |
| 9.2 | GET | `/api/v1/orders` | *chưa tồn tại* | `200` `[OrderSummaryResponse]` |
| 9.3 | GET | `/api/v1/orders/{orderId}` | *chưa tồn tại* | `200` `OrderResponse` |
| 9.4 | POST | `/api/v1/orders/{orderId}/cancel` | *chưa tồn tại* | `200` `OrderResponse` |
| 9.5 | GET | `/api/v1/admin/orders` | *chưa tồn tại* | `200` `[AdminOrderSummaryResponse]` |
| 9.6 | GET | `/api/v1/admin/orders/{orderId}` | *chưa tồn tại* | `200` `AdminOrderResponse` |
| 9.7 | POST | `/api/v1/admin/orders/{orderId}/ship` | *chưa tồn tại* | `200` `AdminOrderResponse` |
| 9.8 | POST | `/api/v1/admin/orders/{orderId}/complete` | *chưa tồn tại* | `200` `AdminOrderResponse` |
| 9.9 | POST | `/api/v1/admin/orders/{orderId}/transfer` | *chưa tồn tại* | `200` `AdminOrderResponse` |

Không có endpoint nào của module khác bị **đổi** hay **xoá** bởi feature này. **9 mới, 0 đổi, 0 xoá.**

---

## 5. Bảng mã lỗi

**Riêng module order** (**9** mã):

| Code | HTTP | FE nên làm |
|---|---|---|
| `ORDER_NOT_FOUND` | 404 | Đơn không tồn tại **hoặc** của khách khác. Hiện "không tìm thấy đơn", tải lại danh sách. `details` không có |
| `ORDER_CART_EMPTY` | 409 | Giỏ rỗng khi checkout. Báo khách giỏ trống, mời thêm sản phẩm; **không** tự thử lại |
| `ORDER_ITEM_NOT_PURCHASABLE` | 409 | Một dòng không còn bán/đã xoá. `details[].field = "productId"`. Hiện món gây lỗi, đưa khách về giỏ |
| `ORDER_ITEM_PRICE_CHANGED` | 409 | Giá một dòng đã đổi so với giá khách thấy. `details[].field = "productId"`. Báo khách xem lại giỏ rồi checkout lại |
| `ORDER_QUANTITY_EXCEEDS_AVAILABLE` | 409 | Một dòng vượt tồn, hoặc không giữ được hàng. `details[].field = "productId"`, `issue` nêu số khả dụng. Gợi ý giảm còn số đó |
| `ORDER_NO_ADDRESS` | 409 | Khách chưa có địa chỉ giao. Mời khách thêm địa chỉ (module 02) rồi checkout lại |
| `ORDER_STATE_TRANSITION_INVALID` | 409 | Bước chuyển không hợp lệ với trạng thái hiện tại; `message` nêu trạng thái. Tải lại đơn để thấy trạng thái thật |
| `ORDER_NOT_TRANSFERABLE` | 409 | Đơn chưa `PAID` nên không chuyển nhượng được. Ẩn/ disable nút transfer cho đơn chưa trả tiền |
| `ORDER_TRANSFER_TARGET_NOT_FOUND` | 404 | Email không ứng với tài khoản nào. Báo operator kiểm tra email |

**Dùng chung / tái sử dụng**: `VALIDATION_ERROR` 400 (kèm `details[].field`), `MALFORMED_REQUEST` 400,
`UNAUTHENTICATED` 401, `FORBIDDEN` 403, `RATE_LIMITED` 429, `INTERNAL_ERROR` 500.

Ba mã nêu món (`ORDER_ITEM_NOT_PURCHASABLE`, `ORDER_ITEM_PRICE_CHANGED`,
`ORDER_QUANTITY_EXCEEDS_AVAILABLE`) mang `error.details[].field = "productId"`; riêng mã số lượng,
`issue` nêu số còn khả dụng — FE gắn lỗi vào đúng dòng.

---

## 6. Trạng thái, giữ chỗ và chuyển nhượng (nhắc lại cho FE)

- **Checkout giữ hàng, không bán**: số vật lý **không** đổi, số khả dụng **giảm**. FE **không** cần
  gọi API tồn kho; đó là việc nội bộ giữa order và inventory.
- **Đơn tự huỷ**: một đơn `PENDING_PAYMENT` quá 15 phút (`expires_at`) tự thành `CANCELLED` và trả
  hàng. FE nên hiển thị đếm ngược tới `createdAt + 15 phút` nếu muốn, nhưng **không** giả định trạng
  thái là bất biến — hãy đọc lại đơn.
- **Trạng thái qua endpoint riêng**: để đổi trạng thái, gọi `cancel`/`ship`/`complete`, **không** gửi
  `status` trong body. Một bước sai trả `409` kèm trạng thái hiện tại.
- **Không có `pay`**: một đơn ở `PENDING_PAYMENT` chưa có cách trả tiền qua HTTP. Đừng giả định có
  luồng thanh toán ở dữ liệu này cho tới khi module 08 ra đời.
- **Chuyển nhượng** chỉ đổi chủ; dòng/trạng thái/tổng giữ nguyên, tồn kho không đổi. FE cập nhật
  `userId` sau khi thành công; không tải lại dòng.

---

## 7. Checklist FE

- [ ] Mọi request đơn gọi `/api/v1/orders*` (khách) hoặc `/api/v1/admin/orders*` (admin) với Bearer;
      **không** gửi `userId`/`ownerId` ở body/query.
- [ ] Hiển thị tiền bằng `amount` (số nguyên) + `currency`; **không** tự quy đổi hay làm tròn.
- [ ] Xử lý `total` là **chỉ tiền hàng** (không phí ship); không cộng phí ship vào tổng.
- [ ] Hiển thị `status` theo bảng §3; ẩn/disable nút `cancel` cho đơn không `PENDING_PAYMENT`, nút
      `transfer` cho đơn không `PAID`.
- [ ] Xử lý `409 ORDER_STATE_TRANSITION_INVALID` bằng cách tải lại đơn (trạng thái đã đổi), dùng
      `message` để giải thích.
- [ ] Xử lý `404 ORDER_NOT_FOUND` bằng **một** thông báo chung (không phân biệt "không tồn tại" và
      "của người khác"), rồi tải lại danh sách.
- [ ] Gắn lỗi ba mã `ORDER_ITEM_*`/`ORDER_QUANTITY_*` vào dòng qua `details[].field = "productId"`, và
      hiện số khả dụng khi có.
- [ ] Khi checkout báo `ORDER_NO_ADDRESS`, đưa khách sang màn hình thêm địa chỉ (module 02).
- [ ] Dùng `addressId` **tuỳ chọn**; bỏ trống để dùng địa chỉ mặc định.
- [ ] Trang admin: chỉ gọi `/admin/orders*` khi token có role `ADMIN`; xử lý `403 FORBIDDEN` riêng.

---

## 8. Changelog & đối chiếu

**2026-10-09** — Viết guide lần đầu cho feature 009 (hiến pháp §Governance).

Đối chiếu với implementation, **có đếm** — mọi con số dưới đây được đếm lại từ chính nguồn, không chép
từ tài liệu khác.

**Endpoint và route** — **9**, đối chiếu hai chiều:

| Hướng | Nguồn | Kết quả |
|---|---|---|
| Guide → code | §2 liệt kê **9** mục | `internal/modules/order/presentation/http/router.go` khai đúng **9** route (4 trong `Router` + 5 trong `AdminRouter`) |
| Code → guide | router **9 route** | cả 9 đều có mặt ở §2 |

Cùng khớp `specs/009-order/contracts/openapi.yaml` (**9** operation dưới `paths:`) và
`docs/api-reference.md` §9 (**9** mục `9.1`–`9.9`). **9 mới, 0 đổi, 0 xoá.**

**Trường JSON** — `internal/modules/order/presentation/dto/dto.go` có **31 tag `json:"…"`** trên **9
kiểu**, thành **25 tên member phân biệt**:

| Kiểu | Số member | Member |
|---|---|---|
| `CheckoutRequest` | 1 | `addressId` |
| `TransferRequest` | 1 | `email` |
| `MoneyResponse` | 2 | `amount`, `currency` |
| `OrderLineResponse` | 6 | `productId`, `name`, `slug`, `quantity`, `unitPrice`, `lineTotal` |
| `OrderAddressResponse` | 7 | `recipientName`, `recipientPhone`, `provinceCode`, `provinceName`, `wardCode`, `wardName`, `streetAddress` |
| `OrderSummaryResponse` | 5 | `id`, `status`, `total`, `itemCount`, `createdAt` |
| `OrderResponse` | 7 | `id`, `status`, `total`, `itemCount`, `createdAt`, `address`, `lines` |
| `AdminOrderSummaryResponse` | 1 (+6 nhúng) | `userId` (+ `OrderSummaryResponse`) |
| `AdminOrderResponse` | 1 (+7 nhúng) | `userId` (+ `OrderResponse`) |

**25/25** tên phân biệt đều có mặt trong guide. Tổng **31** tag (đếm theo khai báo `json:` trong DTO)
→ **25** tên vì nhiều tên lặp ở nhiều kiểu: `id`/`status`/`total`/`itemCount`/`createdAt` xuất hiện ở
cả hai hình dạng summary/detail, `userId` xuất hiện ở hai kiểu admin. Không có tag nào ngoài guide.

**Giá trị enum** — **5**: `OrderStatus` = `PENDING_PAYMENT`, `PAID`, `SHIPPED`, `COMPLETED`,
`CANCELLED` (khớp `domain/constant/codes.go`, `contracts/openapi.yaml` `OrderStatus`, và cột
`orders_status_ck`). §3 liệt kê đủ 5.

**Mã lỗi** — **9** mã riêng của module (`ORDER_NOT_FOUND`, `ORDER_CART_EMPTY`,
`ORDER_ITEM_NOT_PURCHASABLE`, `ORDER_ITEM_PRICE_CHANGED`, `ORDER_QUANTITY_EXCEEDS_AVAILABLE`,
`ORDER_NO_ADDRESS`, `ORDER_STATE_TRANSITION_INVALID`, `ORDER_NOT_TRANSFERABLE`,
`ORDER_TRANSFER_TARGET_NOT_FOUND`, khớp `domain/constant/codes.go` và `contracts/error-codes.md`) cộng
**6** mã **tái sử dụng** (`VALIDATION_ERROR`, `MALFORMED_REQUEST`, `UNAUTHENTICATED`, `FORBIDDEN`,
`RATE_LIMITED`, `INTERNAL_ERROR`).

**HTTP status khẳng định** — **9**: `200`, `201`, `400`, `401`, `403`, `404`, `409`, `429`, `500`.
Mỗi status là một câu trả lời thật: `200` do các handler đọc/đổi; `201` do checkout; `400`
(`VALIDATION_ERROR`/`MALFORMED_REQUEST` do handler và `mapError`); `401` (`UNAUTHENTICATED` do
middleware/handler); `403` (`FORBIDDEN` do role guard của admin); `404` (`ORDER_NOT_FOUND`,
`ORDER_TRANSFER_TARGET_NOT_FOUND` do `mapError`); `409` (bảy mã `ORDER_*` do `mapError`); `429`
(`RATE_LIMITED`); `500` (`INTERNAL_ERROR` mặc định của `mapError`). Module **không** dùng `204`, `405`,
`413`, `503` nên các status đó vắng mặt có chủ ý.

**Annotation Swagger** — cả **9** handler trong `handler.go` mang đủ `@Summary`, `@Tags`, `@Param`
(đường dẫn và/hoặc body), `@Success`, `@Failure`, `@Router`. Không handler nào thiếu.
