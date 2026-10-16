# Frontend Integration Guide — Product SKU Model (015)

Tài liệu bàn giao cho client về việc tái cấu trúc **sản phẩm cha + variant (SKU)** (modules 04–07).
Nó liệt kê **mọi endpoint bị ảnh hưởng** với method + path, đánh dấu **mới / đổi / gỡ**, và với mỗi
endpoint **đổi** thì nêu **trước → sau**. Nguồn authoritative vẫn là
[`docs/api-reference.md`](../../docs/api-reference.md); tài liệu này là điểm vào nhanh cho frontend.

> **Thay thế:** guide này thay guide của **feature 014**
> ([`specs/014-product-variants/frontend-guide.md`](../014-product-variants/frontend-guide.md)). Mọi
> thứ về `/product-groups*` và bộ chọn `variantGroup` trong guide cũ **không còn hiệu lực**.

## 1. Tóm tắt một câu

Một sản phẩm là **cha** giữ nội dung dùng chung (tên, mã sản phẩm, giá mặc định, mô tả, slug, gallery)
và có một mảng **variant** con; mỗi variant có **tên, SKU, giá riêng (tuỳ chọn), ảnh riêng, thứ tự và
trạng thái bán riêng**. **Variant là đơn vị mua**: khách chọn một variant, giỏ và đơn khoá theo
`variantId`, tồn kho theo variant. Catalogue hiện **một mục mỗi sản phẩm**.

## 2. Bảng endpoint bị ảnh hưởng

### Module 04 — Product

| Method | Path | Trạng thái | Ghi chú |
|---|---|---|---|
| GET | `/api/v1/products` | **Đổi** | Một mục mỗi sản phẩm; giá thấp nhất trong variant đang bán; ảnh variant mặc định; **bỏ** `type` |
| GET | `/api/v1/products/{slug}` | **Đổi** | Thêm `variants[]` (mỗi variant có `sellState`/`inStock`); **bỏ** bộ chọn `variantGroup` |
| GET | `/api/v1/admin/products` | **Đổi** | Hình dạng `AdminProduct` mới (`code`, `defaultPrice`, `variantCount`) |
| POST | `/api/v1/admin/products` | **Đổi** | Body **bắt buộc** có `code` và `variants` (≥1) |
| GET | `/api/v1/admin/products/{id}` | **Đổi** | Trả kèm `variants[]` (đủ `sku`, `effectivePrice`, `sellState`) |
| PATCH | `/api/v1/admin/products/{id}` | **Đổi** | Sửa `name`/`code`/`slug`/`description`/`defaultPrice`/`categoryId`/`position`; **không** đụng variant |
| DELETE | `/api/v1/admin/products/{id}` | **Đổi** | Xoá theo cả các variant (204) |
| POST | `/api/v1/admin/products/{id}/variants` | **Mới** | Thêm variant (201) |
| PATCH | `/api/v1/admin/products/{id}/variants/{variantId}` | **Mới** | Sửa variant; `price` ba trạng thái |
| DELETE | `/api/v1/admin/products/{id}/variants/{variantId}` | **Mới** | Gỡ variant; `409 VARIANT_LAST_REMOVAL` nếu là cái cuối |
| POST | `/api/v1/admin/products/{id}/variants/{variantId}/image` | **Mới** | Tải ảnh variant (multipart) (201) |
| DELETE | `/api/v1/admin/products/{id}/variants/{variantId}/image` | **Mới** | Gỡ ảnh variant (204) |
| POST | `/api/v1/admin/products/{id}/variants/{variantId}/state` | **Mới** | Chuyển trạng thái bán của variant |
| POST | `/api/v1/admin/products/{id}/images` | Không đổi | Gallery sản phẩm (route ảnh cũ giữ nguyên) |
| DELETE | `/api/v1/admin/products/{id}/images/{imageId}` | Không đổi | |
| POST | `/api/v1/admin/products/{id}/images/{imageId}/primary` | Không đổi | |
| POST | `/api/v1/admin/products/{id}/state` | **GỠ** | Sản phẩm không còn trạng thái bán; thay bằng state của variant |

