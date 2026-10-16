# Superseded — Product Variants (014)

**Feature này đã bị thay thế bởi [`specs/015-product-sku-model/`](../015-product-sku-model/spec.md)
(feature 015, module 04).**

Feature 014 gom nhiều **sản phẩm đầy đủ** vào một **variant group** để khách chọn, giữ giỏ/đơn/kho
khoá theo **product id**. Feature 015 thay bằng mô hình **sản phẩm cha + mảng variant (SKU)**: variant
là **đơn vị bán** (tồn/giỏ/đơn đều theo variant), sản phẩm có mã sản phẩm và giá mặc định, mỗi
variant có SKU, giá tuỳ chọn, ảnh và trạng thái bán riêng.

Hệ quả cho các artifact của 014:

- **Cơ chế gom nhóm bị xoá**: các bảng `product_variant_groups`, `product_variant_group_images`,
  `product_set_items` và hai cột `products.variant_group_id` / `products.variant_label` bị drop bởi
  migration `00014_product_sku_model.sql`.
- **Mọi route của 014 bị gỡ**: `GET /api/v1/product-groups/{slug}` và
  `GET`/`POST`/`PATCH`/`DELETE /api/v1/admin/product-groups*` cùng các route ảnh nhóm.
- **`docs/api-reference.md`, `docs/modules/04-product.md` đã cập nhật** theo mô hình mới; các mục mô tả
  variant group trong đó không còn hiệu lực.
- **Guide frontend của 014 bị thay**: xem
  [`specs/015-product-sku-model/frontend-guide.md`](../015-product-sku-model/frontend-guide.md).
- Quyết định gốc: [ADR 020](../../docs/decisions/020-product-variants.md) ở trạng thái
  **Superseded by [ADR 021](../../docs/decisions/021-product-sku-model.md)**.

Artifact trong thư mục này (`spec.md`, `plan.md`, `research.md`, `data-model.md`, `contracts/`,
`tasks.md`, `quickstart.md`, `frontend-guide.md`, `deferred.md`) được **giữ lại như bản ghi lịch sử**
của quyết định đã bị thay thế, không còn là nguồn authoritative. Mục hoãn nào của 014 còn áp dụng
(biến thể nhiều chiều, thứ tự lựa chọn ngoài `position`) được mang tiếp vào
[`specs/015-product-sku-model/deferred.md`](../015-product-sku-model/deferred.md); khả dụng theo từng
lựa chọn — thứ 014 hoãn — nay **đã giao** (product detail trả `inStock` theo variant).
