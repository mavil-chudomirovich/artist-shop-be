# Module 03 — Category

- **Trạng thái Spec Kit**: Chưa specify
- **Spec**: _(chưa có)_
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

- CRUD danh mục (admin) + danh sách công khai.
- Ràng buộc duy nhất và ràng buộc xóa có test.

## Ghi chú / câu hỏi mở

- MVP dùng danh mục phẳng (một cấp)? → mặc định: phẳng.

## Tham chiếu

- `docs/product/backend-spec.md` (Database: categories)
