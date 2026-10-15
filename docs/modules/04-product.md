# Module 04 — Product

- **Trạng thái Spec Kit**: Hoàn tất (implement + converge). Nghĩa vụ **D1** — trạng thái bán tự
  chuyển khi hết/hồi kho — đã được **module 05 Inventory** đóng; điểm còn lại (xoá mềm) nêu ở phần
  "Điểm chưa giao". **Bổ sung feature 014** (variant group một chiều) đã giao.
- **Spec**: [`specs/006-product-catalog/spec.md`](../../specs/006-product-catalog/spec.md)
- **Plan**: [`specs/006-product-catalog/plan.md`](../../specs/006-product-catalog/plan.md)
- **Tasks**: [`specs/006-product-catalog/tasks.md`](../../specs/006-product-catalog/tasks.md)
- **Contract**: [`specs/006-product-catalog/contracts/openapi.yaml`](../../specs/006-product-catalog/contracts/openapi.yaml),
  [`error-codes.md`](../../specs/006-product-catalog/contracts/error-codes.md)
- **Spec (feature 014 — variant group)**: [`specs/014-product-variants/spec.md`](../../specs/014-product-variants/spec.md)
- **Plan / Tasks / Contract (feature 014)**:
  [`plan.md`](../../specs/014-product-variants/plan.md),
  [`tasks.md`](../../specs/014-product-variants/tasks.md),
  [`contracts/openapi.yaml`](../../specs/014-product-variants/contracts/openapi.yaml),
  [`contracts/error-codes.md`](../../specs/014-product-variants/contracts/error-codes.md)
- **Code**: `internal/modules/product/{domain,application,infrastructure,presentation}`,
  `migrations/00006_product.sql`, `migrations/00013_product_variants.sql`
- **Ưu tiên / Giai đoạn**: V1.0
- **Phụ thuộc**: category

## Mục đích

Quản lý sản phẩm vật lý (acrylic stand, shikishi, badge, print, keychain, combo
set) và trạng thái bán, làm nền cho giỏ hàng, đơn hàng và kho.

## Phạm vi MVP

Có:
- Danh sách + chi tiết sản phẩm công khai (ảnh, giá, mô tả, trạng thái).
- Admin tạo/sửa/xóa sản phẩm. Xoá là **hard delete**, không phải xóa mềm — xem
  "Hai điểm chưa giao".
- Ảnh sản phẩm (nhiều ảnh, ảnh chính) qua Cloudinary, tối đa 10 ảnh/sản phẩm.
- Trạng thái bán: COMING_SOON → ACTIVE ↔ OUT_OF_STOCK, và mọi trạng thái → DISCONTINUED.
- Combo set (nhóm sản phẩm/bundle).
- Pre-order (đặt trước).
- **Biến thể một chiều** (feature 014 — đã giao): một *variant group* gom nhiều *lựa chọn*, mỗi
  lựa chọn là một **sản phẩm đầy đủ** (giá/tồn/trạng thái/ảnh/link riêng). Nhóm có gallery ảnh
  riêng, **không** có giá/tồn/trạng thái riêng và **không** bán trực tiếp; khách mua đúng lựa chọn
  đã chọn, nên giỏ/đơn/kho **không** đổi. Xem
  [`specs/014-product-variants/spec.md`](../../specs/014-product-variants/spec.md).

Không (hoãn):
- Biến thể **nhiều chiều** (màu × size) và ma trận tổ hợp, cùng khả dụng theo từng lựa chọn trên
  trang nhóm — xem [`specs/014-product-variants/deferred.md`](../../specs/014-product-variants/deferred.md)
  (D-01, D-02).
- Đa ngôn ngữ nội dung sản phẩm.

## Thực thể dữ liệu

- `products`: tên, slug, mô tả, giá (`price_amount` đơn vị nhỏ + `currency`), danh mục
  (`category_id` → `categories(id)` `ON DELETE RESTRICT`), vị trí, trạng thái bán, cờ
  pre-order/combo, `normalized_slug` (khoá chuẩn hoá, chỉ để index/so khớp — không bao giờ
  trả ra ngoài).
