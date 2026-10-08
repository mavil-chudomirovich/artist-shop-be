# Module 04 — Product

- **Trạng thái Spec Kit**: Hoàn tất (implement + converge; hai điểm module doc mô tả nhưng
  feature này **không** giao được nêu rõ ở phần "Hai điểm chưa giao")
- **Spec**: [`specs/006-product-catalog/spec.md`](../../specs/006-product-catalog/spec.md)
- **Plan**: [`specs/006-product-catalog/plan.md`](../../specs/006-product-catalog/plan.md)
- **Tasks**: [`specs/006-product-catalog/tasks.md`](../../specs/006-product-catalog/tasks.md)
- **Contract**: [`specs/006-product-catalog/contracts/openapi.yaml`](../../specs/006-product-catalog/contracts/openapi.yaml),
  [`error-codes.md`](../../specs/006-product-catalog/contracts/error-codes.md)
- **Code**: `internal/modules/product/{domain,application,infrastructure,presentation}`,
  `migrations/00006_product.sql`
- **Ưu tiên / Giai đoạn**: Giai đoạn 1
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

Không (hoãn):
- Biến thể phức tạp (size/màu) nâng cao.
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

### Hai điểm module doc mô tả nhưng feature này **chưa giao**

Đây là phần đã được mang tiếp sang nơi khác, **không** phải phần đã xong. Ghi rõ ở đây để một
luật chưa xong không trở nên vô hình khi module được đánh dấu hoàn tất:
[`specs/006-product-catalog/deferred.md`](../../specs/006-product-catalog/deferred.md).

| Điểm chưa giao | Vì sao | Gỡ ở đâu |
|---|---|---|
| **Trạng thái tự chuyển khi hết/hồi kho** | Không có thực thể kho nào trong hệ thống; feature này **không** lưu cột tồn kho (FR-038), nên `OUT_OF_STOCK` hiện do operator đặt tay | **Module 05 Inventory**: thêm thực thể kho + bảng `inventory_transactions`, rồi gọi hai cạnh `ACTIVE ↔ OUT_OF_STOCK` đã có sẵn trong state machine của module 04. `deferred.md` D1 |
| **Xoá mềm (`xóa mềm`)** | Module doc ghi xoá mềm, feature này chọn **hard delete** vì hiện không có gì đọc sản phẩm đã xoá (chưa có đơn hàng); xoá mềm bây giờ là một trạng thái vô hình thêm vào mọi đường đọc mà không ai cần | **Module 07 Order**: khi dòng đơn phải đọc lại sản phẩm, chọn soft delete (thêm `deleted_at`) hoặc chụp dữ liệu sản phẩm vào dòng đơn. `deferred.md` D3, và [decisions/013](../decisions/013-product-visibility-media-and-hard-delete.md) |

Hai mục "Không (hoãn)" ở trên (biến thể phức tạp, đa ngôn ngữ) vẫn hoãn, mỗi mục là một
feature riêng khi có nhu cầu — `deferred.md` D7.

## Ghi chú / câu hỏi mở

Hai câu hỏi dưới đây đã được chốt lúc specify — xem `specs/006-product-catalog/spec.md`, mục
`Clarifications`, session 2026-10-07:

- **Combo set tính giá thế nào (giá cố định hay tổng thành phần)?** → **Operator đặt giá cho cả
  set** (FR-039). Set là một sản phẩm độc lập có giá riêng; các sản phẩm trong set không quyết
  định giá đó.
- **Pre-order giới hạn số lượng theo đợt? Trạng thái riêng?** → **Không**. Pre-order là một nhãn
  kèm ngày dự kiến trên sản phẩm bình thường, không thêm trạng thái bán (FR-040).

Còn để ngỏ cho **module 05 Inventory**: luồng "trạng thái tự chuyển khi hết/hồi kho". Module 04
không theo dõi tồn kho, nên `OUT_OF_STOCK` hiện do operator đặt (FR-038); phần tự động hoá là việc
của module 05.

Hợp đồng HTTP của module nằm ở [api-reference.md](../api-reference.md) mục 6 — danh sách
endpoint và mã lỗi chỉ có ở đó, file này không nhân bản.

## Tham chiếu

- `docs/product/project_overview.md` (Shop, product lifecycle)
- `docs/product/backend-spec.md` (Database: products, product_images)
- [`specs/006-product-catalog/deferred.md`](../../specs/006-product-catalog/deferred.md) — các
  điểm cố ý hoãn
- [decisions/013](../decisions/013-product-visibility-media-and-hard-delete.md) — hợp đồng
  hiển thị liên module, nhà chung của media, và hard delete