### Module 05 — Inventory (admin)

| Method | Path | Trạng thái | Ghi chú |
|---|---|---|---|
| GET | `/api/v1/admin/inventory/{variantId}` | **Đổi** | Khoá theo `variantId` (trước là `{productId}`) |
| GET | `/api/v1/admin/inventory/{variantId}/movements` | **Đổi** | Khoá theo `variantId`; mỗi `StockMovement` có `variantId` (trước là `productId`) |
| POST | `/api/v1/admin/inventory/{variantId}/restock` | **Đổi** | Khoá theo `variantId` |
| POST | `/api/v1/admin/inventory/{variantId}/damage` | **Đổi** | Khoá theo `variantId` |
| POST | `/api/v1/admin/inventory/{variantId}/adjustment` | **Đổi** | Khoá theo `variantId` |

### Module 06 — Cart (khách)

| Method | Path | Trạng thái | Ghi chú |
|---|---|---|---|
| GET | `/api/v1/cart` | **Đổi** | Mỗi dòng có `productName`/`variantName` (trước là `name`/`slug`) |
| POST | `/api/v1/cart/items` | **Đổi** | Body nhận `variantId` (trước là `productId`) |
| PATCH | `/api/v1/cart/items/{variantId}` | **Đổi** | Khoá theo `variantId` (trước là `{productId}`) |
| DELETE | `/api/v1/cart/items/{variantId}` | **Đổi** | Khoá theo `variantId` (trước là `{productId}`) |

### Module 07 — Order (khách + admin)

| Method | Path | Trạng thái | Ghi chú |
|---|---|---|---|
| POST | `/api/v1/orders` | **Đổi** | Dòng đơn chụp `productName` + `variantName`; mỗi cặp (mã sản phẩm, SKU) một dòng |
| GET | `/api/v1/orders` | **Đổi** | Danh sách không đổi hình dạng, nhưng mỗi dòng chi tiết mang `variantName` |
| GET | `/api/v1/orders/{orderId}` | **Đổi** | Dòng đơn mang `variantName`; **bỏ** `name`/`slug` |
| PUT | `/api/v1/orders/{orderId}` | **Đổi** | `lines[].variantId` (trước là `productId`) |
| POST | `/api/v1/orders/{orderId}/cancel` | Không đổi | |
| GET | `/api/v1/admin/orders` | **Đổi** | Dòng đơn chi tiết mang `variantName` |
| GET | `/api/v1/admin/orders/{orderId}` | **Đổi** | Dòng đơn mang `variantName` |
| POST | `/api/v1/admin/orders/{orderId}/{confirm,reject,ship,complete,transfer}` | Không đổi | Hình dạng dòng theo view khách |
| POST/GET | `/api/v1/orders/{orderId}/payment` | Không đổi | Payment khoá theo đơn |

### Gỡ bỏ (feature 014)

| Method | Path | Trạng thái |
|---|---|---|
| GET | `/api/v1/product-groups/{slug}` | **GỠ** |
| GET | `/api/v1/admin/product-groups` | **GỠ** |
| POST | `/api/v1/admin/product-groups` | **GỠ** |
| GET | `/api/v1/admin/product-groups/{id}` | **GỠ** |
| PATCH | `/api/v1/admin/product-groups/{id}` | **GỠ** |
| DELETE | `/api/v1/admin/product-groups/{id}` | **GỠ** |
| POST | `/api/v1/admin/product-groups/{id}/images` | **GỠ** |
| DELETE | `/api/v1/admin/product-groups/{id}/images/{imageId}` | **GỠ** |
| POST | `/api/v1/admin/product-groups/{id}/images/{imageId}/primary` | **GỠ** |

## 3. Chi tiết từng endpoint

### 3.1 `GET /api/v1/products` — **Đổi**

