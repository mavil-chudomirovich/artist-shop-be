# ADR 021 - Product SKU Model: sản phẩm cha + variant (SKU) là đơn vị bán, tồn/giỏ/đơn khoá theo variant, mã sản phẩm & SKU duy nhất, và thay thế feature 014

- **Status**: Accepted
- **Decision Date**: 2026-10-16
- **Decision Maker**: Mavil
- **Supersedes**: [ADR 020](020-product-variants.md) (variant group một chiều)
- **Feature**: `specs/015-product-sku-model` (modules 04, 05, 06, 07)
- **Liên quan**: [ADR 013](013-product-visibility-media-and-hard-delete.md) (sản phẩm: hợp đồng hiển
  thị liên module, nhà chung của media, hard delete), [ADR 014](014-inventory-hold-and-availability.md)
  (tồn kho: hợp đồng liên module, mô hình ba bảng), [ADR 016](016-cart-singleton-loose-reference-and-snapshot.md)
  (giỏ: tài nguyên đơn, tham chiếu lỏng, giá snapshot), [ADR 017](017-order-checkout.md) /
  [ADR 018](018-order-confirmation-and-editing.md) (đơn hàng: snapshot dòng)

## 1. Bối cảnh

Yêu cầu: một sản phẩm có thể có nhiều **variant** (ví dụ Card Genshin mở ra chọn Flins, Dottore) và
**mỗi variant là một đơn vị bán riêng** — có giá, tồn kho, ảnh và trạng thái bán riêng. Feature 014
đã thử một hướng khác: một **variant group** gom nhiều **sản phẩm đầy đủ** làm lựa chọn, giữ giỏ/đơn/
kho khoá theo **product id** và không đổi hợp đồng liên module. Yêu cầu mới không khớp mô hình đó
("mỗi lựa chọn là một sản phẩm riêng" thay vì "một sản phẩm có nhiều SKU"), nên feature 015 **thay
thế** 014.

Bốn ràng buộc định hình cách làm (giống 014):

1. **Constitution I** cấm một module đọc bảng của module khác; mọi giao tiếp liên module qua
   `internal/contracts`.
2. **Constitution II** buộc tiền là **integer minor units + currency** và mọi thay đổi tồn kho nằm
   trong một transaction có ghi `inventory_transactions`.
3. **Constitution III & IV** buộc trạng thái bán là state machine tường minh với test dương + âm.
4. **Constitution VII (YAGNI)**: giữ modular monolith, không thêm hạ tầng hay dependency.

Câu hỏi thật: chuyển **đơn vị bán** từ sản phẩm sang variant đòi hỏi chạm vào hợp đồng liên module và
bốn module (04–07) — làm sao để mọi thứ vẫn nhất quán, tồn kho không âm, đơn giữ được SKU đã bán, và
dữ liệu cũ không mất. Các quyết định dưới đây (research D1–D14) có ảnh hưởng lâu dài nên được ghi
thành ADR, không chỉ nằm trong `research.md`.

## 2. Quyết định

### 2.1 Sản phẩm cha + variant con; thay thế 014 (D1, D2)

`products` trở thành **cha** giữ nội dung dùng chung (tên, **mã sản phẩm** `code`, giá mặc định,
danh mục, mô tả, slug, gallery) và một bảng mới **`product_variants`** giữ các con. `products` **mất**
`sell_state`, `is_preorder`, `preorder_expected_at`, `is_set` và hai cột gắn nhóm của 014
(`variant_group_id`, `variant_label`); các bảng của 014 (`product_variant_groups`,
`product_variant_group_images`, `product_set_items`) bị **xoá**. Sản phẩm **không** được bán một
mình.

### 2.2 Variant là một dòng, có ảnh riêng, giá tuỳ chọn (D3, D17)

`product_variants` gồm `id`, `product_id` (FK cascade), `name`, `sku` (duy nhất), `price_amount bigint
NULL`, bốn cột ảnh nullable (một ảnh, hoặc không), `position`, `sell_state`, thời gian. Giá **tuỳ
chọn**: null nghĩa là dùng **giá mặc định** của sản phẩm (FR-017); currency luôn là của sản phẩm nên
variant không nhận currency. Ảnh variant đi qua **cùng nhà media** như ảnh sản phẩm (chỉ lưu tham
chiếu, không lưu byte).

### 2.3 Trạng thái bán nằm ở variant, tự chuyển khi tồn cắt qua 0 (D4)

Variant mang state machine `COMING_SOON → ACTIVE ↔ OUT_OF_STOCK`, mọi trạng thái trừ `DISCONTINUED` →
`DISCONTINUED` (cuối). Module 05 Inventory gọi hai cạnh **tự động** `ACTIVE ↔ OUT_OF_STOCK` qua hợp
đồng product, nay khoá theo **variantID**, **trong cùng transaction** với thay đổi kho. Hai tín hiệu
là **no-op khoan dung**: `MarkVariantOutOfStock` chỉ tác động khi variant `ACTIVE`,
`MarkVariantOnSale` chỉ khi variant `OUT_OF_STOCK`, nên một lần restock một variant chưa mở bán không
ép được một chuyển trái phép.

