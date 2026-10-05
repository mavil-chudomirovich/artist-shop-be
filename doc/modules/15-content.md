# Module 15 — Content

- **Trạng thái Spec Kit**: Chưa specify
- **Spec**: _(chưa có)_
- **Ưu tiên / Giai đoạn**: Giai đoạn 3 (ngoài MVP)
- **Phụ thuộc**: cross-cutting

## Mục đích

Quản lý nội dung tĩnh của trang: gallery, giới thiệu, thông tin commission, các
khối nội dung có thể chỉnh sửa không cần deploy.

## Phạm vi MVP

Có:
- Khối nội dung có khoá (key) và nội dung (JSON/markdown).
- Admin cập nhật nội dung; public đọc nội dung đã publish.

Không (hoãn):
- CMS đầy đủ, lịch sử phiên bản, đa ngôn ngữ.

## Thực thể dữ liệu

- `content_blocks`: khoá, loại, nội dung, trạng thái publish, thời gian cập nhật.

## Yêu cầu chức năng sơ bộ

- Mỗi khoá nội dung duy nhất.
- Chỉ nội dung đã publish hiển thị cho khách.
- Ẩn danh (sanitize) nội dung trước khi hiển thị nếu cho phép HTML.

## Tiêu chí hoàn thành

- CRUD nội dung (admin) + đọc công khai có test.
- Kiểm tra publish/ẩn có test.

## Tham chiếu

- `doc/project_overview.md` (Admin: Content)
- `doc/backend-spec.md` (Database: content_blocks)
