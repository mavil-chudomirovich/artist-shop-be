# Frontend Integration Guide — Sản phẩm

**Feature**: `006-product-catalog`
**Trạng thái**: Hoàn thiện phần endpoint; 11 endpoint của module 04 đã được ghi vào
`docs/api-reference.md` §6, kèm thay đổi `409 CATEGORY_IN_USE` ở §5.7.
**Đối tượng đọc**: Lập trình viên frontend (trang khách + trang quản trị).
**Mục tiêu**: FE triển khai duyệt sản phẩm và màn quản trị sản phẩm chỉ với tài liệu này —
không cần đọc code backend hay Swagger.

> Nguồn authoritative là [`docs/api-reference.md`](../../docs/api-reference.md) §6. Guide này
> **phải khớp** với file đó; khi lệch, `docs/api-reference.md` thắng. Mọi tên trường dưới đây
> là **đúng** `json:"…"` mà DTO phát ra.

---

## 0. Tóm tắt nhanh (TL;DR)

- **11 endpoint**: 2 công khai (`/products`, địa chỉ theo **slug**) + 9 quản trị
  (`/admin/products`, vai trò `ADMIN`, địa chỉ theo **định danh**).
- **Hai hình dạng response.** Công khai: 6 member ở danh sách, thêm `description` + `images`
  ở chi tiết. Quản trị: 15 member (có `sellState`, `position`, `categoryId`, `isSet`,
  `preorderExpectedAt`, `createdAt`, `updatedAt`…), thêm `images` và `members` ở chi tiết.
- **Sản phẩm bị ẩn và slug chưa từng tồn tại trả lời y hệt nhau** (`404 PRODUCT_NOT_FOUND`).
  FE **không thể** và không nên cố phân biệt. Gồm cả sản phẩm thuộc **danh mục bị ẩn**.
- **Trạng thái bán đi qua endpoint riêng** `POST .../state`, không phải một field của `PATCH`.
  Chuyển không hợp lệ trả `409 PRODUCT_STATE_TRANSITION_INVALID` kèm trạng thái hiện tại.
- **Giá là số nguyên đơn vị nhỏ + `currency`**, không bao giờ là số thực.
- **`preorderExpectedAt` ghi theo `format: date`** (`YYYY-MM-DD`), không kèm giờ.
- Ảnh: tối đa **10**/sản phẩm, JPEG/PNG/WebP nhận theo **byte**, trần **2 MB**.
- **`meta.total` vắng mặt khi bằng 0** — FE đọc thiếu `total` là `0`.
- Xoá là **hard delete** (dòng biến mất, audit ở lại).

---

## 1. Quy ước

- Base path `/api/v1`; envelope `{data, meta}` / `{error}` như các guide trước.
- Công khai: **không cần token**. Quản trị: `Bearer` + role `ADMIN` (thiếu quyền → `403
  FORBIDDEN`, ghi audit `AUTH_PRIVILEGE_DENIED`).
- Phân trang (cả hai danh sách): `page` default `1` (min `1`), `pageSize` default `20` (`1..100`).
- Module **không** có rate limit riêng; chỉ chịu hạn mức toàn cục (`RATE_LIMIT_RPS`).
- Thứ tự danh sách **ổn định**: `position`, rồi `createdAt`, rồi `id` — hai request giống nhau
  trả cùng thứ tự.
- Lọc danh sách công khai: `?category=<slug>` (slug danh mục). Slug ẩn **hoặc** không tồn tại
  đều trả `data: []` — **không** `404`.

### Kiểu dữ liệu

`Price` (2 member, dùng ở mọi nơi có giá):
```json
{ "amount": 120000, "currency": "VND" }
```

`PublicProduct` (6 member) — một phần tử của danh sách công khai:
```json
{
  "id": "<uuid>",
  "name": "Acrylic stand Aki",
  "slug": "acrylic-stand-aki",
  "price": { "amount": 120000, "currency": "VND" },
  "imageUrl": "https://res.cloudinary.com/demo/image/upload/stand.jpg",
  "isPreorder": false
}
```

