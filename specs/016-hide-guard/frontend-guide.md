# Frontend Integration Guide — Hide Guard with Pending Orders (016)

Tài liệu bàn giao cho client về feature **chặn ẩn khi còn đơn chờ** (modules 03
Category, 04 Product). Nó liệt kê **mọi endpoint bị ảnh hưởng** với method +
path, đánh dấu **mới / đổi / không đổi**, và với mỗi endpoint **đổi** thì nêu
**trước → sau**. Nguồn authoritative vẫn là
[`docs/api-reference.md`](../../docs/api-reference.md) (mục 1.4, 5.6, 6.6,
6.13 và changelog 2026-10-16); tài liệu này là điểm vào nhanh cho frontend.

> **Kế thừa, không thay thế:** guide này không thay thế guide của feature 015
> ([`specs/015-product-sku-model/frontend-guide.md`](../015-product-sku-model/frontend-guide.md)).
> Mọi nội dung 015 giữ nguyên hiệu lực, trừ ba điểm đổi ở mục 3 dưới đây
> (`UpdateProductRequest` thêm `isVisible`, `AdminProduct` 11 → 12 member,
> `AdminProductDetail` 13 → 14 member) và ba mã 409 mới ở mục 4.

## 1. Tóm tắt một câu

Ẩn danh mục (`isVisible: false`), tắt cờ hiển thị sản phẩm (`isVisible:
false`) và mọi bước chuyển variant **rời** trạng thái đang bán đều bị từ chối
`409` khi còn đơn `PENDING`/`PAYMENT_PENDING` giữ variant liên quan (thân từ
chối nêu từng đơn chặn); ẩn thành công thì các variant bị ẩn bị gỡ khỏi mọi
giỏ trong cùng transaction. **Không thêm/không gỡ route, không đổi
method/path**; DTO chỉ thêm member `isVisible` (tuỳ chọn ở request, thêm ở
response quản trị).

## 2. Bảng endpoint bị ảnh hưởng

| Method | Path | Trạng thái | Ghi chú |
|---|---|---|---|
| PATCH | `/api/v1/admin/categories/{categoryId}` | **Đổi** | Thêm `409 CATEGORY_HAS_PENDING_ORDERS` khi `isVisible: false` mà còn đơn chờ; DTO không đổi |
| PATCH | `/api/v1/admin/products/{id}` | **Đổi** | Request thêm `isVisible?`; response `AdminProductDetail` thêm `isVisible`; thêm `409 PRODUCT_HAS_PENDING_ORDERS` |
| POST | `/api/v1/admin/products/{id}/variants/{variantId}/state` | **Đổi** | Thêm `409 VARIANT_HAS_PENDING_ORDERS` cho mọi bước **rời** trạng thái đang bán; request DTO không đổi, response `AdminProductDetail` thêm `isVisible` |

Mọi endpoint khác của module 03/04 **không đổi** và không liệt kê ở đây. Cả ba
endpoint trên đều yêu cầu Bearer access token, role `ADMIN`; tác nhân lấy từ
**session**, không bao giờ từ body.

## 3. Chi tiết từng endpoint

### 3.1 `PATCH /api/v1/admin/categories/{categoryId}` — **Đổi** (thêm 409, DTO không đổi)

- **Trước**: lỗi `VALIDATION_ERROR` 400 · `MALFORMED_REQUEST` 400 ·
  `UNAUTHENTICATED` 401 · `FORBIDDEN` 403 · `CATEGORY_NOT_FOUND` 404 ·
  `CATEGORY_NAME_TAKEN` 409 · `CATEGORY_SLUG_TAKEN` 409 · `RATE_LIMITED` 429.
  Trả `200` với hình dạng quản trị danh mục.
- **Sau**: giữ nguyên tất cả trên, **thêm** `CATEGORY_HAS_PENDING_ORDERS` 409
  — chỉ khi ẩn (`isVisible: false`) mà còn đơn `PENDING`/`PAYMENT_PENDING` giữ
  một variant của danh mục. Request/response DTO **không đổi** (`isVisible`
  của danh mục đã tồn tại từ trước feature này).
