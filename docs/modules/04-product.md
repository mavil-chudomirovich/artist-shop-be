# Module 04 — Product

- **Trạng thái Spec Kit**: Hoàn tất (implement + converge). Nghĩa vụ **D1** — trạng thái bán tự
  chuyển khi hết/hồi kho — do **module 05 Inventory** đóng, nay theo từng variant. **Feature 015**
  (sản phẩm cha + variant/SKU) đã giao và **thay thế feature 014** (variant group một chiều).
- **Spec**: [`specs/006-product-catalog/spec.md`](../../specs/006-product-catalog/spec.md),
  [`specs/015-product-sku-model/spec.md`](../../specs/015-product-sku-model/spec.md)
- **Plan**: [`specs/006-product-catalog/plan.md`](../../specs/006-product-catalog/plan.md),
  [`specs/015-product-sku-model/plan.md`](../../specs/015-product-sku-model/plan.md)
- **Tasks**: [`specs/006-product-catalog/tasks.md`](../../specs/006-product-catalog/tasks.md),
  [`specs/015-product-sku-model/tasks.md`](../../specs/015-product-sku-model/tasks.md)
- **Contract**: [`specs/015-product-sku-model/contracts/openapi.yaml`](../../specs/015-product-sku-model/contracts/openapi.yaml),
  [`error-codes.md`](../../specs/015-product-sku-model/contracts/error-codes.md) — feature 015 thay thế
  contract của 006 và chấm dứt contract của 014
- **Frontend guide**: [`specs/015-product-sku-model/frontend-guide.md`](../../specs/015-product-sku-model/frontend-guide.md)
  — thay thế guide của 014
- **Code**: `internal/modules/product/{domain,application,infrastructure,presentation}`,
  `migrations/00006_product.sql`, `migrations/00014_product_sku_model.sql`,
  `migrations/00015_product_code_as_link_segment.sql`
- **Ưu tiên / Giai đoạn**: V1.0
- **Phụ thuộc**: category

## Mục đích

Quản lý **sản phẩm cha** (nội dung dùng chung) và các **variant (SKU)** con — đơn vị khách chọn,
thêm vào giỏ và mua — làm nền cho giỏ hàng, đơn hàng và kho.

## Phạm vi MVP

Có:
- Danh sách **một mục mỗi sản phẩm** (giá thấp nhất trong các variant đang bán, ảnh variant mặc
  định) và chi tiết sản phẩm kèm danh sách variant hiển thị (giá + trạng thái còn hàng).
- Admin tạo/sửa/xóa sản phẩm; tạo **bắt buộc** kèm ≥1 variant. Xoá là **hard delete** (đơn đã chụp
  nội dung nên không phụ thuộc sản phẩm còn tồn tại).
- Ảnh sản phẩm (nhiều ảnh, ảnh chính) qua Cloudinary, tối đa 10 ảnh/sản phẩm, cùng **một ảnh riêng
  cho mỗi variant**.
- **Variant**: tên, SKU (duy nhất toàn hệ), giá riêng **tuỳ chọn** (bỏ trống → dùng giá mặc định của
  sản phẩm), ảnh riêng, thứ tự, và **trạng thái bán riêng** với state machine
  `COMING_SOON → ACTIVE ↔ OUT_OF_STOCK`, mọi trạng thái → `DISCONTINUED` (cuối).
- **Kéo-thả sắp xếp** thứ tự hiển thị: sản phẩm có **một thứ tự chung toàn catalogue** (không theo
  từng danh mục) và các variant có thứ tự **trong sản phẩm** (id đầu tiên là **variant mặc định**);
  mỗi lần lưu gửi trọn danh sách định danh theo thứ tự mới (`PUT /admin/products/reorder` và
  `PUT /admin/products/{id}/variants/reorder`, feature `018-catalog-reorder`). Tạo sản phẩm/variant mà
  bỏ trống `position` đặt bản ghi mới **lên đầu** (variant trong một lần tạo hàng loạt theo thứ tự
  mảng, variant đầu tiên là mặc định).
- Mã sản phẩm (`code`) bắt buộc và duy nhất; **`code` cũng là đoạn link công khai** của sản phẩm
  (URL-safe: chữ thường, chữ số, dấu gạch đơn; ≤140 ký tự) — sản phẩm **không** có `slug` riêng
  (danh mục vẫn giữ `slug` của nó). Mỗi sản phẩm ≥1 variant (gỡ variant cuối bị từ chối).