`PublicProductDetail` (8 member = `PublicProduct` + 2): thêm `description` và `images`.

`PublicImage` (4 member):
```json
{ "id": "<uuid>", "url": "https://res.cloudinary.com/.../stand.jpg", "width": 1600, "height": 1200 }
```

`AdminProduct` (15 member) — một phần tử của danh sách quản trị:
```json
{
  "id": "<uuid>",
  "name": "Acrylic stand Aki",
  "slug": "acrylic-stand-aki",
  "description": "Acrylic stand 15cm",
  "price": { "amount": 120000, "currency": "VND" },
  "categoryId": "<uuid>",
  "position": 10,
  "sellState": "COMING_SOON",
  "isSet": false,
  "isPreorder": false,
  "preorderExpectedAt": null,
  "imageCount": 1,
  "imageUrl": "https://res.cloudinary.com/.../stand.jpg",
  "createdAt": "2026-10-07T08:15:04Z",
  "updatedAt": "2026-10-07T08:15:04Z"
}
```

`AdminProductDetail` (17 member = `AdminProduct` + 2): thêm `images` (mảng `AdminImage`) và
`members` (mảng `SetMember`, **vắng mặt** khi không phải set).

`AdminImage` (7 member): `id`, `publicId`, `url`, `width`, `height`, `position`, `isPrimary`.

`SetMember` (3 member):
```json
{ "id": "<uuid>", "name": "Acrylic stand Aki", "slug": "acrylic-stand-aki" }
```

---

## 2. Bảng endpoint

| # | Method | Path | Auth | Trả về |
|---|---|---|---|---|
| 6.1 | GET | `/api/v1/products` | — | `200` list `PublicProduct` + phân trang |
| 6.2 | GET | `/api/v1/products/{slug}` | — | `200` `PublicProductDetail` |
| 6.3 | GET | `/api/v1/admin/products` | ADMIN | `200` list `AdminProduct` + phân trang |
| 6.4 | POST | `/api/v1/admin/products` | ADMIN | `201` `AdminProductDetail` |
| 6.5 | GET | `/api/v1/admin/products/{id}` | ADMIN | `200` `AdminProductDetail` |
| 6.6 | PATCH | `/api/v1/admin/products/{id}` | ADMIN | `200` `AdminProductDetail` |
| 6.7 | DELETE | `/api/v1/admin/products/{id}` | ADMIN | `204` (không body) |
| 6.8 | POST | `/api/v1/admin/products/{id}/state` | ADMIN | `200` `AdminProductDetail` |
| 6.9 | POST | `/api/v1/admin/products/{id}/images` | ADMIN | `201` `AdminProductDetail` |
| 6.10 | DELETE | `/api/v1/admin/products/{id}/images/{imageId}` | ADMIN | `204` (không body) |
| 6.11 | POST | `/api/v1/admin/products/{id}/images/{imageId}/primary` | ADMIN | `200` `AdminProductDetail` |

### Chi tiết

**6.1 `GET /products`** — chỉ sản phẩm **đang hiển thị**: đang bán (`ACTIVE`) **hoặc** là
pre-order đang thông báo, và **danh mục chưa bị ẩn**. `meta.total` đếm đúng số đó. Rỗng trả
`data: []` (**không** `null`, **không** lỗi). Query `category` lọc theo slug danh mục.
Lỗi: `VALIDATION_ERROR` 400 (`page`/`pageSize`) · `RATE_LIMITED` 429.

**6.2 `GET /products/{slug}`** — `slug` khớp mẫu URL-safe (`^[a-z0-9]+(-[a-z0-9]+)*$`, ≤ 140).
Trả `description` và **toàn bộ** `images` theo thứ tự.
Lỗi: `PRODUCT_NOT_FOUND` 404 · `RATE_LIMITED` 429.
> **Ẩn / hết hàng / ngừng bán / danh mục bị ẩn / đã xoá / chưa từng tồn tại → cùng `404
> PRODUCT_NOT_FOUND`.** Bắt buộc để không xác nhận sản phẩm operator chọn không công bố.

