# Module 12 — Notification

- **Trạng thái Spec Kit**: Chưa specify
- **Spec**: _(chưa có)_
- **Ưu tiên / Giai đoạn**: Giai đoạn 2
- **Phụ thuộc**: user

## Mục đích

Thông báo cho người dùng về các sự kiện quan trọng (đơn hàng, thanh toán,
commission, chat) qua kênh trong hệ thống và email.

## Phạm vi MVP

Có:
- Thông báo trong hệ thống (in-app) + đếm chưa đọc.
- Email cho sự kiện quan trọng.
- Sự kiện: trạng thái đơn, thanh toán, trạng thái commission, tin nhắn mới.
- Đánh dấu đã đọc.

Không (hoãn):
- Push notification, SMS.
- Tuỳ chọn nhận thông báo chi tiết theo loại/kênh.

## Thực thể dữ liệu

- `notifications`: người nhận, loại sự kiện, nội dung, trạng thái đã đọc, thời gian.

## Luồng nghiệp vụ chính

1. Module nghiệp vụ phát sự kiện (đơn/thanh toán/commission/chat).
2. Hệ thống tạo thông báo in-app và/hoặc gửi email.
3. Người dùng xem và đánh dấu đã đọc.

## Yêu cầu chức năng sơ bộ

- Chỉ người nhận đọc được thông báo của mình.
- Gửi email thất bại MUST không làm hỏng giao dịch nghiệp vụ gốc (chịu lỗi/async).
- Thông báo derived sự kiện MUST không trùng lặp khi sự kiện được xử lý lại.

## Tiêu chí hoàn thành

- Tạo/đọc/đánh dấu đã đọc có test.
- Tích hợp phát sự kiện từ ít nhất đơn hàng + commission.
- Email gửi qua nhà cung cấp dịch vụ email đã chọn (cần chốt).

## Ghi chú / câu hỏi mở

- Chọn nhà cung cấp email (Resend/SES/SMTP…). Đây cũng là phụ thuộc của auth
  (đặt lại mật khẩu).

## Tham chiếu

- `docs/product/backend-spec.md` (API, Database: notifications)