Catalogue công khai. Giờ là **một mục mỗi sản phẩm** (không còn danh sách gộp).

- **Trước**: `data` là mảng `PublicCatalogueEntry` (7 member: `type`, `id`, `name`, `slug`, `price`,
  `imageUrl`, `isPreorder`). `type` là `"product"` hoặc `"variant-group"`.
- **Sau**: `data` là mảng `PublicProduct` (**5 member**: `id`, `name`, `slug`, `price`, `imageUrl`).
  **Không còn** `type` và `isPreorder`.
  - `price` là giá **thấp nhất** trong các variant **đang bán** (một variant hết hàng rẻ hơn **không**
    quyết định giá card).
  - `imageUrl` là ảnh của **variant mặc định** (variant `position` nhỏ nhất), fallback ảnh chính của
    gallery sản phẩm, hoặc `null`.
  - Sản phẩm chỉ xuất hiện khi **danh mục hiển thị** **và** còn **ít nhất một variant đang bán**.
- **Việc client phải làm**: bỏ nhánh theo `type`; chỉ mở `/api/v1/products/{slug}`. Một sản phẩm hết
  hàng hoàn toàn (mọi variant `OUT_OF_STOCK`/`COMING_SOON`/`DISCONTINUED`) biến mất khỏi danh sách.

### 3.2 `GET /api/v1/products/{slug}` — **Đổi**

Chi tiết sản phẩm.

- **Trước**: `data` là `PublicProductDetail` (8 member `PublicProduct` + `description` + `images`, hoặc
  9 member khi sản phẩm là một lựa chọn, có thêm `variantGroup`).
- **Sau**: `data` là `PublicProductDetail` (**8 member**: `id`, `name`, `slug`, `price`, `imageUrl`,
  `description`, `images`, `variants`). **Bỏ** `variantGroup`; **thêm** `variants`.
  - Mỗi phần tử `variants` là `PublicVariant` (**6 member**: `id`, `name`, `price`, `imageUrl`,
    `sellState`, `inStock`). `price` là **giá hiệu lực** (giá riêng của variant, hoặc giá mặc định của
    sản phẩm khi variant không có giá).
  - `sellState` nhận `COMING_SOON` / `ACTIVE` / `OUT_OF_STOCK` / `DISCONTINUED`; `inStock` là boolean.
  - Chỉ trả variant **`ACTIVE` hoặc `OUT_OF_STOCK`**; variant `COMING_SOON`/`DISCONTINUED` bị ẩn. **SKU
    không** được trả cho khách.
  - `price`/`imageUrl` ở cấp sản phẩm là giá thấp nhất trong variant đang bán và ảnh variant mặc định.
- **Việc client phải làm**: render danh sách variant; dùng `variant.id` (không phải SKU) khi thêm vào
  giỏ; hiển thị "hết hàng" khi `inStock: false`.

### 3.3 `POST /api/v1/admin/products` — **Đổi**

- **Trước**: body `CreateProductRequest` có `name`, `slug`, `description`, `price`, `categoryId`,
  `position`, và các cờ `isSet`/`memberProductIds`/`isPreorder`/`preorderExpectedAt`/
  `variantGroupId`/`variantLabel`.
- **Sau**: body có `name`, **`code`**, `slug`, `description`, **`defaultPrice`**, `categoryId`,
  `position`, **`variants`**.
  - `code` **bắt buộc**, duy nhất toàn bộ sản phẩm (trùng → `409 PRODUCT_CODE_TAKEN`).
  - `defaultPrice` là **giá mặc định** (`{ amount, currency }`).
  - `variants` **bắt buộc**, **≥1** phần tử `{ name, sku, price?, position }`; `price` tuỳ chọn
    (`{ "amount": <int> }` hoặc `null`/bỏ trống = dùng giá mặc định; **không** nhận `currency`).
    `sku` duy nhất toàn hệ (trùng → `409 VARIANT_SKU_TAKEN`). Variant mới ở `COMING_SOON`.
  - **Bỏ** `isSet`, `memberProductIds`, `isPreorder`, `preorderExpectedAt`, `variantGroupId`,
    `variantLabel`.