**6.3 `GET /admin/products`** — toàn bộ sản phẩm kể cả không hiển thị.
Lỗi: `VALIDATION_ERROR` 400 · `UNAUTHENTICATED` 401 · `FORBIDDEN` 403 · `RATE_LIMITED` 429.

**6.4 `POST /admin/products`** — tạo sản phẩm ở `COMING_SOON`. Body `CreateProductRequest`
(bên dưới). Sản phẩm mang `isPreorder: true` **hiện ngay** với khách; sản phẩm thường thì ẩn
cho tới khi `ACTIVE`.
Lỗi: `VALIDATION_ERROR` 400 (field sai — `details[].field` chỉ đúng tên member) ·
`MALFORMED_REQUEST` 400 (body không parse được hoặc có member lạ) · `UNAUTHENTICATED` 401 ·
`FORBIDDEN` 403 · `PRODUCT_SLUG_TAKEN` 409 · `RATE_LIMITED` 429.

**6.5 `GET /admin/products/{id}`** — `id` là UUID; đọc kể cả sản phẩm không hiển thị.
Lỗi: `VALIDATION_ERROR` 400 · `UNAUTHENTICATED` 401 · `FORBIDDEN` 403 · `PRODUCT_NOT_FOUND`
404 · `RATE_LIMITED` 429.

**6.6 `PATCH /admin/products/{id}`** — sửa một phần; member bỏ trống giữ nguyên. **Không bao
giờ đổi `sellState`** (dùng 6.8). Gửi lại đúng slug hiện tại **thành công** (không tự trùng
chính nó). Gửi `memberProductIds` thay **toàn bộ** danh sách thành viên (chỉ khi là set); gửi
`[]` xoá hết; `isPreorder: false` cũng xoá `preorderExpectedAt`.
Lỗi: `VALIDATION_ERROR` 400 · `MALFORMED_REQUEST` 400 · `UNAUTHENTICATED` 401 · `FORBIDDEN`
403 · `PRODUCT_NOT_FOUND` 404 · `PRODUCT_SLUG_TAKEN` 409 · `RATE_LIMITED` 429.

**6.7 `DELETE /admin/products/{id}`** — hard delete, `204` không body; ảnh của sản phẩm được
giải phóng và các dòng membership bị xoá theo; **thành viên của một set bị xoá không bị đụng**.
Xoá lại sản phẩm đã xoá cũng `404 PRODUCT_NOT_FOUND`.
Lỗi: `VALIDATION_ERROR` 400 · `UNAUTHENTICATED` 401 · `FORBIDDEN` 403 · `PRODUCT_NOT_FOUND`
404 · `RATE_LIMITED` 429.

**6.8 `POST /admin/products/{id}/state`** — chuyển trạng thái bán. Body `ChangeStateRequest`
`{ "to": "<SellState>" }`. Các cạnh hợp lệ:
`COMING_SOON → ACTIVE`; `ACTIVE ↔ OUT_OF_STOCK`; `* → DISCONTINUED` (`DISCONTINUED` là cuối).
Lên `ACTIVE` **xoá nhãn pre-order**. Chuyển không hợp lệ trả `409
PRODUCT_STATE_TRANSITION_INVALID` và **không đổi gì**.
Lỗi: `VALIDATION_ERROR` 400 (`to` không thuộc 4 giá trị) · `MALFORMED_REQUEST` 400 ·
`UNAUTHENTICATED` 401 · `FORBIDDEN` 403 · `PRODUCT_NOT_FOUND` 404 ·
`PRODUCT_STATE_TRANSITION_INVALID` 409 · `RATE_LIMITED` 429.