- `product_images`: tham chiếu Cloudinary (`public_id`, `secure_url`), kích thước, thứ tự,
  ảnh chính (partial unique index giữ "tối đa một ảnh chính").
- `product_set_items`: thành viên của một combo set; một set chỉ ghi nhận thành viên, giá do
  operator đặt và **không** suy ra từ chúng.
- `product_variant_groups` (feature 014): nhóm biến thể — tên, slug, mô tả, `category_id`
  (`ON DELETE RESTRICT`), vị trí, tên chiều chọn `option_name`; **không** có giá, tồn kho hay
  trạng thái bán.
- `product_variant_group_images` (feature 014): gallery ảnh riêng của nhóm, cùng luật media và
  bất biến "tối đa một ảnh chính" như `product_images`.
- `products` (feature 014) thêm hai cột `variant_group_id` (FK → `product_variant_groups(id)`
  `ON DELETE RESTRICT`) và `variant_label`: một sản phẩm thuộc **tối đa một** nhóm, nhãn lựa chọn
  **duy nhất trong nhóm**, và một sản phẩm **không** thể vừa là combo set vừa là lựa chọn — cả ba
  ràng buộc ở tầng lưu trữ.

## Luồng nghiệp vụ chính

1. Admin tạo sản phẩm + ảnh + trạng thái.
2. Khách duyệt/tìm sản phẩm, xem chi tiết.
3. Trạng thái tự chuyển khi hết/hồi kho (phối hợp inventory) — **chưa giao ở đây**, thuộc
   module 05 Inventory; xem "Hai điểm chưa giao".

## Yêu cầu chức năng sơ bộ

- Slug sản phẩm duy nhất.
- Sản phẩm chỉ bán được khi ở trạng thái ACTIVE.
- Giá lưu dạng số nguyên đơn vị nhỏ + đơn vị tiền tệ.
- Chuyển trạng thái bán theo quy tắc rõ ràng, từ chối chuyển không hợp lệ.

## Tiêu chí hoàn thành

- [x] CRUD sản phẩm + quản lý ảnh + danh sách công khai có test — mười một endpoint, xem
  [api-reference.md](../api-reference.md) mục 6.
- [x] Quy tắc trạng thái bán có test (kể cả chuyển không hợp lệ) — mỗi cạnh một test dương và
  một test âm; `DISCONTINUED` là trạng thái cuối.
- [x] Ràng buộc giá/tiền tệ tuân thủ constitution — `price_amount bigint` đơn vị nhỏ +
  `currency char(3)`, check ở tầng lưu trữ, không dùng số thực.
- [x] Hai nghĩa vụ kế thừa từ feature 005: tham chiếu `products.category_id` với
  `ON DELETE RESTRICT` (đóng nửa "không xoá danh mục còn sản phẩm" của module 03) và lọc
  `GET /api/v1/products?category=<slug>`.

### Một điểm module doc mô tả nhưng feature này **chưa giao**, và một điểm đã đóng sau đó

Đây là phần đã được mang tiếp sang nơi khác, **không** phải phần đã xong, cộng điểm đã hoàn tất
sau này. Ghi rõ ở đây để một luật chưa xong không trở nên vô hình khi module được đánh dấu hoàn tất:
[`specs/006-product-catalog/deferred.md`](../../specs/006-product-catalog/deferred.md).

