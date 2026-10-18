# Frontend Integration Guide — Catalog Reorder (Drag & Drop) (018)

Tài liệu bàn giao cho client về feature **kéo-thả sắp xếp** danh mục, sản phẩm và
biến thể (modules 03 Category, 04 Product). Nó liệt kê **mọi endpoint bị ảnh
hưởng** với method + path, đánh dấu **mới / đổi / gỡ**, và với mỗi endpoint
**đổi** thì nêu **trước → sau**. Nguồn authoritative vẫn là
[`docs/api-reference.md`](../../docs/api-reference.md) (mục 5, 6 và changelog);
tài liệu này là điểm vào nhanh cho frontend, và phải khớp với
[contracts/openapi.yaml](./contracts/openapi.yaml).

> **Kế thừa, không thay thế:** guide này không thay thế guide của feature 015/016.
> Mọi nội dung 015/016 giữ nguyên hiệu lực, trừ những điểm đổi ở mục 3 dưới đây.

## 1. Tóm tắt một câu

Admin kéo-thả để đổi thứ tự danh mục, sản phẩm và biến thể; mỗi lần lưu gửi **toàn
bộ** danh sách id theo thứ tự mới tới một trong ba endpoint reorder mới, và backend
áp dụng bằng một câu ghi duy nhất. Trường `position` trở thành **tuỳ chọn** khi
tạo (bỏ trống = bản ghi mới lên đầu; sửa mà bỏ trống thì giữ nguyên thứ tự). Hai danh sách quản trị thêm tham số
`all=true` để tải trọn danh sách phục vụ kéo-thả. **Thêm 3 route, đổi 5 route,
không gỡ route nào.**

## 2. Bảng endpoint bị ảnh hưởng

| Method | Path | Trạng thái | Ghi chú |
|---|---|---|---|
| PUT | `/api/v1/admin/categories/reorder` | **Mới** | Gửi `categoryIds` đầy đủ theo thứ tự mới |
| PUT | `/api/v1/admin/products/reorder` | **Mới** | Gửi `productIds` đầy đủ theo thứ tự mới |
| PUT | `/api/v1/admin/products/{id}/variants/reorder` | **Mới** | Gửi `variantIds` đầy đủ của sản phẩm theo thứ tự mới |
| GET | `/api/v1/admin/categories` | **Đổi** | Thêm `?all=true` để lấy trọn danh sách |
| GET | `/api/v1/admin/products` | **Đổi** | Thêm `?all=true` để lấy trọn danh sách |
| POST | `/api/v1/admin/categories` | **Đổi** | `position` thành tuỳ chọn; bỏ trống = lên đầu |
| POST | `/api/v1/admin/products` | **Đổi** | `position` (sản phẩm + từng variant) thành tuỳ chọn |
| POST | `/api/v1/admin/products/{id}/variants` | **Đổi** | `position` thành tuỳ chọn; bỏ trống = lên đầu |

Không gỡ endpoint nào. Mọi endpoint trên yêu cầu Bearer access token, role `ADMIN`;
tác nhân lấy từ **session**, không bao giờ từ body.

## 3. Chi tiết từng endpoint

### 3.1 `PUT /api/v1/admin/categories/reorder` — **Mới**

- **Body**: `{ "categoryIds": ["<uuid>", ...] }` — **toàn bộ** danh mục, đúng thứ
  tự mong muốn, không trùng. Id đầu tiên là danh mục đầu tiên.
- **Thành công** `200`: `data = { "updatedCount": <số nguyên> }`.
- **Lỗi**: `400 VALIDATION_ERROR` (mảng rỗng hoặc thiếu danh mục) ·
  `400 CATEGORY_REORDER_DUPLICATE_ID` (trùng id) · `401 UNAUTHENTICATED` ·
  `403 FORBIDDEN` · `404 CATEGORY_NOT_FOUND` (id không tồn tại) · `429 RATE_LIMITED`.
- **Việc client làm**: dùng nút lưu của màn kéo-thả; gửi mảng id của **trọn** danh
  sách (lấy qua `?all=true`), không gửi mảng của riêng một trang.

