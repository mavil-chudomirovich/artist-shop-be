# Frontend Guide: Address Facebook URL (021-address-facebook-url)

**Feature**: `021-address-facebook-url` — link Facebook bắt buộc của địa chỉ giao hàng.
Tài liệu nguồn: [spec.md](spec.md) · [contracts/openapi-addition.yaml](contracts/openapi-addition.yaml) ·
[contracts/error-codes.md](contracts/error-codes.md) · [quickstart.md](quickstart.md)
Tham chiếu authoritative: [docs/api-reference.md](../../docs/api-reference.md) (mục 4.5–4.7, 4.10, 9, 9.1, 9.4 và change log).

## 1. Tổng quan cho frontend

- **Endpoint mới: KHÔNG có. Endpoint bị xoá: KHÔNG có.**
- **Endpoint đổi hình dạng (request/response): 18** — 6 route user (sổ địa chỉ + tra cứu
  admin) và 12 route order (mọi route trả chi tiết đơn đầy đủ).
- Bản chất thay đổi: **cộng thêm 1 member `facebookUrl`** (string, tối đa 500 ký tự)
  vào mọi hình dạng địa chỉ; chiều ghi tạo mới **bắt buộc** (breaking — xem §5).
- Danh sách (`GET /orders`, `GET /admin/orders`) trả summary **không** có địa chỉ nên
  **không đổi**.

## 2. Bảng endpoint (method + path + trạng thái)

| # | Method + Path | Trạng thái | Ghi chú |
|---|---|---|---|
| 1 | `POST /api/v1/users/me/addresses` | **ĐỔI** | Request + response: thêm `facebookUrl` **bắt buộc** |
| 2 | `GET /api/v1/users/me/addresses` | **ĐỔI** | Response: mỗi địa chỉ thêm `facebookUrl` |
| 3 | `PATCH /api/v1/users/me/addresses/{addressId}` | **ĐỔI** | Request: thêm `facebookUrl` tuỳ chọn; response thêm `facebookUrl` |
| 4 | `DELETE /api/v1/users/me/addresses/{addressId}` | không đổi wire | Vẫn `204` không body; không có field nào để frontend sửa |
| 5 | `POST /api/v1/users/me/addresses/{addressId}/default` | **ĐỔI** | Response: địa chỉ trả về thêm `facebookUrl` |
| 6 | `GET /api/v1/users/{userId}` (ADMIN) | **ĐỔI** | Mỗi địa chỉ trong `addresses` thêm `facebookUrl` (vẫn **không** có `divisionNeedsReview`) |
| 7 | `POST /api/v1/orders` (checkout) | **ĐỔI** | Response `address` thêm `facebookUrl` đã chụp; thêm lỗi giữ chỗ (§6) |
| 8 | `GET /api/v1/orders/{orderId}` | **ĐỔI** | Response `address` thêm `facebookUrl` đã chụp |
| 9 | `PUT /api/v1/orders/{orderId}` | **ĐỔI** | Response `address` thêm `facebookUrl`; đổi `addressId` thì chụp lại link mới; thêm lỗi giữ chỗ (§6) |
| 10 | `POST /api/v1/orders/{orderId}/cancel` | **ĐỔI** | Response là chi tiết đơn đầy đủ → `address` thêm `facebookUrl` (request không đổi) |
| 11 | `POST /api/v1/orders/{orderId}/complete` | **ĐỔI** | Như trên (request không đổi) |
| 12 | `POST /api/v1/orders/{orderId}/feedback` | **ĐỔI** | Như trên (request multipart không đổi) |
| 13 | `GET /api/v1/admin/orders/{orderId}` | **ĐỔI** | Chi tiết admin: `address` thêm `facebookUrl` (cộng `userId`, như cũ) |
| 14 | `POST /api/v1/admin/orders/{orderId}/confirm` | **ĐỔI** | Response chi tiết admin → `address` thêm `facebookUrl` (request không đổi) |
| 15 | `POST /api/v1/admin/orders/{orderId}/reject` | **ĐỔI** | Như trên |
| 16 | `POST /api/v1/admin/orders/{orderId}/ship` | **ĐỔI** | Như trên (request multipart ảnh không đổi) |
| 17 | `POST /api/v1/admin/orders/{orderId}/deliver` | **ĐỔI** | Như trên (request multipart ảnh không đổi) |
| 18 | `POST /api/v1/admin/orders/{orderId}/transfer` | **ĐỔI** | Như trên (request `{email}` không đổi) |
| — | `GET /api/v1/orders`, `GET /api/v1/admin/orders` | không đổi | Summary không có địa chỉ — frontend giữ nguyên |
| — | `GET /api/v1/orders/stream`, `GET /api/v1/admin/orders/stream` | không đổi | Event stream không mang địa chỉ — giữ nguyên |