**6.9 `POST /admin/products/{id}/images`** — body `multipart/form-data`, một part tên `image`.
Byte được nhận diện theo chữ ký (JPEG/PNG/WebP), **không** tin tên file hay `Content-Type`
client khai. Ảnh đầu tiên thành `isPrimary: true`. Tối đa **10** ảnh; ảnh thứ 11 bị từ chối
**trước khi** lên provider.
Lỗi: `VALIDATION_ERROR` 400 (thiếu part `image`) · `MALFORMED_REQUEST` 400 ·
`PRODUCT_IMAGE_TYPE_UNSUPPORTED` 400 · `UNAUTHENTICATED` 401 · `FORBIDDEN` 403 ·
`PRODUCT_NOT_FOUND` 404 · `PRODUCT_IMAGE_LIMIT_REACHED` 409 · `PRODUCT_IMAGE_TOO_LARGE` 413 ·
`RATE_LIMITED` 429 · `PRODUCT_MEDIA_UNAVAILABLE` 503.

**6.10 `DELETE /admin/products/{id}/images/{imageId}`** — `204` không body. Xoá ảnh chính sẽ
**thăng cấp** ảnh kế theo `position`, nên sản phẩm còn ảnh luôn có đúng một ảnh chính. Ảnh của
sản phẩm khác trả cùng `404 PRODUCT_NOT_FOUND`.
Lỗi: `VALIDATION_ERROR` 400 · `UNAUTHENTICATED` 401 · `FORBIDDEN` 403 · `PRODUCT_NOT_FOUND`
404 · `RATE_LIMITED` 429.

**6.11 `POST /admin/products/{id}/images/{imageId}/primary`** — đặt ảnh đó làm ảnh chính.
Lỗi: `VALIDATION_ERROR` 400 · `UNAUTHENTICATED` 401 · `FORBIDDEN` 403 · `PRODUCT_NOT_FOUND`
404 · `RATE_LIMITED` 429.

### Kiểu request quản trị

`CreateProductRequest` (10 member; `name`, `slug`, `price`, `categoryId`, `position` bắt buộc):
```json
{
  "name": "Acrylic stand Aki",
  "slug": "acrylic-stand-aki",
  "description": "Acrylic stand 15cm",
  "price": { "amount": 120000, "currency": "VND" },
  "categoryId": "<uuid>",
  "position": 10,
  "isSet": false,
  "memberProductIds": ["<uuid>", "<uuid>"],
  "isPreorder": false,
  "preorderExpectedAt": null
}
```

`UpdateProductRequest` (10 member, **tất cả tuỳ chọn**; bỏ trống = giữ nguyên):
```json
{
  "name": "...", "slug": "...", "description": "...",
  "price": { "amount": 130000, "currency": "VND" },
  "categoryId": "<uuid>", "position": 5,
  "isSet": false, "memberProductIds": ["<uuid>"],
  "isPreorder": true, "preorderExpectedAt": "2026-12-01"
}
```

`ChangeStateRequest` (1 member bắt buộc):
```json
{ "to": "ACTIVE" }
```

---

## 3. Bảng giá trị / trạng thái

| Trường | Giá trị | Ý nghĩa / FE |
|---|---|---|
| `sellState` | `COMING_SOON` / `ACTIVE` / `OUT_OF_STOCK` / `DISCONTINUED` | `COMING_SOON`: thông báo, ẩn trừ khi có pre-order. `ACTIVE`: hiện và mua được. `OUT_OF_STOCK`: ẩn. `DISCONTINUED`: cuối, không quay lại |
| `isPreorder` | `true` / `false` | Nhãn pre-order. Sản phẩm `isPreorder: true` **hiện** cho khách dù chưa bán, nhưng **không mua được** |
| `isSet` | `true` / `false` | Combo set: một sản phẩm độc lập có giá riêng do operator đặt |
| `position` | số nguyên bất kỳ (kể cả `0`, âm) | Thứ tự ưu tiên; trùng thì tie-break theo `createdAt` rồi `id` |
| `preorderExpectedAt` | `YYYY-MM-DD` hoặc `null` | Chỉ có nghĩa khi `isPreorder: true`; lên `ACTIVE` sẽ bị xoá |
| `price.amount` | số nguyên dương (đơn vị nhỏ) | Không bao giờ là số thực; round-trip đúng giá trị operator nhập |
| `price.currency` | 3 chữ in hoa (`VND`) | Gửi kèm mỗi giá |
| `slug` | `^[a-z0-9]+(-[a-z0-9]+)*$`, ≤ 140 | Đoạn link công khai; do operator viết, sửa được (đổi làm hỏng link đã lưu) |
| `name` | ≤ 120 ký tự (đếm **rune**) | **Không** duy nhất — sản phẩm được phép trùng tên |
| `description` | ≤ 5000 ký tự, có thể rỗng | Mô tả khách đọc |
| ảnh | JPEG/PNG/WebP, ≤ 2 MB, ≤ 10 ảnh | Nhận theo byte; ảnh chính `isPrimary` |