### 3.2 `PUT /api/v1/admin/products/reorder` — **Mới**

- **Body**: `{ "productIds": ["<uuid>", ...] }` — **toàn bộ** sản phẩm, đúng thứ
  tự, không trùng.
- **Thành công** `200`: `data = { "updatedCount": <số nguyên> }`.
- **Lỗi**: như 3.1 nhưng với `PRODUCT_REORDER_DUPLICATE_ID` và `PRODUCT_NOT_FOUND`.
- **Việc client làm**: như 3.1, dùng `GET /admin/products?all=true`.

### 3.3 `PUT /api/v1/admin/products/{id}/variants/reorder` — **Mới**

- **Body**: `{ "variantIds": ["<uuid>", ...] }` — **toàn bộ** variant của sản phẩm,
  đúng thứ tự, không trùng. Variant đầu tiên là **variant mặc định**.
- **Thành công** `200`: `data = { "updatedCount": <số nguyên> }`.
- **Lỗi**: `400 VALIDATION_ERROR` (rỗng/thiếu variant) ·
  `400 VARIANT_REORDER_DUPLICATE_ID` (trùng id) ·
  `400 VARIANT_BELONGS_TO_OTHER_PRODUCT` (id không thuộc sản phẩm trên path) ·
  `401` · `403` · `404 PRODUCT_NOT_FOUND` (không thấy sản phẩm cha) · `429`.
- **Việc client làm**: màn kéo-thả variant lấy id từ `AdminProductDetail.variants`
  của đúng sản phẩm; xử lý `VARIANT_BELONGS_TO_OTHER_PRODUCT` như lỗi dữ liệu cũ,
  tải lại chi tiết sản phẩm.

### 3.4 `GET /api/v1/admin/categories` và `GET /api/v1/admin/products` — **Đổi** (thêm `all`)

- **Trước**: chỉ có `page` + `pageSize` (mặc định 1/20, tối đa 100).
- **Sau**: thêm `all` (boolean, mặc định `false`). Khi `all=true` trả **trọn** danh
  sách đã sắp xếp, bỏ qua phân trang, giới hạn bởi ngưỡng an toàn (tối đa 1000 bản
  ghi); vượt ngưỡng → `400 VALIDATION_ERROR`. Hình dạng `data` không đổi.
- **Việc client làm**: màn kéo-thả gọi `?all=true` để có đủ id; các màn phân trang
  khác giữ nguyên.

### 3.5 `POST /api/v1/admin/categories` — **Đổi** (`position` tuỳ chọn)

- **Trước**: `position` là số (mặc định `0` khi bỏ trống); bản ghi mới nằm giữa các
  bản ghi cùng vị trí.
- **Sau**: `position` **có thể bỏ trống**. Bỏ trống = bản ghi mới **lên đầu danh
  sách**. Gửi số cụ thể vẫn được tôn trọng như cũ. Số member của request/response
  **không đổi**.
- **Việc client làm**: bỏ ô nhập số thứ tự khỏi form tạo; không gửi `position` nếu
  muốn bản ghi mới lên đầu.

```json
{ "name": "Điêu khắc", "slug": "diau-khac", "description": "Tác phẩm điêu khắc" }
```

### 3.6 `POST /api/v1/admin/products` — **Đổi** (`position` tuỳ chọn)

- **Trước**: `position` của sản phẩm và của từng variant là số (mặc định `0`).
- **Sau**: cả hai **có thể bỏ trống**. Sản phẩm bỏ trống `position` → lên đầu
  catalogue. Variant bỏ trống `position` → theo **thứ tự trong mảng** `variants`
  (variant đầu tiên là mặc định). Gửi số cụ thể vẫn được tôn trọng. Số member
  request/response **không đổi**.
- **Việc client làm**: bỏ các ô nhập số thứ tự khỏi form tạo sản phẩm.

