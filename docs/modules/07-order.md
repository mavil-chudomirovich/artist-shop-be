# Module 07 — Order

- **Trạng thái Spec Kit**: Chưa specify
- **Spec**: _(chưa có)_
- **Ưu tiên / Giai đoạn**: Giai đoạn 1
- **Phụ thuộc**: cart, product, inventory, user

## Mục đích

Biến giỏ hàng thành đơn hàng, quản lý vòng đời đơn và tính tiền chính xác.

## Phạm vi MVP

Có:
- Tạo đơn từ giỏ (checkout), snapshot giá và thông tin giao hàng.
- Vòng đời đơn hàng rõ ràng, từ chối chuyển trạng thái không hợp lệ.
- Danh sách/chi tiết đơn của khách; admin quản lý đơn.
- Phối hợp trừ/hoàn kho qua inventory khi thanh toán/hủy.

Không (hoãn):
- Tách đơn (split), đơn định kỳ, hoàn tiền từng phần phức tạp.
- Mã giảm giá/flash sale.

## Thực thể dữ liệu

- `orders`: mã đơn, người dùng, địa chỉ giao, tổng tiền, trạng thái, thời gian.
- `order_items`: sản phẩm, số lượng, đơn giá snapshot, thành tiền.

## Luồng nghiệp vụ chính

1. Checkout → kiểm tra tồn kho → tạo đơn trạng thái chờ thanh toán.
2. Thanh toán thành công → đơn chuyển sang đã thanh toán, trừ kho.
3. Hủy đơn → hoàn kho.
4. Xử lý/giao hàng/cập nhật trạng thái bởi admin.

## Yêu cầu chức năng sơ bộ

- Tổng tiền tính bằng đơn vị tiền tệ nhỏ, không dùng số thực.
- Checkout MUST chạy trong một giao dịch; không tạo đơn thiếu item.
- Trạng thái đơn là state machine có danh sách chuyển hợp lệ.
- Trạng thái thanh toán và trạng thái đơn tách biệt nhưng liên kết chặt.

## Tiêu chí hoàn thành

- Tạo đơn + tính tiền + chuyển trạng thái có test (kể cả ca không hợp lệ).
- Tích hợp kho (trừ/hoàn) có test giao dịch.
- Test quyền: khách chỉ thấy đơn của mình.

## Ghi chú / câu hỏi mở

- Danh sách trạng thái đơn chuẩn cần chốt (kèm sơ đồ chuyển).

## Tham chiếu

- `docs/product/project_overview.md` (Shop, MVP)
- `docs/product/backend-spec.md` (Database: orders, order_items; Inventory)