Không (hoãn):
- Biến thể **nhiều chiều** (màu × size) và ma trận tổ hợp; xem
  [`specs/014-product-variants/deferred.md`](../../specs/014-product-variants/deferred.md) (D-02) và
  [`specs/015-product-sku-model/deferred.md`](../../specs/015-product-sku-model/deferred.md).
- Combo set (nhóm sản phẩm bán chung) và pre-order — đã **bị bỏ** ở phiên bản này, dự kiến làm bằng
  chức năng khác.
- Đa ngôn ngữ nội dung sản phẩm.

## Thực thể dữ liệu

- `products`: tên, mã sản phẩm (`code`, duy nhất trên `lower(btrim(code))`, **cũng là đoạn link công
  khai** `/products/{code}` — URL-safe, ≤140 ký tự), mô tả, **giá mặc định** (`price_amount` đơn vị
  nhỏ + `currency`), danh mục (`category_id` → `categories(id)` `ON DELETE RESTRICT`), vị trí. Sản
  phẩm **không** còn trạng thái bán, cờ pre-order/combo, cột gắn nhóm, hay `slug`/`normalized_slug`
  (hai cột này bị migration `00015` gỡ; `code` đã giữ đoạn link).
- `product_variants`: con của sản phẩm — `product_id` (FK `ON DELETE CASCADE`), tên hiển thị, `sku`
  (duy nhất trên `lower(btrim(sku))`), `price_amount NULL` (null → dùng giá mặc định), bốn cột ảnh
  nullable (một ảnh, hoặc không), `position` (nhỏ nhất là variant mặc định), `sell_state`, thời gian.
- `product_images`: tham chiếu Cloudinary (`public_id`, `secure_url`), kích thước, thứ tự, ảnh chính
  (partial unique index giữ "tối đa một ảnh chính").

## Luồng nghiệp vụ chính

1. Admin tạo sản phẩm **kèm ≥1 variant**, gắn ảnh gallery, rồi đưa variant sang `ACTIVE`.
2. Khách duyệt catalogue (một mục mỗi sản phẩm), xem chi tiết và chọn một variant.
3. Trạng thái bán của variant **tự chuyển** khi hết/hồi kho (phối hợp inventory): module 05 gọi hai
   cạnh `ACTIVE ↔ OUT_OF_STOCK` qua hợp đồng product, trong cùng transaction với thay đổi kho; xem
   [decisions/014](../decisions/014-inventory-hold-and-availability.md).

## Yêu cầu chức năng sơ bộ

- Mã sản phẩm duy nhất trên toàn bộ sản phẩm; SKU duy nhất trên toàn hệ; cả hai bắt buộc.
- Sản phẩm chỉ hiển thị cho khách khi còn **ít nhất một variant đang bán**.
- Variant chỉ mua được khi đang bán và còn tồn.
- Giá lưu dạng số nguyên đơn vị nhỏ + đơn vị tiền tệ (một currency cho mỗi sản phẩm).
- Chuyển trạng thái bán của variant theo quy tắc rõ ràng, từ chối chuyển không hợp lệ và nêu trạng
  thái hiện tại.

## Tiêu chí hoàn thành

- [x] CRUD sản phẩm + quản lý ảnh + danh sách công khai + quản lý variant có test — mười sáu endpoint,
  xem [api-reference.md](../api-reference.md) mục 6.
- [x] Quy tắc trạng thái bán của variant có test (kể cả chuyển không hợp lệ) — mỗi cạnh một test dương
  và một test âm; `DISCONTINUED` là trạng thái cuối.
- [x] Ràng buộc giá/tiền tệ tuân thủ constitution — `price_amount bigint` đơn vị nhỏ + `currency
  char(3)`, check ở tầng lưu trữ; giá variant nullable và dùng cùng currency của sản phẩm.
- [x] Ràng buộc "≥1 variant" và "mã/SKU duy nhất" có test, trong đó mã/SKU buộc ở **tầng lưu trữ**
  (unique index trên biểu thức chuẩn hoá); "≥1 variant" là bất biến liên dòng do ứng dụng giữ.
- [x] Hai nghĩa vụ kế thừa từ feature 005: tham chiếu `products.category_id` với `ON DELETE RESTRICT`
  (đóng nửa "không xoá danh mục còn sản phẩm" của module 03) và lọc
  `GET /api/v1/products?category=<slug>`.