```json
{
  "name": "Acrylic stand Aki",
  "code": "aki-stand",
  "description": "",
  "defaultPrice": { "amount": 130000, "currency": "VND" },
  "categoryId": "<uuid>",
  "variants": [
    { "name": "Aki", "sku": "AKI-STAND-AKI", "price": { "amount": 130000 } },
    { "name": "Dottore", "sku": "AKI-STAND-DOT", "price": null }
  ]
}
```

### 3.7 `POST /api/v1/admin/products/{id}/variants` — **Đổi** (`position` tuỳ chọn)

- **Trước**: `position` là số (mặc định `0`); variant mới có thể nằm giữa.
- **Sau**: `position` **có thể bỏ trống**; bỏ trống = variant mới **lên đầu** (trở
  thành mặc định). Gửi số cụ thể vẫn được tôn trọng.
- **Việc client làm**: bỏ ô nhập số thứ tự khỏi form thêm variant.

## 4. Mã lỗi — thêm mới

| Code | HTTP | Khi nào | Thân từ chối |
|---|---|---|---|
| `CATEGORY_REORDER_DUPLICATE_ID` | 400 | `PUT /admin/categories/reorder` có id trùng | `error.details` nêu member `categoryIds`; không gì bị đổi |
| `PRODUCT_REORDER_DUPLICATE_ID` | 400 | `PUT /admin/products/reorder` có id trùng | `error.details` nêu member `productIds`; không gì bị đổi |
| `VARIANT_REORDER_DUPLICATE_ID` | 400 | `PUT .../variants/reorder` có id trùng | `error.details` nêu member `variantIds`; không gì bị đổi |
| `VARIANT_BELONGS_TO_OTHER_PRODUCT` | 400 | `PUT .../variants/reorder` có variant không thuộc sản phẩm trên path | `error.details` nêu member `variantIds`; không gì bị đổi |

Mảng **rỗng** hoặc **thiếu** một sibling dùng `VALIDATION_ERROR` 400 (không thêm
mã mới). Id **không tồn tại** dùng `CATEGORY_NOT_FOUND` / `PRODUCT_NOT_FOUND` 404.
Không có mã `PRODUCT_BELONGS_TO_OTHER_CATEGORY` (thứ tự sản phẩm là toàn catalogue).

## 5. Enum và shape client cần biết

- Response reorder: `data.updatedCount` (số nguyên) — số bản ghi đã ghi vị trí.
- `categoryIds` / `productIds` / `variantIds`: mảng UUID **đầy đủ**, không trùng,
  tối thiểu 1 phần tử.
- Đối tượng `AdminCategory` / `AdminProduct` / `AdminVariant` **không đổi hình
  dạng** (vẫn có member `position`).

## 6. HTTP status guide này dùng

- `200`: reorder thành công (3.1–3.3).
- `201`: tạo mới thành công (3.5, 3.6, 3.7).
- `400`: `VALIDATION_ERROR` (mảng rỗng/thiếu, giá trị sai hình dạng) hoặc một trong
  bốn mã mới ở mục 4.
- `401`: `UNAUTHENTICATED`. `403`: `FORBIDDEN`.
- `404`: `CATEGORY_NOT_FOUND` / `PRODUCT_NOT_FOUND`.
- `409`: va chạm cũ ở create (`CATEGORY_NAME_TAKEN`, `CATEGORY_SLUG_TAKEN`,
  `PRODUCT_CODE_TAKEN`, `VARIANT_SKU_TAKEN`) — không đổi ở feature này.
- `429`: `RATE_LIMITED` (hạn mức toàn cục).

## 7. Phụ lục đồng bộ (sync appendix)

Đồng bộ lần này đọc lại source/contract ngày 2026-10-18 và đếm theo quy tắc
frontend-guide sync: **mọi** route guide liệt kê, **mọi** mã lỗi, **mọi** member
DTO mà feature chạm, **mọi** status guide tuyên bố.

### 7.1 Route (8 route: 3 mới, 5 đổi, 0 gỡ)

