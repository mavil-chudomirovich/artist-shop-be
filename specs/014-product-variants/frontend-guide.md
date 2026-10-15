# Frontend Integration Guide — Product Variants (014)

Tài liệu bàn giao cho client về feature **variant group một chiều** (module 04 Product). Nó liệt kê
**mọi endpoint bị ảnh hưởng** với method + path, đánh dấu **mới / đổi / không đổi**, và với mỗi
endpoint **đổi** thì nêu **trước → sau**. Nguồn authoritative vẫn là
[`docs/api-reference.md`](../../docs/api-reference.md) mục 6; tài liệu này là điểm vào nhanh cho
frontend.

## 1. Tóm tắt một câu

Một sản phẩm có thể được chào dưới nhiều **lựa chọn** ("Card Flins", "Card Dottore") gom trong một
**variant group**. Khách thấy nhóm như **một** mục, mở ra chọn một lựa chọn, và mua **đúng lựa chọn
đó** — mỗi lựa chọn vẫn là một sản phẩm bình thường với giá, tồn, trạng thái bán, ảnh và link riêng.
Vì vậy **giỏ hàng, đơn hàng, kho không đổi**: khách vẫn thêm một `productId`, chỉ là id ấy là của
một lựa chọn.

## 2. Bảng endpoint bị ảnh hưởng

| Method | Path | Trạng thái | Ghi chú |
|---|---|---|---|
| GET | `/api/v1/products` | **Đổi** | Danh sách gộp; mỗi mục có thêm `type` |
| GET | `/api/v1/products/{slug}` | **Đổi** | Thêm bộ chọn `variantGroup` khi là lựa chọn |
| GET | `/api/v1/product-groups/{slug}` | **Mới** | Chi tiết nhóm (công khai) |
| POST | `/api/v1/admin/products` | **Đổi** | Body thêm `variantGroupId` / `variantLabel` |
| PATCH | `/api/v1/admin/products/{id}` | **Đổi** | Body thêm `variantGroupId` / `variantLabel` (ba trạng thái) |
| GET | `/api/v1/admin/product-groups` | **Mới** | Danh sách nhóm (ADMIN) |
| POST | `/api/v1/admin/product-groups` | **Mới** | Tạo nhóm (ADMIN) |
| GET | `/api/v1/admin/product-groups/{id}` | **Mới** | Chi tiết nhóm, gồm lựa chọn ẩn (ADMIN) |
| PATCH | `/api/v1/admin/product-groups/{id}` | **Mới** | Sửa một phần nhóm (ADMIN) |
| DELETE | `/api/v1/admin/product-groups/{id}` | **Mới** | Xoá nhóm (ADMIN); `409` nếu còn lựa chọn |
| POST | `/api/v1/admin/product-groups/{id}/images` | **Mới** | Gắn ảnh nhóm (ADMIN) |
| DELETE | `/api/v1/admin/product-groups/{id}/images/{imageId}` | **Mới** | Gỡ ảnh nhóm (ADMIN) |
| POST | `/api/v1/admin/product-groups/{id}/images/{imageId}/primary` | **Mới** | Đặt ảnh chính nhóm (ADMIN) |
| POST | `/api/v1/cart/items` | Không đổi | Thêm **một lựa chọn** bằng chính `productId` của nó |
| GET | `/api/v1/cart` | Không đổi | Mỗi lựa chọn là một dòng riêng |

Các endpoint còn lại của module 04 (admin list/get/delete sản phẩm, đổi trạng thái, ảnh sản phẩm)
**không đổi** và không liệt kê ở đây.

## 3. Chi tiết từng endpoint

### 3.1 `GET /api/v1/products` — **Đổi**

Danh sách catalogue công khai. Giờ đây là danh sách **gộp**: sản phẩm độc lập **và** nhóm.

- **Trước**: `data` là mảng `PublicProduct` (6 member: `id`, `name`, `slug`, `price`, `imageUrl`,
  `isPreorder`). Mọi sản phẩm khách thấy được đều là một phần tử.
