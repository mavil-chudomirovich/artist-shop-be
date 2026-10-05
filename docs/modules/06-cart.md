# Module 06 — Cart

- **Trạng thái Spec Kit**: Chưa specify
- **Spec**: _(chưa có)_
- **Ưu tiên / Giai đoạn**: Giai đoạn 1
- **Phụ thuộc**: product

## Mục đích

Cho khách tập hợp sản phẩm muốn mua trước khi thanh toán.

## Phạm vi MVP

Có:
- Thêm/xóa sản phẩm, cập nhật số lượng.
- Tính tạm tính (giá × số lượng).
- Kiểm tra tồn kho và trạng thái bán tại thời điểm thao tác/thanh toán.
- Gắn giỏ với tài khoản (khách đã đăng nhập).

Không (hoãn):
- Giỏ cho khách vãng lai (chưa đăng nhập) — cân nhắc sau.
- Lưu giỏ, mã giảm giá, upsell.

## Thực thể dữ liệu

- `carts`: thuộc về người dùng.
- `cart_items`: sản phẩm, số lượng, giá tại thời điểm thêm (snapshot).

## Luồng nghiệp vụ chính

1. Khách thêm sản phẩm → giỏ cập nhật.
2. Khách đổi số lượng/xóa.
3. Khách chuyển sang thanh toán.

## Yêu cầu chức năng sơ bộ

- Không thêm sản phẩm không bán được (không ACTIVE).
- Số lượng không vượt tồn kho khả dụng.
- Tạm tính dùng giá snapshot nhất quán với bước tạo đơn.
- Một người dùng có một giỏ hoạt động.

## Tiêu chí hoàn thành

- CRUD giỏ có test, kể cả ràng buộc tồn kho/trạng thái.
- Tính tạm tính chính xác (dùng đơn vị tiền tệ nhỏ) có test.

## Ghi chú / câu hỏi mở

- Giá trong giỏ: cố định theo thời điểm thêm hay cập nhật theo giá hiện tại?

## Tham chiếu

- `docs/product/backend-spec.md` (API, Database)