- Mới: `PUT /admin/categories/reorder`, `PUT /admin/products/reorder`,
  `PUT /admin/products/{id}/variants/reorder` — nguồn `@Router` của 3
  handler mới trong `presentation/http/handler.go` (module category, product).
- Đổi: `GET /admin/categories`, `GET /admin/products` (thêm `all`);
  `POST /admin/categories`, `POST /admin/products`,
  `POST /admin/products/{id}/variants` (`position` tuỳ chọn).

### 7.2 Mã lỗi (4 mới, 0 gỡ, 0 đổi nghĩa)

`CATEGORY_REORDER_DUPLICATE_ID`, `PRODUCT_REORDER_DUPLICATE_ID`,
`VARIANT_REORDER_DUPLICATE_ID`, `VARIANT_BELONGS_TO_OTHER_PRODUCT` — mỗi mã 400,
khớp `contracts/error-codes.md`.

### 7.3 DTO (7 DTO: 4 mới, 3 đổi)

- Mới: `ReorderCategoriesRequest` (**1** member: `categoryIds`),
  `ReorderProductsRequest` (**1**: `productIds`), `ReorderVariantsRequest`
  (**1**: `variantIds`); `ReorderResponse` (**1**: `updatedCount`).
- Đổi: `CreateCategoryRequest` (4 member, `position` → tuỳ chọn),
  `CreateProductRequest` (7 member, `position` + variant `position` tuỳ chọn),
  `CreateVariantRequest` (4 member, `position` tuỳ chọn).

### 7.4 HTTP status (8 status)

`200`, `201`, `400`, `401`, `403`, `404`, `409`, `429` — đối chiếu với dòng “Lỗi:”
của các endpoint trong [`docs/api-reference.md`](../../docs/api-reference.md).

### 7.5 Mismatch đã tìm và sửa

1. Plan gốc của người dùng ghi `POST /admin/products/reorder` có `categoryId` và mã
   `PRODUCT_BELONGS_TO_OTHER_CATEGORY`; quyết định requirements là thứ tự sản phẩm
   **toàn catalogue**, nên guide bỏ `categoryId` và mã đó.
2. Plan gốc ghi kéo variant lên `position=0` đổi cả **giá** lẫn ảnh card; thực tế chỉ
   **thứ tự hiển thị + variant mặc định** đổi, **giá** là min giá variant đang bán —
   guide ghi đúng hành vi (xem Scenario B của quickstart).

### 7.6 Đồng bộ sau triển khai (2026-10-18)

Đợt sync cuối, đọc lại guide và **đối chiếu có đếm** với mã nguồn đã giao:

- **Route (8: 3 mới, 5 đổi)** — đếm 3 `@Router … [put]` mới
  (`/admin/categories/reorder`, `/admin/products/reorder`,
  `/admin/products/{id}/variants/reorder`) trong
  `internal/modules/{category,product}/presentation/http/handler.go`; cả ba dùng
  `{id}` (không còn `{productId}`) — khớp.
- **DTO (7: 4 mới, 3 đổi)** — đếm thẻ `json`: `categoryIds`, `productIds`,
  `variantIds`, `updatedCount` (mới) và `position` nay là `*int` trên 3 create body
  (`CreateCategoryRequest`, `CreateProductRequest`, `CreateVariantRequest`); mọi thẻ
  đều xuất hiện trong guide.
- **Mã lỗi (4 mới)** — `CATEGORY_REORDER_DUPLICATE_ID`,
  `PRODUCT_REORDER_DUPLICATE_ID`, `VARIANT_REORDER_DUPLICATE_ID`,
  `VARIANT_BELONGS_TO_OTHER_PRODUCT`; khớp `domain/constant/codes.go` cả hai module.
- **HTTP status (8)** — `200`, `201`, `400`, `401`, `403`, `404`, `409`, `429`; mỗi
  status là một status handler có thể trả (reorder `200`; create `201`; lỗi
  `400`/`401`/`403`/`404`; va chạm create `409`; hạn mức toàn cục `429`).

Không phát hiện mismatch mới; không đổi nội dung guide ngoài mục này.