---

## 4. Trước → sau

### Endpoint MỚI (11)

Toàn bộ endpoint ở §2 là **mới** trong feature này. Không có endpoint sản phẩm nào từng tồn
tại trước đó.

### Endpoint ĐỔI (1) — thuộc module 03 Category

**`DELETE /api/v1/admin/categories/{id}`** — trước → sau:

| | Trước (module 03) | Sau (feature 006) |
|---|---|---|
| Xoá danh mục còn sản phẩm | Không kiểm chứng được vì chưa có thực thể sản phẩm; hành vi thực tế là xoá thành công | **Bị từ chối `409 CATEGORY_IN_USE`** |
| Cơ chế | Không có tham chiếu | `products.category_id` với `ON DELETE RESTRICT`; database từ chối, module 03 dịch thành `409` |
| Danh mục/sản phẩm sau khi bị từ chối | — | Cả hai **nguyên vẹn** |
| Xoá danh mục **không** còn sản phẩm | `204` | `204` (không đổi) |

FE chỉ cần thêm xử lý `409 CATEGORY_IN_USE` ở màn xoá danh mục, hiển thị thông báo "danh mục
còn sản phẩm"; mọi status khác giữ nguyên.

---

## 5. Bảng mã lỗi

**Dùng chung**: `VALIDATION_ERROR` 400, `MALFORMED_REQUEST` 400, `UNAUTHENTICATED` 401,
`FORBIDDEN` 403, `RATE_LIMITED` 429, `INTERNAL_ERROR` 500.

**Riêng module product** (7 mã):

| Code | HTTP | FE nên làm |
|---|---|---|
| `PRODUCT_NOT_FOUND` | 404 | Công khai: hiện "không tìm thấy" chung — gồm cả sản phẩm bị ẩn/ngừng bán/thuộc danh mục ẩn. Admin: id không tồn tại hoặc đã xoá, hoặc ảnh không thuộc sản phẩm |
| `PRODUCT_SLUG_TAKEN` | 409 | Lỗi cạnh ô **slug** (`details[].field = "slug"`) |
| `PRODUCT_STATE_TRANSITION_INVALID` | 409 | Chuyển trạng thái không hợp lệ; message nêu trạng thái hiện tại. Tải lại trạng thái rồi thử cạnh hợp lệ |
| `PRODUCT_IMAGE_LIMIT_REACHED` | 409 | Đã đủ 10 ảnh; xoá một ảnh trước khi thêm |
| `PRODUCT_IMAGE_TYPE_UNSUPPORTED` | 400 | File không phải ảnh hỗ trợ; cho chọn file khác |
| `PRODUCT_IMAGE_TOO_LARGE` | 413 | Ảnh > 2 MB; yêu cầu nén/chọn file nhỏ hơn |
| `PRODUCT_MEDIA_UNAVAILABLE` | 503 | Lỗi tạm của dịch vụ media; **có thể thử lại**, dữ liệu chưa đổi |

**Thuộc module 03, nay mới xuất hiện** (1 mã):

| Code | HTTP | FE nên làm |
|---|---|---|
| `CATEGORY_IN_USE` | 409 | Màn xoá danh mục: "danh mục còn sản phẩm, không thể xoá" |