- **Sau**: `data` là mảng `PublicCatalogueEntry` (**7 member**, thêm `type`). `type` là
  `"product"` hoặc `"variant-group"`:
  - `"product"` → mở `/api/v1/products/{slug}`.
  - `"variant-group"` → mở `/api/v1/product-groups/{slug}`; `price` là giá **thấp nhất** trong các
    lựa chọn đang hiển thị; `imageUrl` là ảnh chính của nhóm; `isPreorder` luôn `false`.
- **Quan trọng**: một sản phẩm **đã thuộc một nhóm** **không** còn xuất hiện như một mục riêng —
  nó chỉ hiện gián tiếp qua nhóm. Nhóm chỉ hiện khi còn ít nhất một lựa chọn hiển thị.
- **Việc client phải làm**: đọc `type` và rẽ nhánh route. Không được giả định mọi mục đều có slug
  của sản phẩm.

Ví dụ một mục nhóm:

```json
{
  "type": "variant-group",
  "id": "1a2b3c4d-5e6f-4a1b-9c2d-3e4f5a6b7c8d",
  "name": "Card Genshin",
  "slug": "card-genshin",
  "price": { "amount": 50000, "currency": "VND" },
  "imageUrl": "https://res.cloudinary.com/demo/image/upload/banner.jpg",
  "isPreorder": false
}
```

Status: `200`, `400` (`page`/`pageSize`), `429`.

### 3.2 `GET /api/v1/products/{slug}` — **Đổi**

Chi tiết một sản phẩm theo slug.

- **Trước**: `data` = `PublicProductDetail` (8 member = `PublicProduct` + `description` + `images`).
- **Sau**: không đổi với sản phẩm độc lập. Khi sản phẩm là một **lựa chọn**, `data` mang thêm
  `variantGroup` (**9 member** khi có): nội dung chung của nhóm (`id`, `slug`, `name`,
  `optionName`, `images`) và danh sách `options` (các lựa chọn đang hiển thị, mỗi lựa chọn là
  `{ id, slug, label, price, imageUrl, isPreorder }`).
- **Việc client phải làm**: nếu `variantGroup` có mặt, hiển thị bộ chọn lựa chọn; đổi lựa chọn thì
  đổi ảnh đang hiển thị sang ảnh **của lựa chọn đó** (`option.imageUrl`), và nút "mua" thêm đúng
  `option.id` vào giỏ. Link trỏ thẳng vào một lựa chọn vẫn mở bình thường.

Status: `200`, `404` (`PRODUCT_NOT_FOUND` — kể cả sản phẩm ẩn), `429`.

### 3.3 `GET /api/v1/product-groups/{slug}` — **Mới**

Chi tiết một nhóm theo slug (công khai).

`data` = `PublicVariantGroupDetail` (**7 member**: `id`, `name`, `slug`, `description`,
`optionName`, `images`, `options`).

- `optionName`: tên **đúng một** chiều chọn, ví dụ `"Card"` — dùng làm nhãn cho bộ chọn.
- `images`: ảnh chung (banner) của nhóm, mỗi phần tử `{ id, url, width, height }`.
- `options`: các lựa chọn đang hiển thị, mỗi phần tử `{ id, slug, label, price, imageUrl,
  isPreorder }`. `label` là giá trị khách chọn (`"Flins"`), không phải tên đầy đủ của sản phẩm.

```json
{
  "id": "1a2b3c4d-5e6f-4a1b-9c2d-3e4f5a6b7c8d",
  "name": "Card Genshin",
  "slug": "card-genshin",
  "description": "Bộ card nhân vật",
  "optionName": "Card",
  "images": [ { "id": "aa11", "url": "https://.../banner.jpg", "width": 1200, "height": 1600 } ],
  "options": [
    { "id": "bb22", "slug": "card-flins", "label": "Flins", "price": { "amount": 50000, "currency": "VND" }, "imageUrl": "https://.../flins.jpg", "isPreorder": false }
  ]
}
```

Status: `200`, `404` (`VARIANT_GROUP_NOT_FOUND` — nhóm ẩn, rỗng, danh mục ẩn, đã xoá, hoặc slug lạ
đều trả cùng một câu trả lời), `429`.