- **Trả về `201`**: `data` = `AdminProductDetail` (14 member = `AdminProduct` 12 member + `images` +
  `variants`).

### 3.4 `GET /api/v1/admin/products/{id}` — **Đổi**

- **Trước**: `data` = `AdminProductDetail` 17 member (`AdminProduct` 15 member + `images` + `members`).
- **Sau**: `data` = `AdminProductDetail` **14 member** = `AdminProduct` **12 member** (`id`, `name`,
  `code`, `slug`, `description`, `defaultPrice`, `categoryId`, `position`, `variantCount`, `imageUrl`,
  `createdAt`, `updatedAt`) + `images` (7 member: `id`, `publicId`, `url`, `width`, `height`, `position`,
  `isPrimary`) + `variants` (mỗi phần tử `AdminVariant` **8 member**: `id`, `name`, `sku`, `price`,
  `effectivePrice`, `imageUrl`, `position`, `sellState`). **Bỏ** `members`, `sellState` cấp sản phẩm,
  `isSet`, `isPreorder`, `preorderExpectedAt`, `imageCount`. `price` của variant là `null` khi dùng giá
  mặc định; `effectivePrice` là giá thực thu.

### 3.5 `PATCH /api/v1/admin/products/{id}` — **Đổi**

- **Trước**: nhận thêm `isSet`/`memberProductIds`/`isPreorder`/`preorderExpectedAt` và
  `variantGroupId`/`variantLabel` (ba trạng thái).
- **Sau**: chỉ nhận `name`, `code`, `slug`, `description`, `defaultPrice`, `categoryId`, `position`.
  **Không** đụng variant; **không** đổi trạng thái bán (nằm ở variant). Variant sửa qua các route
  riêng 3.7–3.11.

### 3.6 `DELETE /api/v1/admin/products/{id}` — **Đổi**

- **Sau**: xoá sản phẩm **và các variant của nó** (204). Các sản phẩm khác không bị đụng.

### 3.7 `POST /api/v1/admin/products/{id}/variants` — **Mới**

- Body `{ name, sku, price?, position }` (như một phần tử của `variants` ở 3.3). Trả `201` với
  `AdminProductDetail`. Variant mới ở `COMING_SOON`. Lỗi: `404 PRODUCT_NOT_FOUND`,
  `409 VARIANT_SKU_TAKEN`, `400 VALIDATION_ERROR`.

### 3.8 `PATCH /api/v1/admin/products/{id}/variants/{variantId}` — **Mới**

- Body `{ name?, sku?, price?, position? }`. `price` là **ba trạng thái theo khoá JSON**: bỏ qua = giữ
  nguyên amount hiện tại; `null` = **xoá** (quay về giá mặc định); `{ "amount": <int> }` = đặt amount
  riêng. Trả `200` với `AdminProductDetail`. Lỗi: `404 VARIANT_NOT_FOUND`, `409 VARIANT_SKU_TAKEN`.

### 3.9 `DELETE /api/v1/admin/products/{id}/variants/{variantId}` — **Mới**

- Trả `204`. **Bị từ chối khi là variant cuối** → `409 VARIANT_LAST_REMOVAL`. Lỗi:
  `404 VARIANT_NOT_FOUND`.

### 3.10 `POST /api/v1/admin/products/{id}/variants/{variantId}/image` — **Mới**

- `multipart/form-data`, part `image` (JPEG/PNG/WebP, trần 2 MB, **thay** ảnh cũ). Trả `201` với
  `AdminProductDetail`. Lỗi: `404 VARIANT_NOT_FOUND`, `413 PRODUCT_IMAGE_TOO_LARGE`,
  `400 PRODUCT_IMAGE_TYPE_UNSUPPORTED`, `503 PRODUCT_MEDIA_UNAVAILABLE`.