## 3. Trước → sau từng hình dạng

Quy ước: `facebookUrl` là string URL `https://...`, tối đa 500 ký tự. Mọi ví dụ dưới
đây dùng `https://facebook.com/nguyen.thi.an`.

### 3.1 Tạo địa chỉ — `POST /users/me/addresses` (201)

Request trước (7 member): `recipientName`, `recipientPhone`, `provinceCode`,
`provinceName?`, `wardCode`, `wardName?`, `streetAddress`.
Request sau (8 member): 7 member cũ + **`facebookUrl` (bắt buộc)**.

```json
// TRƯỚC
{ "recipientName": "...", "recipientPhone": "0912345678", "provinceCode": "01",
  "wardCode": "00004", "streetAddress": "12 Ngõ 129 Dịch Vọng" }
// SAU — thiếu facebookUrl nhận 400, xem §5
{ "recipientName": "...", "recipientPhone": "0912345678", "provinceCode": "01",
  "wardCode": "00004", "streetAddress": "12 Ngõ 129 Dịch Vọng",
  "facebookUrl": "https://facebook.com/nguyen.thi.an" }
```

Response `201` sau: một `AddressResponse` (11 member) gồm 10 member cũ
(`id`, `recipientName`, `recipientPhone`, `provinceCode`, `provinceName`, `wardCode`,
`wardName`, `streetAddress`, `isDefault`, `divisionNeedsReview`) + **`facebookUrl`**.

### 3.2 Đọc danh sách — `GET /users/me/addresses` (200)

Trước: mỗi phần tử 10 member (như trên, không có `facebookUrl`).
Sau: mỗi phần tử 11 member, thêm **`"facebookUrl": "https://facebook.com/..."`**.
`facebookUrl` **luôn có mặt**, kể cả ở dữ liệu cũ (giá trị giữ chỗ
`https://facebook.com/unknown`, xem §6). Phân trang/`meta` không đổi.

### 3.3 Sửa địa chỉ — `PATCH /users/me/addresses/{addressId}` (200)

Request trước (7 member tuỳ chọn): `recipientName?`, `recipientPhone?`, `provinceCode?`,
`provinceName?`, `wardCode?`, `wardName?`, `streetAddress?`.
Request sau (8 member tuỳ chọn): 7 member cũ + **`facebookUrl?` (tuỳ chọn)**.

- Vắng mặt `facebookUrl` trên địa chỉ thường → giữ link cũ, các field khác vẫn đổi.
- `facebookUrl` rỗng/sai → `400 VALIDATION_ERROR`, toàn bộ địa chỉ (kể cả `isDefault`)
  giữ nguyên.
- Địa chỉ đang mang giữ chỗ `https://facebook.com/unknown` → **mọi** `PATCH` (kể cả chỉ
  sửa field khác) đều `400 VALIDATION_ERROR` field `facebookUrl` cho tới khi gửi link thật.

Response `200` sau: `AddressResponse` 11 member (như §3.1).

### 3.4 Đặt mặc định — `POST /users/me/addresses/{addressId}/default` (200)

Request: không body (không đổi). Response `200` sau: `AddressResponse` 11 member,
thêm `facebookUrl`, với `isDefault: true`.

