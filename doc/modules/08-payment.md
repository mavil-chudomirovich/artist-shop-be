# Module 08 — Payment

- **Trạng thái Spec Kit**: Chưa specify
- **Spec**: _(chưa có)_
- **Ưu tiên / Giai đoạn**: Giai đoạn 1
- **Phụ thuộc**: order

## Mục đích

Xử lý thanh toán cho đơn hàng và commission qua MoMo và VietQR, an toàn và
idempotent, thông qua một lớp trừu tượng nhà cung cấp.

## Phạm vi MVP

Có:
- Trừu tượng `PaymentService` → Provider → Result (không rò rỉ kiểu nhà cung cấp).
- MoMo và VietQR.
- Callback/webhook idempotent (chống áp dụng trùng).
- Xác minh VietQR thủ công cho MVP.
- Xử lý an toàn giao dịch cùng order/inventory.
- Trạng thái thanh toán rõ ràng.

Không (hoãn):
- PayPal, hoàn tiền tự động đầy đủ, nhiều tiền tệ.
- Thanh toán định kỳ.

## Thực thể dữ liệu

- `payments`: đơn/commission, nhà cung cấp, số tiền, trạng thái, mã giao dịch,
  khoá idempotency, thời gian.

## Luồng nghiệp vụ chính

1. Khách chọn phương thức → tạo yêu cầu thanh toán.
2. Nhà cung cấp xác nhận qua callback/webhook.
3. Hệ thống áp dụng kết quả một lần duy nhất → chuyển trạng thái đơn.
4. VietQR: admin xác minh thủ công theo quy trình ghi nhận.

## Yêu cầu chức năng sơ bộ

- Webhook trùng MUST không tạo hiệu ứng thứ hai.
- Số tiền MUST khớp giữa yêu cầu và xác nhận.
- Cập nhật thanh toán MUST trong một giao dịch và ghi audit.
- Lỗi nhà cung cấp MUST không để lại trạng thái trung gian không nhất quán.

## Tiêu chí hoàn thành

- Luồng MoMo và VietQR (sandbox/thủ công) có test.
- Test idempotency (gửi lại webhook) đạt.
- Trừu tượng provider có test contract.

## Ghi chú / câu hỏi mở

- Chữ ký/xác thực webhook MoMo theo cơ chế nào?
- Chính sách hoàn tiền/hủy sau thanh toán?

## Tham chiếu

- `doc/backend-spec.md` (Payment System)
- `.specify/memory/constitution.md` (Transactional Integrity, Security)