### 3.11 `DELETE /api/v1/admin/products/{id}/variants/{variantId}/image` — **Mới**

- Trả `204`. Lỗi: `404 VARIANT_NOT_FOUND`.

### 3.12 `POST /api/v1/admin/products/{id}/variants/{variantId}/state` — **Mới**

- Body `{ "to": "COMING_SOON" | "ACTIVE" | "OUT_OF_STOCK" | "DISCONTINUED" }`. Trả `200` với
  `AdminProductDetail`. Chuyển không hợp lệ → `409 VARIANT_STATE_TRANSITION_INVALID` (message nêu trạng
  thái hiện tại). **Thay** `POST /api/v1/admin/products/{id}/state` (đã gỡ).

### 3.13 Inventory: `/api/v1/admin/inventory/{variantId}*` — **Đổi**

- **Trước**: path khoá theo `{productId}`; `StockMovement` có `productId`.
- **Sau**: path khoá theo **`{variantId}`**; `StockMovement` (9 member) có **`variantId`** thay
  `productId`. `StockView` không đổi (3 member: `physicalQuantity`, `heldQuantity`,
  `availableQuantity`). Variant lạ → `404 VARIANT_NOT_FOUND` (trước là `PRODUCT_NOT_FOUND`).

### 3.14 Cart: `GET /api/v1/cart` + `POST/PATCH/DELETE /api/v1/cart/items[/{variantId}]` — **Đổi**

- **Trước**: `AddItemRequest` = `{ productId, quantity }`; `CartLineResponse` (8 member: `productId`,
  `name`, `slug`, `quantity`, `unitPrice`, `lineTotal`, `buyable`, `availableQuantity`); path
  `{productId}`.
- **Sau**:
  - `POST /cart/items` body = `{ "variantId": <uuid>, "quantity": <int ≥ 1> }`; **thêm sản phẩm không
    kèm variant** bị từ chối `400 VALIDATION_ERROR` (`details[].field = "variantId"`).
  - `PATCH`/`DELETE /cart/items/{variantId}` khoá theo **`variantId`**.
  - `CartLineResponse` (**9 member**: `productId`, **`productName`**, **`variantId`**,
    **`variantName`**, `quantity`, `unitPrice`, `lineTotal`, `buyable`, `availableQuantity`). Giữ
    `productId`; **thêm** `variantId`/`variantName` (tên variant) và đổi `name`/`slug` thành
    **`productName`**.
  - `409 CART_PRODUCT_NOT_PURCHASABLE` đổi `details[].field` thành `"variantId"`.
  - Một giỏ có thể giữ **hai variant của cùng một sản phẩm** (hai dòng).

### 3.15 Order: `POST /orders`, `GET /orders`, `GET /orders/{orderId}`, `PUT /orders/{orderId}`, admin order GET — **Đổi**

- **Trước**: mỗi dòng đơn là `{ productId, name, slug, quantity, unitPrice, lineTotal }`; `PUT` nhận
  `lines[].productId`.
- **Sau**:
  - Dòng đơn (`OrderLineResponse`, **8 member**: `productId`, **`productName`**, **`variantId`**,
    **`variantName`**, `quantity`, `unitPrice`, `lineTotal`, `position`). **Bỏ** `name`/`slug`.
    Hiển thị cho khách là "**`productName` + `variantName`**".
  - **Snapshot lưu nhưng không trả ra**: mã sản phẩm (`product_code`) và **SKU**. Đơn **vẫn** giữ được
    SKU đã bán, nhưng view khách không trả chúng.
  - Một dòng đơn = một cặp **(mã sản phẩm, SKU)**; một sản phẩm mua hai variant cho ra **hai** dòng.
  - `PUT /orders/{orderId}`: `EditLineRequest` = `{ "variantId": <uuid>, "quantity": <int ≥ 1> }` (trước
    là `productId`). Các `details[].field` của `ORDER_ITEM_*`/`ORDER_QUANTITY_*` đổi từ `"productId"`
    sang `"variantId"`.
  - `POST /orders` (checkout) request **không đổi** (`{ addressId? }`).