---

## 6. Checklist FE

- [ ] Trang khách chỉ gọi `/products*` (không token); trang admin gọi `/admin/products*`.
- [ ] Dùng `slug` để dựng link công khai; `id` để định danh trong màn quản trị.
- [ ] **Cảnh báo operator khi sửa `slug`** rằng link đã lưu có thể hỏng.
- [ ] Hiển thị `409` đúng ô (slug) dựa vào `error.details[].field`.
- [ ] Xử lý `404` công khai bằng một thông báo chung, không suy đoán sản phẩm/danh mục tồn tại.
- [ ] Đổi trạng thái qua `POST .../state`, **không** gửi `sellState` trong `PATCH`.
- [ ] Giá gửi dạng `{ amount, currency }`; không nhân/chia số thực ở FE.
- [ ] `preorderExpectedAt` gửi và hiển thị dạng `YYYY-MM-DD`.
- [ ] Upload ảnh dùng `multipart/form-data` part `image`; xử lý `413`/`400`/`409`/`503` riêng.
- [ ] Đọc `meta.total` thiếu là `0`; xử lý `data: []`.
- [ ] Chỉ gửi đúng member trong contract (`additionalProperties: false` → member lạ trả
      `MALFORMED_REQUEST`).

---

## 7. Changelog & đối chiếu

**2026-10-08** — Viết guide lần đầu cho feature 006 (hiến pháp v1.7.0 §Governance).

Đối chiếu với nguồn (có đếm):

- endpoint trong §2: **11 mới** + **1 đổi** (module 03 `DELETE /admin/categories/{id}`) — khớp
  `docs/api-reference.md` §6 (và §5.7), `specs/006-product-catalog/contracts/openapi.yaml`, và
  `internal/modules/product/presentation/http/router.go` (**11 route**: 2 công khai + 9 quản trị).
- `json:"…"` trong `internal/modules/product/presentation/dto/dto.go`: **64 tag** trên **12
  kiểu** (26 tên member phân biệt). Đã đối chiếu **64/64** tên trường trong guide với tag tương
  ứng: `Price` 2 · `PublicProduct` 6 · `PublicImage` 4 · `PublicProductDetail` 2 ·
  `PriceRequest` 2 · `CreateProductRequest` 10 · `UpdateProductRequest` 10 · `ChangeStateRequest`
  1 · `AdminProduct` 15 · `AdminImage` 7 · `SetMember` 3 · `AdminProductDetail` 2.
- giá trị `sellState`: **4** (`COMING_SOON`, `ACTIVE`, `OUT_OF_STOCK`, `DISCONTINUED`) — đối
  chiếu **hai chiều** với `internal/modules/product/domain/constant/sellstate.go`: mọi giá trị
  guide nêu đều có trong code, và mọi hằng trong code đều có trong guide.
- mã lỗi riêng module: **7** — khớp `internal/modules/product/domain/constant/codes.go` và
  `contracts/error-codes.md`; cộng **1** mã mới xuất hiện ở module 03 (`CATEGORY_IN_USE`).
- HTTP status khẳng định: **12** — `200`, `201`, `204`, `400`, `401`, `403`, `404`, `409`,
  `413`, `429`, `500`, `503`. Mỗi status đều là một câu trả lời thật:
  `200`/`201`/`204` do handler; `400` (`VALIDATION_ERROR` / `MALFORMED_REQUEST`); `401`
  (`UNAUTHENTICATED` do middleware/handler); `403` (`FORBIDDEN` do guard `ADMIN`); `404`
  (`PRODUCT_NOT_FOUND`); `409` (slug/state/limit, và `CATEGORY_IN_USE` của module 03); `413`
  (`PRODUCT_IMAGE_TOO_LARGE`); `429` (`RATE_LIMITED`); `500` (`INTERNAL_ERROR` mặc định của
  `mapError`); `503` (`PRODUCT_MEDIA_UNAVAILABLE`).
