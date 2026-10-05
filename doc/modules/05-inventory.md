# Module 05 — Inventory

- **Trạng thái Spec Kit**: Chưa specify
- **Spec**: _(chưa có)_
- **Ưu tiên / Giai đoạn**: Giai đoạn 1
- **Phụ thuộc**: product

## Mục đích

Theo dõi tồn kho và lịch sử biến động kho một cách chính xác, an toàn giao dịch.

## Phạm vi MVP

Có:
- Tồn kho theo sản phẩm.
- Tự động: đơn đã thanh toán → giảm kho; đơn hủy → hoàn kho.
- Thủ công: nhập thêm (restock), hư hỏng (damage), điều chỉnh (adjustment).
- Mọi thay đổi tạo một bản ghi `inventory_transactions`.

Không (hoãn):
- Quản lý kho nhiều địa điểm.
- Dự báo tồn kho, cảnh báo tự động nâng cao.

## Thực thể dữ liệu

- `inventory_transactions`: sản phẩm, loại biến động, số lượng thay đổi, tham chiếu
  nguồn (đơn/điều chỉnh), ghi chú, người thao tác, thời gian.

## Luồng nghiệp vụ chính

1. Thanh toán thành công → giảm kho + ghi transaction.
2. Hủy đơn → hoàn kho + ghi transaction.
3. Admin restock/damage/adjustment → ghi transaction.

## Yêu cầu chức năng sơ bộ

- Không cho tồn kho âm (chặn oversell).
- Cập nhật kho MUST nằm trong transaction cùng sự kiện nguồn.
- Mọi thay đổi MUST bất biến (append-only) và truy vết được.
- Chống trùng khi xử lý lại cùng sự kiện thanh toán (idempotent).

## Tiêu chí hoàn thành

- Giảm/hoàn kho có test, kể cả chống oversell và xử lý trùng.
- Lịch sử biến động đầy đủ, không mất bản ghi.
- Tuân thủ constitution về tính toàn vẹn giao dịch kho/tiền.

## Ghi chú / câu hỏi mở

- Ngưỡng cảnh báo tồn kho thấp có cần ở MVP không?

## Tham chiếu

- `doc/backend-spec.md` (Inventory)