### Một điểm module doc mô tả nhưng feature 006 **chưa giao**, và một điểm đã đóng sau đó

Đây là phần đã được mang tiếp sang nơi khác, **không** phải phần đã xong, cộng điểm đã hoàn tất sau
này. Ghi rõ ở đây để một luật chưa xong không trở nên vô hình khi module được đánh dấu hoàn tất:
[`specs/006-product-catalog/deferred.md`](../../specs/006-product-catalog/deferred.md).

| Điểm | Vì sao / Trạng thái | Gỡ ở đâu |
|---|---|---|
| **Trạng thái tự chuyển khi hết/hồi kho** — **đã đóng bởi feature 007** | Module 05 Inventory thêm thực thể kho và **gọi hai cạnh `ACTIVE ↔ OUT_OF_STOCK`** qua hợp đồng hiển thị của module 04, không cần tác nhân admin. **Feature 015 chuyển trạng thái này xuống variant**: cùng hai cạnh, nay theo `variantID` (`MarkVariantOutOfStock`/`MarkVariantOnSale`) | **Đã gỡ:** `internal/contracts/product.go` (hợp đồng hai chiều), adapter `product/infrastructure/implement/{availability,catalog}/`, và nghĩa vụ D1 trong `specs/006-product-catalog/deferred.md` nay ghi đã đóng. Xem [decisions/014](../decisions/014-inventory-hold-and-availability.md) và [decisions/021](../decisions/021-product-sku-model.md) |
| **Xoá mềm (`xóa mềm`)** | Module doc ghi xoá mềm, feature 006 chọn **hard delete**; feature 015 giữ hard delete (xoá sản phẩm xoá theo các variant) vì module 07 Order nay **chụp** nội dung đã mua vào dòng đơn, nên đơn không phụ thuộc sản phẩm còn tồn tại | **Đã đóng:** module 07 Order chụp `name`/giá vào `order_items` (và, sau 015, thêm mã sản phẩm + SKU + tên variant; cột `slug` của dòng đơn bị `00015` gỡ). `deferred.md` D3, [decisions/013](../decisions/013-product-visibility-media-and-hard-delete.md) |

Mục "Không (hoãn)" còn lại ở trên (đa ngôn ngữ nội dung sản phẩm) vẫn hoãn, là một feature riêng khi
có nhu cầu — `specs/006-product-catalog/deferred.md` D7. Cơ chế **variant group một chiều** của feature
014 đã **bị thay thế** bởi mô hình sản phẩm cha + variant của feature 015; các mục hoãn của nó
(biến thể nhiều chiều, khả dụng theo từng lựa chọn, thứ tự lựa chọn ngoài `position`) nằm ở
[`specs/014-product-variants/deferred.md`](../../specs/014-product-variants/deferred.md).

## Ghi chú / câu hỏi mở

Hai câu hỏi dưới đây đã được chốt lúc specify — xem `specs/006-product-catalog/spec.md`, mục
`Clarifications`, session 2026-10-07:

- **Combo set tính giá thế nào (giá cố định hay tổng thành phần)?** → **Operator đặt giá cho cả
  set** (FR-039). Combo set nay **đã bị bỏ** ở phiên bản này.
- **Pre-order giới hạn số lượng theo đợt? Trạng thái riêng?** → **Không**. Pre-order nay **đã bị
  bỏ** ở phiên bản này.

Hợp đồng HTTP của module nằm ở [api-reference.md](../api-reference.md) mục 6 — danh sách endpoint và
mã lỗi chỉ có ở đó, file này không nhân bản.

## Tham chiếu

- `docs/product/project_overview.md` (Shop, product lifecycle)
- `docs/product/backend-spec.md` (Database: products, product_images)
- [`specs/006-product-catalog/deferred.md`](../../specs/006-product-catalog/deferred.md) — các điểm
  cố ý hoãn
- [`specs/015-product-sku-model/deferred.md`](../../specs/015-product-sku-model/deferred.md) — các
  điểm cố ý hoãn của feature 015
- [decisions/013](../decisions/013-product-visibility-media-and-hard-delete.md) — hợp đồng hiển thị
  liên module, nhà chung của media, và hard delete
- [decisions/021](../decisions/021-product-sku-model.md) — sản phẩm cha + variant, tồn/giỏ/đơn theo
  variant, thay thế 014