| Điểm | Vì sao / Trạng thái | Gỡ ở đâu |
|---|---|---|
| **Trạng thái tự chuyển khi hết/hồi kho** — **đã đóng bởi feature 007** | Không có thực thể kho nào trong hệ thống; feature 006 **không** lưu cột tồn kho (FR-038), nên `OUT_OF_STOCK` khi đó do operator đặt tay. Module 05 Inventory đã thêm thực thể kho và **gọi hai cạnh `ACTIVE ↔ OUT_OF_STOCK` đã có sẵn** của module 04 qua hợp đồng `ProductAvailability`, với một use case hệ thống riêng (`product/application/implement/availability.go`) không cần tác nhân admin | **Đã gỡ:** `internal/contracts/product.go` (`ProductAvailability`, `ProductLookup`), adapter `product/infrastructure/implement/availability/`, và nghĩa vụ D1 trong `specs/006-product-catalog/deferred.md` nay ghi đã đóng. Xem [decisions/014](../decisions/014-inventory-hold-and-availability.md) và `docs/modules/05-inventory.md` |
| **Xoá mềm (`xóa mềm`)** | Module doc ghi xoá mềm, feature này chọn **hard delete** vì hiện không có gì đọc sản phẩm đã xoá (chưa có đơn hàng); xoá mềm bây giờ là một trạng thái vô hình thêm vào mọi đường đọc mà không ai cần | **Module 07 Order**: khi dòng đơn phải đọc lại sản phẩm, chọn soft delete (thêm `deleted_at`) hoặc chụp dữ liệu sản phẩm vào dòng đơn. `deferred.md` D3, và [decisions/013](../decisions/013-product-visibility-media-and-hard-delete.md) |

Mục "Không (hoãn)" còn lại ở trên (đa ngôn ngữ nội dung sản phẩm) vẫn hoãn, là một feature riêng
khi có nhu cầu — `specs/006-product-catalog/deferred.md` D7. Phần biến thể **một chiều** nay **đã
giao** (feature 014); các phần của nó còn hoãn — biến thể nhiều chiều, khả dụng theo từng lựa chọn,
thứ tự lựa chọn ngoài `position`, và trần 10 ảnh nhóm dưới đồng thời — nằm ở
[`specs/014-product-variants/deferred.md`](../../specs/014-product-variants/deferred.md).

## Ghi chú / câu hỏi mở

Hai câu hỏi dưới đây đã được chốt lúc specify — xem `specs/006-product-catalog/spec.md`, mục
`Clarifications`, session 2026-10-07:

- **Combo set tính giá thế nào (giá cố định hay tổng thành phần)?** → **Operator đặt giá cho cả
  set** (FR-039). Set là một sản phẩm độc lập có giá riêng; các sản phẩm trong set không quyết
  định giá đó.
- **Pre-order giới hạn số lượng theo đợt? Trạng thái riêng?** → **Không**. Pre-order là một nhãn
  kèm ngày dự kiến trên sản phẩm bình thường, không thêm trạng thái bán (FR-040).

Đã **đóng** cho **module 05 Inventory**: luồng "trạng thái tự chuyển khi hết/hồi kho". Module 04
vẫn không theo dõi tồn kho và `PATCH` vẫn không nhận `sellState`, nhưng giờ đây module 05 gọi hai
cạnh `ACTIVE ↔ OUT_OF_STOCK` qua hợp đồng `ProductAvailability` mỗi khi khả dụng cắt qua 0, trong
cùng transaction với thay đổi kho. Việc chuyển vẫn đi qua state machine của module 04 — module 05
**không** ghi thẳng trạng thái. Xem [decisions/014](../decisions/014-inventory-hold-and-availability.md).

Hợp đồng HTTP của module nằm ở [api-reference.md](../api-reference.md) mục 6 — danh sách
endpoint và mã lỗi chỉ có ở đó, file này không nhân bản.

## Tham chiếu

- `docs/product/project_overview.md` (Shop, product lifecycle)
- `docs/product/backend-spec.md` (Database: products, product_images)
- [`specs/006-product-catalog/deferred.md`](../../specs/006-product-catalog/deferred.md) — các
  điểm cố ý hoãn
- [decisions/013](../decisions/013-product-visibility-media-and-hard-delete.md) — hợp đồng
  hiển thị liên module, nhà chung của media, và hard delete
