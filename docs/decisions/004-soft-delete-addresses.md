# ADR 004 - Xoá địa chỉ bằng soft delete

- **Status**: Accepted
- **Decision Date**: 2026-10-05
- **Decision Maker**: Mavil
- **Feature**: `specs/003-user-profile`

## 1. Bối cảnh

Khách xoá một địa chỉ đang được đơn hàng trong quá khứ tham chiếu. Nếu xoá vật lý,
lịch sử vận chuyển mất thông tin địa chỉ thật — đúng thứ cần khi có khiếu nại giao hàng.

## 2. Quyết định

Dùng soft delete: cột `deleted_at timestamptz`. Địa chỉ đã ẩn thì:

- không xuất hiện trong danh sách của khách;
- không thể là mặc định (partial unique index đã loại `deleted_at IS NOT NULL`);
- vẫn còn trong bảng để đơn hàng tham chiếu được.

Ngoài ra, mỗi đơn hàng lưu **bản chụp** riêng của địa chỉ tại thời điểm đặt, nên việc
sửa hay xoá địa chỉ sau này không bao giờ sửa lịch sử đơn.

## 3. Hệ quả

| Hệ quả | Chi tiết |
|---|---|
| Tích cực | Lịch sử đơn hàng luôn tái dựng được |
| Tích cực | Xoá là thao tác có thể hoàn tác |
| Tiêu cực | Mọi truy vấn phải nhớ `deleted_at IS NULL` ⇒ sót một chỗ là lộ dữ liệu ẩn |
| Tiêu cực | Bảng không tự co lại nếu khách tạo/xoá nhiều |
| Bù đắp | Quy tắc "luôn lọc địa chỉ đang ẩn" ghi trong coding-conventions; index hỗ trợ việc lọc |

## 4. Cách tiếp cận bị loại

| Phương án | Vì sao không chọn |
|---|---|
| Xoá cứng + `ON DELETE SET NULL` ở `orders` | Mất địa chỉ mà khách thực sự dùng |
| Cấm xoá khi còn đơn tham chiếu | Lộ sự tồn tại của đơn hàng ra màn hình địa chỉ, và chặn một thao tác sửa hợp lệ |
| Xoá cứng, đơn hàng tự chụp đủ dữ liệu | Vẫn mất khả năng truy vết khi có khiếu nại về chính thao tác xoá; và ràng buộc khóa ngoại trở nên lỏng |