### 3.4 `POST /api/v1/admin/products` — **Đổi** (ADMIN)

- **Trước**: body 10 member (`name`, `slug`, `description`, `price`, `categoryId`, `position`,
  `isSet`, `memberProductIds`, `isPreorder`, `preorderExpectedAt`).
- **Sau**: thêm **hai member tuỳ chọn** `variantGroupId` (uuid) và `variantLabel` (string). Gửi
  **cả hai** để tạo sản phẩm và gắn nó làm lựa chọn của nhóm trong cùng thao tác; bỏ qua hoặc gửi
  cả hai `null` để tạo sản phẩm độc lập. Gửi lệch (chỉ một trong hai) trả `400`.

Status: `201`, `400`, `401`, `403`, `404` (`VARIANT_GROUP_NOT_FOUND` khi `variantGroupId` không tồn
tại), `409` (`PRODUCT_SLUG_TAKEN`, `VARIANT_LABEL_TAKEN`), `429`.

Response vẫn là `AdminProductDetail` như trước — gồm `images` và (với combo set) `members` — không
đổi ngoài việc sản phẩm có thể mang thêm tình trạng gắn nhóm.

### 3.5 `PATCH /api/v1/admin/products/{id}` — **Đổi** (ADMIN)

- **Trước**: sửa một phần; member bỏ trống giữ nguyên.
- **Sau**: thêm `variantGroupId` / `variantLabel` với **ba trạng thái**:
  - **Bỏ qua** cả hai → giữ nguyên tình trạng gắn nhóm hiện tại.
  - Gửi **cả hai `null`** → gỡ sản phẩm khỏi nhóm (trở thành sản phẩm độc lập).
  - Gửi **cả hai có giá trị** → gắn sản phẩm vào nhóm với nhãn đó.
  - Gắn/gỡ **không** đụng giá, ảnh, trạng thái bán hay slug.
- Một sản phẩm đã thuộc nhóm **không** gắn thẳng sang nhóm khác được — phải gỡ trước, nếu không trả
  `409 PRODUCT_ALREADY_IN_VARIANT_GROUP`.

Status: `200`, `400`, `401`, `403`, `404` (`PRODUCT_NOT_FOUND`; hoặc `VARIANT_GROUP_NOT_FOUND` khi
`variantGroupId` không tồn tại), `409` (`PRODUCT_SLUG_TAKEN`, `VARIANT_LABEL_TAKEN`,
`PRODUCT_ALREADY_IN_VARIANT_GROUP`), `429`.

### 3.6 `GET /api/v1/admin/product-groups` — **Mới** (ADMIN)

`data` là mảng `AdminVariantGroup` (**11 member**: `id`, `name`, `slug`, `description`,
`categoryId`, `position`, `optionName`, `imageCount`, `imageUrl`, `createdAt`, `updatedAt`); `meta`
phân trang `page`/`pageSize`.

Status: `200`, `400`, `401`, `403`, `429`.

### 3.7 `POST /api/v1/admin/product-groups` — **Mới** (ADMIN)

Body `CreateVariantGroupRequest` (**6 member**, bắt buộc cả sáu): `name`, `slug`, `description`,
`categoryId`, `position`, `optionName`. `slug` do operator viết (không sinh từ `name`);
`optionName` là **đúng một** tên chiều chọn.

`data` = `AdminVariantGroupDetail` (**13 member** = `AdminVariantGroup` + `images` + `options`).
Nhóm mới chưa có lựa chọn và chưa có ảnh — đó là trạng thái hợp lệ.

Status: `201`, `400`, `401`, `403`, `409` (`VARIANT_GROUP_SLUG_TAKEN`), `429`.

### 3.8 `GET /api/v1/admin/product-groups/{id}` — **Mới** (ADMIN)

Đọc nhóm theo định danh, kể cả nhóm ẩn, với **toàn bộ** lựa chọn (kể cả lựa chọn khách không thấy).

`data` = `AdminVariantGroupDetail`:

- `images`: mảng `AdminGroupImage` (**7 member**: `id`, `publicId`, `url`, `width`, `height`,
  `position`, `isPrimary`).
- `options`: mảng `AdminGroupOption` (**9 member**: `id`, `name`, `slug`, `label`, `price`,
  `imageUrl`, `position`, `sellState`, `isPreorder`). `sellState` là một trong **bốn** giá trị
  `COMING_SOON`, `ACTIVE`, `OUT_OF_STOCK`, `DISCONTINUED` (giống trạng thái bán của sản phẩm).

Status: `200`, `400` (`id` không phải UUID), `401`, `403`, `404` (`VARIANT_GROUP_NOT_FOUND`), `429`.

### 3.9 `PATCH /api/v1/admin/product-groups/{id}` — **Mới** (ADMIN)

Body `UpdateVariantGroupRequest` (mọi member tuỳ chọn: `name`, `slug`, `description`, `categoryId`,
`position`, `optionName`). Ảnh sửa qua các endpoint riêng.

Status: `200`, `400`, `401`, `403`, `404` (`VARIANT_GROUP_NOT_FOUND`), `409`
(`VARIANT_GROUP_SLUG_TAKEN`), `429`.

### 3.10 `DELETE /api/v1/admin/product-groups/{id}` — **Mới** (ADMIN)

**Bị từ chối khi nhóm còn lựa chọn** — operator phải gỡ lựa chọn trước. Các sản phẩm từng là lựa
chọn **không** bị xoá; chúng trở lại là sản phẩm độc lập.

Status: `204`, `400`, `401`, `403`, `404` (`VARIANT_GROUP_NOT_FOUND`), `409`
(`VARIANT_GROUP_HAS_OPTIONS`), `429`.

### 3.11 `POST /api/v1/admin/product-groups/{id}/images` — **Mới** (ADMIN)

`multipart/form-data`, đúng một part tên `image`. Cùng luật media như ảnh sản phẩm: JPEG/PNG/WebP,
trần **2 MB**, tối đa **10** ảnh, ảnh đầu tiên là ảnh chính.

Status: `201`, `400` (`VALIDATION_ERROR` thiếu part; `PRODUCT_IMAGE_TYPE_UNSUPPORTED`),
`401`, `403`, `404` (`VARIANT_GROUP_NOT_FOUND`), `409` (`PRODUCT_IMAGE_LIMIT_REACHED`),
`413` (`PRODUCT_IMAGE_TOO_LARGE`), `429`, `503` (`PRODUCT_MEDIA_UNAVAILABLE`).

### 3.12 `DELETE /api/v1/admin/product-groups/{id}/images/{imageId}` — **Mới** (ADMIN)

Gỡ một ảnh khỏi gallery nhóm; gỡ ảnh chính thì ảnh kế theo `position` được thăng cấp.

Status: `204`, `400` (`id`/`imageId` không phải UUID), `401`, `403`, `404`
(`VARIANT_GROUP_NOT_FOUND`), `429`.

### 3.13 `POST /api/v1/admin/product-groups/{id}/images/{imageId}/primary` — **Mới** (ADMIN)

Đặt một ảnh làm ảnh chính của nhóm.

Status: `200`, `400`, `401`, `403`, `404` (`VARIANT_GROUP_NOT_FOUND`), `429`.

### 3.14 `POST /api/v1/cart/items` — **Không đổi**

Không có gì thay đổi ở hợp đồng. Điểm cần nhớ: một **lựa chọn được thêm vào giỏ bằng chính
`productId` của nó**, y như một sản phẩm thường, ở đúng giá của nó. Hai lựa chọn cùng nhóm là hai
dòng riêng. Nếu lựa chọn không đang bán, endpoint trả `409 CART_PRODUCT_NOT_PURCHASABLE` như mọi
sản phẩm khác.

## 4. Mã lỗi mới