## 4. Mã lỗi — thêm và gỡ

**Thêm:** `PRODUCT_CODE_TAKEN` 409 · `VARIANT_SKU_TAKEN` 409 · `VARIANT_NOT_FOUND` 404 ·
`VARIANT_LAST_REMOVAL` 409 · `VARIANT_STATE_TRANSITION_INVALID` 409.

**Gỡ:** `VARIANT_GROUP_NOT_FOUND`, `VARIANT_GROUP_SLUG_TAKEN`, `VARIANT_GROUP_HAS_OPTIONS`,
`VARIANT_LABEL_TAKEN`, `PRODUCT_ALREADY_IN_VARIANT_GROUP` (feature 014) và
`PRODUCT_STATE_TRANSITION_INVALID` (feature 006).

**Đổi nghĩa:** `PRODUCT_NOT_FOUND` 404 nay bao gồm "sản phẩm không còn variant nào đang bán" (bỏ nhãn
pre-order, "chưa lên ACTIVE"). Inventory dùng `VARIANT_NOT_FOUND` (trước `PRODUCT_NOT_FOUND`); cart vẫn
giữ `PRODUCT_NOT_FOUND` cho một variant lạ (mã cũ đã lập, chỉ đổi khoá).

## 5. Phụ lục đồng bộ (sync appendix)

Phần này là bằng chứng cho lần **đồng bộ guide với source** (T054). Mọi field dưới đây là một thẻ
`json:"…"` trong DTO của feature (module 04–07, presentation). Con số là số field.

### 5.1 Field của DTO (product)

- `PublicProductResponse` (5): `id`, `name`, `slug`, `price`, `imageUrl`.
- `PriceResponse` (2): `amount`, `currency`.
- `PublicImageResponse` (4): `id`, `url`, `width`, `height`.
- `PublicVariantResponse` (6): `id`, `name`, `price`, `imageUrl`, `sellState`, `inStock`.
- `PublicProductDetailResponse` (8): `id`, `name`, `slug`, `price`, `imageUrl`, `description`,
  `images`, `variants`.
- `CreateProductRequest` (8): `name`, `code`, `slug`, `description`, `defaultPrice`, `categoryId`,
  `position`, `variants`.
- `PriceRequest` (2): `amount`, `currency`.
- `CreateVariantRequest` (4): `name`, `sku`, `price`, `position`.
- `VariantPriceRequest` (1): `amount`.
- `UpdateProductRequest` (7): `name`, `code`, `slug`, `description`, `defaultPrice`, `categoryId`,
  `position`.
- `UpdateVariantRequest` (4): `name`, `sku`, `price`, `position`.
- `ChangeVariantStateRequest` (1): `to`.
- `AdminProductResponse` (12): `id`, `name`, `code`, `slug`, `description`, `defaultPrice`,
  `categoryId`, `position`, `variantCount`, `imageUrl`, `createdAt`, `updatedAt`.
- `AdminImageResponse` (7): `id`, `publicId`, `url`, `width`, `height`, `position`, `isPrimary`.
- `AdminVariantResponse` (8): `id`, `name`, `sku`, `price`, `effectivePrice`, `imageUrl`, `position`,
  `sellState`.
- `AdminVariantPriceResponse` (1): `amount`.
- `AdminProductDetailResponse` (14): 12 của `AdminProductResponse` + `images` + `variants`.

### 5.2 Field của DTO (cart)

- `MoneyResponse` (2): `amount`, `currency`.
- `CartLineResponse` (9): `productId`, `productName`, `variantId`, `variantName`, `quantity`,
  `unitPrice`, `lineTotal`, `buyable`, `availableQuantity`.
