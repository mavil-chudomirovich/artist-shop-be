# Module 13 — Review

- **Trạng thái Spec Kit**: Chưa specify
- **Spec**: _(chưa có)_
- **Ưu tiên / Giai đoạn**: Giai đoạn 3 (ngoài MVP)
- **Phụ thuộc**: product, order

## Mục đích

Cho khách đánh giá sản phẩm đã mua, có kiểm duyệt, tăng độ tin cậy.

## Phạm vi MVP

Có:
- Chỉ khách đã mua và đã nhận sản phẩm được đánh giá.
- Điểm số + nhận xét; có thể kèm ảnh.
- Admin kiểm duyệt/ẩn đánh giá.

Không (hoãn):
- Đánh giá commission, bình luận/trả lời, hữu ích (helpful votes).

## Thực thể dữ liệu

- `reviews`: sản phẩm, người đánh giá, đơn hàng, điểm, nội dung, trạng thái kiểm duyệt.

## Yêu cầu chức năng sơ bộ

- Mỗi mục trong đơn chỉ được đánh giá một lần.
- Đánh giá mặc định ở trạng thái chờ kiểm duyệt.
- Điểm trung bình sản phẩm cập nhật nhất quán.

## Tiêu chí hoàn thành

- Điều kiện "đã mua" và giới hạn một lần có test.
- Kiểm duyệt + hiển thị điểm trung bình có test.

## Tham chiếu

- `docs/product/project_overview.md` (Shop: Reviews)
- `docs/product/backend-spec.md` (Database: reviews)
