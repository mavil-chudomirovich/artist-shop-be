# Module 03 — Category

- **Trạng thái Spec Kit**: Hoàn tất phần danh mục (implement + converge; hai tiêu chí phụ
  thuộc sản phẩm được chuyển tiếp — xem "Các tiêu chí chưa đáp ứng được")
- **Spec**: [`specs/005-category-catalog/spec.md`](../../specs/005-category-catalog/spec.md)
  — đặc tả danh mục sản phẩm
- **Plan**: [`specs/005-category-catalog/plan.md`](../../specs/005-category-catalog/plan.md)
- **Tasks**: [`specs/005-category-catalog/tasks.md`](../../specs/005-category-catalog/tasks.md)
- **Contract**: [`specs/005-category-catalog/contracts/openapi.yaml`](../../specs/005-category-catalog/contracts/openapi.yaml),
  [`error-codes.md`](../../specs/005-category-catalog/contracts/error-codes.md)
- **Code**: `internal/modules/category/{domain,application,infrastructure,presentation}`,
  `migrations/00005_category.sql`
- **Ưu tiên / Giai đoạn**: Giai đoạn 0
- **Phụ thuộc**: cross-cutting

## Mục đích

Phân loại sản phẩm để khách duyệt và tìm kiếm.

## Phạm vi MVP

Có:
- Admin tạo/sửa/xóa danh mục.
- Khách xem danh sách danh mục và sản phẩm theo danh mục.
- Thứ tự hiển thị danh mục.

Không (hoãn):
- Danh mục đa cấp lồng nhau (trừ khi chốt cần).
- Bộ lọc/thuộc tính động (attributes, tags nâng cao).

## Thực thể dữ liệu

- `categories`: tên, slug, mô tả, thứ tự hiển thị, trạng thái hiển thị.

## Luồng nghiệp vụ chính

1. Admin quản lý danh mục.
2. Khách duyệt danh mục → xem sản phẩm thuộc danh mục.

## Yêu cầu chức năng sơ bộ

- Tên/slug danh mục duy nhất.
- Không xóa cứng danh mục còn sản phẩm (chặn hoặc chuyển sản phẩm).
- Danh mục ẩn không xuất hiện với khách.

## Tiêu chí hoàn thành

- [x] CRUD danh mục (admin) + danh sách công khai — bảy endpoint, xem
  [api-reference.md](../api-reference.md) mục 5.
- [x] Ràng buộc duy nhất có test — hai unique index trên `normalized_name` /
  `normalized_slug` ở tầng lưu trữ, kiểm chứng với PostgreSQL thật và qua endpoint.
- [x] Danh mục ẩn không xuất hiện với khách, và trả lời y hệt một slug chưa từng tồn tại.
- [ ] **Ràng buộc "không xoá cứng danh mục còn sản phẩm"** — feature này **chưa thể** đáp
  ứng; xem bảng bên dưới.
- [ ] **"Khách xem sản phẩm thuộc danh mục"** — feature này **chưa thể** đáp ứng; cùng lý do.

### Các tiêu chí chưa đáp ứng được (đã chuyển tiếp)

Phạm vi MVP của module có hai điểm phụ thuộc vào thực thể **sản phẩm**, mà thực thể đó chưa
tồn tại — hôm nay hệ thống chỉ lưu tài khoản, địa chỉ và vết audit. Feature
`005-category-catalog` giao trọn phần danh mục và **nói rõ** phần không thể kiểm chứng,
thay vì đánh dấu hoàn tất rồi để một luật chưa xong trở nên vô hình. Chi tiết đầy đủ ở
[`specs/005-category-catalog/deferred.md`](../../specs/005-category-catalog/deferred.md)
(D1, D2).

| Tiêu chí | Vì sao chưa đáp ứng được | Giao khi |
|---|---|---|
| "Khách xem sản phẩm thuộc danh mục" | Không có bảng `products` thì không có gì để liệt kê theo danh mục | **Module 04 Product** — endpoint "sản phẩm theo danh mục" lọc bảng sản phẩm theo định danh danh mục |
| Ràng buộc "không xoá cứng danh mục còn sản phẩm" (có test) | Tham chiếu `products.category_id` nằm ở bảng `products`, không phải bảng `categories`; thêm nó bây giờ là tạo data model của module khác | **Module 04 Product** thêm `products.category_id` tham chiếu `categories(id)` với `ON DELETE RESTRICT`; khi đó ràng buộc trở thành bảo đảm ở tầng lưu trữ |

Nửa **kiểm chứng được ngay** đã giao kèm feature này: định danh mà sản phẩm sẽ tham chiếu
là bất biến và duy nhất (FR-015), và luật xoá đã định nghĩa (hard delete, dòng audit ở
lại). Phần còn lại — chặn xoá khi còn sản phẩm — trở thành bảo đảm ở tầng lưu trữ ngay khi
module 04 thêm tham chiếu.

Hợp đồng HTTP của module nằm ở [api-reference.md](../api-reference.md) mục 5 — danh sách
endpoint và mã lỗi chỉ có ở đó, file này không nhân bản.

## Ghi chú / câu hỏi mở

- MVP dùng danh mục phẳng (một cấp)? → mặc định: phẳng.

## Tham chiếu

- `docs/product/backend-spec.md` (Database: categories)
- [api-reference.md](../api-reference.md) — endpoint và error code của module (mục 5)
- [decisions/012](../decisions/012-category-folding-and-two-response-shapes.md) — chuẩn hoá
  ở tầng ứng dụng và hai hình dạng response trên một bảng
- [`specs/005-category-catalog/deferred.md`](../../specs/005-category-catalog/deferred.md) —
  các điểm cố ý hoãn