### 3.5 Tra cứu admin — `GET /users/{userId}` (200, role ADMIN)

Trước: mỗi phần tử `addresses` 9 member (`id`, `recipientName`, `recipientPhone`,
`provinceCode`, `provinceName`, `wardCode`, `wardName`, `streetAddress`, `isDefault`).
Sau: 10 member, thêm **`facebookUrl`**. Vẫn **không** có `divisionNeedsReview`
(cố ý — góc nhìn admin không khẳng định mã còn hiệu lực).

### 3.6 Đơn hàng — `address` trong chi tiết đơn (khách + admin)

Trước (`OrderAddressResponse` 7 member): `recipientName`, `recipientPhone`,
`provinceCode`, `provinceName`, `wardCode`, `wardName`, `streetAddress`.
Sau (8 member): 7 member cũ + **`facebookUrl`** — là **ảnh chụp** tại lúc chốt đơn
(hoặc lúc sửa đơn có đổi địa chỉ), **bất biến** khi địa chỉ gốc bị sửa sau đó.

Áp dụng cho mọi response chi tiết: `POST /orders` (201), `GET /orders/{orderId}`,
`PUT /orders/{orderId}`, `POST .../cancel`, `POST .../complete`,
`POST .../feedback` (200) và phía admin `GET /admin/orders/{orderId}`, `POST
.../confirm|reject|ship|deliver|transfer` (200, cộng `userId`).

Request checkout (`{addressId?}`) và request sửa đơn (`{addressId?, lines[]}`)
**không đổi** hình dạng — chỉ thêm quy tắc giữ chỗ (§6).

## 4. Validate `facebookUrl` (frontend nên kiểm tra trước khi gửi)

Máy chủ trim khoảng trắng đầu/cuối rồi kiểm tra; frontend nên làm giống để báo lỗi sớm:

- bắt buộc, không rỗng, không chỉ-khoảng-trắng, tối đa **500 ký tự**;
- phải là URL `https` (không `http`/`ftp`, không thiếu scheme);
- host là `facebook.com`/`fb.com` hoặc mọi subdomain của chúng (`www.`, `m.`…;
  không phân biệt hoa/thường; port tường minh được bỏ qua);
- path phải có nội dung (`https://facebook.com` hoặc `https://facebook.com/` bị từ chối);
- từ chối host gần giống (`faceboook.com`, `myfacebook.com`, `facebook.com.evil.com`)
  và giá trị giữ chỗ `https://facebook.com/unknown` trên mọi write mới.

## 5. Hành vi lỗi (không thêm mã lỗi mới)

Mọi vi phạm `facebookUrl` dùng chung mã hiện hành **`VALIDATION_ERROR` (400)** với
`error.details[].field = "facebookUrl"` — frontend bắt `field` này để báo lỗi cạnh
ô nhập link. Các trường hợp:

| Tình huống | HTTP | `details[].field` | Ghi chú |
|---|---|---|---|
| Tạo địa chỉ thiếu/rỗng/sai link | 400 `VALIDATION_ERROR` | `facebookUrl` | Không lưu gì; thử lại với link đúng |
| Sửa địa chỉ: link mới rỗng/sai | 400 `VALIDATION_ERROR` | `facebookUrl` | Địa chỉ cũ nguyên vẹn (kể cả `isDefault`) |
| Sửa bất kỳ field nào trên địa chỉ giữ chỗ | 400 `VALIDATION_ERROR` | `facebookUrl` | Phải gửi link thật mới sửa được |
| Checkout / sửa đơn đổi sang địa chỉ giữ chỗ | 400 `VALIDATION_ERROR` | `facebookUrl` | Đơn giữ nguyên; khách phải cập nhật địa chỉ trước |
| Client cũ (trước feature) tạo địa chỉ không gửi field | 400 `VALIDATION_ERROR` | `facebookUrl` | **Breaking ở chiều ghi — đúng thiết kế**, xem change log trong [docs/api-reference.md](../../docs/api-reference.md) |