- **Thân từ chối**: `error.details` có **một phần tử cho mỗi đơn chặn** —
  `{"field": "orderId", "issue": "<định danh đơn>"}`; danh mục và giỏ nguyên
  vẹn. Hiện danh mục (`isVisible: true`) **không bao giờ** bị chặn; ẩn thành
  công gỡ các variant của danh mục khỏi mọi giỏ trong cùng transaction.
- **Việc client phải làm**: khi gửi `{"isVisible": false}` thì xử lý thêm nhánh
  `409 CATEGORY_HAS_PENDING_ORDERS` — đọc `error.details[].issue` để nêu đúng
  đơn chặn cho operator; các member khác giữ nguyên cách xử lý cũ.

### 3.2 `PATCH /api/v1/admin/products/{id}` — **Đổi** (thêm `isVisible` + thêm 409)

- **Trước**: body `UpdateProductRequest` **6 member** (`name`, `code`,
  `description`, `defaultPrice`, `categoryId`, `position`); response
  `AdminProduct` **11 member** (chưa có `isVisible`), `AdminProductDetail`
  **13 member**; lỗi như mục 3.1 nhưng với `PRODUCT_NOT_FOUND` 404 /
  `PRODUCT_CODE_TAKEN` 409.
- **Sau**:
  - Body `UpdateProductRequest` **7 member** — thêm **`isVisible?: boolean**
    (tuỳ chọn; bỏ trống = giữ nguyên). Gửi `false` = yêu cầu tắt hiển thị
    (đi qua hide guard); gửi `true` = bật hiển thị (không bao giờ bị chặn).
  - Response `AdminProduct` **12 member** — thêm **`isVisible: boolean`**;
    `AdminProductDetail` **14 member** (= 12 của `AdminProduct` + `images` +
    `variants`).
  - Lỗi **thêm** `PRODUCT_HAS_PENDING_ORDERS` 409 — chỉ khi
    `isVisible: false` mà còn đơn `PENDING`/`PAYMENT_PENDING` giữ một variant
    của sản phẩm. Thân từ chối và ngữ nghĩa nguyên vẹn/gỡ giỏ giống 3.1.
- **Việc client phải làm**: thêm toggle `isVisible` vào form sửa sản phẩm
  (bỏ trống khi không muốn đổi); render `isVisible` từ response quản trị;
  xử lý nhánh `409 PRODUCT_HAS_PENDING_ORDERS` như 3.1.

```json
{ "name": "Acrylic stand Aki mới", "position": 5, "isVisible": false }
```

### 3.3 `POST /api/v1/admin/products/{id}/variants/{variantId}/state` — **Đổi** (thêm 409, DTO không đổi)

- **Trước**: body `ChangeVariantStateRequest` **1 member** (`to`); response
  `AdminProductDetail` 13 member; lỗi `VALIDATION_ERROR` 400 ·
  `MALFORMED_REQUEST` 400 · `UNAUTHENTICATED` 401 · `FORBIDDEN` 403 ·
  `PRODUCT_NOT_FOUND` 404 · `VARIANT_NOT_FOUND` 404 ·
  `VARIANT_STATE_TRANSITION_INVALID` 409 · `RATE_LIMITED` 429. Trả `200`.
- **Sau**: request DTO **không đổi** (`to` vẫn là 1 member); response
  `AdminProductDetail` 13 → **14 member** (do `AdminProduct` thêm `isVisible`,
  xem 3.2); lỗi **thêm** `VARIANT_HAS_PENDING_ORDERS` 409 cho **mọi bước rời
  trạng thái đang bán** khi còn đơn chờ giữ variant (`ACTIVE → OUT_OF_STOCK`,
  `ACTIVE → DISCONTINUED` và mọi bước rời đang bán khác).
- **Không bị chặn**: chuyển **về** đang bán (`… → ACTIVE`) và
  `COMING_SOON → DISCONTINUED` (không rời đang bán) — đi qua không cần kiểm
  tra. Tín hiệu hết hàng của hệ thống bị chặn thì thành **no-op đã ghi nhận**,
  không phải lỗi (tồn kho vẫn ghi đúng).
- **Việc client phải làm**: xử lý thêm nhánh `409
  VARIANT_HAS_PENDING_ORDERS` khi chuyển variant khỏi đang bán — đọc
  `error.details[].issue` để nêu đơn chặn; đừng coi `409` này là transition
  không hợp lệ (mã đó vẫn tồn tại riêng).

```json
{ "to": "OUT_OF_STOCK" }
```

## 4. Mã lỗi — thêm mới

| Code | HTTP | Khi nào | Thân từ chối |
|---|---|---|---|
| `CATEGORY_HAS_PENDING_ORDERS` | 409 | `PATCH /admin/categories/{categoryId}` với `isVisible: false` mà còn đơn chờ giữ một variant của danh mục | Một phần tử `error.details` cho mỗi đơn chặn: `{"field": "orderId", "issue": "<định danh đơn>"}`; không gì bị đổi |
| `PRODUCT_HAS_PENDING_ORDERS` | 409 | `PATCH /admin/products/{id}` với `isVisible: false` mà còn đơn chờ giữ một variant của sản phẩm | Như trên |
| `VARIANT_HAS_PENDING_ORDERS` | 409 | `POST .../variants/{variantId}/state` với bước **rời** trạng thái đang bán mà còn đơn chờ giữ variant | Như trên |

Không gỡ mã nào; không đổi nghĩa mã nào. Ba endpoint trên giữ nguyên mọi mã
cũ (xem chi tiết ở mục 3 và
[`docs/api-reference.md`](../../docs/api-reference.md) mục 1.4/5.6/6.6/6.13).
Client rẽ nhánh theo `error.code`, không theo `error.message`. Hiện/bật hiển
thị và chuyển **về** đang bán không bao giờ trả ba mã này.

## 5. Enum client cần biết (không đổi ở feature này)

- `sellState` (và `to` trong `ChangeVariantStateRequest`): `COMING_SOON`,
  `ACTIVE`, `OUT_OF_STOCK`, `DISCONTINUED` — **4** giá trị. Trạng thái đang
  bán = `ACTIVE`; mọi bước **rời** `ACTIVE` đi qua guard, mọi bước **về**
  `ACTIVE` không bao giờ bị chặn.
- `status` của đơn: `PENDING`, `PAYMENT_PENDING`, `PAID`, `SHIPPED`,
  `COMPLETED`, `CANCELLED` — **6** giá trị. Chỉ `PENDING` và
  `PAYMENT_PENDING` chặn ẩn; bốn trạng thái còn lại không chặn.

## 6. HTTP status guide này dùng

- `200`: sửa/thành công của cả ba endpoint đổi (`PATCH` × 2, `POST` state).
- `400`: `VALIDATION_ERROR` (giá trị sai hình dạng; `details[].field` chỉ đúng
  member, ví dụ `"to"` ở endpoint state) hoặc `MALFORMED_REQUEST` (body không
  parse/member lạ) — cả ba endpoint.
- `401`: `UNAUTHENTICATED` (thiếu/sai phiên) — cả ba endpoint.
- `403`: `FORBIDDEN` (không phải ADMIN) — cả ba endpoint.
- `404`: `CATEGORY_NOT_FOUND` (endpoint danh mục); `PRODUCT_NOT_FOUND` /
  `VARIANT_NOT_FOUND` (hai endpoint sản phẩm/variant) — variant không thuộc
  sản phẩm cũng là `VARIANT_NOT_FOUND`.
- `409`: va chạm cũ (`CATEGORY_NAME_TAKEN`, `CATEGORY_SLUG_TAKEN`,
  `PRODUCT_CODE_TAKEN`, `VARIANT_STATE_TRANSITION_INVALID`) **cộng** ba mã mới
  ở mục 4 (mỗi endpoint thêm đúng một mã mới).
- `429`: `RATE_LIMITED` (hạn mức toàn cục; module không có hạn mức riêng) —
  cả ba endpoint.

## 7. Phụ lục đồng bộ (sync appendix)

Lần đồng bộ này đọc lại source ngày 2026-10-16 và đếm theo quy tắc
frontend-guide sync: **mọi** thẻ `json:"…"` của các DTO mà feature chạm,
**mọi** giá trị của **mọi** enum mà guide nêu, **mọi** route mà guide liệt kê,
**mọi** status mà guide tuyên bố. Mismatch tìm được và cách sửa ghi ở cuối
mục.

### 7.1 Field của DTO mà feature chạm (3 DTO, 21 thẻ `json` trực tiếp)

- `UpdateProductRequest` (**7**): `name`, `code`, `description`,
  `defaultPrice`, `categoryId`, `position`, **`isVisible`** (mới ở feature
  này; nguồn
  `internal/modules/product/presentation/dto/dto.go`).
- `AdminProductResponse` (**12**): `id`, `name`, `code`, `description`,
  `defaultPrice`, `categoryId`, `position`, **`isVisible`** (mới),
  `variantCount`, `imageUrl`, `createdAt`, `updatedAt` (cùng nguồn).
- `AdminProductDetailResponse` (**2** thẻ trực tiếp + kế thừa): `images`,
  `variants` (cùng nguồn) cộng toàn bộ 12 member của `AdminProductResponse`
  qua embed — phẳng hoá thành **14 member** (`AdminProduct` + `images` +
  `variants`), khớp [`docs/api-reference.md`](../../docs/api-reference.md)
  mục 6 (“`AdminProduct` (12 member, thêm `isVisible`)”,
  “`AdminProductDetail` (14 member …)”).
- `ChangeVariantStateRequest` (`to`) và các DTO danh mục **không đổi** ở
  feature này nên không đếm ở đây; chúng được nêu ở mục 3/5 để đủ ngữ cảnh.

### 7.2 Enum (2 enum, 10 giá trị)

- `sellState`/`to` (**4**): `COMING_SOON`, `ACTIVE`, `OUT_OF_STOCK`,
  `DISCONTINUED` — khớp `internal/modules/product/domain/constant/sellstate.go`
  và check constraint ở tầng lưu trữ.
- `status` của đơn (**6**): `PENDING`, `PAYMENT_PENDING`, `PAID`, `SHIPPED`,
  `COMPLETED`, `CANCELLED` — khớp
  `internal/modules/order/domain/constant/codes.go`.

### 7.3 Route (3 route, 0 thêm / 0 gỡ / 3 đổi)

- `PATCH /api/v1/admin/categories/{categoryId}` — method/path không đổi,
  thêm 1 mã 409 (nguồn `@Router /admin/categories/{categoryId} [patch]`,
  khớp [`docs/api-reference.md`](../../docs/api-reference.md) mục 5.6).
- `PATCH /api/v1/admin/products/{id}` — method/path không đổi, thêm
  `isVisible` + 1 mã 409 (nguồn `@Router /admin/products/{id} [patch]`,
  khớp api-reference mục 6.6).
- `POST /api/v1/admin/products/{id}/variants/{variantId}/state` —
  method/path không đổi, thêm 1 mã 409 (nguồn
  `@Router /admin/products/{id}/variants/{variantId}/state [post]`, khớp
  api-reference mục 6.13).

### 7.4 HTTP status (7 status)

`200`, `400`, `401`, `403`, `404`, `409`, `429` — mỗi status ở mục 6 đã đối
chiếu với dòng “Lỗi:” của đúng endpoint trong
[`docs/api-reference.md`](../../docs/api-reference.md) (5.6/6.6/6.13); không
endpoint nào trong phạm vi trả `201`/`204`/`413`/`503`.

### 7.5 Mismatch đã tìm và sửa (3 mục)

1. `UpdateProductRequest` **6 → 7 member**: guide 015 (§5.1) liệt kê 6 member
   (chưa có `isVisible`). Guide này liệt kê đủ 7, khớp thẻ `json` trong
   source và ví dụ request ở api-reference mục 6.6.
2. `AdminProductResponse` **11 → 12 member**, `AdminProductDetailResponse`
   **13 → 14 member**: guide 015 (§3.3/§3.4/§5.1) ghi 11/13 (chưa có
   `isVisible`). Guide này ghi 12/14, khớp struct trong source và câu
   “(12 member, thêm `isVisible`)” / “(14 member …)” trong api-reference.
3. Premise “không đổi DTO client-facing” (plan Constitution Check, tasks
   T023): không còn đúng với code đã giao (code đã thêm `isVisible` vào 1
   request + 2 response, api-reference changelog 2026-10-16 cũng ghi “DTO
   chỉ thêm member `isVisible`”). Guide này được tạo chính để sửa khoảng
   trống đó; T023 giữ nguyên văn bản lịch sử, không sửa.
