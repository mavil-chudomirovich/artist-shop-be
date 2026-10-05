# Module 11 — Chat

- **Trạng thái Spec Kit**: Chưa specify
- **Spec**: _(chưa có)_
- **Ưu tiên / Giai đoạn**: Giai đoạn 2
- **Phụ thuộc**: user

## Mục đích

Kênh liên lạc trực tiếp giữa khách hàng và artist (cho commission và hỗ trợ đơn
hàng).

## Phạm vi MVP

Có:
- Hội thoại 1-1 giữa khách và artist.
- Tin nhắn văn bản, ảnh, tệp (qua Cloudinary), và sự kiện hệ thống.
- REST + polling; đếm tin chưa đọc.
- Phân quyền: chỉ thành viên hội thoại truy cập được tin/tệp.

Không (hoãn):
- WebSocket/thời gian thực (tùy chọn sau).
- Nhóm chat, gọi thoại/video, thu hồi tin nhắn phức tạp.

## Thực thể dữ liệu

- `conversations`: các thành viên, ngữ cảnh gắn kết (đơn/commission), thời gian.
- `messages`: người gửi, loại (text/image/file/system), nội dung, trạng thái đã đọc.

## Luồng nghiệp vụ chính

1. Hội thoại được tạo khi khách cần trao đổi (thường gắn commission/đơn).
2. Hai bên gửi tin nhắn; hệ thống ghi tin chưa đọc.
3. Sự kiện hệ thống tự sinh khi trạng thái đơn/commission đổi.

## Yêu cầu chức năng sơ bộ

- Chỉ thành viên hội thoại đọc/ghi được.
- Tệp đính kèm phải validate loại/kích thước.
- Polling MUST không trả về dữ liệu của hội thoại khác.
- Tin hệ thống không thể sửa/xóa bởi người dùng.

## Tiêu chí hoàn thành

- Gửi/đọc tin + đếm chưa đọc có test.
- Test phân quyền truy cập hội thoại/tệp.
- Ghi sự kiện hệ thống khi trạng thái thay đổi (tích hợp).

## Ghi chú / câu hỏi mở

- Tần suất polling đề xuất? Có giới hạn tốc độ gửi tin?

## Tham chiếu

- `docs/product/backend-spec.md` (Chat)
