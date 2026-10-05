# ADR 002 - Nhúng dataset hành chính Việt Nam vào binary

- **Status**: Accepted
- **Decision Date**: 2026-10-05
- **Decision Maker**: Mavil
- **Feature**: `specs/003-user-profile`

## 1. Bối cảnh

Địa chỉ giao hàng phải dùng dữ liệu hành chính Việt Nam chính thức, theo mô hình
2 cấp (tỉnh → phường/xã), và khách chọn bằng ô select dạng cascading. Đây là dữ liệu
tham chiếu: nhỏ (khoảng 35 tỉnh, ~700 phường), chỉ đọc, và được cả user, order,
shipping, commission đọc tới.

Câu hỏi: đặt dữ liệu này ở đâu.

## 2. Quyết định

Nhúng dataset dưới dạng JSON vào binary bằng `//go:embed`, nạp vào bộ nhớ một lần khi
khởi động, trong `internal/share/administrative`. **Không** tạo bảng
`provinces`/`wards` trong PostgreSQL.

Địa chỉ lưu **mã** tỉnh và mã phường, cộng với tên đã chụp lại tại thời điểm lưu, để
lịch sử vẫn đọc được nếu đơn vị hành chính được đổi tên hoặc sáp nhập.

## 3. Hệ quả

| Hệ quả | Chi tiết |
|---|---|
| Tích cực | Kiểm tra "phường có thuộc tỉnh không" là hàm thuần trong `domain`, không có vòng tròn I/O |
| Tích cực | Không cần bước seed, không có độ trễ đồng bộ, không có cửa sổ dữ liệu cũ |
| Tích cực | Đổi tên phường không cần migration mới |
| Tích cực | Dev không cần bước cài đặt nào; chạy là có |
| Tiêu cực | Không có ràng buộc khoá ngoại ở DB; tính hợp lệ phải do tầng ứng dụng bảo đảm |
| Tiêu cực | Cập nhật dataset phải phát hành lại bản build mới |
| Bù đắp | Rủi ro mất tính toàn vẹn được đền bằng test bắt buộc ở tầng use case và repository |

**Xem lại quyết định khi**: dataset vượt quá vài MB; hoặc khi cần truy vấn hành chính
theo tỉnh từ SQL (báo cáo, tính phí vận chuyển) — lúc đó bảng tham chiếu sẽ hợp lý
hơn.

## 4. Cách tiếp cận bị loại

| Phương án | Vì sao không chọn |
|---|---|
| Bảng tham chiếu + migration seed | Migration ~700 dòng khó review; mỗi lần đổi dataset là một migration mới; nguy cơ lệch giữa DB và dataset |
| Gọi API hành chính công cộng lúc chạy | Thêm phụ thuộc bên thứ ba vào đường ghi dữ liệu; lỗi mạng thành lỗi nghiệp vụ; trái với giả định "dataset đi kèm bản phát hành" |
| Text tự do cho tỉnh/phường | Khách yêu cầu dữ liệu chính xác, chọn bằng select; text tự do không kiểm tra được tính hợp lệ |