- `CartResponse` (2): `lines`, `subtotal`.
- `AddItemRequest` (2): `variantId`, `quantity`.
- `QuantityRequest` (1): `quantity`.

### 5.3 Field của DTO (inventory)

- `StockResponse` (3): `physicalQuantity`, `heldQuantity`, `availableQuantity`.
- `MovementResponse` (9): `id`, `variantId`, `kind`, `delta`, `resultingQuantity`, `sourceReference`,
  `actorId`, `note`, `createdAt`.
- `QuantityRequest` (2): `quantity`, `note`.
- `AdjustmentRequest` (2): `quantity`, `note`.

### 5.4 Field của DTO (order)

- `OrderLineResponse` (8): `productId`, `productName`, `variantId`, `variantName`, `quantity`,
  `unitPrice`, `lineTotal`, `position`.
- `EditLineRequest` (2): `variantId`, `quantity`.
- `EditOrderRequest` (2): `addressId`, `lines`.
- `OrderAddressResponse` (7): `recipientName`, `recipientPhone`, `provinceCode`, `provinceName`,
  `wardCode`, `wardName`, `streetAddress`.
- `OrderSummaryResponse` (5): `id`, `status`, `total`, `itemCount`, `createdAt`.
- `OrderResponse` (7): 5 của summary + `address` + `lines`.
- `AdminOrderSummaryResponse` (6): 5 của summary + `userId`.
- `AdminOrderResponse` (8): 7 của `OrderResponse` + `userId`.
- `CheckoutRequest` (1): `addressId`.
- `TransferRequest` (1): `email` — **không đổi** ở feature này (chuyển nhượng đơn không liên quan tới
  variant; liệt kê để đủ bộ field của các DTO module 07).

### 5.5 Enum

- `sellState` (và `to` trong `ChangeVariantStateRequest`): `COMING_SOON`, `ACTIVE`, `OUT_OF_STOCK`,
  `DISCONTINUED` — **4** giá trị, khớp hằng số Go trong `product/domain/constant` và check constraint
  ở tầng lưu trữ.
- `kind` của `StockMovement`: `RESTOCK`, `DAMAGE`, `ADJUSTMENT`, `SALE` — **4** giá trị.
- `status` của đơn: `PENDING`, `PAYMENT_PENDING`, `PAID`, `SHIPPED`, `COMPLETED`, `CANCELLED` — **6**
  giá trị (không đổi ở feature này).

### 5.6 Route (gin `:param` → `{param}`)

Feature chạm các route: 16 route product (mục 2, dòng module 04), 5 route inventory, 4 route cart,
8 route order cùng 1 route gỡ cấp-sản-phẩm, 9 route product-group **gỡ**. Client dùng dạng `{param}`.

### 5.7 HTTP status guide dùng

- `200`: đọc, sửa, chuyển trạng thái (GET/PATCH/POST thường).
- `201`: tạo (sản phẩm, variant, ảnh variant, ảnh gallery) và checkout.
- `204`: xoá (sản phẩm, variant, ảnh, dòng giỏ) và các route không có body trả về.
- `400`: `VALIDATION_ERROR` (field sai) hoặc `MALFORMED_REQUEST` (body không parse/member lạ).
- `401`: `UNAUTHENTICATED` (thiếu/sai phiên).
- `403`: `FORBIDDEN` (khách gọi route admin).
- `404`: `PRODUCT_NOT_FOUND` / `VARIANT_NOT_FOUND` / `ORDER_NOT_FOUND`.
- `409`: `PRODUCT_CODE_TAKEN`, `VARIANT_SKU_TAKEN`, `VARIANT_LAST_REMOVAL`,
  `VARIANT_STATE_TRANSITION_INVALID`, `CART_*`, `ORDER_*`.
- `413`: `PRODUCT_IMAGE_TOO_LARGE` (ảnh vượt trần 2 MB).
- `503`: `PRODUCT_MEDIA_UNAVAILABLE` (media provider không dùng được; không có gì bị đổi).