| Code | HTTP | Khi nào |
|---|---|---|
| `VARIANT_GROUP_NOT_FOUND` | 404 | Nhóm không tồn tại/ẩn/rỗng/danh mục ẩn/đã xoá; hoặc `variantGroupId` trỏ tới nhóm không tồn tại khi gắn |
| `VARIANT_GROUP_SLUG_TAKEN` | 409 | Slug đã có nhóm khác dùng |
| `VARIANT_GROUP_HAS_OPTIONS` | 409 | Xoá nhóm còn lựa chọn |
| `VARIANT_LABEL_TAKEN` | 409 | Nhãn đã có lựa chọn khác trong cùng nhóm dùng |
| `PRODUCT_ALREADY_IN_VARIANT_GROUP` | 409 | Gắn sản phẩm đã thuộc nhóm khác |

Mã media của nhóm **tái dùng** các mã `PRODUCT_IMAGE_*` / `PRODUCT_MEDIA_UNAVAILABLE` sẵn có. Nhãn
lựa chọn rỗng khi đang gắn nhóm, tên/slug/mô tả nhóm vượt độ dài, slug nhóm không URL-safe,
`categoryId` nhóm không tồn tại, và một sản phẩm vừa là combo set vừa là lựa chọn → `400
VALIDATION_ERROR` với `error.details[].field`; gắn/gỡ nhóm trên một sản phẩm **không tồn tại** →
`404 PRODUCT_NOT_FOUND`.

## 5. Ghi chú tích hợp

- **`type` là bắt buộc để phân nhánh route.** Mọi mục trong `GET /products` đều có nó; client cũ
  đọc cứng `slug` như một sản phẩm sẽ gửi người dùng tới sai route với mục nhóm.
- **Nhóm không mua được trực tiếp.** Không có nút "thêm nhóm vào giỏ"; chỉ lựa chọn mới mua được.
- **Nhóm và sản phẩm là hai không gian tên slug riêng**: cùng một slug có thể có ở cả hai. Đừng giả
  định slug là duy nhất toàn hệ.
- **Ba trạng thái gắn nhóm ở `PATCH`**: phân biệt "bỏ qua" với "gửi `null`". Gửi `null` là **gỡ**,
  không phải giữ nguyên.
- **Không hiển thị khả dụng theo từng lựa chọn trên trang nhóm** trong feature này: khách biết một
  lựa chọn không mua được khi thêm vào giỏ. Đây là chủ ý, ghi ở
  [`deferred.md`](./deferred.md) (D-01).

## 6. Đồng bộ với nguồn (T043)

Guide được đối chiếu trực tiếp với nguồn của feature 014:

- **Trường JSON**: **33** tên trường `json:"..."` duy nhất trên toàn bộ DTO ở
  [`dto.go`](../../internal/modules/product/presentation/dto/dto.go) — mỗi trường đều xuất hiện
  trong guide (33/33). Riêng `members` chỉ xuất hiện ở dạng ghi chú "giữ nguyên" (thuộc
  `AdminProductDetail`, không đổi bởi feature).
- **Enum**: `type` (`"product"`, `"variant-group"`) đối chiếu hằng số
  `CatalogueEntryTypeProduct`/`CatalogueEntryTypeVariantGroup` — khớp **2↔2**; `sellState`
  (`COMING_SOON`, `ACTIVE`, `OUT_OF_STOCK`, `DISCONTINUED`) đối chiếu khối hằng
  `SellState*` — khớp **4↔4**.
- **Route**: **15** route trong bảng §2 (13 của module 04 + 2 của module 06 cart). Nguồn dùng
  `chi` với cú pháp `{param}` sẵn (không phải `:param` của gin), nên không cần đổi dạng.
- **HTTP status**: **11** mã khác nhau được guide nêu — `200`, `201`, `204`, `400`, `401`, `403`,
  `404`, `409`, `413`, `429`, `503` — mỗi mã đối chiếu annotation của handler tương ứng trong
  [`handler.go`](../../internal/modules/product/presentation/http/handler.go) và bảng mã ở
  [`errors.go`](../../internal/modules/product/presentation/http/errors.go). `429` là hạn mức toàn
  cục (mọi route), không phải annotation riêng.
- **Không đối chiếu được**: không có phần nào phải bỏ qua.