Các mã khác (`USER_INVALID_PHONE`, `USER_UNKNOWN_PROVINCE`, `USER_UNKNOWN_WARD`,
`USER_WARD_PROVINCE_MISMATCH`, `USER_ADDRESS_NOT_FOUND`, `ORDER_*`, `401/403/404/409/429`)
giữ nguyên.

## 6. Quy tắc giữ chỗ và snapshot (placeholder-checkout)

- Dữ liệu cũ chuyển đổi một lần mang `facebookUrl = "https://facebook.com/unknown"`;
  chi tiết đơn cũ hiển thị nguyên văn giá trị này.
- Đơn mới **không bao giờ** chụp giữ chỗ: checkout hoặc sửa đơn trỏ tới địa chỉ giữ
  chỗ bị chặn (`400`, §5) — frontend nên phát hiện địa chỉ giữ chỗ trong sổ địa chỉ
  và nhắc khách cập nhật link thật **trước** khi bấm đặt đơn.
- Đơn đã chốt là ảnh bất biến: sửa `facebookUrl` ở địa chỉ gốc không đổi đơn cũ;
  chỉ sửa đơn có đổi `addressId` mới chụp lại link của địa chỉ mới.

## 7. Đối chiếu với mã nguồn (T025 — đếm số)

Đối chiếu ngày 2026-10-19 sau khi mã nguồn ổn định; mọi số dưới đây đếm trực tiếp từ
nguồn (không ước lượng):

- **DTO user** (`internal/modules/user/presentation/dto/dto.go`): `CreateAddressRequest`
  **8** field JSON, `UpdateAddressRequest` **8** field JSON, `AddressResponse` **11**
  field JSON, `OperatorAddressResponse` **10** field JSON, `CustomerLookupResponse`
  **6** field JSON — khớp §3.1–§3.5.
- **DTO order** (`internal/modules/order/presentation/dto/dto.go`): `OrderAddressResponse`
  **8** field JSON; `CheckoutRequest` **1** field (`addressId`, không đổi),
  `EditOrderRequest` **2** field (`addressId`, `lines`, không đổi) — khớp §3.6.
- **Route user** (`internal/modules/user/presentation/http/router.go`): nhóm `/users`
  có **10** route (9 self-service + `GET /{userId}` lookup ADMIN); **6** route liên quan
  địa chỉ trong guide (§2 #1–#6) khớp method+path từng route. `DELETE` trả `204`
  không body — guide ghi "không đổi wire", đúng mã nguồn.
- **Route order** (`internal/modules/order/presentation/http/router.go`): nhóm khách
  **8** route, nhóm admin **8** route; **12** route trả chi tiết đầy đủ trong guide
  (§2 #7–#18) khớp method+path từng route; 2 route list trả summary (không địa chỉ)
  và 2 route stream — guide ghi "không đổi", đúng mã nguồn.
- **Status HTTP** (handler user `http/handler.go`, handler order `http/handler.go`):
  đã kiểm tra **10** mã thành công được guide khẳng định — `POST` địa chỉ `201`,
  `GET`/`PATCH`/`default`/lookup `200`, `DELETE` địa chỉ `204`, checkout `201`,
  các đọc/sửa đơn khách và admin `200` — tất cả khớp.
- **Enum**: không thêm/sửa enum nào (bộ 7 trạng thái đơn giữ nguyên) — guide không
  nêu enum mới, đúng mã nguồn.
- **Mã lỗi**: không thêm mã mới; mọi từ chối `facebookUrl` dùng `VALIDATION_ERROR`
  với `field = "facebookUrl"` — khớp [contracts/error-codes.md](contracts/error-codes.md)
  và handler (`fieldFacebookURL`, required-check ở `CreateAddress`, guard giữ chỗ ở
  `checkout.go toAddress`).
- **Swagger**: annotation trên các handler địa chỉ/đơn giữ nguyên, chỉ thêm field vào
  schema DTO — việc chạy `make swagger` và `make swagger-check` do orchestrator thực
  hiện ở gate (ngoài phạm vi worker phase này).