### 2.4 Hợp đồng liên module đổi khoá từ product sang variant (D6)

`internal/contracts/product.go`: `ProductCatalog` **bị thay** bằng một hợp đồng **variant-facts** mà
giỏ/đơn tiêu thụ — `Variants(variantIDs) []VariantSummary` với `{VariantID, ProductID, ProductName,
ProductCode, VariantName, SKU, OnSale, Price}` — cộng một **variant-existence lookup** cho kho.
`internal/contracts/inventory.go`: `AvailableQuantity(variantIDs)` và `Reserve`/`Release`/`ApplySale`
nhận **variant id**. `internal/contracts/cart.go`: `CartLine` mang **VariantID** cộng snapshot
(`ProductID`, `ProductName`, `ProductCode`, `VariantName`, `SKU`, `Quantity`, `UnitPrice`). Đây là
một thay đổi hợp đồng **xuyên module**, ghi ở Complexity Tracking của `plan.md`.

### 2.5 Tồn kho, giỏ, đơn đều khoá theo variant (D5, D7, D8)

- `stock_levels` khoá chính `variant_id`; `inventory_transactions` và `stock_holds` mang `variant_id`;
  giữ chỗ hoạt động duy nhất trên `(order_id, variant_id)`.
- `cart_items` mang `variant_id` (duy nhất trên `(cart_id, variant_id)`); bỏ `product_id`. Thêm vào
  giỏ **bắt buộc** có variant.
- `order_items` mang `variant_id` (tham chiếu thông tin, không FK) và chụp thêm `product_code`,
  `variant_name`, `sku` (tên sản phẩm dùng lại cột `name`); natural key là `(order_id, product_code,
  sku)` — mỗi cặp (mã sản phẩm, SKU) một dòng.

### 2.6 Danh mục một mục mỗi sản phẩm; chi tiết có variant (D9, D10)

`GET /products` trả **một mục mỗi sản phẩm**: giá **thấp nhất** trong các variant **đang bán** và ảnh
chính của gallery sản phẩm (fallback ảnh variant mặc định; đổi từ variant-first sang gallery-first ngày
2026-10-17), và sản phẩm chỉ hiện khi **có ít nhất một variant đang
bán**. Phần tử **không** còn `type`. Danh mục chạy trong **một câu SQL** (predicate hiển thị và tổng
tính trong cùng câu), giữ quy ước phân trang của module. Chi tiết trả nội dung dùng chung + các
variant **đang bán hoặc hết hàng** (kèm `sellState`, `inStock`), gọi hợp đồng khả dụng của module 05
**một lần bulk** (D10). Đây là một read product → inventory mới (chiều ngược tín hiệu cũ); cả hai
chiều đều qua hợp đồng, composition root cấp adapter.

### 2.7 Ràng buộc quan trọng ở tầng lưu trữ (D2, D3, D11; FR-023)

- `products_code_key` unique trên `lower(btrim(code))`; `product_variants_sku_key` unique trên
  `lower(btrim(sku))`.
- `product_variants_price_ck CHECK (price_amount IS NULL OR price_amount > 0)`;
  `product_variants_sell_state_ck` cho bốn trạng thái; check ảnh (https khi có, width/height > 0, đủ
  bốn cột hoặc không cột nào).
- Chỉ mục sắp xếp `(product_id, position, created_at, id)` (danh sách variant + "mặc định = đầu
  tiên").

Bất biến **"mỗi sản phẩm ≥1 variant"** là bất biến liên dòng, không diễn đạt được bằng một ràng buộc
một dòng; **ứng dụng** giữ nó (tạo sản phẩm bắt buộc kèm ≥1 variant; gỡ variant cuối bị từ chối),
ghi rõ để người đọc không mong một check.

### 2.8 Migration 00014: backfill và xoá 014 (D11)

Một migration `00014_product_sku_model.sql`: tạo `product_variants`; thêm `products.code` (backfill =
slug rồi NOT NULL + unique); backfill **một variant mặc định mỗi sản phẩm** (`sku = code`, giá = giá
cũ, `sell_state` = state cũ, ảnh = ảnh chính, `position = 0`); chuyển `stock_levels`/
`inventory_transactions`/`stock_holds`/`cart_items` sang `variant_id`; mở rộng `order_items` và đổi
natural key; **drop** các bảng/cột của 014 và các cờ sản phẩm (`DROP ... IF EXISTS`); đủ
`-- +goose Down`. `00013_product_variants.sql` được **giữ** như một migration lịch sử (guard test của
nó ở lại), nên cả một DB đã chạy 00013 lẫn một DB chưa chạy đều tới mô hình mới.

### 2.9 Gỡ endpoint cấp sản phẩm và của 014 (D12)

Xoá `POST /admin/products/{id}/state` (feature 006) — sản phẩm không còn trạng thái bán. Thay bằng
`POST /admin/products/{id}/variants/{variantId}/state`. Xoá mọi route `/product-groups*` và
`/admin/product-groups*` của 014 cùng annotation, test, tài liệu. Thêm endpoint variant:
`POST`/`PATCH`/`DELETE /admin/products/{id}/variants[/{variantId}]`, `POST`/`DELETE
.../variants/{variantId}/image`, `POST .../variants/{variantId}/state`.

### 2.10 Vị trí code (giữ nguyên nguyên tắc của 014)

Variant là **thực thể của domain product** (module 04): model/port/adapter/DTO nằm trong module 04;
inventory/cart/order giữ lớp của chúng và chỉ đổi **khoá** sang variant qua hợp đồng. Không có module
mới, không gói share mới, không dependency mới.

## 3. Hệ quả

| Hệ quả | Chi tiết |
|---|---|
| Tích cực | **Một cơ chế biến thể duy nhất**: 014 bị xoá, không còn hai mô hình gom nhóm chồng nhau (FR-025) |
| Tích cực | **Đơn vị bán nhất quán**: variant là thứ được chọn, giữ chỗ, bán, và chụp vào đơn (SC-003) |
| Tích cực | **Đơn giữ được SKU đã bán**: dòng đơn chụp mã sản phẩm + SKU + tên variant, sống lâu hơn sản phẩm/variant |
| Tích cực | **Ràng buộc ở tầng lưu trữ**: mã/SKU duy nhất (so khớp bỏ hoa/thường, trim), giá dương, trạng thái trong tập — không đường code nào tạo dữ liệu sai (FR-023, SC-006) |
| Tiêu cực | **Đổi hợp đồng xuyên module**: `ProductCatalog`/`ProductLookup`/`Inventory*`/`CartLine` đổi khoá sang variant, kéo theo bốn module và test của chúng (Complexity Tracking) |
| Tiêu cực | **Migration nặng**: vừa backfill (một variant mỗi sản phẩm, repoint bốn bảng) vừa drop 014 và các cột sản phẩm; có giả định 014 chưa chạy trên DB thật (`DROP ... IF EXISTS`) |
| Tiêu cực | **Thêm một read product → inventory** cho trạng thái còn hàng per-variant (chiều ngược tín hiệu cũ) |
| Tiêu cực | **Combo set và pre-order bị bỏ**; sẽ trở lại như một capability riêng |
| Tiêu cực | **Bất biến "≥1 variant" chỉ ở ứng dụng**, không ở lưu trữ (không diễn đạt được bằng một ràng buộc dòng) |

**Xem lại quyết định khi**: cần biến thể **nhiều chiều** (màu × size) và ma trận tổ hợp; cần combo set
hoặc pre-order trở lại; hoặc bất biến "≥1 variant" cần được ép ở tầng lưu trữ (ví dụ bằng trigger
hoặc một bảng cha bắt buộc).

## 4. Cách tiếp cận bị loại

| Phương án | Vì sao không chọn |
|---|---|
| Giữ 014 (variant group) và thêm mảng variant | Hai cơ chế gom nhóm chồng nhau, không ai sở hữu; spec 015 thay thế 014 (FR-025) |
| Self-reference "sản phẩm cha" trên `products` | Cha phải có giá placeholder và/hoặc trạng thái bán — bẩn state machine và có nguy cơ bán nhầm cha (014 D1 đã loại) |
| Giữ tồn/giỏ/đơn khoá theo product, thêm variant như một nhãn | Variant phải là đơn vị tồn và đơn vị mua (spec Clarifications); giữ product id sẽ mất SKU trên dòng đơn và buộc consumer đoán variant |
| Bảng `product_variant_images` cho một ảnh mỗi variant | Một bảng cho một ảnh nặng hơn bốn cột nullable; ảnh sản phẩm đã đi qua nhà media |
| Lưu currency trên variant | Một currency cho mỗi sản phẩm (spec Assumptions); variant chỉ mang amount |
| Giữ `sell_state` trên sản phẩm như một rollup | Nguồn sự thật thứ hai mâu thuẫn với trạng thái per-variant (FR-015) |
| Giữ endpoint `POST /admin/products/{id}/state` | Sản phẩm không còn trường trạng thái; endpoint sẽ phục vụ một field không tồn tại (Constitution VIII) |
| Migration chỉ thêm, không drop 014 và cờ sản phẩm | Để lại hai cơ chế biến thể và một trạng thái bán cấp sản phẩm trái với mô hình per-variant |
| Giữ combo set / pre-order trong phiên bản này | Spec Clarifications bỏ cả hai; giữ chúng làm phức tạp mô hình chưa đo được nhu cầu |
| Một namespace slug cho cả variant | Variant được định địa chỉ trong sản phẩm của nó, không có link riêng (D3) |

## 5. Bổ sung (2026-10-16)

Các mục cố ý hoãn của feature 015 — biên product → inventory cho trạng thái còn hàng, biến thể nhiều
chiều, và các điểm khác — nằm ở
[`specs/015-product-sku-model/deferred.md`](../../specs/015-product-sku-model/deferred.md). Bàn giao
frontend nằm ở
[`specs/015-product-sku-model/frontend-guide.md`](../../specs/015-product-sku-model/frontend-guide.md).
